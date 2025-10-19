package frontend

import (
	"fmt"
	"io"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteIndexCache_PrewarmRoutes(t *testing.T) {
	tests := []struct {
		name        string
		filesystem  fs.FS
		configData  interface{}
		boundsData  interface{}
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful prewarm with head.json",
			filesystem: fstest.MapFS{
				"index.html": &fstest.MapFile{
					Data: []byte("<html><head></head><body></body></html>"),
				},
				"head.json": &fstest.MapFile{
					Data: []byte(`{
						"_default": {"raw": "<meta name=\"default\">"},
						"/": {"raw": "<title>Home</title>"},
						"/about": {"raw": "<title>About</title>"}
					}`),
				},
			},
			configData:  map[string]string{"version": "1.0"},
			boundsData:  map[string]int{"max": 100},
			expectError: false,
		},
		{
			name: "successful prewarm without head.json",
			filesystem: fstest.MapFS{
				"index.html": &fstest.MapFile{
					Data: []byte("<html><head></head><body></body></html>"),
				},
			},
			configData:  map[string]string{"version": "1.0"},
			boundsData:  map[string]int{"max": 100},
			expectError: false,
		},
		{
			name: "missing index.html returns error",
			filesystem: fstest.MapFS{
				"other.html": &fstest.MapFile{
					Data: []byte("<html></html>"),
				},
			},
			configData:  map[string]string{},
			boundsData:  map[string]string{},
			expectError: true,
			errorMsg:    "failed to open index.html",
		},
		{
			name: "invalid HTML returns error",
			filesystem: fstest.MapFS{
				"index.html": &fstest.MapFile{
					Data: []byte("<html><body></body></html>"), // Missing <head>
				},
			},
			configData:  map[string]string{},
			boundsData:  map[string]string{},
			expectError: true,
			errorMsg:    "failed to create default injected HTML",
		},
		{
			name: "invalid head.json returns error",
			filesystem: fstest.MapFS{
				"index.html": &fstest.MapFile{
					Data: []byte("<html><head></head><body></body></html>"),
				},
				"head.json": &fstest.MapFile{
					Data: []byte("invalid json"),
				},
			},
			configData:  map[string]string{},
			boundsData:  map[string]string{},
			expectError: true,
			errorMsg:    "failed to parse head.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &RouteIndexCache{}

			logger := logrus.New()
			logger.SetOutput(io.Discard)
			err := cache.PrewarmRoutes(
				logger,
				tt.filesystem,
				tt.configData,
				tt.boundsData,
			)

			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}

				return
			}

			require.NoError(t, err)

			// Verify cache is populated
			original := cache.GetOriginal()
			assert.NotEmpty(t, original)

			// Default route should always exist
			defaultHTML := cache.GetForRoute("_default")
			assert.NotEmpty(t, defaultHTML)
			assert.Greater(t, len(defaultHTML), len(original), "injected should be larger than original")
		})
	}
}

func TestRouteIndexCache_GetForRoute(t *testing.T) {
	cache := &RouteIndexCache{}

	filesystem := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<html><head></head><body></body></html>"),
		},
		"head.json": &fstest.MapFile{
			Data: []byte(`{
				"_default": {"raw": "<meta name=\"default\">"},
				"/": {"raw": "<title>Home</title>"},
				"/about": {"raw": "<title>About</title>"}
			}`),
		},
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	err := cache.PrewarmRoutes(
		logger,
		filesystem,
		map[string]string{"test": "data"},
		map[string]string{},
	)
	require.NoError(t, err)

	t.Run("returns specific route", func(t *testing.T) {
		homeHTML := cache.GetForRoute("/")
		require.NotEmpty(t, homeHTML)
		assert.Contains(t, string(homeHTML), "Home")
		assert.Contains(t, string(homeHTML), "window.__CONFIG__")
	})

	t.Run("returns about route", func(t *testing.T) {
		aboutHTML := cache.GetForRoute("/about")
		require.NotEmpty(t, aboutHTML)
		assert.Contains(t, string(aboutHTML), "About")
		assert.Contains(t, string(aboutHTML), "window.__CONFIG__")
	})

	t.Run("falls back to default for unknown route", func(t *testing.T) {
		unknownHTML := cache.GetForRoute("/unknown")
		require.NotEmpty(t, unknownHTML)
		assert.Contains(t, string(unknownHTML), "default")
		assert.Contains(t, string(unknownHTML), "window.__CONFIG__")
	})

	t.Run("normalizes empty string to /", func(t *testing.T) {
		homeHTML := cache.GetForRoute("")
		require.NotEmpty(t, homeHTML)
		assert.Contains(t, string(homeHTML), "Home")
	})
}

