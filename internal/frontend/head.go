package frontend

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
)

const (
	// indexFileName is the name of the index HTML file.
	indexFileName = "index.html"
)

// HeadData represents the structure of head.json.
type HeadData map[string]RouteHead

// RouteHead represents the head metadata for a specific route.
type RouteHead struct {
	Meta    []interface{} `json:"meta,omitempty"`
	Links   []interface{} `json:"links,omitempty"`
	Styles  []interface{} `json:"styles,omitempty"`
	Scripts []interface{} `json:"scripts,omitempty"`
	Raw     string        `json:"raw"` // Pre-rendered HTML to inject
}

// LoadHeadData reads and parses head.json from the filesystem.
// Returns empty HeadData if file doesn't exist.
func LoadHeadData(filesystem fs.FS) (HeadData, error) {
	// Try to open head.json
	file, err := filesystem.Open("head.json")
	if err != nil {
		// If file doesn't exist, return empty HeadData (not an error)
		return make(HeadData), nil
	}
	defer file.Close()

	// Read and parse the JSON
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read head.json: %w", err)
	}

	var headData HeadData
	if err := json.Unmarshal(data, &headData); err != nil {
		return nil, fmt.Errorf("failed to parse head.json: %w", err)
	}

	return headData, nil
}

// GetRouteHead returns the head data for a specific route.
// Falls back to "_default" if the specific route is not found.
func (h HeadData) GetRouteHead(route string) *RouteHead {
	// Normalize the route
	if route == "" || route == indexFileName {
		route = "/"
	}

	// Try to find exact match
	if routeHead, ok := h[route]; ok {
		return &routeHead
	}

	// Fall back to _default if available
	if defaultHead, ok := h["_default"]; ok {
		return &defaultHead
	}

	// Return nil if no match found
	return nil
}

// GetAllRoutes returns all routes that have head data.
func (h HeadData) GetAllRoutes() []string {
	routes := make([]string, 0, len(h))
	for route := range h {
		routes = append(routes, route)
	}

	return routes
}
