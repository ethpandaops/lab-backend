package cartographoor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
)

// Service is a stateless fetcher that retrieves network data from Cartographoor API.
type Service struct {
	config     *Config
	logger     logrus.FieldLogger
	httpClient *http.Client
}

// New creates a new cartographoor service.
func New(cfg *Config, logger logrus.FieldLogger) (*Service, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		config:     cfg,
		logger:     logger.WithField("component", "cartographoor"),
		httpClient: cfg.HTTPClient(),
	}, nil
}

// FetchNetworks fetches network data from Cartographoor API and returns it.
func (s *Service) FetchNetworks(
	ctx context.Context,
) (map[string]*Network, error) {
	s.logger.Debug("Fetching cartographoor data")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.SourceURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var rawResponse CartographoorResponse
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	networks := s.processNetworks(&rawResponse)

	s.logger.WithFields(logrus.Fields{
		"total_networks":  len(networks),
		"active_networks": s.countActive(networks),
	}).Debug("Fetched cartographoor data")

	return networks, nil
}

// processNetworks converts raw cartographoor data to Network structs.
func (s *Service) processNetworks(
	response *CartographoorResponse,
) map[string]*Network {
	networks := make(map[string]*Network, len(response.Networks))

	for networkName, rawNet := range response.Networks {
		// Get metadata if available
		var displayName, description string
		if meta, exists := response.NetworkMetadata[networkName]; exists {
			displayName = meta.DisplayName
			description = meta.Description
		}

		// Fallback display name
		if displayName == "" {
			displayName = s.formatDisplayName(networkName)
		}

		// Construct CBT API URL from network name
		targetURL := s.constructTargetURL(networkName)

		networks[networkName] = &Network{
			Name:         networkName,
			DisplayName:  displayName,
			Description:  description,
			Status:       rawNet.Status,
			ChainID:      rawNet.ChainID,
			GenesisTime:  rawNet.GenesisConfig.GenesisTime,
			GenesisDelay: rawNet.GenesisConfig.GenesisDelay,
			Forks:        rawNet.Forks,
			TargetURL:    targetURL,
			LastUpdated:  rawNet.LastUpdated,
		}
	}

	return networks
}

// constructTargetURL builds the CBT API URL for a network.
func (s *Service) constructTargetURL(networkName string) string {
	// Construct from network name using standard pattern
	// Network names in cartographoor JSON are already clean (e.g., "mainnet", "fusaka-devnet-3")
	return fmt.Sprintf("https://cbt-api-%s.analytics.production.platform.ethpandaops.io/api/v1", networkName)
}

// formatDisplayName creates a display name from network name.
func (s *Service) formatDisplayName(name string) string {
	// Capitalize first letter of network name
	if len(name) > 0 {
		return strings.ToUpper(name[:1]) + name[1:]
	}

	return name
}

// countActive counts active networks in a map.
func (s *Service) countActive(networks map[string]*Network) int {
	count := 0

	for _, net := range networks {
		if net.Status == NetworkStatusActive {
			count++
		}
	}

	return count
}
