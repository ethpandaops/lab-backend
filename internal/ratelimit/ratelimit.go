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
	s.log.Info("rate limiter service started")

	return nil
}

func (s *service) Stop() error {
	s.log.Info("rate limiter service stopped")

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

	// Increment counter
	count, err := s.redis.Incr(ctx, redisKey).Result()
	if err != nil {
		s.log.WithError(err).Error("failed to increment rate limit counter in Redis")

		// Handle failure based on configured mode
		if s.failureMode == "fail_closed" {
			return false, 0, time.Time{}, fmt.Errorf("rate limiter unavailable: %w", err)
		}

		// fail_open: allow request
		return true, 0, time.Time{}, nil
	}

	// Set expiry on first request
	if count == 1 {
		if expireErr := s.redis.Expire(ctx, redisKey, window).Err(); expireErr != nil {
			s.log.WithError(expireErr).Warn("failed to set rate limit TTL")
		}
	}

	// Calculate reset time
	ttl, err := s.redis.TTL(ctx, redisKey).Result()
	if err != nil {
		s.log.WithError(err).Warn("failed to get rate limit TTL")

		ttl = window // Fallback
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
