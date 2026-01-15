//nolint:tagliatelle // superior snake-case yo.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/sirupsen/logrus"
)

// Verify interface compliance at compile time.
var _ http.Handler = (*ConfigHandler)(nil)

// ConfigResponse is the JSON response for /api/v1/config.
type ConfigResponse struct {
	Networks []NetworkInfo `json:"networks"`
	Features []Feature     `json:"features"`
}

// NetworkInfo represents network metadata.
type NetworkInfo struct {
	Name         string              `json:"name"`         // "mainnet", "sepolia", etc.
	DisplayName  string              `json:"display_name"` // "Mainnet", "Sepolia", etc.
	ChainID      int64               `json:"chain_id"`
	GenesisTime  int64               `json:"genesis_time"`
	GenesisDelay int64               `json:"genesis_delay"` // Genesis delay in seconds
	Forks        Forks               `json:"forks"`
	ServiceUrls  map[string]string   `json:"service_urls"`            // Map of service name to URL
	BlobSchedule []BlobScheduleEntry `json:"blob_schedule,omitempty"` // Optional blob schedule
}

// Forks contains fork information for a network (API response format with snake_case).
type Forks struct {
	Consensus map[string]ConsensusFork `json:"consensus"`           // Map of fork name to fork info
	Execution map[string]ExecutionFork `json:"execution,omitempty"` // Map of execution fork name to fork info
}

// ConsensusFork represents a single consensus fork with epoch and minimum client versions (API response format with snake_case).
type ConsensusFork struct {
	Epoch             int64             `json:"epoch"`
	Timestamp         int64             `json:"timestamp,omitempty"`
	MinClientVersions map[string]string `json:"min_client_versions,omitempty"` // Map of client name to version
}

// ExecutionFork represents an execution layer fork with block number and timestamp.
type ExecutionFork struct {
	Block     int64 `json:"block"`
	Timestamp int64 `json:"timestamp"`
}

// BlobScheduleEntry represents a single entry in the blob schedule defining
// the maximum number of blobs per block starting at a specific epoch.
type BlobScheduleEntry struct {
	Epoch            int64 `json:"epoch"`
	Timestamp        int64 `json:"timestamp,omitempty"`
	MaxBlobsPerBlock int64 `json:"max_blobs_per_block"`
}

// Feature represents feature configuration.
// Features are enabled by default for all networks unless explicitly disabled.
type Feature struct {
	Path             string   `json:"path"`
	DisabledNetworks []string `json:"disabled_networks"`
}

// ConfigHandler handles /api/v1/config requests.
type ConfigHandler struct {
	config   *config.Config
	provider cartographoor.Provider
	logger   logrus.FieldLogger
}

// NewConfigHandler creates a new config API handler.
func NewConfigHandler(
	logger logrus.FieldLogger,
	cfg *config.Config,
	provider cartographoor.Provider,
) *ConfigHandler {
	return &ConfigHandler{
		config:   cfg,
		provider: provider,
		logger:   logger.WithField("handler", "config"),
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
	response := h.GetConfigData(r.Context())

	// Set headers.
	w.Header().Set("Content-Type", "application/json")

	// Encode response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)

		return
	}
}

// GetConfigData returns the config data structure for both API and frontend use.
// This ensures both endpoints use the same logic and return consistent data.
func (h *ConfigHandler) GetConfigData(ctx context.Context) ConfigResponse {
	return ConfigResponse{
		Networks: h.buildNetworks(ctx),
		Features: h.buildFeatures(ctx),
	}
}

// buildNetworks converts config.NetworkConfig to NetworkInfo slice.
// Uses merged NetworkConfig which already has cartographoor + config.yaml overlay applied.
// Only returns enabled networks.
func (h *ConfigHandler) buildNetworks(ctx context.Context) []NetworkInfo {
	// Build merged network list (cartographoor base + config.yaml overrides)
	mergedNetworks := config.BuildMergedNetworkList(ctx, h.logger, h.config, h.provider)

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
			serviceUrls                        map[string]string
			blobSchedule                       []BlobScheduleEntry
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

		// Get forks, serviceUrls, and blobSchedule from cartographoor if available
		if h.provider != nil {
			if cartNet, exists := h.provider.GetNetwork(ctx, net.Name); exists {
				// Transform cartographoor.Forks to API Forks
				forks = transformForks(cartNet.Forks)
				// Copy serviceUrls from cartographoor
				serviceUrls = cartNet.ServiceUrls
				// Transform blobSchedule from cartographoor
				blobSchedule = transformBlobSchedule(cartNet.BlobSchedule)
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
			ServiceUrls:  serviceUrls,
			BlobSchedule: blobSchedule,
		})
	}

	// Sort networks alphabetically by name for deterministic ordering
	sort.Slice(networks, func(i, j int) bool {
		return networks[i].Name < networks[j].Name
	})

	return networks
}

// buildFeatures converts config features slice to API response array.
func (h *ConfigHandler) buildFeatures(_ context.Context) []Feature {
	features := make([]Feature, 0, len(h.config.Features))

	for _, feature := range h.config.Features {
		// Copy disabled_networks slice to avoid sharing underlying array
		disabledNetworks := make([]string, len(feature.DisabledNetworks))
		copy(disabledNetworks, feature.DisabledNetworks)

		features = append(features, Feature{
			Path:             feature.Path,
			DisabledNetworks: disabledNetworks,
		})
	}

	// Sort features alphabetically by path for deterministic ordering
	sort.Slice(features, func(i, j int) bool {
		return features[i].Path < features[j].Path
	})

	return features
}

// transformForks converts cartographoor.Forks to API Forks format (for snake_case output).
func transformForks(cartForks cartographoor.Forks) Forks {
	consensus := make(map[string]ConsensusFork, len(cartForks.Consensus))
	for forkName, cartFork := range cartForks.Consensus {
		consensus[forkName] = ConsensusFork{
			Epoch:             cartFork.Epoch,
			Timestamp:         cartFork.Timestamp,
			MinClientVersions: cartFork.MinClientVersions,
		}
	}

	var execution map[string]ExecutionFork
	if len(cartForks.Execution) > 0 {
		execution = make(map[string]ExecutionFork, len(cartForks.Execution))
		for forkName, cartFork := range cartForks.Execution {
			execution[forkName] = ExecutionFork{
				Block:     cartFork.Block,
				Timestamp: cartFork.Timestamp,
			}
		}
	}

	return Forks{
		Consensus: consensus,
		Execution: execution,
	}
}

// transformBlobSchedule converts cartographoor.BlobScheduleEntry to API BlobScheduleEntry format (for snake_case output).
func transformBlobSchedule(cartSchedule []cartographoor.BlobScheduleEntry) []BlobScheduleEntry {
	if cartSchedule == nil {
		return nil
	}

	schedule := make([]BlobScheduleEntry, len(cartSchedule))
	for i, entry := range cartSchedule {
		schedule[i] = BlobScheduleEntry{
			Epoch:            entry.Epoch,
			Timestamp:        entry.Timestamp,
			MaxBlobsPerBlock: entry.MaxBlobsPerBlock,
		}
	}

	return schedule
}
