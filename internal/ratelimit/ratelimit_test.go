package ratelimit

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestService_Allow_FirstRequest verifies that the first request is allowed
// and the counter is set correctly.
func TestService_Allow_FirstRequest(t *testing.T) {
	// Setup miniredis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := NewService(logger, client, "fail_open")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First request should be allowed
	allowed, remaining, resetAt, err := svc.Allow(ctx, "192.168.1.1", "api", 10, 1*time.Minute)

	require.NoError(t, err)
	assert.True(t, allowed, "first request should be allowed")
	assert.Equal(t, 9, remaining, "should have 9 requests remaining")
	assert.False(t, resetAt.IsZero(), "reset time should be set")
	assert.True(t, resetAt.After(time.Now()), "reset time should be in the future")

	// Verify Redis key was created with correct value
	count, err := client.Get(ctx, "rate_limit:192.168.1.1:api").Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "counter should be 1 after first request")

	// Verify TTL was set
	ttl, err := client.TTL(ctx, "rate_limit:192.168.1.1:api").Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, 50*time.Second, "TTL should be close to the window duration")
	assert.LessOrEqual(t, ttl, 1*time.Minute, "TTL should not exceed window duration")
}

// TestService_Allow_UnderLimit verifies that multiple requests under the limit
// are all allowed and counters are tracked correctly.
func TestService_Allow_UnderLimit(t *testing.T) {
	// Setup miniredis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := NewService(logger, client, "fail_open")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	limit := 5
	ip := "10.0.0.1"
	key := "test"

	// Make requests up to the limit
	for i := 0; i < limit; i++ {
		allowed, remaining, resetAt, err := svc.Allow(ctx, ip, key, limit, 1*time.Minute)

		require.NoError(t, err)
		assert.True(t, allowed, "request %d should be allowed", i+1)
		assert.Equal(t, limit-i-1, remaining, "incorrect remaining count for request %d", i+1)
		assert.False(t, resetAt.IsZero(), "reset time should be set for request %d", i+1)
	}

	// Verify final counter value
	count, err := client.Get(ctx, fmt.Sprintf("rate_limit:%s:%s", ip, key)).Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(limit), count)
}

// TestService_Allow_OverLimit verifies that requests exceeding the limit
// are denied with correct remaining count and reset time.
func TestService_Allow_OverLimit(t *testing.T) {
	// Setup miniredis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := NewService(logger, client, "fail_open")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	limit := 3
	ip := "172.16.0.1"
	key := "limited"

	// Make requests up to and exceeding the limit
	for i := 0; i < limit+2; i++ {
		allowed, remaining, resetAt, err := svc.Allow(ctx, ip, key, limit, 1*time.Minute)

		require.NoError(t, err)

		if i < limit {
			// Requests within limit should be allowed
			assert.True(t, allowed, "request %d should be allowed", i+1)
			assert.Equal(t, limit-i-1, remaining, "incorrect remaining count for request %d", i+1)
		} else {
			// Requests over limit should be denied
			assert.False(t, allowed, "request %d should be denied", i+1)
			assert.Equal(t, 0, remaining, "remaining should be 0 when over limit")
		}

		assert.False(t, resetAt.IsZero(), "reset time should always be set")
		assert.True(t, resetAt.After(time.Now()), "reset time should be in the future")
	}

	// Verify counter incremented beyond limit
	count, err := client.Get(ctx, fmt.Sprintf("rate_limit:%s:%s", ip, key)).Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(limit+2), count, "counter should track all requests, even denied ones")
}

// TestService_Allow_WindowExpiry verifies that the counter resets after
// the time window expires.
func TestService_Allow_WindowExpiry(t *testing.T) {
	// Setup miniredis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := NewService(logger, client, "fail_open")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	limit := 2
	window := 2 * time.Second
	ip := "192.168.100.1"
	key := "expiry_test"

	// Use up the limit
	for i := 0; i < limit; i++ {
		allowed, _, _, err := svc.Allow(ctx, ip, key, limit, window)
		require.NoError(t, err)
		assert.True(t, allowed, "request %d should be allowed", i+1)
	}

	// Next request should be denied
	allowed, remaining, _, err := svc.Allow(ctx, ip, key, limit, window)
	require.NoError(t, err)
	assert.False(t, allowed, "should be rate limited")
	assert.Equal(t, 0, remaining)

	// Fast-forward time in miniredis to expire the key
	mr.FastForward(window + 1*time.Second)

	// Now the request should be allowed again (new window)
	allowed, remaining, resetAt, err := svc.Allow(ctx, ip, key, limit, window)
	require.NoError(t, err)
	assert.True(t, allowed, "should be allowed after window expiry")
	assert.Equal(t, limit-1, remaining, "should have limit-1 remaining in new window")
	assert.False(t, resetAt.IsZero())

	// Verify counter was reset
	count, err := client.Get(ctx, fmt.Sprintf("rate_limit:%s:%s", ip, key)).Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "counter should be reset to 1 in new window")
}

