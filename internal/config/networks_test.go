package config

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	cartomocks "github.com/ethpandaops/lab-backend/internal/cartographoor/mocks"
)

func TestBuildMergedNetworkList(t *testing.T) {
	enabled := true
	disabled := false
	chainID1 := int64(1)
	chainID11155111 := int64(11155111)
	genesisTime1 := int64(1606824023)
	genesisTime2 := int64(1655733600)
	genesisDelay1 := int64(0)

	tests := []struct {
		name             string
		cartoNetworks    map[string]*cartographoor.Network
		configNetworks   []NetworkConfig
		expectedNetworks map[string]NetworkConfig
	}{
		{
			name:             "empty providers returns empty map",
			cartoNetworks:    map[string]*cartographoor.Network{},
			configNetworks:   []NetworkConfig{},
			expectedNetworks: map[string]NetworkConfig{},
		},
		{
			name: "cartographoor only - returns all active networks",
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:         "mainnet",
					DisplayName:  "Ethereum Mainnet",
					TargetURL:    "https://cbt.mainnet.example.com",
					ChainID:      1,
					GenesisTime:  1606824023,
					GenesisDelay: 0,
					Status:       "active",
					LastUpdated:  time.Now(),
				},
				"sepolia": {
					Name:         "sepolia",
					DisplayName:  "Sepolia Testnet",
					TargetURL:    "https://cbt.sepolia.example.com",
					ChainID:      11155111,
					GenesisTime:  1655733600,
					GenesisDelay: 0,
					Status:       "active",
					LastUpdated:  time.Now(),
				},
			},
			configNetworks: []NetworkConfig{},
			expectedNetworks: map[string]NetworkConfig{
				"mainnet": {
					Name:         "mainnet",
					Enabled:      &enabled,
					DisplayName:  "Ethereum Mainnet",
					TargetURL:    "https://cbt.mainnet.example.com",
					ChainID:      &chainID1,
					GenesisTime:  &genesisTime1,
					GenesisDelay: &genesisDelay1,
				},
				"sepolia": {
					Name:         "sepolia",
					Enabled:      &enabled,
					DisplayName:  "Sepolia Testnet",
					TargetURL:    "https://cbt.sepolia.example.com",
					ChainID:      &chainID11155111,
					GenesisTime:  &genesisTime2,
					GenesisDelay: &genesisDelay1,
				},
			},
		},
		{
			name: "config overrides cartographoor target URL",
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:        "mainnet",
					DisplayName: "Ethereum Mainnet",
					TargetURL:   "https://cbt.mainnet.example.com",
					ChainID:     1,
					Status:      "active",
				},
			},
			configNetworks: []NetworkConfig{
				{
					Name:      "mainnet",
					TargetURL: "https://custom.mainnet.example.com",
				},
			},
			expectedNetworks: map[string]NetworkConfig{
				"mainnet": {
					Name:         "mainnet",
					Enabled:      &enabled,
					DisplayName:  "Ethereum Mainnet",
					TargetURL:    "https://custom.mainnet.example.com",
					ChainID:      &chainID1,
					GenesisTime:  nil,
					GenesisDelay: nil,
				},
			},
		},
		{
			name: "config overrides cartographoor display name",
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:        "mainnet",
					DisplayName: "Ethereum Mainnet",
					TargetURL:   "https://cbt.mainnet.example.com",
					ChainID:     1,
					Status:      "active",
				},
			},
			configNetworks: []NetworkConfig{
				{
					Name:        "mainnet",
					DisplayName: "Custom Mainnet Name",
				},
			},
			expectedNetworks: map[string]NetworkConfig{
				"mainnet": {
					Name:         "mainnet",
					Enabled:      &enabled,
					DisplayName:  "Custom Mainnet Name",
					TargetURL:    "https://cbt.mainnet.example.com",
					ChainID:      &chainID1,
					GenesisTime:  nil,
					GenesisDelay: nil,
				},
			},
		},
		{
			name: "disabled network excluded from result",
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:        "mainnet",
					DisplayName: "Ethereum Mainnet",
					TargetURL:   "https://cbt.mainnet.example.com",
					ChainID:     1,
					Status:      "active",
				},
				"sepolia": {
					Name:        "sepolia",
					DisplayName: "Sepolia Testnet",
					TargetURL:   "https://cbt.sepolia.example.com",
					ChainID:     11155111,
					Status:      "active",
				},
			},
			configNetworks: []NetworkConfig{
				{
					Name:    "sepolia",
					Enabled: &disabled,
				},
			},
			expectedNetworks: map[string]NetworkConfig{
				"mainnet": {
					Name:         "mainnet",
					Enabled:      &enabled,
					DisplayName:  "Ethereum Mainnet",
					TargetURL:    "https://cbt.mainnet.example.com",
					ChainID:      &chainID1,
					GenesisTime:  nil,
					GenesisDelay: nil,
				},
			},
		},
		{
			name:          "config-only network (not in cartographoor)",
			cartoNetworks: map[string]*cartographoor.Network{},
			configNetworks: []NetworkConfig{
				{
					Name:        "custom",
					DisplayName: "Custom Network",
					TargetURL:   "https://custom.example.com",
				},
			},
			expectedNetworks: map[string]NetworkConfig{
				"custom": {
					Name:        "custom",
					Enabled:     &enabled,
					DisplayName: "Custom Network",
					TargetURL:   "https://custom.example.com",
				},
			},
		},
		{
			name:          "config-only disabled network excluded",
			cartoNetworks: map[string]*cartographoor.Network{},
			configNetworks: []NetworkConfig{
				{
					Name:        "custom",
					DisplayName: "Custom Network",
					TargetURL:   "https://custom.example.com",
					Enabled:     &disabled,
				},
			},
			expectedNetworks: map[string]NetworkConfig{},
		},
		{
			name: "mix of cartographoor, overrides, and standalone networks",
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:        "mainnet",
					DisplayName: "Ethereum Mainnet",
					TargetURL:   "https://cbt.mainnet.example.com",
					ChainID:     1,
					Status:      "active",
				},
				"sepolia": {
					Name:        "sepolia",
					DisplayName: "Sepolia Testnet",
					TargetURL:   "https://cbt.sepolia.example.com",
					ChainID:     11155111,
					Status:      "active",
				},
			},
			configNetworks: []NetworkConfig{
				{
					Name:      "mainnet",
					TargetURL: "https://custom.mainnet.example.com",
				},
				{
					Name:    "sepolia",
					Enabled: &disabled,
				},
				{
					Name:        "custom",
					DisplayName: "Custom Network",
					TargetURL:   "https://custom.example.com",
				},
			},
			expectedNetworks: map[string]NetworkConfig{
				"mainnet": {
					Name:         "mainnet",
					Enabled:      &enabled,
					DisplayName:  "Ethereum Mainnet",
					TargetURL:    "https://custom.mainnet.example.com",
					ChainID:      &chainID1,
					GenesisTime:  nil,
					GenesisDelay: nil,
				},
				"custom": {
					Name:        "custom",
					Enabled:     &enabled,
					DisplayName: "Custom Network",
					TargetURL:   "https://custom.example.com",
				},
			},
		},
		{
			name:          "nil provider returns only config networks",
			cartoNetworks: nil,
			configNetworks: []NetworkConfig{
				{
					Name:        "custom",
					DisplayName: "Custom Network",
					TargetURL:   "https://custom.example.com",
				},
			},
			expectedNetworks: map[string]NetworkConfig{
				"custom": {
					Name:        "custom",
					Enabled:     &enabled,
					DisplayName: "Custom Network",
					TargetURL:   "https://custom.example.com",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var mockProvider cartographoor.Provider

			if tt.cartoNetworks != nil {
				mock := cartomocks.NewMockProvider(ctrl)
				mock.EXPECT().
					GetActiveNetworks(gomock.Any()).
					Return(tt.cartoNetworks).
					Times(1)
				mockProvider = mock
			}

			cfg := &Config{Networks: tt.configNetworks}
			ctx := context.Background()
			logger := logrus.New()
			logger.SetOutput(io.Discard)

			result := BuildMergedNetworkList(ctx, logger, cfg, mockProvider)

			require.Len(t, result, len(tt.expectedNetworks), "network count mismatch")

			for name, expected := range tt.expectedNetworks {
				actual, exists := result[name]
				require.True(t, exists, "expected network %s not found", name)

				assert.Equal(t, expected.Name, actual.Name, "name mismatch for %s", name)
				assert.Equal(t, expected.DisplayName, actual.DisplayName, "display name mismatch for %s", name)
				assert.Equal(t, expected.TargetURL, actual.TargetURL, "target URL mismatch for %s", name)

				if expected.Enabled != nil {
					require.NotNil(t, actual.Enabled, "enabled should not be nil for %s", name)
					assert.Equal(t, *expected.Enabled, *actual.Enabled, "enabled mismatch for %s", name)
				}

				if expected.ChainID != nil {
					require.NotNil(t, actual.ChainID, "chain ID should not be nil for %s", name)
					assert.Equal(t, *expected.ChainID, *actual.ChainID, "chain ID mismatch for %s", name)
				}

				if expected.GenesisTime != nil {
					require.NotNil(t, actual.GenesisTime, "genesis time should not be nil for %s", name)
					assert.Equal(t, *expected.GenesisTime, *actual.GenesisTime, "genesis time mismatch for %s", name)
				}

				if expected.GenesisDelay != nil {
					require.NotNil(t, actual.GenesisDelay, "genesis delay should not be nil for %s", name)
					assert.Equal(t, *expected.GenesisDelay, *actual.GenesisDelay, "genesis delay mismatch for %s", name)
				}
			}
		})
	}
}

