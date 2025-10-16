package leader

import (
	"context"
	"sync"
	"time"

	"github.com/ethpandaops/lab-backend/internal/redis"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Elector manages leader election using Redis SETNX.
type Elector interface {
	Start(ctx context.Context) error
	Stop() error
	IsLeader() bool
}

type elector struct {
	log            logrus.FieldLogger
	cfg            Config
	redis          redis.Client
	id             string // Unique instance ID
	isLeader       bool
	loggedFollower bool // Track if we've logged follower status
	mu             sync.RWMutex
	done           chan struct{}
	wg             sync.WaitGroup
}

// NewElector creates a new leader elector.
func NewElector(log logrus.FieldLogger, cfg Config, redisClient redis.Client) Elector {
	return &elector{
		log:   log.WithField("component", "leader"),
		cfg:   cfg,
		redis: redisClient,
		id:    uuid.New().String(),
		done:  make(chan struct{}),
	}
}

// Start begins the leader election process.
func (e *elector) Start(ctx context.Context) error {
	e.log.WithField("instance_id", e.id).Info("Starting leader election")

	e.wg.Add(1)

	go e.electionLoop(ctx)

	return nil
}

// Stop stops the leader election process.
func (e *elector) Stop() error {
	e.log.Info("Stopping leader election")
	close(e.done)
	e.wg.Wait()

	// Release leadership if we hold it
	e.mu.Lock()

	if e.isLeader {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = e.redis.Del(ctx, e.cfg.LockKey)
		e.isLeader = false
	}

	e.mu.Unlock()

	return nil
}

// IsLeader returns true if this instance is the current leader.
func (e *elector) IsLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.isLeader
}

func (e *elector) electionLoop(ctx context.Context) {
	defer e.wg.Done()

	// Try to acquire leadership immediately on startup (don't wait for first ticker)
	e.tryAcquireLeadership(ctx)

	renewTicker := time.NewTicker(e.cfg.RenewInterval)
	defer renewTicker.Stop()

	retryTicker := time.NewTicker(e.cfg.RetryInterval)
	defer retryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.done:
			return
		case <-renewTicker.C:
			if e.IsLeader() {
				e.renewLeadership(ctx)
			}
		case <-retryTicker.C:
			if !e.IsLeader() {
				e.tryAcquireLeadership(ctx)
			}
		}
	}
}

func (e *elector) tryAcquireLeadership(ctx context.Context) {
	acquired, err := e.redis.SetNX(ctx, e.cfg.LockKey, e.id, e.cfg.LockTTL)
	if err != nil {
		e.log.WithError(err).Warn("Failed to acquire leadership lock")

		return
	}

	if acquired {
		e.mu.Lock()
		e.isLeader = true
		e.loggedFollower = false // Reset flag if we gain leadership
		e.mu.Unlock()
		e.log.WithField("instance_id", e.id).Info("Acquired leadership")
	} else {
		// Only log follower status once (on first attempt)
		e.mu.Lock()
		shouldLog := !e.loggedFollower
		e.loggedFollower = true
		e.mu.Unlock()

		if shouldLog {
			// Get current leader ID for logging
			currentLeader, _ := e.redis.Get(ctx, e.cfg.LockKey)
			e.log.WithFields(logrus.Fields{
				"instance_id": e.id,
				"leader_id":   currentLeader,
			}).Info("Running as follower")
		}
	}
}

func (e *elector) renewLeadership(ctx context.Context) {
	// Get current lock holder
	currentHolder, err := e.redis.Get(ctx, e.cfg.LockKey)
	if err != nil {
		e.log.WithError(err).Warn("Failed to check lock holder, losing leadership")
		e.mu.Lock()
		e.isLeader = false
		e.mu.Unlock()

		return
	}

	// Only renew if we still hold the lock
	if currentHolder == e.id {
		if err := e.redis.Set(ctx, e.cfg.LockKey, e.id, e.cfg.LockTTL); err != nil {
			e.log.WithError(err).Warn("Failed to renew leadership lock")
			e.mu.Lock()
			e.isLeader = false
			e.mu.Unlock()

			return
		}

		e.log.Debug("Renewed leadership lock")
	} else {
		e.log.Warn("Lost leadership to another instance")
		e.mu.Lock()
		e.isLeader = false
		e.mu.Unlock()
	}
}

// Compile-time interface compliance check.
var _ Elector = (*elector)(nil)
