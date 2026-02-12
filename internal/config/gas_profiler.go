//nolint:tagliatelle // superior snake-case yo.
package config

import (
	"fmt"
	"net/http"
	"time"
)

// GasProfilerConfig holds gas profiler simulation service configuration.
type GasProfilerConfig struct {
	Enabled        bool                  `yaml:"enabled"`
	Endpoints      []GasProfilerEndpoint `yaml:"endpoints"`       // List of Erigon RPC endpoints
	RequestTimeout time.Duration         `yaml:"request_timeout"` // HTTP request timeout for RPC calls
	HealthInterval time.Duration         `yaml:"health_interval"` // Interval between endpoint health checks (default 30s)
}

// GasProfilerEndpoint defines a single Erigon RPC endpoint.
type GasProfilerEndpoint struct {
	Name    string `yaml:"name"`    // Friendly name (e.g., "mainnet-1", "mainnet-2")
	Network string `yaml:"network"` // Network identifier to match in requests
	URL     string `yaml:"url"`     // Erigon JSON-RPC URL
}

// Validate validates the gas profiler configuration.
func (c *GasProfilerConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if len(c.Endpoints) == 0 {
		return fmt.Errorf("at least one endpoint is required when enabled")
	}

	// Set default timeout
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 60 * time.Second
	}

	if c.RequestTimeout < 5*time.Second {
		return fmt.Errorf("request_timeout must be at least 5 seconds, got %v", c.RequestTimeout)
	}

	// Set default health interval
	if c.HealthInterval == 0 {
		c.HealthInterval = 30 * time.Second
	}

	if c.HealthInterval < 10*time.Second {
		return fmt.Errorf("health_interval must be at least 10 seconds, got %v", c.HealthInterval)
	}

	// Validate each endpoint and check for duplicate names
	names := make(map[string]bool)

	for i, ep := range c.Endpoints {
		if ep.Name == "" {
			return fmt.Errorf("endpoints[%d].name is required", i)
		}

		if ep.Network == "" {
			return fmt.Errorf("endpoints[%d].network is required", i)
		}

		if ep.URL == "" {
			return fmt.Errorf("endpoints[%d].url is required", i)
		}

		if names[ep.Name] {
			return fmt.Errorf("duplicate endpoint name: %s", ep.Name)
		}

		names[ep.Name] = true
	}

	return nil
}

// GetEndpointsForNetwork returns all endpoints for a given network.
func (c *GasProfilerConfig) GetEndpointsForNetwork(network string) []*GasProfilerEndpoint {
	var endpoints []*GasProfilerEndpoint

	for i := range c.Endpoints {
		if c.Endpoints[i].Network == network {
			endpoints = append(endpoints, &c.Endpoints[i])
		}
	}

	return endpoints
}

// GetNetworks returns a list of unique networks configured.
func (c *GasProfilerConfig) GetNetworks() []string {
	seen := make(map[string]bool)

	var networks []string

	for _, ep := range c.Endpoints {
		if !seen[ep.Network] {
			seen[ep.Network] = true
			networks = append(networks, ep.Network)
		}
	}

	return networks
}

// HTTPClient returns a configured HTTP client for RPC requests.
func (c *GasProfilerConfig) HTTPClient() *http.Client {
	return &http.Client{
		Timeout: c.RequestTimeout,
	}
}
