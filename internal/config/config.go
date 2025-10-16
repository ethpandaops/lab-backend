//nolint:tagliatelle // superior snake-case yo.
package config

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"gopkg.in/yaml.v3"
)

// Config represents the complete application configuration.
type Config struct {
	Server        ServerConfig                  `yaml:"server"`
	Redis         RedisConfig                   `yaml:"redis"`
	Leader        LeaderConfig                  `yaml:"leader"`
	Networks      []NetworkConfig               `yaml:"networks"`
	Experiments   map[string]ExperimentSettings `yaml:"experiments"`
	Cartographoor cartographoor.Config          `yaml:"cartographoor"`
	Bounds        BoundsConfig                  `yaml:"bounds"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Port            int           `yaml:"port"`
	Host            string        `yaml:"host"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	LogLevel        string        `yaml:"log_level"`
}

// RedisConfig holds Redis client configuration.
type RedisConfig struct {
	Address      string        `yaml:"address"`
	Password     string        `yaml:"password"`
	DB           int           `yaml:"db"`
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	PoolSize     int           `yaml:"pool_size"`
}

// LeaderConfig holds leader election configuration.
type LeaderConfig struct {
	LockKey       string        `yaml:"lock_key"`
	LockTTL       time.Duration `yaml:"lock_ttl"`
	RenewInterval time.Duration `yaml:"renew_interval"`
	RetryInterval time.Duration `yaml:"retry_interval"`
}

// BoundsConfig holds bounds service configuration.
type BoundsConfig struct {
	RefreshInterval time.Duration `yaml:"refresh_interval"` // How often to refresh bounds
	RequestTimeout  time.Duration `yaml:"request_timeout"`  // HTTP request timeout
	BoundsTTL       time.Duration `yaml:"bounds_ttl"`       // Redis TTL for bounds data (0 = no expiration)
}

// Validate validates the configuration and sets defaults.
func (c *BoundsConfig) Validate() error {
	// Set defaults
	if c.RefreshInterval == 0 {
		c.RefreshInterval = 7 * time.Second
	}

	if c.RequestTimeout == 0 {
		c.RequestTimeout = 30 * time.Second
	}

	// Validate ranges
	if c.RefreshInterval < 5*time.Second {
		return fmt.Errorf(
			"refresh_interval must be at least 5 seconds, got %v",
			c.RefreshInterval,
		)
	}

	if c.RequestTimeout < 5*time.Second {
		return fmt.Errorf(
			"request_timeout must be at least 5 seconds, got %v",
			c.RequestTimeout,
		)
	}

	return nil
}

// HTTPClient returns a configured HTTP client for upstream requests.
func (c *BoundsConfig) HTTPClient() *http.Client {
	return &http.Client{
		Timeout: c.RequestTimeout,
	}
}

// Load loads configuration from a YAML file.
func Load(path string) (*Config, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	// Validate server config
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Server.Host == "" {
		return fmt.Errorf("server host cannot be empty")
	}

	if c.Server.ReadTimeout <= 0 {
		return fmt.Errorf("read_timeout must be positive")
	}

	if c.Server.WriteTimeout <= 0 {
		return fmt.Errorf("write_timeout must be positive")
	}

	if c.Server.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown_timeout must be positive")
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"trace": true, "debug": true, "info": true,
		"warn": true, "error": true, "fatal": true, "panic": true,
	}
	if !validLogLevels[c.Server.LogLevel] {
		return fmt.Errorf("invalid log level: %s", c.Server.LogLevel)
	}

	// Redis is mandatory infrastructure
	if c.Redis.Address == "" {
		return fmt.Errorf("redis.address is required")
	}

	if c.Redis.DialTimeout <= 0 {
		return fmt.Errorf("redis.dial_timeout must be positive")
	}

	if c.Redis.PoolSize <= 0 {
		return fmt.Errorf("redis.pool_size must be positive")
	}

	// Leader election is mandatory infrastructure
	if c.Leader.LockKey == "" {
		return fmt.Errorf("leader.lock_key is required")
	}

	if c.Leader.LockTTL <= 0 {
		return fmt.Errorf("leader.lock_ttl must be positive")
	}

	if c.Leader.RenewInterval <= 0 {
		return fmt.Errorf("leader.renew_interval must be positive")
	}

	if c.Leader.RetryInterval <= 0 {
		return fmt.Errorf("leader.retry_interval must be positive")
	}

	// Validate individual network configs if any are provided
	networkNames := make(map[string]bool)

	for i, network := range c.Networks {
		if err := network.Validate(); err != nil {
			return fmt.Errorf("network %d: %w", i, err)
		}

		// Check for duplicate network names
		if networkNames[network.Name] {
			return fmt.Errorf("duplicate network name: %s", network.Name)
		}

		networkNames[network.Name] = true
	}

	// Validate cartographoor config
	if err := c.Cartographoor.Validate(); err != nil {
		return fmt.Errorf("cartographoor: %w", err)
	}

	// Validate bounds config
	if err := c.Bounds.Validate(); err != nil {
		return fmt.Errorf("bounds: %w", err)
	}

	return nil
}
