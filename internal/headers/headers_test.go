package headers

import (
	"testing"

	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	tests := []struct {
		name      string
		policies  []config.HeaderPolicy
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid policies compile successfully",
			policies: []config.HeaderPolicy{
				{
					Name:        "test",
					PathPattern: `^/api/.*`,
					Headers:     map[string]string{"Cache-Control": "max-age=60"},
				},
			},
			wantError: false,
		},
		{
			name: "multiple valid policies",
			policies: []config.HeaderPolicy{
				{
					Name:        "static",
					PathPattern: `\.(js|css)$`,
					Headers:     map[string]string{"Cache-Control": "max-age=31536000"},
				},
				{
					Name:        "api",
					PathPattern: `^/api/`,
					Headers:     map[string]string{"Cache-Control": "no-cache"},
				},
			},
			wantError: false,
		},
		{
			name: "invalid regex pattern returns error",
			policies: []config.HeaderPolicy{
				{
					Name:        "invalid",
					PathPattern: `[unclosed`,
					Headers:     map[string]string{"Cache-Control": "max-age=60"},
				},
			},
			wantError: true,
			errorMsg:  "invalid path_pattern in policy \"invalid\"",
		},
		{
			name:      "empty policy list succeeds",
			policies:  []config.HeaderPolicy{},
			wantError: false,
		},
		{
			name: "policy with empty headers map succeeds",
			policies: []config.HeaderPolicy{
				{
					Name:        "empty",
					PathPattern: `.*`,
					Headers:     map[string]string{},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager(tt.policies)

			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, mgr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, mgr)
				assert.Len(t, mgr.policies, len(tt.policies))
			}
		})
	}
}

