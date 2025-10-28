package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/lab-backend/internal/config"
)

// mockRateLimitService is a mock implementation of ratelimit.Service for testing.
type mockRateLimitService struct {
	allowFunc func(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error)
}

func (m *mockRateLimitService) Start(ctx context.Context) error { return nil }
func (m *mockRateLimitService) Stop() error                     { return nil }

func (m *mockRateLimitService) Allow(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
	if m.allowFunc != nil {
		return m.allowFunc(ctx, ip, key, limit, window)
	}

	return true, limit - 1, time.Now().Add(window), nil
}

// TestRateLimit_AllowsUnderLimit verifies that requests under the limit
// all receive 200 status codes.
func TestRateLimit_AllowsUnderLimit(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	limit := 5
	requestCount := 0

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, lim int, window time.Duration) (bool, int, time.Time, error) {
			requestCount++

			remaining := limit - requestCount
			if remaining < 0 {
				remaining = 0
			}

			return requestCount <= limit, remaining, time.Now().Add(1 * time.Minute), nil
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_open",
		Rules: []config.RateLimitRule{
			{
				Name:        "api",
				PathPattern: "^/api/.*",
				Limit:       limit,
				Window:      1 * time.Minute,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("success"))
		require.NoError(t, err)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	// Send N requests (all should succeed)
	for i := 0; i < limit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/data", http.NoBody)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "request %d should succeed", i+1)
		assert.Equal(t, "success", rec.Body.String())
	}

	assert.Equal(t, limit, requestCount, "should have made exactly limit requests")
}

// TestRateLimit_DeniesOverLimit verifies that the (N+1)th request
// receives a 429 status code when limit is N.
func TestRateLimit_DeniesOverLimit(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	limit := 3
	requestCount := 0

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, lim int, window time.Duration) (bool, int, time.Time, error) {
			requestCount++

			remaining := limit - requestCount
			if remaining < 0 {
				remaining = 0
			}

			allowed := requestCount <= limit

			return allowed, remaining, time.Now().Add(1 * time.Minute), nil
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_open",
		Rules: []config.RateLimitRule{
			{
				Name:        "api",
				PathPattern: "^/api/.*",
				Limit:       limit,
				Window:      1 * time.Minute,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("success"))
		require.NoError(t, err)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	// Send N requests (should all succeed)
	for i := 0; i < limit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "request %d should succeed", i+1)
	}

	// Send (N+1)th request (should fail with 429)
	req := httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "request %d should be rate limited", limit+1)

	// Verify error response
	var response map[string]any

	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "rate limit exceeded", response["error"])
	assert.Equal(t, float64(http.StatusTooManyRequests), response["status"])
}

// TestRateLimit_HeadersPresent verifies that X-RateLimit-* headers
// are present on both 200 and 429 responses.
func TestRateLimit_HeadersPresent(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	limit := 5
	remaining := limit - 1
	resetAt := time.Now().Add(1 * time.Minute)

	tests := []struct {
		name         string
		allowed      bool
		expectedCode int
	}{
		{
			name:         "allowed request has headers",
			allowed:      true,
			expectedCode: http.StatusOK,
		},
		{
			name:         "denied request has headers",
			allowed:      false,
			expectedCode: http.StatusTooManyRequests,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRateLimitService{
				allowFunc: func(ctx context.Context, ip, key string, lim int, window time.Duration) (bool, int, time.Time, error) {
					return tt.allowed, remaining, resetAt, nil
				},
			}

			cfg := config.RateLimitingConfig{
				Enabled:     true,
				FailureMode: "fail_open",
				Rules: []config.RateLimitRule{
					{
						Name:        "api",
						PathPattern: "^/api/.*",
						Limit:       limit,
						Window:      1 * time.Minute,
					},
				},
			}

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			middleware := RateLimit(logger, cfg, mock)
			wrapped := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedCode, rec.Code)

			// Verify rate limit headers
			assert.Equal(t, strconv.Itoa(limit), rec.Header().Get("X-RateLimit-Limit"),
				"X-RateLimit-Limit header should be present")
			assert.Equal(t, strconv.Itoa(remaining), rec.Header().Get("X-RateLimit-Remaining"),
				"X-RateLimit-Remaining header should be present")
			assert.Equal(t, strconv.FormatInt(resetAt.Unix(), 10), rec.Header().Get("X-RateLimit-Reset"),
				"X-RateLimit-Reset header should be present")
		})
	}
}

