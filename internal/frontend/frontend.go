package frontend

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/api"
	"github.com/ethpandaops/lab-backend/internal/bounds"
	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/web"
)

// Frontend serves static frontend files with caching and config injection.
type Frontend struct {
	fs                    fs.FS                  // Embedded or local filesystem
	indexCache            *IndexCache            // Prewarmed index.html cache
	configHandler         *api.ConfigHandler     // Handler for config data
	boundsProvider        bounds.Provider        // Provider for bounds data
	cartographoorProvider cartographoor.Provider // Provider for cartographoor data
	logger                logrus.FieldLogger
	devMode               bool           // True if using local filesystem
	done                  chan struct{}  // Signal to stop refresh loop
	wg                    sync.WaitGroup // Wait group for goroutines
}

// New creates a new frontend server.
// Attempts to use embedded FS first, falls back to local filesystem in dev.
// Prewarms index.html into memory cache with config and bounds injected.
// The cache is automatically refreshed when bounds or cartographoor data updates (event-driven).
func New(
	logger logrus.FieldLogger,
	configHandler *api.ConfigHandler,
	boundsProvider bounds.Provider,
	cartographoorProvider cartographoor.Provider,
) (*Frontend, error) {
	log := logger.WithField("component", "frontend")

	// Try embedded FS first
	embedFS, err := web.GetFS()
	devMode := false

	if err != nil || !web.Exists() {
		log.Info("Embedded FS not available, using local filesystem (dev mode)")

		devMode = true
		embedFS = os.DirFS("web/frontend")
	} else {
		log.Info("Using embedded filesystem")
	}

	// Fetch initial data
	ctx := context.Background()
	configData := configHandler.GetConfigData(ctx)
	boundsData := buildBoundsData(ctx, boundsProvider)

	// Create index cache and prewarm
	indexCache := &IndexCache{}
	if err := indexCache.Prewarm(log, embedFS, configData, boundsData); err != nil {
		return nil, fmt.Errorf("failed to prewarm index cache: %w", err)
	}

	return &Frontend{
		fs:                    embedFS,
		indexCache:            indexCache,
		configHandler:         configHandler,
		boundsProvider:        boundsProvider,
		cartographoorProvider: cartographoorProvider,
		logger:                log,
		devMode:               devMode,
		done:                  make(chan struct{}),
	}, nil
}

// Start starts the frontend server and background cache refresh listener.
func (f *Frontend) Start(ctx context.Context) error {
	f.logger.Info("Starting frontend cache refresh listener")

	// Start background refresh loop that listens for bounds update notifications
	f.wg.Add(1)

	go f.refreshLoop(ctx)

	return nil
}

// Stop stops the background cache refresh listener.
func (f *Frontend) Stop() error {
	f.logger.Info("Stopping frontend cache refresh listener")

	close(f.done)
	f.wg.Wait()

	return nil
}

// ServeHTTP handles frontend requests.
func (f *Frontend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean path and remove leading slash
	cleanPath := path.Clean(r.URL.Path)
	if cleanPath == "/" {
		cleanPath = "index.html"
	} else {
		cleanPath = strings.TrimPrefix(cleanPath, "/")
	}

	// Special handling for index.html (root or explicit)
	if cleanPath == "index.html" || cleanPath == "" {
		f.serveIndex(w, r)

		return
	}

	// Try to serve static file
	file, err := f.fs.Open(cleanPath)
	if err != nil {
		// File not found - fall back to index.html for SPA routing
		f.logger.WithFields(logrus.Fields{
			"path":    r.URL.Path,
			"cleaned": cleanPath,
		}).Debug("File not found, serving index.html for SPA routing")

		f.serveIndex(w, r)

		return
	}

	defer file.Close()

	// Get file info for size and modtime
	stat, err := file.Stat()
	if err != nil {
		f.logger.WithError(err).Error("Failed to stat file")

		http.Error(w, "Internal Server Error", http.StatusInternalServerError)

		return
	}

	// Serve file with appropriate cache headers
	f.setCacheHeaders(w, cleanPath)

	// Serve the file (ReadSeeker interface required for http.ServeContent)
	readSeeker, ok := file.(io.ReadSeeker)
	if !ok {
		f.logger.WithField("path", cleanPath).Error("File does not implement io.ReadSeeker")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)

		return
	}

	http.ServeContent(w, r, cleanPath, stat.ModTime(), readSeeker)
}

