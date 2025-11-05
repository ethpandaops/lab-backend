package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestHeadersConfig_Loading(t *testing.T) {
	yamlConfig := `
server:
  port: 8080
  host: "0.0.0.0"
  read_timeout: 30s
  write_timeout: 30s
  shutdown_timeout: 10s
  log_level: "info"

redis:
  address: "localhost:6379"
  password: ""
  db: 0
  dial_timeout: 5s
  read_timeout: 3s
  write_timeout: 3s
  pool_size: 10

leader:
  lock_key: "lab:leader:lock"
  lock_ttl: 30s
  renew_interval: 10s
  retry_interval: 5s

cartographoor:
  source_url: "https://example.com/networks.json"
  refresh_interval: 5m
  request_timeout: 30s
  networks_ttl: 0s

bounds:
  refresh_interval: 10s
  request_timeout: 30s
  bounds_ttl: 0s

rate_limiting:
  enabled: false

headers:
  policies:
    - name: "static_assets"
      path_pattern: "\\.(js|css|png)$"
      headers:
        Cache-Control: "public, max-age=31536000, immutable"
        Vary: "Accept-Encoding"

    - name: "api_config"
      path_pattern: "^/api/v1/config$"
      headers:
        Cache-Control: "public, max-age=1, s-maxage=60"

    - name: "default"
      path_pattern: ".*"
      headers:
        Cache-Control: "public, max-age=1"
`

	var cfg Config

	err := yaml.Unmarshal([]byte(yamlConfig), &cfg)
	require.NoError(t, err)

	// Verify headers config loaded
	assert.Len(t, cfg.Headers.Policies, 3)

	// Verify first policy
	assert.Equal(t, "static_assets", cfg.Headers.Policies[0].Name)
	assert.Equal(t, `\.(js|css|png)$`, cfg.Headers.Policies[0].PathPattern)
	assert.Len(t, cfg.Headers.Policies[0].Headers, 2)
	assert.Equal(t, "public, max-age=31536000, immutable", cfg.Headers.Policies[0].Headers["Cache-Control"])
	assert.Equal(t, "Accept-Encoding", cfg.Headers.Policies[0].Headers["Vary"])

	// Verify second policy
	assert.Equal(t, "api_config", cfg.Headers.Policies[1].Name)
	assert.Equal(t, "^/api/v1/config$", cfg.Headers.Policies[1].PathPattern)
	assert.Len(t, cfg.Headers.Policies[1].Headers, 1)
	assert.Equal(t, "public, max-age=1, s-maxage=60", cfg.Headers.Policies[1].Headers["Cache-Control"])

	// Verify third policy
	assert.Equal(t, "default", cfg.Headers.Policies[2].Name)
	assert.Equal(t, ".*", cfg.Headers.Policies[2].PathPattern)
}

func TestExampleConfig_LoadsSuccessfully(t *testing.T) {
	// Load the actual config.example.yaml file
	cfg, err := Load("../../config.example.yaml")
	require.NoError(t, err)

	// Verify headers section exists and has policies
	assert.NotEmpty(t, cfg.Headers.Policies, "config.example.yaml should have header policies")

	// Log what we found
	t.Logf("Loaded %d header policies from config.example.yaml", len(cfg.Headers.Policies))

	for i, policy := range cfg.Headers.Policies {
		t.Logf("  Policy %d: %s with %d headers", i+1, policy.Name, len(policy.Headers))
	}
}

func TestHeadersConfig_EmptyPolicies(t *testing.T) {
	yamlConfig := `
server:
  port: 8080
  host: "0.0.0.0"
  read_timeout: 30s
  write_timeout: 30s
  shutdown_timeout: 10s
  log_level: "info"

redis:
  address: "localhost:6379"
  dial_timeout: 5s
  pool_size: 10

leader:
  lock_key: "lab:leader:lock"
  lock_ttl: 30s
  renew_interval: 10s
  retry_interval: 5s

cartographoor:
  source_url: "https://example.com/networks.json"
  refresh_interval: 5m
  request_timeout: 30s

bounds:
  refresh_interval: 10s
  request_timeout: 30s

rate_limiting:
  enabled: false

headers:
  policies: []
`

	var cfg Config

	err := yaml.Unmarshal([]byte(yamlConfig), &cfg)
	require.NoError(t, err)

	// Empty policies should be fine
	assert.Empty(t, cfg.Headers.Policies)
}

func TestHeadersConfig_NoHeadersSection(t *testing.T) {
	// Create minimal config without headers section
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)

	defer os.Remove(tmpFile.Name())

	yamlConfig := `
server:
  port: 8080
  host: "0.0.0.0"
  read_timeout: 30s
  write_timeout: 30s
  shutdown_timeout: 10s
  log_level: "info"

redis:
  address: "localhost:6379"
  dial_timeout: 5s
  read_timeout: 3s
  write_timeout: 3s
  pool_size: 10

leader:
  lock_key: "lab:leader:lock"
  lock_ttl: 30s
  renew_interval: 10s
  retry_interval: 5s

cartographoor:
  source_url: "https://example.com/networks.json"
  refresh_interval: 5m
  request_timeout: 30s

bounds:
  refresh_interval: 10s
  request_timeout: 30s

rate_limiting:
  enabled: false
`

	_, err = tmpFile.WriteString(yamlConfig)
	require.NoError(t, err)
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	require.NoError(t, err)

	// Missing headers section should result in empty policies
	assert.Empty(t, cfg.Headers.Policies)
}
