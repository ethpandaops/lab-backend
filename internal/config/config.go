//nolint:tagliatelle // superior snake-case yo.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete application configuration.
type Config struct {
	Server      ServerConfig     `yaml:"server"`
	Networks    []NetworkConfig  `yaml:"networks"`
	Experiments ExperimentConfig `yaml:"experiments"`
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

	// Validate networks
	if len(c.Networks) == 0 {
		return fmt.Errorf("at least one network must be configured")
	}

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

	// Validate experiments
	if err := c.Experiments.Validate(networkNames); err != nil {
		return fmt.Errorf("experiments: %w", err)
	}

	return nil
}
