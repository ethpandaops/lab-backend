package frontend

import (
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/lab-backend/internal/testutil"
)

func TestIndexCache_Prewarm(t *testing.T) {
	tests := []struct {
		name        string
		filesystem  fs.FS
		configData  interface{}
		boundsData  interface{}
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful prewarm",
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
			errorMsg:    "failed to inject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &IndexCache{}

			err := cache.Prewarm(
				testutil.NewTestLogger(),
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
			injected := cache.GetInjected()

			assert.NotEmpty(t, original)
			assert.NotEmpty(t, injected)
			assert.Greater(t, len(injected), len(original), "injected should be larger than original")
		})
	}
}

func TestIndexCache_GetInjected(t *testing.T) {
	cache := &IndexCache{}

	filesystem := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<html><head></head><body></body></html>"),
		},
	}

	err := cache.Prewarm(
		testutil.NewTestLogger(),
		filesystem,
		map[string]string{"test": "data"},
		map[string]string{},
	)
	require.NoError(t, err)

	injected := cache.GetInjected()
	require.NotEmpty(t, injected)

	// Verify injected content contains config
	assert.Contains(t, string(injected), "window.__CONFIG__")
	assert.Contains(t, string(injected), "window.__BOUNDS__")
}

func TestIndexCache_GetOriginal(t *testing.T) {
	cache := &IndexCache{}

	originalHTML := "<html><head></head><body>Test</body></html>"
	filesystem := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(originalHTML),
		},
	}

	err := cache.Prewarm(
		testutil.NewTestLogger(),
		filesystem,
		map[string]string{},
		map[string]string{},
	)
	require.NoError(t, err)

	original := cache.GetOriginal()
	require.NotEmpty(t, original)

	assert.Equal(t, originalHTML, string(original))
}

func TestIndexCache_Update(t *testing.T) {
	cache := &IndexCache{}

	filesystem := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<html><head></head><body></body></html>"),
		},
	}

	// Initial prewarm
	err := cache.Prewarm(
		testutil.NewTestLogger(),
		filesystem,
		map[string]string{"version": "1.0"},
		map[string]string{},
	)
	require.NoError(t, err)

	initialInjected := cache.GetInjected()

	// Update with new data
	err = cache.Update(
		map[string]string{"version": "2.0"},
		map[string]int{"max": 200},
	)
	require.NoError(t, err)

	updatedInjected := cache.GetInjected()

	// Verify injected was updated
	assert.NotEqual(t, string(initialInjected), string(updatedInjected))
	assert.Contains(t, string(updatedInjected), "2.0")
}

func TestIndexCache_Update_InvalidHTML(t *testing.T) {
	cache := &IndexCache{}

	// Manually set invalid original HTML
	cache.mu.Lock()
	cache.original = []byte("<html><body></body></html>") // Missing <head>
	cache.mu.Unlock()

	err := cache.Update(map[string]string{}, map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to inject")
}

func TestIndexCache_ConcurrentAccess(t *testing.T) {
	cache := &IndexCache{}

	filesystem := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<html><head></head><body></body></html>"),
		},
	}

	// Initial prewarm
	err := cache.Prewarm(
		testutil.NewTestLogger(),
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

			for j := 0; j < iterations; j++ {
				original := cache.GetOriginal()
				injected := cache.GetInjected()

				assert.NotEmpty(t, original)
				assert.NotEmpty(t, injected)
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
	injected := cache.GetInjected()

	assert.NotEmpty(t, original)
	assert.NotEmpty(t, injected)
}