func TestManagerMatch(t *testing.T) {
	tests := []struct {
		name     string
		policies []config.HeaderPolicy
		path     string
		want     map[string]string
	}{
		{
			name: "exact path matches",
			policies: []config.HeaderPolicy{
				{
					Name:        "config",
					PathPattern: `^/api/v1/config$`,
					Headers:     map[string]string{"Cache-Control": "max-age=60"},
				},
			},
			path: "/api/v1/config",
			want: map[string]string{"Cache-Control": "max-age=60"},
		},
		{
			name: "regex pattern matches file extension",
			policies: []config.HeaderPolicy{
				{
					Name:        "js",
					PathPattern: `\.js$`,
					Headers:     map[string]string{"Cache-Control": "max-age=31536000"},
				},
			},
			path: "/static/app.js",
			want: map[string]string{"Cache-Control": "max-age=31536000"},
		},
		{
			name: "first match wins with multiple patterns",
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
			path: "/api/v1/config",
			want: map[string]string{"X-Policy": "first"},
		},
		{
			name: "no match returns nil",
			policies: []config.HeaderPolicy{
				{
					Name:        "api",
					PathPattern: `^/api/.*`,
					Headers:     map[string]string{"Cache-Control": "max-age=60"},
				},
			},
			path: "/static/app.js",
			want: nil,
		},
		{
			name: "multiple headers in policy",
			policies: []config.HeaderPolicy{
				{
					Name:        "multi",
					PathPattern: `^/api/.*`,
					Headers: map[string]string{
						"Cache-Control": "max-age=60",
						"Vary":          "Accept-Encoding",
						"X-Custom":      "value",
						"X-Another":     "test",
					},
				},
			},
			path: "/api/v1/data",
			want: map[string]string{
				"Cache-Control": "max-age=60",
				"Vary":          "Accept-Encoding",
				"X-Custom":      "value",
				"X-Another":     "test",
			},
		},
		{
			name: "case sensitive regex",
			policies: []config.HeaderPolicy{
				{
					Name:        "lowercase",
					PathPattern: `^/api/.*`,
					Headers:     map[string]string{"X-Policy": "lowercase"},
				},
			},
			path: "/API/test",
			want: nil,
		},
		{
			name: "complex regex pattern",
			policies: []config.HeaderPolicy{
				{
					Name:        "complex",
					PathPattern: `\.(js|css|png|jpg|jpeg|gif|svg)$`,
					Headers:     map[string]string{"Cache-Control": "max-age=31536000, immutable"},
				},
			},
			path: "/assets/styles.css",
			want: map[string]string{"Cache-Control": "max-age=31536000, immutable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager(tt.policies)
			require.NoError(t, err)

			got := mgr.Match(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestManagerMatch_EdgeCases tests edge cases for path matching.
func TestManagerMatch_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		policies []config.HeaderPolicy
		path     string
		want     map[string]string
	}{
		{
			name: "empty path",
			policies: []config.HeaderPolicy{
				{
					Name:        "root",
					PathPattern: `^/$`,
					Headers:     map[string]string{"X-Root": "true"},
				},
			},
			path: "",
			want: nil,
		},
		{
			name: "very long path",
			policies: []config.HeaderPolicy{
				{
					Name:        "long",
					PathPattern: `^/api/`,
					Headers:     map[string]string{"X-Long": "true"},
				},
			},
			path: "/api/" + string(make([]byte, 10000)),
			want: map[string]string{"X-Long": "true"},
		},
		{
			name: "path with special characters",
			policies: []config.HeaderPolicy{
				{
					Name:        "special",
					PathPattern: `^/api/.*`,
					Headers:     map[string]string{"X-Special": "true"},
				},
			},
			path: "/api/test?foo=bar&baz=qux#fragment",
			want: map[string]string{"X-Special": "true"},
		},
		{
			name: "path with encoded characters",
			policies: []config.HeaderPolicy{
				{
					Name:        "encoded",
					PathPattern: `^/files/`,
					Headers:     map[string]string{"X-Encoded": "true"},
				},
			},
			path: "/files/my%20file%20name.txt",
			want: map[string]string{"X-Encoded": "true"},
		},
		{
			name: "path with unicode",
			policies: []config.HeaderPolicy{
				{
					Name:        "unicode",
					PathPattern: `^/files/`,
					Headers:     map[string]string{"X-Unicode": "true"},
				},
			},
			path: "/files/文件.txt",
			want: map[string]string{"X-Unicode": "true"},
		},
		{
			name: "dot dot path traversal attempt",
			policies: []config.HeaderPolicy{
				{
					Name:        "traversal",
					PathPattern: `^\.\./`,
					Headers:     map[string]string{"X-Bad": "true"},
				},
			},
			path: "../etc/passwd",
			want: map[string]string{"X-Bad": "true"},
		},
		{
			name: "nil headers map returns nil",
			policies: []config.HeaderPolicy{
				{
					Name:        "nil",
					PathPattern: `^/test$`,
					Headers:     nil,
				},
			},
			path: "/test",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager(tt.policies)
			require.NoError(t, err)

			got := mgr.Match(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestManagerMatch_OrderMatters verifies first-match-wins behavior.
func TestManagerMatch_OrderMatters(t *testing.T) {
	policies := []config.HeaderPolicy{
		{
			Name:        "specific",
			PathPattern: `^/api/v1/users/\d+$`,
			Headers:     map[string]string{"X-Policy": "specific"},
		},
		{
			Name:        "general",
			PathPattern: `^/api/.*`,
			Headers:     map[string]string{"X-Policy": "general"},
		},
		{
			Name:        "catch-all",
			PathPattern: `.*`,
			Headers:     map[string]string{"X-Policy": "catch-all"},
		},
	}

	mgr, err := NewManager(policies)
	require.NoError(t, err)

	tests := []struct {
		path string
		want string
	}{
		{"/api/v1/users/123", "specific"},
		{"/api/v1/config", "general"},
		{"/static/app.js", "catch-all"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			headers := mgr.Match(tt.path)
			require.NotNil(t, headers)
			assert.Equal(t, tt.want, headers["X-Policy"])
		})
	}
}

// TestManagerMatch_Concurrent tests concurrent access to Manager.Match().
func TestManagerMatch_Concurrent(t *testing.T) {
	policies := []config.HeaderPolicy{
		{
			Name:        "api",
			PathPattern: `^/api/.*`,
			Headers:     map[string]string{"Cache-Control": "max-age=60"},
		},
		{
			Name:        "static",
			PathPattern: `\.(js|css)$`,
			Headers:     map[string]string{"Cache-Control": "max-age=31536000"},
		},
	}

	mgr, err := NewManager(policies)
	require.NoError(t, err)

	// Run concurrent matches
	const numGoroutines = 100

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer func() { done <- true }()

			paths := []string{
				"/api/v1/config",
				"/static/app.js",
				"/api/v2/users",
				"/styles/main.css",
			}

			for j := 0; j < 100; j++ {
				path := paths[j%len(paths)]
				headers := mgr.Match(path)
				assert.NotNil(t, headers)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

// TestNewManager_ErrorHandling tests error handling for invalid configurations.
func TestNewManager_ErrorHandling(t *testing.T) {
	tests := []struct {
		name      string
		policies  []config.HeaderPolicy
		wantError string
	}{
		{
			name: "unclosed bracket",
			policies: []config.HeaderPolicy{
				{Name: "bad", PathPattern: `[unclosed`, Headers: map[string]string{"X": "1"}},
			},
			wantError: "invalid path_pattern in policy \"bad\"",
		},
		{
			name: "invalid quantifier",
			policies: []config.HeaderPolicy{
				{Name: "bad", PathPattern: `*invalid`, Headers: map[string]string{"X": "1"}},
			},
			wantError: "invalid path_pattern in policy \"bad\"",
		},
		{
			name: "unclosed group",
			policies: []config.HeaderPolicy{
				{Name: "bad", PathPattern: `(unclosed`, Headers: map[string]string{"X": "1"}},
			},
			wantError: "invalid path_pattern in policy \"bad\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager(tt.policies)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
			assert.Nil(t, mgr)
		})
	}
}

// BenchmarkManagerMatch benchmarks path matching performance.
func BenchmarkManagerMatch(b *testing.B) {
	policies := []config.HeaderPolicy{
		{Name: "static", PathPattern: `\.(js|css|png|jpg|jpeg|gif|svg)$`, Headers: map[string]string{"Cache-Control": "max-age=31536000"}},
		{Name: "html", PathPattern: `\.html$`, Headers: map[string]string{"Cache-Control": "max-age=1"}},
		{Name: "api_config", PathPattern: `^/api/v1/config$`, Headers: map[string]string{"Cache-Control": "max-age=60"}},
		{Name: "api_proxy", PathPattern: `^/api/v1/.+/.+`, Headers: map[string]string{"Cache-Control": "max-age=1"}},
		{Name: "health", PathPattern: `^/(health|metrics)$`, Headers: map[string]string{"Cache-Control": "no-cache"}},
		{Name: "default", PathPattern: `.*`, Headers: map[string]string{"Cache-Control": "public"}},
	}

	mgr, err := NewManager(policies)
	require.NoError(b, err)

	paths := []string{
		"/static/app.js",
		"/api/v1/config",
		"/api/v1/mainnet/beacon/slots",
		"/health",
		"/index.html",
		"/some/random/path",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		mgr.Match(path)
	}
}

// BenchmarkNewManager benchmarks manager creation with regex compilation.
func BenchmarkNewManager(b *testing.B) {
	policies := []config.HeaderPolicy{
		{Name: "static", PathPattern: `\.(js|css|png|jpg|jpeg|gif|svg)$`, Headers: map[string]string{"Cache-Control": "max-age=31536000"}},
		{Name: "html", PathPattern: `\.html$`, Headers: map[string]string{"Cache-Control": "max-age=1"}},
		{Name: "api_config", PathPattern: `^/api/v1/config$`, Headers: map[string]string{"Cache-Control": "max-age=60"}},
		{Name: "api_proxy", PathPattern: `^/api/v1/.+/.+`, Headers: map[string]string{"Cache-Control": "max-age=1"}},
		{Name: "health", PathPattern: `^/(health|metrics)$`, Headers: map[string]string{"Cache-Control": "no-cache"}},
		{Name: "default", PathPattern: `.*`, Headers: map[string]string{"Cache-Control": "public"}},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = NewManager(policies)
	}
}
