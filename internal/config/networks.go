//nolint:tagliatelle // superior snake-case yo.
package config

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/sirupsen/logrus"
)

// NetworkConfig defines a single network's configuration.
// When used in config.yaml, all fields except Name are optional.
// Cartographoor values are used as defaults, config.yaml provides overrides.
type NetworkConfig struct {
	Name         string `yaml:"name"`                    // Required: "mainnet", "sepolia", etc.
	Enabled      *bool  `yaml:"enabled,omitempty"`       // Optional: Whether this network is active
	TargetURL    string `yaml:"target_url,omitempty"`    // Optional: Backend CBT API URL
	DisplayName  string `yaml:"display_name,omitempty"`  // Optional: Human-readable name
	ChainID      *int64 `yaml:"chain_id,omitempty"`      // Optional: Numeric chain ID
	GenesisTime  *int64 `yaml:"genesis_time,omitempty"`  // Optional: Unix timestamp
	GenesisDelay *int64 `yaml:"genesis_delay,omitempty"` // Optional: Genesis delay in seconds
}

// ExperimentSettings defines settings for a single experiment.
type ExperimentSettings struct {
	Enabled  bool     `yaml:"enabled"`
	Networks []string `yaml:"networks,omitempty"` // Empty/omitted = all networks
}

// Validate validates a network configuration.
func (n *NetworkConfig) Validate() error {
	if n.Name == "" {
		return fmt.Errorf("network name cannot be empty")
	}

	// Skip target_url validation for disabled networks
	// (they might be cartographoor overrides with only enabled: false)
	if n.Enabled != nil && !*n.Enabled {
		return nil
	}

	// If target_url is not set, it's expected to come from cartographoor
	if n.TargetURL == "" {
		return nil
	}

	// Validate URL format if provided
	parsedURL, err := url.Parse(n.TargetURL)
	if err != nil {
		return fmt.Errorf("network %s: invalid target_url: %w", n.Name, err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("network %s: target_url must use http or https scheme", n.Name)
	}

	return nil
}

// GetNetworkByName looks up a network by name.
func (c *Config) GetNetworkByName(name string) (*NetworkConfig, error) {
	for i := range c.Networks {
		if c.Networks[i].Name == name {
			return &c.Networks[i], nil
		}
	}

	return nil, fmt.Errorf("network not found: %s", name)
}

// GetEnabledNetworks returns only enabled networks.
func (c *Config) GetEnabledNetworks() []NetworkConfig {
	enabled := make([]NetworkConfig, 0, len(c.Networks))
	for _, network := range c.Networks {
		// If Enabled is not set (nil), default to true
		// If Enabled is set, use its value
		if network.Enabled == nil || *network.Enabled {
			enabled = append(enabled, network)
		}
	}

	return enabled
}

// BuildMergedNetworkList creates merged network list: cartographoor base + config.yaml overlay.
// Priority: cartographoor is the source of truth, config.yaml provides overrides.
// Cartographoor provider already filters for healthy networks, so this just merges data.
func BuildMergedNetworkList(
	ctx context.Context,
	logger logrus.FieldLogger,
	cfg *Config,
	provider cartographoor.Provider,
) map[string]NetworkConfig {
	networks := make(map[string]NetworkConfig)

	// Step 1: Start with cartographoor networks (if available)
	// Store ALL metadata from cartographoor as the base layer
	if provider != nil {
		for name, net := range provider.GetActiveNetworks(ctx) {
			enabled := true
			networks[name] = NetworkConfig{
				Name:         net.Name,
				Enabled:      &enabled,
				TargetURL:    net.TargetURL,
				DisplayName:  net.DisplayName,
				ChainID:      &net.ChainID,
				GenesisTime:  &net.GenesisTime,
				GenesisDelay: &net.GenesisDelay,
			}
		}
	}

	// Step 2: Apply config.yaml overrides and additions
	for _, configNet := range cfg.Networks {
		if existing, exists := networks[configNet.Name]; exists {
			// Override cartographoor network with config.yaml values
			// Only override fields that are explicitly set in config.yaml.
			if configNet.Enabled != nil {
				existing.Enabled = configNet.Enabled
			}

			if configNet.TargetURL != "" {
				existing.TargetURL = configNet.TargetURL
			}

			if configNet.DisplayName != "" {
				existing.DisplayName = configNet.DisplayName
			}

			if configNet.ChainID != nil {
				existing.ChainID = configNet.ChainID
			}

			if configNet.GenesisTime != nil {
				existing.GenesisTime = configNet.GenesisTime
			}

			if configNet.GenesisDelay != nil {
				existing.GenesisDelay = configNet.GenesisDelay
			}

			networks[configNet.Name] = existing
		} else {
			// Add standalone network (not in cartographoor)
			// For standalone networks, if Enabled is not set, default to true.
			if configNet.Enabled == nil {
				enabled := true
				configNet.Enabled = &enabled
			}

			networks[configNet.Name] = configNet
		}
	}

	// Step 3: Filter out disabled networks
	// Note: Cartographoor provider already filtered for healthy networks
	enabledNetworks := make(map[string]NetworkConfig)

	for name, network := range networks {
		// Skip disabled networks
		if network.Enabled != nil && !*network.Enabled {
			logger.WithField("network", name).Debug("Network disabled in config, skipping")

			continue
		}

		enabledNetworks[name] = network
	}

	return enabledNetworks
}
