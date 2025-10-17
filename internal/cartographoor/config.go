//nolint:tagliatelle // superior snake-case yo.
package cartographoor

import (
	"fmt"
	"net/http"
	"time"
)

const DefaultCartographoorURL = "https://ethpandaops-platform-production-cartographoor.ams3.cdn.digitaloceanspaces.com/networks.json"

// Config holds cartographoor service configuration.
type Config struct {
	SourceURL       string        `yaml:"source_url"`       // Cartographoor JSON URL
	RefreshInterval time.Duration `yaml:"refresh_interval"` // How often to refresh
	RequestTimeout  time.Duration `yaml:"request_timeout"`  // HTTP request timeout
	NetworksTTL     time.Duration `yaml:"networks_ttl"`     // Redis TTL for networks data (0 = no expiration)
}

// Validate validates and sets defaults for Config.
func (c *Config) Validate() error {
	// Set defaults
	if c.SourceURL == "" {
		c.SourceURL = DefaultCartographoorURL
	}

	if c.RefreshInterval == 0 {
		c.RefreshInterval = 5 * time.Minute
	}

	if c.RequestTimeout == 0 {
		c.RequestTimeout = 30 * time.Second
	}

	// Validate ranges
	if c.RefreshInterval < 1*time.Minute {
		return fmt.Errorf("refresh_interval must be at least 1 minute, got %v", c.RefreshInterval)
	}

	if c.RequestTimeout < 1*time.Second {
		return fmt.Errorf("request_timeout must be at least 1 second, got %v", c.RequestTimeout)
	}

	return nil
}

// HTTPClient creates an HTTP client with configured timeout.
func (c *Config) HTTPClient() *http.Client {
	return &http.Client{
		Timeout: c.RequestTimeout,
	}
}
