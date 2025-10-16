//nolint:tagliatelle // superior snake-case yo.
package api

import (
	"encoding/json"
	"net/http"
	"sort"
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
// Uses same cartographoor-first, config-overlay approach as proxy.
// Only returns enabled networks.
func (h *ConfigHandler) buildNetworks() []NetworkInfo {
	// Build merged network list (cartographoor + config overlay)
	mergedNetworks := config.BuildMergedNetworkList(h.config, h.provider)

	// Convert to NetworkInfo slice (only enabled networks)
	networks := make([]NetworkInfo, 0, len(mergedNetworks))
	for _, net := range mergedNetworks {
		// Skip disabled networks
		if !net.Enabled {
			continue
		}

		// Get display name and status from cartographoor if available
		displayName := net.Name
		status := "active"

		if h.provider != nil {
			if cartNet, exists := h.provider.GetNetwork(net.Name); exists {
				displayName = cartNet.DisplayName
				status = cartNet.Status
			}
		}

		// Capitalize first letter if no cartographoor display name
		if displayName == net.Name && len(displayName) > 0 {
			displayName = strings.ToUpper(displayName[:1]) + displayName[1:]
		}

		networks = append(networks, NetworkInfo{
			Name:        net.Name,
			DisplayName: displayName,
			Enabled:     true, // All returned networks are enabled
			Status:      status,
		})
	}

	// Sort networks alphabetically by name for deterministic ordering
	sort.Slice(networks, func(i, j int) bool {
		return networks[i].Name < networks[j].Name
	})

	return networks
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
