package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	cartomocks "github.com/ethpandaops/lab-backend/internal/cartographoor/mocks"
	"github.com/ethpandaops/lab-backend/internal/config"
)

func TestConfigHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		cartoNetworks  map[string]*cartographoor.Network
		configNetworks []config.NetworkConfig
		experiments    map[string]config.ExperimentSettings
		expectedStatus int
		validateResp   func(t *testing.T, resp *ConfigResponse)
	}{
		{
			name:   "GET request returns 200 with JSON",
			method: http.MethodGet,
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:        "mainnet",
					DisplayName: "Ethereum Mainnet",
					Status:      cartographoor.NetworkStatusActive,
					ChainID:     1,
					GenesisTime: 1606824000,
				},
			},
			configNetworks: []config.NetworkConfig{},
			experiments: map[string]config.ExperimentSettings{
				"test-experiment": {
					Enabled:  true,
					Networks: []string{"mainnet"},
				},
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, resp *ConfigResponse) {
				t.Helper()

				require.NotNil(t, resp)
				assert.Len(t, resp.Networks, 1)
				assert.Len(t, resp.Experiments, 1)

				// Verify network data
				assert.Equal(t, "mainnet", resp.Networks[0].Name)
				assert.Equal(t, "Ethereum Mainnet", resp.Networks[0].DisplayName)
				assert.Equal(t, int64(1), resp.Networks[0].ChainID)

				// Verify experiment data
				assert.Equal(t, "test-experiment", resp.Experiments[0].Name)
				assert.True(t, resp.Experiments[0].Enabled)
			},
		},
		{
			name:           "POST request returns 405",
			method:         http.MethodPost,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:   "disabled networks excluded from response",
			method: http.MethodGet,
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:        "mainnet",
					DisplayName: "Mainnet",
					Status:      cartographoor.NetworkStatusActive,
				},
				"sepolia": {
					Name:        "sepolia",
					DisplayName: "Sepolia",
					Status:      cartographoor.NetworkStatusActive,
				},
			},
			configNetworks: []config.NetworkConfig{
				{
					Name:    "sepolia",
					Enabled: boolPtr(false),
				},
			},
			experiments:    map[string]config.ExperimentSettings{},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, resp *ConfigResponse) {
				t.Helper()

				require.NotNil(t, resp)
				assert.Len(t, resp.Networks, 1)
				assert.Equal(t, "mainnet", resp.Networks[0].Name)
			},
		},
		{
			name:           "no networks returns empty array",
			method:         http.MethodGet,
			cartoNetworks:  map[string]*cartographoor.Network{},
			configNetworks: []config.NetworkConfig{},
			experiments:    map[string]config.ExperimentSettings{},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, resp *ConfigResponse) {
				t.Helper()

				require.NotNil(t, resp)
				assert.Empty(t, resp.Networks)
				assert.Empty(t, resp.Experiments)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Setup mocks
			var mockProvider cartographoor.Provider

			if tt.cartoNetworks != nil {
				mock := cartomocks.NewMockProvider(ctrl)
				mock.EXPECT().
					GetActiveNetworks(gomock.Any()).
					Return(tt.cartoNetworks).
					AnyTimes()

				// Mock GetNetwork for fork data lookups
				for name, network := range tt.cartoNetworks {
					net := network // Capture for closure
					mock.EXPECT().
						GetNetwork(gomock.Any(), name).
						Return(net, true).
						AnyTimes()
				}

				mockProvider = mock
			}

			cfg := &config.Config{
				Networks:    tt.configNetworks,
				Experiments: tt.experiments,
			}

			logger := logrus.New()
			logger.SetOutput(io.Discard)
			handler := NewConfigHandler(logger, cfg, mockProvider)

			// Create request
			req := httptest.NewRequest(tt.method, "/api/v1/config", http.NoBody)
			rec := httptest.NewRecorder()

			// Execute
			handler.ServeHTTP(rec, req)

			// Assert status
			assert.Equal(t, tt.expectedStatus, rec.Code)

			// Validate response if expected to succeed
			if tt.expectedStatus == http.StatusOK && tt.validateResp != nil {
				var resp ConfigResponse

				err := json.NewDecoder(rec.Body).Decode(&resp)
				require.NoError(t, err)

				tt.validateResp(t, &resp)
			}
		})
	}
}