// serveIndex serves the cached index.html with injected config.
func (f *Frontend) serveIndex(w http.ResponseWriter, r *http.Request) {
	html := f.indexCache.GetInjected()

	// Set no-cache headers for index.html
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(html); err != nil {
		f.logger.WithError(err).Error("Failed to write index.html response")
	}
}

// setCacheHeaders sets appropriate cache headers based on file type.
func (f *Frontend) setCacheHeaders(w http.ResponseWriter, filePath string) {
	// Determine content type
	ext := path.Ext(filePath)
	contentType := "application/octet-stream"

	switch ext {
	case ".html":
		contentType = "text/html; charset=utf-8"
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".js":
		contentType = "application/javascript; charset=utf-8"
	case ".json":
		contentType = "application/json; charset=utf-8"
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".svg":
		contentType = "image/svg+xml"
	case ".woff":
		contentType = "font/woff"
	case ".woff2":
		contentType = "font/woff2"
	case ".ttf":
		contentType = "font/ttf"
	case ".eot":
		contentType = "application/vnd.ms-fontobject"
	case ".ico":
		contentType = "image/x-icon"
	}

	w.Header().Set("Content-Type", contentType)

	// Static assets get long cache (1 year)
	// index.html gets no-cache (handled in serveIndex)
	if !strings.HasSuffix(filePath, "index.html") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
}

// refreshLoop listens for bounds and cartographoor update notifications and refreshes the cached index.html.
// This ensures the frontend cache stays in sync with data updates (event-driven).
func (f *Frontend) refreshLoop(ctx context.Context) {
	defer f.wg.Done()

	// Get notification channels from providers
	var boundsNotifyChan <-chan struct{}
	if f.boundsProvider != nil {
		boundsNotifyChan = f.boundsProvider.NotifyChannel()
	}

	var cartographoorNotifyChan <-chan struct{}
	if f.cartographoorProvider != nil {
		cartographoorNotifyChan = f.cartographoorProvider.NotifyChannel()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-f.done:
			return
		case <-boundsNotifyChan:
			// Bounds data has been updated, refresh the cache
			f.logger.Debug("Bounds updated, refreshing frontend cache")

			f.refreshCache(ctx)
		case <-cartographoorNotifyChan:
			// Cartographoor data has been updated, refresh the cache
			f.logger.Debug("Cartographoor updated, refreshing frontend cache")

			f.refreshCache(ctx)
		}
	}
}

// refreshCache fetches fresh config and bounds data and updates the index cache.
func (f *Frontend) refreshCache(ctx context.Context) {
	f.logger.Debug("Refreshing frontend cache with latest config and bounds data")

	// Fetch fresh data
	configData := f.configHandler.GetConfigData(ctx)
	boundsData := buildBoundsData(ctx, f.boundsProvider)

	// Update cache
	if err := f.indexCache.Update(configData, boundsData); err != nil {
		f.logger.WithError(err).Error("Failed to update frontend cache")

		return
	}

	f.logger.Debug("Frontend cache refreshed successfully")
}

// buildBoundsData fetches all bounds and returns them in the format expected by the frontend.
func buildBoundsData(ctx context.Context, boundsProvider bounds.Provider) map[string]map[string]bounds.TableBounds {
	boundsData := make(map[string]map[string]bounds.TableBounds)

	if boundsProvider != nil {
		allBounds := boundsProvider.GetAllBounds(ctx)
		for network, data := range allBounds {
			if data != nil {
				boundsData[network] = data.Tables
			}
		}
	}

	return boundsData
}
