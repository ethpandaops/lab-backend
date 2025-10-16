//nolint:tagliatelle // superior snake-case yo.
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
)

// ConfigResponse is the JSON response for /api/v1/config.
type ConfigResponse struct {
	Networks    []NetworkInfo          `json:"networks"`
	Experiments map[string]Experiment  `json:"experiments"`
	Bounds      map[string]TableBounds `json:"bounds"`
}

// NetworkInfo represents network metadata.
type NetworkInfo struct {
	Name        string `json:"name"`         // "mainnet", "sepolia", etc.
	DisplayName string `json:"display_name"` // "Mainnet", "Sepolia", etc.
	Enabled     bool   `json:"enabled"`
	Status      string `json:"status"` // "active", "inactive", etc.
}

// Experiment represents experiment configuration.
type Experiment struct {
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Networks    []string `json:"networks"`    // empty = all networks
	Description string   `json:"description"` // optional
}

// TableBounds represents CBT table min/max bounds.
type TableBounds struct {
	Min int64 `json:"min"` // minimum slot
	Max int64 `json:"max"` // maximum slot
}

// ConfigHandler handles /api/v1/config requests.
type ConfigHandler struct {
	config   *config.Config
	provider cartographoor.Provider
}

// NewConfigHandler creates a new config API handler.
func NewConfigHandler(cfg *config.Config, provider cartographoor.Provider) *ConfigHandler {
	return &ConfigHandler{
		config:   cfg,
		provider: provider,
	}
}

// ServeHTTP implements http.Handler interface.
func (h *ConfigHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	// Build ConfigResponse.
	response := ConfigResponse{
		Networks:    h.buildNetworks(),
		Experiments: h.buildExperiments(),
		Bounds:      make(map[string]TableBounds), // Empty map for Phase 1
	}

	// Set headers.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Encode response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)

		return
	}
}

// buildNetworks converts config.NetworkConfig to NetworkInfo slice.
func (h *ConfigHandler) buildNetworks() []NetworkInfo {
	networks := make([]NetworkInfo, 0)

	// Add cartographoor networks if available
	if h.provider != nil {
		cartographoorNets := h.provider.GetActiveNetworks()
		for _, net := range cartographoorNets {
			networks = append(networks, NetworkInfo{
				Name:        net.Name,
				DisplayName: net.DisplayName,
				Enabled:     true,
				Status:      net.Status,
			})
		}
	}

	// Add static config networks (avoid duplicates)
	seenNetworks := make(map[string]bool)
	for _, ni := range networks {
		seenNetworks[ni.Name] = true
	}

	for _, nc := range h.config.Networks {
		if !seenNetworks[nc.Name] {
			networks = append(networks, buildNetworkInfo(nc))
		}
	}

	return networks
}

// buildNetworkInfo converts config.NetworkConfig to NetworkInfo.
func buildNetworkInfo(nc config.NetworkConfig) NetworkInfo {
	// Capitalize first letter for display name
	displayName := nc.Name
	if len(displayName) > 0 {
		displayName = strings.ToUpper(displayName[:1]) + displayName[1:]
	}

	// Set status based on enabled flag
	status := "inactive"
	if nc.Enabled {
		status = "active"
	}

	return NetworkInfo{
		Name:        nc.Name,
		DisplayName: displayName,
		Enabled:     nc.Enabled,
		Status:      status,
	}
}

// buildExperiments converts config.ExperimentConfig to map.
func (h *ConfigHandler) buildExperiments() map[string]Experiment {
	experiments := make(map[string]Experiment, len(h.config.Experiments.Experiments))

	for name, settings := range h.config.Experiments.Experiments {
		// Copy networks slice to avoid sharing underlying array
		networks := make([]string, len(settings.Networks))
		copy(networks, settings.Networks)

		experiments[name] = Experiment{
			Name:        name,
			Enabled:     settings.Enabled,
			Networks:    networks,
			Description: "", // Stub for Phase 1, will be populated from config in Phase 2
		}
	}

	return experiments
}