func TestRouteIndexCache_GetOriginal(t *testing.T) {
	cache := &RouteIndexCache{}

	originalHTML := "<html><head></head><body>Test</body></html>"
	filesystem := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(originalHTML),
		},
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	err := cache.PrewarmRoutes(
		logger,
		filesystem,
		map[string]string{},
		map[string]string{},
	)
	require.NoError(t, err)

	original := cache.GetOriginal()
	require.NotEmpty(t, original)

	assert.Equal(t, originalHTML, string(original))
}

func TestRouteIndexCache_Update(t *testing.T) {
	cache := &RouteIndexCache{}

	filesystem := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<html><head></head><body></body></html>"),
		},
		"head.json": &fstest.MapFile{
			Data: []byte(`{
				"_default": {"raw": "<meta name=\"default\">"},
				"/": {"raw": "<title>Home</title>"}
			}`),
		},
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Initial prewarm
	err := cache.PrewarmRoutes(
		logger,
		filesystem,
		map[string]string{"version": "1.0"},
		map[string]string{},
	)
	require.NoError(t, err)

	initialDefault := cache.GetForRoute("_default")
	initialHome := cache.GetForRoute("/")

	// Update with new data
	err = cache.Update(
		map[string]string{"version": "2.0"},
		map[string]int{"max": 200},
	)
	require.NoError(t, err)

	updatedDefault := cache.GetForRoute("_default")
	updatedHome := cache.GetForRoute("/")

	// Verify content was updated
	assert.NotEqual(t, string(initialDefault), string(updatedDefault))
	assert.NotEqual(t, string(initialHome), string(updatedHome))
	assert.Contains(t, string(updatedDefault), "2.0")
	assert.Contains(t, string(updatedHome), "2.0")
}

func TestRouteIndexCache_Update_InvalidHTML(t *testing.T) {
	cache := &RouteIndexCache{}

	// Manually set invalid original HTML
	cache.mu.Lock()
	cache.original = []byte("<html><body></body></html>") // Missing <head>
	cache.headData = make(HeadData)
	cache.mu.Unlock()

	err := cache.Update(map[string]string{}, map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create default injected HTML")
}

func TestRouteIndexCache_ConcurrentAccess(t *testing.T) {
	cache := &RouteIndexCache{}

	filesystem := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<html><head></head><body></body></html>"),
		},
		"head.json": &fstest.MapFile{
			Data: []byte(`{
				"_default": {"raw": "<meta name=\"default\">"},
				"/": {"raw": "<title>Home</title>"},
				"/about": {"raw": "<title>About</title>"}
			}`),
		},
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Initial prewarm
	err := cache.PrewarmRoutes(
		logger,
		filesystem,
		map[string]string{"initial": "data"},
		map[string]string{},
	)
	require.NoError(t, err)

	var (
		wg         sync.WaitGroup
		iterations = 100
	)

	// Spawn 50 readers
	for i := 0; i < 50; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			routes := []string{"/", "/about", "/unknown", ""}
			for j := 0; j < iterations; j++ {
				route := routes[j%len(routes)]
				html := cache.GetForRoute(route)
				original := cache.GetOriginal()

				assert.NotEmpty(t, html)
				assert.NotEmpty(t, original)
			}
		}()
	}

	// Spawn 10 writers
	for i := 0; i < 10; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				config := map[string]string{
					"writer": fmt.Sprintf("writer-%d", id),
					"iter":   fmt.Sprintf("%d", j),
				}
				err := cache.Update(config, map[string]string{})
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify cache is still valid after concurrent access
	original := cache.GetOriginal()
	defaultHTML := cache.GetForRoute("_default")
	homeHTML := cache.GetForRoute("/")

	assert.NotEmpty(t, original)
	assert.NotEmpty(t, defaultHTML)
	assert.NotEmpty(t, homeHTML)
}
