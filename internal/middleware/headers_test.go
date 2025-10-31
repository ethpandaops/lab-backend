package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/ethpandaops/lab-backend/internal/headers"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaders(t *testing.T) {
	tests := []struct {
		name          string
		policies      []config.HeaderPolicy
		requestPath   string
		wantHeaders   map[string]string
		handlerCalled bool
	}{
		{
			name: "middleware sets headers from matched policy",
			policies: []config.HeaderPolicy{
				{
					Name:        "cache",
					PathPattern: `^/api/.*`,
					Headers:     map[string]string{"Cache-Control": "max-age=60"},
				},
			},
			requestPath:   "/api/v1/config",
			wantHeaders:   map[string]string{"Cache-Control": "max-age=60"},
			handlerCalled: true,
		},
		{
			name: "middleware sets multiple headers",
			policies: []config.HeaderPolicy{
				{
					Name:        "multi",
					PathPattern: `.*`,
					Headers: map[string]string{
						"Cache-Control": "max-age=60",
						"Vary":          "Accept-Encoding",
						"X-Custom":      "test-value",
					},
				},
			},
			requestPath: "/any/path",
			wantHeaders: map[string]string{
				"Cache-Control": "max-age=60",
				"Vary":          "Accept-Encoding",
				"X-Custom":      "test-value",
			},
			handlerCalled: true,
		},
		{
			name: "middleware does nothing for non-matching paths",
			policies: []config.HeaderPolicy{
				{
					Name:        "api",
					PathPattern: `^/api/.*`,
					Headers:     map[string]string{"Cache-Control": "max-age=60"},
				},
			},
			requestPath:   "/static/app.js",
			wantHeaders:   map[string]string{},
			handlerCalled: true,
		},
		{
			name: "middleware doesn't break handler execution",
			policies: []config.HeaderPolicy{
				{
					Name:        "test",
					PathPattern: `.*`,
					Headers:     map[string]string{"X-Test": "value"},
				},
			},
			requestPath:   "/test",
			wantHeaders:   map[string]string{"X-Test": "value"},
			handlerCalled: true,
		},
		{
			name: "first matching policy wins",
			policies: []config.HeaderPolicy{
				{
					Name:        "first",
					PathPattern: `^/api/.*`,
					Headers:     map[string]string{"X-Policy": "first"},
				},
				{
					Name:        "second",
					PathPattern: `^/api/v1/.*`,
					Headers:     map[string]string{"X-Policy": "second"},
				},
			},
			requestPath:   "/api/v1/config",
			wantHeaders:   map[string]string{"X-Policy": "first"},
			handlerCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := headers.NewManager(tt.policies)
			require.NoError(t, err)

			// Create a test logger (discards output)
			log := logrus.New()
			log.SetLevel(logrus.DebugLevel)
			log.SetOutput(httptest.NewRecorder())

			// Track if handler was called
			handlerCalled := false

			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true

				w.WriteHeader(http.StatusOK)
			})

			// Wrap handler with middleware
			middleware := Headers(mgr, log)
			handler := middleware(testHandler)

			// Create test request
			req := httptest.NewRequest(http.MethodGet, tt.requestPath, nil)
			rec := httptest.NewRecorder()

			// Execute request
			handler.ServeHTTP(rec, req)

			// Verify handler was called
			assert.Equal(t, tt.handlerCalled, handlerCalled)

			// Verify headers
			for key, want := range tt.wantHeaders {
				got := rec.Header().Get(key)
				assert.Equal(t, want, got, "header %s mismatch", key)
			}

			// Verify no unexpected headers were set (only check headers we care about)
			if len(tt.wantHeaders) == 0 {
				// No headers should have been set by middleware
				// Note: httptest.ResponseRecorder may set Content-Type, so we only check our headers
				for _, key := range []string{"Cache-Control", "Vary", "X-Custom", "X-Policy", "X-Test"} {
					assert.Empty(t, rec.Header().Get(key), "unexpected header %s", key)
				}
			}
		})
	}
}

// TestHeadersWithRealHandler tests middleware integration with a real handler.
func TestHeadersWithRealHandler(t *testing.T) {
	policies := []config.HeaderPolicy{
		{
			Name:        "static",
			PathPattern: `\.(js|css)$`,
			Headers: map[string]string{
				"Cache-Control": "public, max-age=31536000, immutable",
				"Vary":          "Accept-Encoding",
			},
		},
		{
			Name:        "api",
			PathPattern: `^/api/`,
			Headers:     map[string]string{"Cache-Control": "no-cache, no-store"},
		},
	}

	mgr, err := headers.NewManager(policies)
	require.NoError(t, err)

	log := logrus.New()
	log.SetOutput(httptest.NewRecorder())

	// Real handler that sets its own headers
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	middleware := Headers(mgr, log)
	wrappedHandler := middleware(handler)

	// Test static asset
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rec, req)

	assert.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "Accept-Encoding", rec.Header().Get("Vary"))
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusOK, rec.Code)

	// Test API endpoint
	req = httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	rec = httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rec, req)

	assert.Equal(t, "no-cache, no-store", rec.Header().Get("Cache-Control"))
	assert.Empty(t, rec.Header().Get("Vary"))
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusOK, rec.Code)
}
