package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Compile-time interface compliance check.
var _ Service = (*service)(nil)

// Service interface (as per ethpandaops standards).
type Service interface {
	Start(ctx context.Context) error
	Stop() error
	Allow(
		ctx context.Context,
		ip, key string,
		limit int,
		window time.Duration,
	) (allowed bool, remaining int, resetAt time.Time, err error)
}

type service struct {
	redis *redis.Client
	log   logrus.FieldLogger

	// Failure mode: "fail_open" or "fail_closed"
	failureMode string
}

func NewService(
	log logrus.FieldLogger,
	redisClient *redis.Client,
	failureMode string,
) Service {
	return &service{
		redis:       redisClient,
		failureMode: failureMode,
		log:         log.WithField("package", "ratelimit"),
	}
}

func (s *service) Start(ctx context.Context) error {
	// Test Redis connectivity
	if err := s.redis.Ping(ctx).Err(); err != nil {
		s.log.WithError(err).Warn("Redis connectivity check failed, rate limiter may not work correctly")

		if s.failureMode == "fail_closed" {
			return fmt.Errorf("redis unavailable in fail_closed mode: %w", err)
		}
	}

	s.log.WithFields(logrus.Fields{
		"failure_mode":    s.failureMode,
		"redis_connected": s.redis.Ping(ctx).Err() == nil,
	}).Info("Rate limiter started")

	return nil
}

func (s *service) Stop() error {
	s.log.Info("Rate limiter stopped")

	return nil
}

// Allow implements sliding window rate limiting using Redis INCR + EXPIRE.
func (s *service) Allow(
	ctx context.Context,
	ip, key string,
	limit int,
	window time.Duration,
) (bool, int, time.Time, error) {
	redisKey := fmt.Sprintf("rate_limit:%s:%s", ip, key)

	// EXPIRE NX runs on every request, not only the first, so a counter that lost its TTL
	// (e.g. an EXPIRE not yet replicated when Redis failed over) heals instead of growing forever.
	var (
		incrCmd *redis.IntCmd
		ttlCmd  *redis.DurationCmd
	)

	_, err := s.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		incrCmd = pipe.Incr(ctx, redisKey)
		pipe.ExpireNX(ctx, redisKey, window)
		ttlCmd = pipe.TTL(ctx, redisKey)

		return nil
	})
	if err != nil {
		s.log.WithError(err).Error("failed to update rate limit counter in Redis")

		// Handle failure based on configured mode
		if s.failureMode == "fail_closed" {
			return false, 0, time.Time{}, fmt.Errorf("rate limiter unavailable: %w", err)
		}

		// fail_open: allow request
		return true, 0, time.Time{}, nil
	}

	count := incrCmd.Val()

	ttl := ttlCmd.Val()
	if ttl <= 0 {
		ttl = window
	}

	resetAt := time.Now().Add(ttl)

	// Check if over limit
	if count > int64(limit) {
		remaining := 0

		return false, remaining, resetAt, nil
	}

	remaining := limit - int(count)

	return true, remaining, resetAt, nil
}
