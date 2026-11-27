package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

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

const awkwardValue = "a\tb #c\nd: e\"f'g\\h"

func unsetEnv(t *testing.T, name string) {
	t.Helper()

	t.Setenv(name, "")
	require.NoError(t, os.Unsetenv(name))
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func TestDecodeConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
		set     map[string]string
		unset   []string
		want    map[string]any
		wantErr string
	}{
		{
			name:    "variable is replaced",
			content: "address: redis://user:${REDIS_PASS}@redis:6379/0",
			set:     map[string]string{"REDIS_PASS": "s3cret"},
			want:    map[string]any{"address": "redis://user:s3cret@redis:6379/0"},
		},
		{
			name:    "multiple variables in one value",
			content: "addr: ${HOST}:${PORT}",
			set:     map[string]string{"HOST": "example.com", "PORT": "8080"},
			want:    map[string]any{"addr": "example.com:8080"},
		},
		{
			name:    "default used when variable unset",
			content: "addr: ${HOST:-localhost}",
			unset:   []string{"HOST"},
			want:    map[string]any{"addr": "localhost"},
		},
		{
			name:    "default used when variable empty",
			content: "addr: ${HOST:-localhost}",
			set:     map[string]string{"HOST": ""},
			want:    map[string]any{"addr": "localhost"},
		},
		{
			name:    "default ignored when variable set",
			content: "addr: ${HOST:-localhost}",
			set:     map[string]string{"HOST": "example.com"},
			want:    map[string]any{"addr": "example.com"},
		},
		{
			name:    "empty default allows unset variable",
			content: "token: \"${TOKEN:-}\"",
			unset:   []string{"TOKEN"},
			want:    map[string]any{"token": ""},
		},
		{
			name:    "empty variable without default is an error",
			content: "token: ${TOKEN}",
			set:     map[string]string{"TOKEN": ""},
			wantErr: "config references unset or empty environment variables: TOKEN",
		},
		{
			name:    "unset variables without default are an error",
			content: "user: ${REDIS_USER}\nnested:\n  - pass: ${REDIS_PASS}\nagain: ${REDIS_USER}",
			unset:   []string{"REDIS_USER", "REDIS_PASS"},
			wantErr: "config references unset or empty environment variables: REDIS_USER, REDIS_PASS",
		},
		{
			name:    "comments are not expanded",
			content: "# address: ${REDIS_PASS}\naddr: ${HOST} # ${REDIS_PASS}",
			set:     map[string]string{"HOST": "example.com"},
			unset:   []string{"REDIS_PASS"},
			want:    map[string]any{"addr": "example.com"},
		},
		{
			name:    "keys are not expanded",
			content: "${HOST}: value",
			set:     map[string]string{"HOST": "example.com"},
			want:    map[string]any{"${HOST}": "value"},
		},
		{
			name:    "literal dollar signs are left unchanged",
			content: "password: pa$$word $HOST $5 ${HOST",
			set:     map[string]string{"HOST": "example.com"},
			want:    map[string]any{"password": "pa$$word $HOST $5 ${HOST"},
		},
		{
			name:    "value with YAML syntax arrives unchanged",
			content: "plain: ${P}\ndouble: \"${P}\"\nsingle: '${P}'\nlist:\n  - ${P}\nanchored: &a ${P}\naliased: *a",
			set:     map[string]string{"P": awkwardValue},
			want: map[string]any{
				"plain":    awkwardValue,
				"double":   awkwardValue,
				"single":   awkwardValue,
				"list":     []any{awkwardValue},
				"anchored": awkwardValue,
				"aliased":  awkwardValue,
			},
		},
		{
			name:    "unquoted value resolves its type, quoted value stays a string",
			content: "port: ${PORT}\nquoted: \"${PORT}\"\nenabled: ${ENABLED}",
			set:     map[string]string{"PORT": "8080", "ENABLED": "true"},
			want:    map[string]any{"port": 8080, "quoted": "8080", "enabled": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.set {
				t.Setenv(k, v)
			}

			for _, k := range tt.unset {
				unsetEnv(t, k)
			}

			var got map[string]any

			err := decodeConfig([]byte(tt.content), &got)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDecodeConfigWithoutPlaceholdersIsUnchanged(t *testing.T) {
	data, err := os.ReadFile("../../config.example.yaml")
	require.NoError(t, err)

	var want, got any

	require.NoError(t, yaml.Unmarshal(data, &want))
	require.NoError(t, decodeConfig(data, &got))
	assert.Equal(t, want, got)
}

func TestConfig_Load_ExpandsEnvVars(t *testing.T) {
	t.Setenv("LAB_TEST_REDIS_PASSWORD", awkwardValue)
	t.Setenv("LAB_TEST_REDIS_DB", "7")
	t.Setenv("LAB_TEST_CACHE_CONTROL", "max-age=60")
	t.Setenv("LAB_TEST_TARGET", "cbt-api:8080")

	cfg, err := Load(writeConfig(t, `
redis:
  address: redis:6379
  password: ${LAB_TEST_REDIS_PASSWORD}
  db: ${LAB_TEST_REDIS_DB}
networks:
  - name: mainnet
    target_url: "http://${LAB_TEST_TARGET}/api/v1"
headers:
  policies:
    - name: static
      headers:
        Cache-Control: ${LAB_TEST_CACHE_CONTROL}
`))
	require.NoError(t, err)

	assert.Equal(t, "redis:6379", cfg.Redis.Address)
	assert.Equal(t, awkwardValue, cfg.Redis.Password)
	assert.Equal(t, 7, cfg.Redis.DB)
	require.Len(t, cfg.Networks, 1)
	assert.Equal(t, "http://cbt-api:8080/api/v1", cfg.Networks[0].TargetURL)
	require.Len(t, cfg.Headers.Policies, 1)
	assert.Equal(t, "max-age=60", cfg.Headers.Policies[0].Headers["Cache-Control"])
}

func TestConfig_Load_FailsOnUnsetEnvVar(t *testing.T) {
	unsetEnv(t, "LAB_TEST_REDIS_PASSWORD")

	_, err := Load(writeConfig(t, `
redis:
  address: "redis://user:${LAB_TEST_REDIS_PASSWORD}@redis:6379/0"
`))
	require.ErrorContains(t, err, "LAB_TEST_REDIS_PASSWORD")
}
