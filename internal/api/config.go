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
	Experiments []Experiment           `json:"experiments"`
	Bounds      map[string]TableBounds `json:"bounds"`
}

// NetworkInfo represents network metadata.
type NetworkInfo struct {
	Name         string `json:"name"`         // "mainnet", "sepolia", etc.
	DisplayName  string `json:"display_name"` // "Mainnet", "Sepolia", etc.
	ChainID      int64  `json:"chain_id"`
	GenesisTime  int64  `json:"genesis_time"`
	GenesisDelay int64  `json:"genesis_delay"` // Genesis delay in seconds
	Forks        Forks  `json:"forks"`
}

// Forks contains fork information for a network (API response format with snake_case).
type Forks struct {
	Consensus map[string]Fork `json:"consensus"` // Map of fork name to fork info
}

// Fork represents a single fork with epoch and minimum client versions (API response format with snake_case).
type Fork struct {
	Epoch             int64             `json:"epoch"`
	MinClientVersions map[string]string `json:"min_client_versions"` // Map of client name to version
}

// Experiment represents experiment configuration.
type Experiment struct {
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Networks []string `json:"networks"` // empty = all networks
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

	// Get config data
	response := h.GetConfigData()

	// Set headers.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Encode response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)

		return
	}
}

// GetConfigData returns the config data structure for both API and frontend use.
// This ensures both endpoints use the same logic and return consistent data.
func (h *ConfigHandler) GetConfigData() ConfigResponse {
	return ConfigResponse{
		Networks:    h.buildNetworks(),
		Experiments: h.buildExperiments(),
		Bounds:      make(map[string]TableBounds),
	}
}

// buildNetworks converts config.NetworkConfig to NetworkInfo slice.
// Uses merged NetworkConfig which already has cartographoor + config.yaml overlay applied.
// Only returns enabled networks.
func (h *ConfigHandler) buildNetworks() []NetworkInfo {
	// Build merged network list (cartographoor base + config.yaml overrides)
	mergedNetworks := config.BuildMergedNetworkList(h.config, h.provider)

	// Convert to NetworkInfo slice (only enabled networks)
	networks := make([]NetworkInfo, 0, len(mergedNetworks))
	for _, net := range mergedNetworks {
		// Skip disabled networks
		if net.Enabled != nil && !*net.Enabled {
			continue
		}

		// Use merged NetworkConfig values (already has cartographoor + config.yaml)
		displayName := net.DisplayName

		var (
			chainID, genesisTime, genesisDelay int64
			forks                              Forks
		)

		if net.ChainID != nil {
			chainID = *net.ChainID
		}

		if net.GenesisTime != nil {
			genesisTime = *net.GenesisTime
		}

		if net.GenesisDelay != nil {
			genesisDelay = *net.GenesisDelay
		}

		// Get forks from cartographoor if available (forks not in config.yaml yet)
		if h.provider != nil {
			if cartNet, exists := h.provider.GetNetwork(net.Name); exists {
				// Transform cartographoor.Forks to API Forks
				forks = transformForks(cartNet.Forks)
			}
		}

		// Capitalize first letter if no display name
		if displayName == "" {
			if len(net.Name) > 0 {
				displayName = strings.ToUpper(net.Name[:1]) + net.Name[1:]
			} else {
				displayName = net.Name
			}
		}

		networks = append(networks, NetworkInfo{
			Name:         net.Name,
			DisplayName:  displayName,
			ChainID:      chainID,
			GenesisTime:  genesisTime,
			GenesisDelay: genesisDelay,
			Forks:        forks,
		})
	}

	// Sort networks alphabetically by name for deterministic ordering
	sort.Slice(networks, func(i, j int) bool {
		return networks[i].Name < networks[j].Name
	})

	return networks
}

// buildExperiments converts config experiments map to API response array.
func (h *ConfigHandler) buildExperiments() []Experiment {
	experiments := make([]Experiment, 0, len(h.config.Experiments))

	for name, settings := range h.config.Experiments {
		// Copy networks slice to avoid sharing underlying array
		networks := make([]string, len(settings.Networks))
		copy(networks, settings.Networks)

		experiments = append(experiments, Experiment{
			Name:     name,
			Enabled:  settings.Enabled,
			Networks: networks,
		})
	}

	// Sort experiments alphabetically by name for deterministic ordering
	sort.Slice(experiments, func(i, j int) bool {
		return experiments[i].Name < experiments[j].Name
	})

	return experiments
}

// transformForks converts cartographoor.Forks to API Forks format (for snake_case output).
func transformForks(cartForks cartographoor.Forks) Forks {
	consensus := make(map[string]Fork, len(cartForks.Consensus))
	for forkName, cartFork := range cartForks.Consensus {
		consensus[forkName] = Fork{
			Epoch:             cartFork.Epoch,
			MinClientVersions: cartFork.MinClientVersions,
		}
	}

	return Forks{
		Consensus: consensus,
	}
}