func TestConfigHandler_buildNetworks(t *testing.T) {
	enabled := true
	disabled := false
	chainID1 := int64(1)
	chainID11155111 := int64(11155111)
	genesisTime1 := int64(1606824000)
	genesisTime2 := int64(1655733600)

	tests := []struct {
		name           string
		cartoNetworks  map[string]*cartographoor.Network
		configNetworks []config.NetworkConfig
		expectedNames  []string
	}{
		{
			name: "networks sorted alphabetically",
			cartoNetworks: map[string]*cartographoor.Network{
				"sepolia": {Name: "sepolia", DisplayName: "Sepolia", Status: cartographoor.NetworkStatusActive},
				"mainnet": {Name: "mainnet", DisplayName: "Mainnet", Status: cartographoor.NetworkStatusActive},
				"holesky": {Name: "holesky", DisplayName: "Holesky", Status: cartographoor.NetworkStatusActive},
			},
			configNetworks: []config.NetworkConfig{},
			expectedNames:  []string{"holesky", "mainnet", "sepolia"},
		},
		{
			name: "config overrides applied",
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:        "mainnet",
					DisplayName: "Original Name",
					Status:      cartographoor.NetworkStatusActive,
					ChainID:     1,
				},
			},
			configNetworks: []config.NetworkConfig{
				{
					Name:        "mainnet",
					DisplayName: "Custom Mainnet",
					Enabled:     &enabled,
					ChainID:     &chainID1,
					GenesisTime: &genesisTime1,
				},
			},
			expectedNames: []string{"mainnet"},
		},
		{
			name: "standalone config network included",
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {Name: "mainnet", DisplayName: "Mainnet", Status: cartographoor.NetworkStatusActive},
			},
			configNetworks: []config.NetworkConfig{
				{
					Name:        "custom",
					DisplayName: "Custom Network",
					Enabled:     &enabled,
					ChainID:     &chainID11155111,
					GenesisTime: &genesisTime2,
					TargetURL:   "http://custom.example.com",
				},
			},
			expectedNames: []string{"custom", "mainnet"},
		},
		{
			name: "disabled networks excluded",
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {Name: "mainnet", DisplayName: "Mainnet", Status: cartographoor.NetworkStatusActive},
				"sepolia": {Name: "sepolia", DisplayName: "Sepolia", Status: cartographoor.NetworkStatusActive},
			},
			configNetworks: []config.NetworkConfig{
				{Name: "sepolia", Enabled: &disabled},
			},
			expectedNames: []string{"mainnet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Setup mock provider
			var mockProvider cartographoor.Provider

			if len(tt.cartoNetworks) > 0 {
				mock := cartomocks.NewMockProvider(ctrl)
				mock.EXPECT().
					GetActiveNetworks(gomock.Any()).
					Return(tt.cartoNetworks).
					AnyTimes()

				// Mock GetNetwork for any network (including config-only ones)
				mock.EXPECT().
					GetNetwork(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, name string) (*cartographoor.Network, bool) {
						if net, exists := tt.cartoNetworks[name]; exists {
							return net, true
						}

						return nil, false
					}).
					AnyTimes()

				mockProvider = mock
			}

			cfg := &config.Config{
				Networks: tt.configNetworks,
			}

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			handler := &ConfigHandler{
				config:   cfg,
				provider: mockProvider,
				logger:   logger,
			}

			ctx := context.Background()
			result := handler.buildNetworks(ctx)

			// Verify network names match expected
			actualNames := make([]string, len(result))
			for i, net := range result {
				actualNames[i] = net.Name
			}

			assert.Equal(t, tt.expectedNames, actualNames)
		})
	}
}

func TestConfigHandler_buildExperiments(t *testing.T) {
	tests := []struct {
		name        string
		experiments map[string]config.ExperimentSettings
		expected    int
	}{
		{
			name: "experiments sorted alphabetically",
			experiments: map[string]config.ExperimentSettings{
				"zebra":  {Enabled: true},
				"alpha":  {Enabled: false},
				"middle": {Enabled: true},
			},
			expected: 3,
		},
		{
			name:        "empty experiments",
			experiments: map[string]config.ExperimentSettings{},
			expected:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			cfg := &config.Config{
				Experiments: tt.experiments,
			}

			handler := &ConfigHandler{
				config: cfg,
				logger: logger,
			}

			result := handler.buildExperiments(context.Background())

			assert.Len(t, result, tt.expected)

			// Verify sorting
			for i := 1; i < len(result); i++ {
				assert.True(t, result[i-1].Name < result[i].Name,
					"experiments should be sorted alphabetically")
			}
		})
	}
}

// Helper function to create bool pointers.
func boolPtr(b bool) *bool {
	return &b
}
