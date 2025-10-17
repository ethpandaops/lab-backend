package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: &Config{
				Server: ServerConfig{
					Host:            "localhost",
					Port:            8080,
					ReadTimeout:     time.Second,
					WriteTimeout:    time.Second,
					ShutdownTimeout: 5 * time.Second,
					LogLevel:        "info",
				},
				Redis: RedisConfig{
					Address:     "localhost:6379",
					DialTimeout: 5 * time.Second,
					PoolSize:    10,
				},
				Leader: LeaderConfig{
					LockKey:       "lab:leader",
					LockTTL:       10 * time.Second,
					RenewInterval: 3 * time.Second,
					RetryInterval: 5 * time.Second,
				},
				Cartographoor: cartographoor.Config{
					SourceURL:       "https://example.com",
					RefreshInterval: 60 * time.Second,
					RequestTimeout:  10 * time.Second,
				},
				Bounds: BoundsConfig{
					RefreshInterval: 7 * time.Second,
					RequestTimeout:  10 * time.Second,
				},
			},
			expectError: false,
		},
		{
			name: "invalid port negative",
			config: &Config{
				Server: ServerConfig{
					Host: "localhost",
					Port: -1,
				},
			},
			expectError: true,
			errorMsg:    "invalid server port",
		},
		{
			name: "invalid port too high",
			config: &Config{
				Server: ServerConfig{
					Host: "localhost",
					Port: 99999,
				},
			},
			expectError: true,
			errorMsg:    "invalid server port",
		},
		{
			name: "invalid port zero",
			config: &Config{
				Server: ServerConfig{
					Host: "localhost",
					Port: 0,
				},
			},
			expectError: true,
			errorMsg:    "invalid server port",
		},
		{
			name: "missing host",
			config: &Config{
				Server: ServerConfig{
					Host: "",
					Port: 8080,
				},
			},
			expectError: true,
			errorMsg:    "server host cannot be empty",
		},
		{
			name: "invalid log level",
			config: &Config{
				Server: ServerConfig{
					Host:            "localhost",
					Port:            8080,
					ReadTimeout:     time.Second,
					WriteTimeout:    time.Second,
					ShutdownTimeout: time.Second,
					LogLevel:        "invalid",
				},
			},
			expectError: true,
			errorMsg:    "invalid log level",
		},
		{
			name: "duplicate network names",
			config: &Config{
				Server: ServerConfig{
					Host:            "localhost",
					Port:            8080,
					ReadTimeout:     time.Second,
					WriteTimeout:    time.Second,
					ShutdownTimeout: time.Second,
					LogLevel:        "info",
				},
				Redis: RedisConfig{
					Address:     "localhost:6379",
					DialTimeout: time.Second,
					PoolSize:    10,
				},
				Leader: LeaderConfig{
					LockKey:       "lab:leader",
					LockTTL:       10 * time.Second,
					RenewInterval: 3 * time.Second,
					RetryInterval: 5 * time.Second,
				},
				Networks: []NetworkConfig{
					{Name: "mainnet", TargetURL: "http://example.com"},
					{Name: "mainnet", TargetURL: "http://example2.com"},
				},
				Cartographoor: cartographoor.Config{
					SourceURL:       "https://example.com",
					RefreshInterval: 60 * time.Second,
					RequestTimeout:  10 * time.Second,
				},
				Bounds: BoundsConfig{
					RefreshInterval: 7 * time.Second,
					RequestTimeout:  10 * time.Second,
				},
			},
			expectError: true,
			errorMsg:    "duplicate network name",
		},
		{
			name: "missing redis address",
			config: &Config{
				Server: ServerConfig{
					Host:            "localhost",
					Port:            8080,
					ReadTimeout:     time.Second,
					WriteTimeout:    time.Second,
					ShutdownTimeout: time.Second,
					LogLevel:        "info",
				},
				Redis: RedisConfig{
					Address: "",
				},
			},
			expectError: true,
			errorMsg:    "redis.address is required",
		},
		{
			name: "zero read timeout",
			config: &Config{
				Server: ServerConfig{
					Host:         "localhost",
					Port:         8080,
					ReadTimeout:  0,
					WriteTimeout: time.Second,
				},
			},
			expectError: true,
			errorMsg:    "read_timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBoundsConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      BoundsConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: BoundsConfig{
				RefreshInterval: 7 * time.Second,
				RequestTimeout:  10 * time.Second,
			},
			expectError: false,
		},
		{
			name:   "applies defaults",
			config: BoundsConfig{
				// All zero values - should get defaults
			},
			expectError: false,
		},
		{
			name: "refresh interval too low",
			config: BoundsConfig{
				RefreshInterval: 3 * time.Second,
				RequestTimeout:  10 * time.Second,
			},
			expectError: true,
			errorMsg:    "refresh_interval must be at least 5 seconds",
		},
		{
			name: "request timeout too low",
			config: BoundsConfig{
				RefreshInterval: 7 * time.Second,
				RequestTimeout:  3 * time.Second,
			},
			expectError: true,
			errorMsg:    "request_timeout must be at least 5 seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
				// Verify defaults were applied
				assert.GreaterOrEqual(t, tt.config.RefreshInterval, 5*time.Second)
				assert.GreaterOrEqual(t, tt.config.RequestTimeout, 5*time.Second)
			}
		})
	}
}

func TestConfig_Load(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		expectError bool
		errorMsg    string
		validate    func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid YAML file",
			yamlContent: `
server:
  host: localhost
  port: 8080
  read_timeout: 1s
  write_timeout: 1s
  shutdown_timeout: 5s
  log_level: info
redis:
  address: localhost:6379
  dial_timeout: 5s
  pool_size: 10
leader:
  lock_key: lab:leader
  lock_ttl: 10s
  renew_interval: 3s
  retry_interval: 5s
cartographoor:
  source_url: https://example.com
  refresh_interval: 30s
  timeout: 10
bounds:
  refresh_interval: 7s
  request_timeout: 10s
`,
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()

				assert.Equal(t, 8080, cfg.Server.Port)
				assert.Equal(t, "localhost", cfg.Server.Host)
				assert.Equal(t, "info", cfg.Server.LogLevel)
			},
		},
		{
			name:        "invalid YAML syntax",
			yamlContent: "invalid: yaml: content:",
			expectError: true,
			errorMsg:    "failed to parse config",
		},
		{
			name:        "empty file",
			yamlContent: "",
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()

				// Empty file loads but config won't validate
				assert.NotNil(t, cfg)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")

			err := os.WriteFile(configPath, []byte(tt.yamlContent), 0600)
			require.NoError(t, err)

			// Load config
			cfg, err := Load(configPath)

			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}

				return
			}

			require.NoError(t, err)

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestConfig_Load_NonExistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}