// TestService_Allow_RedisFailure_FailOpen verifies that when Redis fails
// and fail_open mode is configured, requests are allowed.
func TestService_Allow_RedisFailure_FailOpen(t *testing.T) {
	// Create a Redis client with invalid address to simulate failure
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:0", // Invalid address
	})
	defer client.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := NewService(logger, client, "fail_open")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Request should be allowed despite Redis error (fail_open mode)
	allowed, remaining, resetAt, err := svc.Allow(ctx, "192.168.1.1", "api", 10, 1*time.Minute)

	require.NoError(t, err, "fail_open should not return error")
	assert.True(t, allowed, "fail_open should allow request on Redis failure")
	assert.Equal(t, 0, remaining, "remaining should be 0 on failure")
	assert.True(t, resetAt.IsZero(), "reset time should be zero on failure")
}

// TestService_Allow_RedisFailure_FailClosed verifies that when Redis fails
// and fail_closed mode is configured, requests are denied.
func TestService_Allow_RedisFailure_FailClosed(t *testing.T) {
	// Create a Redis client with invalid address to simulate failure
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:0", // Invalid address
	})
	defer client.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := NewService(logger, client, "fail_closed")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Request should be denied on Redis error (fail_closed mode)
	allowed, remaining, resetAt, err := svc.Allow(ctx, "192.168.1.1", "api", 10, 1*time.Minute)

	require.Error(t, err, "fail_closed should return error on Redis failure")
	assert.False(t, allowed, "fail_closed should deny request on Redis failure")
	assert.Equal(t, 0, remaining)
	assert.True(t, resetAt.IsZero())
	assert.Contains(t, err.Error(), "rate limiter unavailable")
}

// TestService_DifferentIPsSeparateLimits verifies that different IPs
// maintain separate rate limit counters.
func TestService_DifferentIPsSeparateLimits(t *testing.T) {
	// Setup miniredis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := NewService(logger, client, "fail_open")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	limit := 2
	key := "api"

	ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}

	// Each IP should have its own limit
	for _, ip := range ips {
		for i := 0; i < limit; i++ {
			allowed, remaining, _, err := svc.Allow(ctx, ip, key, limit, 1*time.Minute)
			require.NoError(t, err)
			assert.True(t, allowed, "IP %s request %d should be allowed", ip, i+1)
			assert.Equal(t, limit-i-1, remaining)
		}

		// Each IP should be rate limited independently
		allowed, _, _, err := svc.Allow(ctx, ip, key, limit, 1*time.Minute)
		require.NoError(t, err)
		assert.False(t, allowed, "IP %s should be rate limited", ip)
	}
}

// TestService_DifferentKeysSeparateLimits verifies that different keys
// maintain separate rate limit counters for the same IP.
func TestService_DifferentKeysSeparateLimits(t *testing.T) {
	// Setup miniredis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := NewService(logger, client, "fail_open")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	limit := 2
	ip := "192.168.1.1"
	keys := []string{"api", "upload", "download"}

	// Each key should have its own limit for the same IP
	for _, key := range keys {
		for i := 0; i < limit; i++ {
			allowed, remaining, _, err := svc.Allow(ctx, ip, key, limit, 1*time.Minute)
			require.NoError(t, err)
			assert.True(t, allowed, "key %s request %d should be allowed", key, i+1)
			assert.Equal(t, limit-i-1, remaining)
		}

		// Each key should be rate limited independently
		allowed, _, _, err := svc.Allow(ctx, ip, key, limit, 1*time.Minute)
		require.NoError(t, err)
		assert.False(t, allowed, "key %s should be rate limited", key)
	}
}

// TestService_StartStop verifies that Start and Stop methods work correctly.
func TestService_StartStop(t *testing.T) {
	// Setup miniredis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := NewService(logger, client, "fail_open")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start should succeed
	err := svc.Start(ctx)
	require.NoError(t, err)

	// Stop should succeed
	err = svc.Stop()
	require.NoError(t, err)
}

// TestService_Allow_ConcurrentRequests verifies that the rate limiter
// handles concurrent requests correctly.
func TestService_Allow_ConcurrentRequests(t *testing.T) {
	// Setup miniredis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := NewService(logger, client, "fail_open")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	limit := 10
	numGoroutines := 20
	ip := "192.168.1.100"
	key := "concurrent"

	// Track results
	type result struct {
		allowed bool
		err     error
	}

	results := make(chan result, numGoroutines)

	// Launch concurrent requests
	for i := 0; i < numGoroutines; i++ {
		go func() {
			allowed, _, _, err := svc.Allow(ctx, ip, key, limit, 1*time.Minute)
			results <- result{allowed: allowed, err: err}
		}()
	}

	// Collect results
	allowedCount := 0
	deniedCount := 0

	for i := 0; i < numGoroutines; i++ {
		res := <-results
		require.NoError(t, res.err)

		if res.allowed {
			allowedCount++
		} else {
			deniedCount++
		}
	}

	// Should have exactly 'limit' allowed and rest denied
	assert.Equal(t, limit, allowedCount, "should allow exactly limit requests")
	assert.Equal(t, numGoroutines-limit, deniedCount, "should deny excess requests")

	// Verify final counter
	count, err := client.Get(ctx, fmt.Sprintf("rate_limit:%s:%s", ip, key)).Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(numGoroutines), count, "all requests should be counted")
}
