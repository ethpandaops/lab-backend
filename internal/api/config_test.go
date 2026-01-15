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
		features       []config.FeatureSettings
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
			features: []config.FeatureSettings{
				{
					Path:             "/ethereum/test-feature",
					DisabledNetworks: []string{"sepolia"},
				},
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, resp *ConfigResponse) {
				t.Helper()

				require.NotNil(t, resp)
				assert.Len(t, resp.Networks, 1)
				assert.Len(t, resp.Features, 1)

				// Verify network data
				assert.Equal(t, "mainnet", resp.Networks[0].Name)
				assert.Equal(t, "Ethereum Mainnet", resp.Networks[0].DisplayName)
				assert.Equal(t, int64(1), resp.Networks[0].ChainID)

				// Verify feature data
				assert.Equal(t, "/ethereum/test-feature", resp.Features[0].Path)
				assert.Equal(t, []string{"sepolia"}, resp.Features[0].DisabledNetworks)
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
			features:       []config.FeatureSettings{},
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
			features:       []config.FeatureSettings{},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, resp *ConfigResponse) {
				t.Helper()

				require.NotNil(t, resp)
				assert.Empty(t, resp.Networks)
				assert.Empty(t, resp.Features)
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
				Networks: tt.configNetworks,
				Features: tt.features,
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

func TestConfigHandler_buildFeatures(t *testing.T) {
	tests := []struct {
		name     string
		features []config.FeatureSettings
		expected int
	}{
		{
			name: "features sorted alphabetically by path",
			features: []config.FeatureSettings{
				{Path: "/zebra", DisabledNetworks: []string{}},
				{Path: "/alpha", DisabledNetworks: []string{"mainnet"}},
				{Path: "/middle", DisabledNetworks: []string{}},
			},
			expected: 3,
		},
		{
			name:     "empty features",
			features: []config.FeatureSettings{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			cfg := &config.Config{
				Features: tt.features,
			}

			handler := &ConfigHandler{
				config: cfg,
				logger: logger,
			}

			result := handler.buildFeatures(context.Background())

			assert.Len(t, result, tt.expected)

			// Verify sorting
			for i := 1; i < len(result); i++ {
				assert.True(t, result[i-1].Path < result[i].Path,
					"features should be sorted alphabetically by path")
			}
		})
	}
}

func TestTransformForks(t *testing.T) {
	tests := []struct {
		name     string
		input    cartographoor.Forks
		expected Forks
	}{
		{
			name: "transforms consensus forks with all fields",
			input: cartographoor.Forks{
				Consensus: map[string]cartographoor.ConsensusFork{
					"altair": {
						Epoch:     74240,
						Timestamp: 1616508000,
						MinClientVersions: map[string]string{
							"lighthouse": "1.5.0",
							"prysm":      "1.4.0",
						},
					},
					"bellatrix": {
						Epoch:     144896,
						Timestamp: 1663224179,
					},
				},
			},
			expected: Forks{
				Consensus: map[string]ConsensusFork{
					"altair": {
						Epoch:     74240,
						Timestamp: 1616508000,
						MinClientVersions: map[string]string{
							"lighthouse": "1.5.0",
							"prysm":      "1.4.0",
						},
					},
					"bellatrix": {
						Epoch:     144896,
						Timestamp: 1663224179,
					},
				},
				Execution: nil,
			},
		},
		{
			name: "transforms execution forks",
			input: cartographoor.Forks{
				Consensus: map[string]cartographoor.ConsensusFork{
					"phase0": {Epoch: 0},
				},
				Execution: map[string]cartographoor.ExecutionFork{
					"frontier": {
						Block:     0,
						Timestamp: 1438269973,
					},
					"homestead": {
						Block:     1150000,
						Timestamp: 1457981393,
					},
					"london": {
						Block:     12965000,
						Timestamp: 1628166822,
					},
				},
			},
			expected: Forks{
				Consensus: map[string]ConsensusFork{
					"phase0": {Epoch: 0},
				},
				Execution: map[string]ExecutionFork{
					"frontier": {
						Block:     0,
						Timestamp: 1438269973,
					},
					"homestead": {
						Block:     1150000,
						Timestamp: 1457981393,
					},
					"london": {
						Block:     12965000,
						Timestamp: 1628166822,
					},
				},
			},
		},
		{
			name: "empty forks",
			input: cartographoor.Forks{
				Consensus: map[string]cartographoor.ConsensusFork{},
			},
			expected: Forks{
				Consensus: map[string]ConsensusFork{},
				Execution: nil,
			},
		},
		{
			name: "nil execution forks stays nil",
			input: cartographoor.Forks{
				Consensus: map[string]cartographoor.ConsensusFork{
					"phase0": {Epoch: 0},
				},
				Execution: nil,
			},
			expected: Forks{
				Consensus: map[string]ConsensusFork{
					"phase0": {Epoch: 0},
				},
				Execution: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformForks(tt.input)

			assert.Equal(t, len(tt.expected.Consensus), len(result.Consensus))

			for name, expectedFork := range tt.expected.Consensus {
				actualFork, exists := result.Consensus[name]
				require.True(t, exists, "consensus fork %s should exist", name)
				assert.Equal(t, expectedFork.Epoch, actualFork.Epoch)
				assert.Equal(t, expectedFork.Timestamp, actualFork.Timestamp)
				assert.Equal(t, expectedFork.MinClientVersions, actualFork.MinClientVersions)
			}

			if tt.expected.Execution == nil {
				assert.Nil(t, result.Execution)
			} else {
				require.NotNil(t, result.Execution)
				assert.Equal(t, len(tt.expected.Execution), len(result.Execution))

				for name, expectedFork := range tt.expected.Execution {
					actualFork, exists := result.Execution[name]
					require.True(t, exists, "execution fork %s should exist", name)
					assert.Equal(t, expectedFork.Block, actualFork.Block)
					assert.Equal(t, expectedFork.Timestamp, actualFork.Timestamp)
				}
			}
		})
	}
}

func TestTransformBlobSchedule(t *testing.T) {
	tests := []struct {
		name     string
		input    []cartographoor.BlobScheduleEntry
		expected []BlobScheduleEntry
	}{
		{
			name: "transforms blob schedule with timestamps",
			input: []cartographoor.BlobScheduleEntry{
				{
					Epoch:            269568,
					Timestamp:        1710338135,
					MaxBlobsPerBlock: 6,
				},
				{
					Epoch:            412672,
					Timestamp:        1750000000,
					MaxBlobsPerBlock: 15,
				},
			},
			expected: []BlobScheduleEntry{
				{
					Epoch:            269568,
					Timestamp:        1710338135,
					MaxBlobsPerBlock: 6,
				},
				{
					Epoch:            412672,
					Timestamp:        1750000000,
					MaxBlobsPerBlock: 15,
				},
			},
		},
		{
			name: "transforms blob schedule without timestamps",
			input: []cartographoor.BlobScheduleEntry{
				{
					Epoch:            269568,
					MaxBlobsPerBlock: 6,
				},
			},
			expected: []BlobScheduleEntry{
				{
					Epoch:            269568,
					Timestamp:        0,
					MaxBlobsPerBlock: 6,
				},
			},
		},
		{
			name:     "nil input returns nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty slice returns empty slice",
			input:    []cartographoor.BlobScheduleEntry{},
			expected: []BlobScheduleEntry{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformBlobSchedule(tt.input)

			if tt.expected == nil {
				assert.Nil(t, result)

				return
			}

			require.NotNil(t, result)
			assert.Equal(t, len(tt.expected), len(result))

			for i, expected := range tt.expected {
				assert.Equal(t, expected.Epoch, result[i].Epoch)
				assert.Equal(t, expected.Timestamp, result[i].Timestamp)
				assert.Equal(t, expected.MaxBlobsPerBlock, result[i].MaxBlobsPerBlock)
			}
		})
	}
}

func TestConfigHandler_ForksAndBlobScheduleInResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cartoNetworks := map[string]*cartographoor.Network{
		"mainnet": {
			Name:        "mainnet",
			DisplayName: "Ethereum Mainnet",
			Status:      cartographoor.NetworkStatusActive,
			ChainID:     1,
			GenesisTime: 1606824000,
			Forks: cartographoor.Forks{
				Consensus: map[string]cartographoor.ConsensusFork{
					"phase0":    {Epoch: 0, Timestamp: 1606824000},
					"altair":    {Epoch: 74240, Timestamp: 1635753600},
					"bellatrix": {Epoch: 144896, Timestamp: 1663224179},
				},
				Execution: map[string]cartographoor.ExecutionFork{
					"frontier":  {Block: 0, Timestamp: 1438269973},
					"homestead": {Block: 1150000, Timestamp: 1457981393},
				},
			},
			BlobSchedule: []cartographoor.BlobScheduleEntry{
				{Epoch: 269568, Timestamp: 1710338135, MaxBlobsPerBlock: 6},
				{Epoch: 412672, Timestamp: 1750000000, MaxBlobsPerBlock: 15},
			},
		},
	}

	mock := cartomocks.NewMockProvider(ctrl)
	mock.EXPECT().
		GetActiveNetworks(gomock.Any()).
		Return(cartoNetworks).
		AnyTimes()
	mock.EXPECT().
		GetNetwork(gomock.Any(), "mainnet").
		Return(cartoNetworks["mainnet"], true).
		AnyTimes()

	cfg := &config.Config{
		Networks: []config.NetworkConfig{},
		Features: []config.FeatureSettings{},
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	handler := NewConfigHandler(logger, cfg, mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ConfigResponse

	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)

	require.Len(t, resp.Networks, 1)
	network := resp.Networks[0]

	// Verify consensus forks
	require.NotNil(t, network.Forks.Consensus)
	assert.Len(t, network.Forks.Consensus, 3)
	assert.Equal(t, int64(0), network.Forks.Consensus["phase0"].Epoch)
	assert.Equal(t, int64(1606824000), network.Forks.Consensus["phase0"].Timestamp)
	assert.Equal(t, int64(74240), network.Forks.Consensus["altair"].Epoch)
	assert.Equal(t, int64(1635753600), network.Forks.Consensus["altair"].Timestamp)

	// Verify execution forks
	require.NotNil(t, network.Forks.Execution)
	assert.Len(t, network.Forks.Execution, 2)
	assert.Equal(t, int64(0), network.Forks.Execution["frontier"].Block)
	assert.Equal(t, int64(1438269973), network.Forks.Execution["frontier"].Timestamp)
	assert.Equal(t, int64(1150000), network.Forks.Execution["homestead"].Block)
	assert.Equal(t, int64(1457981393), network.Forks.Execution["homestead"].Timestamp)

	// Verify blob schedule
	require.Len(t, network.BlobSchedule, 2)
	assert.Equal(t, int64(269568), network.BlobSchedule[0].Epoch)
	assert.Equal(t, int64(1710338135), network.BlobSchedule[0].Timestamp)
	assert.Equal(t, int64(6), network.BlobSchedule[0].MaxBlobsPerBlock)
	assert.Equal(t, int64(412672), network.BlobSchedule[1].Epoch)
	assert.Equal(t, int64(1750000000), network.BlobSchedule[1].Timestamp)
	assert.Equal(t, int64(15), network.BlobSchedule[1].MaxBlobsPerBlock)
}

// Helper function to create bool pointers.
func boolPtr(b bool) *bool {
	return &b
}