// TestRateLimit_RetryAfterHeader verifies that the Retry-After header
// is present on 429 responses.
func TestRateLimit_RetryAfterHeader(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	resetAt := time.Now().Add(30 * time.Second)

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
			return false, 0, resetAt, nil
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_open",
		Rules: []config.RateLimitRule{
			{
				Name:        "api",
				PathPattern: "^/api/.*",
				Limit:       10,
				Window:      1 * time.Minute,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	// Verify Retry-After header
	retryAfter := rec.Header().Get("Retry-After")
	assert.NotEmpty(t, retryAfter, "Retry-After header should be present")

	retrySeconds, err := strconv.Atoi(retryAfter)
	require.NoError(t, err)
	assert.Greater(t, retrySeconds, 0, "Retry-After should be positive")
	assert.LessOrEqual(t, retrySeconds, 60, "Retry-After should be reasonable")

	// Verify JSON response includes retry_after
	var response map[string]any

	err = json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.Contains(t, response, "retry_after")
}

// TestRateLimit_ExemptIP verifies that whitelisted IPs bypass rate limiting.
func TestRateLimit_ExemptIP(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	callCount := 0

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
			callCount++

			t.Errorf("rate limiter should not be called for exempt IPs")

			return false, 0, time.Time{}, nil
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_open",
		ExemptIPs:   []string{"127.0.0.1", "10.0.0.0/24", "192.168.1.100"},
		Rules: []config.RateLimitRule{
			{
				Name:        "api",
				PathPattern: "^/api/.*",
				Limit:       1,
				Window:      1 * time.Minute,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	exemptIPs := []string{
		"127.0.0.1",
		"10.0.0.1",
		"10.0.0.50",
		"10.0.0.255",
		"192.168.1.100",
	}

	for _, ip := range exemptIPs {
		req := httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
		req.RemoteAddr = ip + ":12345"
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "exempt IP %s should bypass rate limiting", ip)
	}

	assert.Equal(t, 0, callCount, "rate limiter should not be called for exempt IPs")
}

// TestRateLimit_RuleMatching verifies that the correct rule is applied
// based on the request path.
func TestRateLimit_RuleMatching(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	var capturedKey string

	var capturedLimit int

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
			capturedKey = key
			capturedLimit = limit

			return true, limit - 1, time.Now().Add(window), nil
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_open",
		Rules: []config.RateLimitRule{
			{
				Name:        "api",
				PathPattern: "^/api/.*",
				Limit:       10,
				Window:      1 * time.Minute,
			},
			{
				Name:        "upload",
				PathPattern: "^/upload/.*",
				Limit:       5,
				Window:      5 * time.Minute,
			},
			{
				Name:        "download",
				PathPattern: "^/download/.*",
				Limit:       20,
				Window:      30 * time.Second,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	tests := []struct {
		path          string
		expectedKey   string
		expectedLimit int
	}{
		{"/api/users", "api", 10},
		{"/api/v1/config", "api", 10},
		{"/upload/file.txt", "upload", 5},
		{"/upload/images/photo.jpg", "upload", 5},
		{"/download/data.csv", "download", 20},
		{"/download/files/archive.zip", "download", 20},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			capturedKey = ""
			capturedLimit = 0

			req := httptest.NewRequest(http.MethodGet, tt.path, http.NoBody)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, tt.expectedKey, capturedKey, "wrong rule matched for path %s", tt.path)
			assert.Equal(t, tt.expectedLimit, capturedLimit, "wrong limit for path %s", tt.path)
		})
	}
}

// TestRateLimit_NoMatchingRule verifies that requests not matching any rule
// are allowed through without rate limiting.
func TestRateLimit_NoMatchingRule(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	callCount := 0

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
			callCount++

			t.Errorf("rate limiter should not be called when no rule matches")

			return false, 0, time.Time{}, nil
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_open",
		Rules: []config.RateLimitRule{
			{
				Name:        "api",
				PathPattern: "^/api/.*",
				Limit:       10,
				Window:      1 * time.Minute,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	// Paths that don't match any rule
	paths := []string{
		"/",
		"/health",
		"/metrics",
		"/static/css/style.css",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "path %s should be allowed", path)
	}

	assert.Equal(t, 0, callCount, "rate limiter should not be called")
}

// TestRateLimit_IPExtraction verifies that client IP is extracted correctly
// from CF-Connecting-IP > X-Forwarded-For > RemoteAddr.
func TestRateLimit_IPExtraction(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	var capturedIP string

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
			capturedIP = ip

			return true, limit - 1, time.Now().Add(window), nil
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_open",
		Rules: []config.RateLimitRule{
			{
				Name:        "api",
				PathPattern: "^/api/.*",
				Limit:       10,
				Window:      1 * time.Minute,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	tests := []struct {
		name           string
		remoteAddr     string
		cfConnectingIP string
		xForwardedFor  string
		xRealIP        string
		expectedIP     string
	}{
		{
			name:           "CF-Connecting-IP takes priority",
			remoteAddr:     "10.0.0.1:12345",
			cfConnectingIP: "203.0.113.1",
			xForwardedFor:  "198.51.100.1, 192.0.2.1",
			xRealIP:        "198.18.0.1",
			expectedIP:     "203.0.113.1",
		},
		{
			name:          "X-Forwarded-For when no CF-Connecting-IP",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "203.0.113.2, 198.51.100.1",
			xRealIP:       "198.18.0.1",
			expectedIP:    "203.0.113.2",
		},
		{
			name:       "X-Real-IP when no CF or X-Forwarded-For",
			remoteAddr: "10.0.0.1:12345",
			xRealIP:    "203.0.113.3",
			expectedIP: "203.0.113.3",
		},
		{
			name:       "RemoteAddr fallback",
			remoteAddr: "203.0.113.4:54321",
			expectedIP: "203.0.113.4",
		},
		{
			name:       "RemoteAddr without port",
			remoteAddr: "203.0.113.5",
			expectedIP: "203.0.113.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capturedIP = ""

			req := httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
			req.RemoteAddr = tt.remoteAddr

			if tt.cfConnectingIP != "" {
				req.Header.Set("CF-Connecting-IP", tt.cfConnectingIP)
			}

			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}

			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, tt.expectedIP, capturedIP, "IP extraction failed for %s", tt.name)
		})
	}
}

// TestRateLimit_RedisError_FailOpen verifies that when the rate limiter
// returns an error in fail_open mode, the request is allowed.
func TestRateLimit_RedisError_FailOpen(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
			// Simulate Redis error with fail_open
			return true, 0, time.Time{}, fmt.Errorf("redis connection failed")
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_open",
		Rules: []config.RateLimitRule{
			{
				Name:        "api",
				PathPattern: "^/api/.*",
				Limit:       10,
				Window:      1 * time.Minute,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("success"))
		require.NoError(t, err)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Request should succeed despite error (fail_open mode)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "success", rec.Body.String())
}

// TestRateLimit_RedisError_FailClosed verifies that when the rate limiter
// returns an error in fail_closed mode, the request is denied.
func TestRateLimit_RedisError_FailClosed(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
			// Simulate Redis error with fail_closed
			return false, 0, time.Time{}, fmt.Errorf("redis connection failed")
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_closed",
		Rules: []config.RateLimitRule{
			{
				Name:        "api",
				PathPattern: "^/api/.*",
				Limit:       10,
				Window:      1 * time.Minute,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Request should be denied (fail_closed mode)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	var response map[string]any

	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "service unavailable", response["error"])
}

// TestRateLimit_MultipleRulesFirstMatch verifies that when multiple rules
// match a path, the first matching rule is applied.
func TestRateLimit_MultipleRulesFirstMatch(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	var capturedKey string

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
			capturedKey = key

			return true, limit - 1, time.Now().Add(window), nil
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_open",
		Rules: []config.RateLimitRule{
			{
				Name:        "specific",
				PathPattern: "^/api/v1/users.*",
				Limit:       5,
				Window:      1 * time.Minute,
			},
			{
				Name:        "general",
				PathPattern: "^/api/.*",
				Limit:       10,
				Window:      1 * time.Minute,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	// Should match first (more specific) rule
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/123", http.NoBody)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "specific", capturedKey, "should match first rule")

	// Should match second (general) rule
	capturedKey = ""
	req = httptest.NewRequest(http.MethodGet, "/api/v2/data", http.NoBody)
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "general", capturedKey, "should match second rule")
}

// TestRateLimit_Integration_RealScenario simulates a realistic scenario
// with multiple clients and varying request patterns.
func TestRateLimit_Integration_RealScenario(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Track requests per IP
	requestCounts := make(map[string]int)

	mock := &mockRateLimitService{
		allowFunc: func(ctx context.Context, ip, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
			requestCounts[ip]++

			remaining := limit - requestCounts[ip]
			if remaining < 0 {
				remaining = 0
			}

			allowed := requestCounts[ip] <= limit

			return allowed, remaining, time.Now().Add(window), nil
		},
	}

	cfg := config.RateLimitingConfig{
		Enabled:     true,
		FailureMode: "fail_open",
		ExemptIPs:   []string{"10.0.0.100"},
		Rules: []config.RateLimitRule{
			{
				Name:        "api",
				PathPattern: "^/api/.*",
				Limit:       3,
				Window:      1 * time.Minute,
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimit(logger, cfg, mock)
	wrapped := middleware(handler)

	// Scenario: 3 clients with different behaviors
	clients := []struct {
		ip              string
		requests        int
		expectedAllowed int
		expectedDenied  int
	}{
		{"192.168.1.1", 5, 3, 2},  // Regular client, exceeds limit
		{"192.168.1.2", 2, 2, 0},  // Regular client, under limit
		{"10.0.0.100", 10, 10, 0}, // Exempt client, unlimited
	}

	for _, client := range clients {
		t.Run(fmt.Sprintf("client_%s", client.ip), func(t *testing.T) {
			allowedCount := 0
			deniedCount := 0

			for i := 0; i < client.requests; i++ {
				req := httptest.NewRequest(http.MethodGet, "/api/data", http.NoBody)
				req.RemoteAddr = client.ip + ":12345"
				rec := httptest.NewRecorder()

				wrapped.ServeHTTP(rec, req)

				switch rec.Code {
				case http.StatusOK:
					allowedCount++
				case http.StatusTooManyRequests:
					deniedCount++
				}
			}

			assert.Equal(t, client.expectedAllowed, allowedCount,
				"client %s: wrong number of allowed requests", client.ip)
			assert.Equal(t, client.expectedDenied, deniedCount,
				"client %s: wrong number of denied requests", client.ip)
		})
	}
}
