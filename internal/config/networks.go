//nolint:tagliatelle // superior snake-case yo.
package config

import (
	"fmt"
	"net/url"
)

// NetworkConfig defines a single network's configuration.
type NetworkConfig struct {
	Name      string `yaml:"name"`       // "mainnet", "sepolia", etc.
	Enabled   bool   `yaml:"enabled"`    // Whether this network is active
	TargetURL string `yaml:"target_url"` // Backend CBT API URL
}

// ExperimentConfig contains experiment feature flags.
type ExperimentConfig struct {
	Experiments map[string]ExperimentSettings `yaml:"experiments"`
}

// ExperimentSettings defines settings for a single experiment.
type ExperimentSettings struct {
	Enabled  bool     `yaml:"enabled"`
	Networks []string `yaml:"networks"` // Empty = all networks
}

// Validate validates a network configuration.
func (n *NetworkConfig) Validate() error {
	if n.Name == "" {
		return fmt.Errorf("network name cannot be empty")
	}

	if n.TargetURL == "" {
		return fmt.Errorf("network %s: target_url cannot be empty", n.Name)
	}

	// Validate URL format
	parsedURL, err := url.Parse(n.TargetURL)
	if err != nil {
		return fmt.Errorf("network %s: invalid target_url: %w", n.Name, err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("network %s: target_url must use http or https scheme", n.Name)
	}

	return nil
}

// Validate validates experiment configuration.
func (ec *ExperimentConfig) Validate(validNetworks map[string]bool) error {
	for expName, settings := range ec.Experiments {
		// Validate that specified networks exist
		for _, networkName := range settings.Networks {
			if !validNetworks[networkName] {
				return fmt.Errorf("experiment %s references unknown network: %s", expName, networkName)
			}
		}
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
		if network.Enabled {
			enabled = append(enabled, network)
		}
	}

	return enabled
}
