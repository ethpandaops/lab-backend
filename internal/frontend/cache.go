package frontend

import (
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// RouteIndexCache caches index.html variations for different routes.
// Each route gets its own cached version with route-specific head tags injected.
type RouteIndexCache struct {
	mu       sync.RWMutex
	original []byte            // Original index.html
	routes   map[string][]byte // Cached HTML per route
	headData HeadData          // Head data from head.json
}

// PrewarmRoutes loads index.html and head.json, then generates cached versions for all routes.
func (ric *RouteIndexCache) PrewarmRoutes(
	logger logrus.FieldLogger,
	filesystem fs.FS,
	configData interface{},
	boundsData interface{},
) error {
	// Open and read index.html
	file, err := filesystem.Open(indexFileName)
	if err != nil {
		return fmt.Errorf("failed to open index.html: %w", err)
	}
	defer file.Close()

	// Read entire file into memory
	original, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read index.html: %w", err)
	}

	logger.WithField("size", len(original)).Info("Loaded index.html into memory")

	// Load head.json data
	headData, err := LoadHeadData(filesystem)
	if err != nil {
		return fmt.Errorf("failed to parse head.json: %w", err)
	}

	// Check if head.json was loaded or is empty
	if len(headData) == 0 {
		logger.Warn("head.json not found or empty - using default configuration with config/bounds only")
	} else {
		logger.WithField("routes", len(headData)).Info("Loaded head.json with route configurations")
	}

	// Initialize cache
	ric.mu.Lock()
	defer ric.mu.Unlock()

	ric.original = original
	ric.headData = headData
	ric.routes = make(map[string][]byte)

	// Generate cached version for each route
	var defaultHeadRaw string

	for route, routeHead := range headData {
		// Handle _default entry separately
		if route == "_default" {
			defaultHeadRaw = routeHead.Raw

			continue
		}

		// Inject config, bounds, and route-specific head
		injected, injectErr := InjectAll(original, configData, boundsData, routeHead.Raw)
		if injectErr != nil {
			logger.WithError(injectErr).WithField("route", route).Error("Failed to inject data for route")

			continue
		}

		ric.routes[route] = injected
		logger.WithFields(logrus.Fields{
			"route": route,
			"size":  len(injected),
		}).Debug("Generated cached HTML for route")
	}

	// Create default version with _default head (if exists) or empty
	defaultInjected, err := InjectAll(original, configData, boundsData, defaultHeadRaw)
	if err != nil {
		return fmt.Errorf("failed to create default injected HTML: %w", err)
	}

	// Store the default version for routes not in head.json
	ric.routes["_default"] = defaultInjected

	logger.WithField("total_routes", len(ric.routes)).Info("Route cache prewarmed successfully")

	return nil
}

// GetForRoute returns the cached HTML for a specific route.
// Falls back to default if route not found.
func (ric *RouteIndexCache) GetForRoute(route string) []byte {
	ric.mu.RLock()
	defer ric.mu.RUnlock()

	// Strip query parameters and hash fragments
	if idx := strings.IndexAny(route, "?#"); idx != -1 {
		route = route[:idx]
	}

	// Normalize the route
	if route == "" || route == "/" || route == indexFileName {
		route = "/"
	}

	// Try to find exact match
	if html, ok := ric.routes[route]; ok {
		return html
	}

	// Return default version
	if defaultHTML, ok := ric.routes["_default"]; ok {
		return defaultHTML
	}

	// Fallback to original if no default (shouldn't happen)
	return ric.original
}

// Update refreshes all cached routes with new config and bounds data.
func (ric *RouteIndexCache) Update(
	configData interface{},
	boundsData interface{},
) error {
	ric.mu.Lock()
	defer ric.mu.Unlock()

	newRoutes := make(map[string][]byte)

	// Regenerate cached version for each route
	var defaultHeadRaw string

	for route, routeHead := range ric.headData {
		// Handle _default entry separately
		if route == "_default" {
			defaultHeadRaw = routeHead.Raw

			continue
		}

		// Inject config, bounds, and route-specific head
		injected, err := InjectAll(ric.original, configData, boundsData, routeHead.Raw)
		if err != nil {
			return fmt.Errorf("failed to inject data for route %s: %w", route, err)
		}

		newRoutes[route] = injected
	}

	defaultInjected, err := InjectAll(ric.original, configData, boundsData, defaultHeadRaw)
	if err != nil {
		return fmt.Errorf("failed to create default injected HTML: %w", err)
	}

	newRoutes["_default"] = defaultInjected

	// Atomically replace the routes map
	ric.routes = newRoutes

	return nil
}

// GetOriginal returns the cached original index.html.
func (ric *RouteIndexCache) GetOriginal() []byte {
	ric.mu.RLock()
	defer ric.mu.RUnlock()

	return ric.original
}
