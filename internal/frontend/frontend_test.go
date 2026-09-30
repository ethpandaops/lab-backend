package frontend

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/ethpandaops/lab-backend/internal/headers"
	"github.com/ethpandaops/lab-backend/internal/middleware"
)

func newTestFrontend(t *testing.T) http.Handler {
	t.Helper()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	filesystem := fstest.MapFS{
		"index.html":         &fstest.MapFile{Data: []byte("<html><head></head><body></body></html>")},
		"assets/app-abc1.js": &fstest.MapFile{Data: []byte("console.log('app')")},
		"favicon.ico":        &fstest.MapFile{Data: []byte("ico")},
	}

	cache := &RouteIndexCache{}
	require.NoError(t, cache.PrewarmRoutes(logger, filesystem, map[string]string{}, map[string]string{}, map[string]string{}))

	f := &Frontend{fs: filesystem, routeCache: cache, logger: logger, done: make(chan struct{})}

	// Mirror production header policies: hashed assets are cached for a long time by path.
	manager, err := headers.NewManager([]config.HeaderPolicy{
		{Name: "static_assets", PathPattern: `\.(js|css|png|svg|woff2|ico)$`, Headers: map[string]string{"Cache-Control": "public, max-age=31536000, immutable"}},
		{Name: "default", PathPattern: `.*`, Headers: map[string]string{"Cache-Control": "public, max-age=1"}},
	})
	require.NoError(t, err)

	return middleware.Headers(manager, logger)(f)
}

func TestFrontend_ServeHTTP_MissingAssetsAreNotServedAsIndex(t *testing.T) {
	handler := newTestFrontend(t)

	tests := []struct {
		name             string
		path             string
		wantStatus       int
		wantCacheControl string
		wantHTML         bool
	}{
		{name: "existing asset", path: "/assets/app-abc1.js", wantStatus: http.StatusOK, wantCacheControl: "public, max-age=31536000, immutable"},
		{name: "missing hashed bundle from another release", path: "/assets/app-zzz9.js", wantStatus: http.StatusNotFound, wantCacheControl: "no-store"},
		{name: "missing asset with no extension under assets", path: "/assets/chunk", wantStatus: http.StatusNotFound, wantCacheControl: "no-store"},
		{name: "missing root static file", path: "/robots.txt", wantStatus: http.StatusNotFound, wantCacheControl: "no-store"},
		{name: "missing uppercase extension", path: "/logo.SVG", wantStatus: http.StatusNotFound, wantCacheControl: "no-store"},
		{name: "SPA route", path: "/ethereum/slots", wantStatus: http.StatusOK, wantCacheControl: "public, max-age=1", wantHTML: true},
		{name: "SPA route containing a dot", path: "/xatu/contributors/node.example", wantStatus: http.StatusOK, wantCacheControl: "public, max-age=1", wantHTML: true},
		{name: "root", path: "/", wantStatus: http.StatusOK, wantCacheControl: "public, max-age=1", wantHTML: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantCacheControl, rec.Header().Get("Cache-Control"))

			if tt.wantHTML {
				assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
			} else {
				assert.NotContains(t, rec.Header().Get("Content-Type"), "text/html")
			}
		})
	}
}