func TestNetworkConfig_Validate(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name        string
		config      NetworkConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config with all fields",
			config: NetworkConfig{
				Name:        "mainnet",
				Enabled:     &enabled,
				TargetURL:   "https://example.com",
				DisplayName: "Mainnet",
			},
			expectError: false,
		},
		{
			name: "valid config with minimal fields",
			config: NetworkConfig{
				Name: "mainnet",
			},
			expectError: false,
		},
		{
			name: "empty name returns error",
			config: NetworkConfig{
				Name: "",
			},
			expectError: true,
			errorMsg:    "network name cannot be empty",
		},
		{
			name: "disabled network with no target URL is valid",
			config: NetworkConfig{
				Name:    "mainnet",
				Enabled: &disabled,
			},
			expectError: false,
		},
		{
			name: "invalid URL scheme returns error",
			config: NetworkConfig{
				Name:      "mainnet",
				TargetURL: "ftp://example.com",
			},
			expectError: true,
			errorMsg:    "must use http or https scheme",
		},
		{
			name: "malformed URL returns error",
			config: NetworkConfig{
				Name:      "mainnet",
				TargetURL: "://invalid",
			},
			expectError: true,
			errorMsg:    "invalid target_url",
		},
		{
			name: "http URL is valid",
			config: NetworkConfig{
				Name:      "mainnet",
				TargetURL: "http://example.com",
			},
			expectError: false,
		},
		{
			name: "https URL is valid",
			config: NetworkConfig{
				Name:      "mainnet",
				TargetURL: "https://example.com",
			},
			expectError: false,
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
