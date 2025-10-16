package frontend

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/web"
)

// Frontend serves static frontend files with caching and config injection.
type Frontend struct {
	fs         fs.FS       // Embedded or local filesystem
	indexCache *IndexCache // Prewarmed index.html cache
	configData interface{} // Config data for injection
	logger     logrus.FieldLogger
	devMode    bool // True if using local filesystem
}

// New creates a new frontend server.
// Attempts to use embedded FS first, falls back to local filesystem in dev.
// Prewarms index.html into memory cache with config injected.
func New(configData interface{}, logger logrus.FieldLogger) (*Frontend, error) {
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

	// Create index cache and prewarm
	indexCache := &IndexCache{}
	if err := indexCache.Prewarm(embedFS, configData, log); err != nil {
		return nil, fmt.Errorf("failed to prewarm index cache: %w", err)
	}

	return &Frontend{
		fs:         embedFS,
		indexCache: indexCache,
		configData: configData,
		logger:     log,
		devMode:    devMode,
	}, nil
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
