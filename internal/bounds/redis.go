package bounds

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ethpandaops/lab-backend/internal/leader"
	"github.com/ethpandaops/lab-backend/internal/redis"
	"github.com/sirupsen/logrus"
)

// Compile-time interface compliance check.
var _ Provider = (*RedisProvider)(nil)

const redisKeyPrefix = "lab:bounds:"

// RedisProvider implements Provider interface using Redis as storage.
type RedisProvider struct {
	log      logrus.FieldLogger
	cfg      Config
	redis    redis.Client
	elector  leader.Elector
	upstream *Service // Stateless fetcher for retrieving upstream data
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewRedisProvider creates a Redis-backed bounds provider.
// upstream is the stateless Service that fetches from Xatu CBT APIs.
func NewRedisProvider(
	log logrus.FieldLogger,
	cfg Config,
	redisClient redis.Client,
	elector leader.Elector,
	upstream *Service,
) Provider {
	return &RedisProvider{
		log:      log.WithField("component", "bounds_redis"),
		cfg:      cfg,
		redis:    redisClient,
		elector:  elector,
		upstream: upstream,
		done:     make(chan struct{}),
	}
}

// Start initializes the provider and starts background refresh loop.
// Blocks until Redis has data or timeout is reached (fail-fast on timeout).
func (r *RedisProvider) Start(ctx context.Context) error {
	r.log.Info("Starting bounds provider")

	// Start background refresh loop
	r.wg.Add(1)

	go r.refreshLoop(ctx)

	// Wait for Redis to be populated (readiness check)
	// This ensures we never start serving requests with empty data
	r.log.Info("Waiting for bounds data")

	readinessTimeout := 30 * time.Second

	readinessCtx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-readinessCtx.Done():
			return fmt.Errorf(
				"readiness timeout: no bounds data in Redis after %v - "+
					"leader may have failed to fetch or no leader elected",
				readinessTimeout,
			)
		case <-ticker.C:
			bounds := r.GetAllBounds(ctx)
			if len(bounds) > 0 {
				r.log.WithField("network_count", len(bounds)).Info(
					"Bounds data ready in Redis",
				)

				return nil
			}

			r.log.Debug("Waiting for bounds data...")
		}
	}
}

// Stop stops the provider.
func (r *RedisProvider) Stop() error {
	r.log.Info("Stopping bounds provider")
	close(r.done)
	r.wg.Wait()

	return nil
}

// GetBounds returns bounds for a specific network by reading directly from Redis.
func (r *RedisProvider) GetBounds(ctx context.Context, network string) (*BoundsData, bool) {
	key := redisKeyPrefix + network

	data, err := r.redis.Get(ctx, key)
	if err != nil {
		r.log.WithError(err).WithField("network", network).Debug("Failed to get bounds from Redis")

		return nil, false
	}

	var boundsData BoundsData
	if err := json.Unmarshal([]byte(data), &boundsData); err != nil {
		r.log.WithError(err).WithField("network", network).Error("Failed to unmarshal bounds")

		return nil, false
	}

	return &boundsData, true
}

// GetAllBounds returns bounds for all networks by reading directly from Redis.
func (r *RedisProvider) GetAllBounds(ctx context.Context) map[string]*BoundsData {
	// Get all bounds keys matching the pattern
	client := r.redis.GetClient()

	keys, err := client.Keys(ctx, redisKeyPrefix+"*").Result()
	if err != nil {
		r.log.WithError(err).Error("Failed to list bounds keys")

		return make(map[string]*BoundsData)
	}

	result := make(map[string]*BoundsData, len(keys))

	for _, key := range keys {
		network := key[len(redisKeyPrefix):] // Strip prefix to get network name

		data, err := r.redis.Get(ctx, key)
		if err != nil {
			r.log.WithError(err).WithField("network", network).Debug("Failed to get bounds from Redis")

			continue
		}

		var boundsData BoundsData
		if err := json.Unmarshal([]byte(data), &boundsData); err != nil {
			r.log.WithError(err).WithField("network", network).Error("Failed to unmarshal bounds")

			continue
		}

		result[network] = &boundsData
	}

	return result
}

func (r *RedisProvider) refreshLoop(ctx context.Context) {
	defer r.wg.Done()

	ticker := time.NewTicker(r.cfg.RefreshInterval)
	defer ticker.Stop()

	// Give leader election a moment to settle (it tries immediately on boot)
	time.Sleep(100 * time.Millisecond)

	// Immediate refresh on startup if leader
	if r.elector.IsLeader() {
		r.refreshData(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.done:
			return
		case <-ticker.C:
			// Only leader refreshes from upstream
			if r.elector.IsLeader() {
				r.refreshData(ctx)
			}
			// Followers do nothing - they read directly from Redis on API calls
		}
	}
}

func (r *RedisProvider) refreshData(ctx context.Context) {
	r.log.Debug("Refreshing bounds data from upstream")

	// Fetch fresh data from upstream (no caching, just HTTP calls)
	allBounds, err := r.upstream.FetchBounds(ctx)
	if err != nil {
		r.log.WithError(err).Error("Failed to fetch bounds from upstream")
		// Continue with partial data if available
	}

	if len(allBounds) == 0 {
		r.log.Warn("No bounds data fetched from upstream")

		return
	}

	// Store each network's bounds in Redis
	for network, boundsData := range allBounds {
		data, err := json.Marshal(boundsData)
		if err != nil {
			r.log.WithError(err).WithField("network", network).Error("Failed to marshal bounds")

			continue
		}

		key := redisKeyPrefix + network
		ttl := r.cfg.BoundsTTL

		if err := r.redis.Set(ctx, key, string(data), ttl); err != nil {
			r.log.WithError(err).WithField("network", network).Error("Failed to store bounds in Redis")

			continue
		}
	}
}
