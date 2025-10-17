package testutil

import (
	"github.com/ethpandaops/lab-backend/internal/config"
)

// NewTestConfig returns a minimal valid config for testing.
func NewTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}
}
