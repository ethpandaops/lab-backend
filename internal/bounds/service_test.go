package bounds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	cartomocks "github.com/ethpandaops/lab-backend/internal/cartographoor/mocks"
	"github.com/ethpandaops/lab-backend/internal/config"
)

func TestService_calculateBounds(t *testing.T) {
	tests := []struct {
		name     string
		records  []IncrementalTableRecord
		expected *BoundsData
	}{
		{
			name:    "empty records returns empty bounds",
			records: []IncrementalTableRecord{},
			expected: &BoundsData{
				Tables: make(map[string]TableBounds),
			},
		},
		{
			name: "single table single record",
			records: []IncrementalTableRecord{
				{Table: "beacon_block", Position: 100, Interval: 10},
			},
			expected: &BoundsData{
				Tables: map[string]TableBounds{
					"beacon_block": {Min: 100, Max: 110},
				},
			},
		},
		{
			name: "single table multiple records finds correct min/max",
			records: []IncrementalTableRecord{
				{Table: "beacon_block", Position: 100, Interval: 10},
				{Table: "beacon_block", Position: 50, Interval: 5},
				{Table: "beacon_block", Position: 200, Interval: 20},
			},
			expected: &BoundsData{
				Tables: map[string]TableBounds{
					"beacon_block": {Min: 50, Max: 220}, // Max = 200 + 20
				},
			},
		},
		{
			name: "multiple tables calculates bounds independently",
			records: []IncrementalTableRecord{
				{Table: "beacon_block", Position: 100, Interval: 10},
				{Table: "beacon_block", Position: 50, Interval: 5},
				{Table: "beacon_state", Position: 200, Interval: 20},
				{Table: "beacon_state", Position: 300, Interval: 30},
			},
			expected: &BoundsData{
				Tables: map[string]TableBounds{
					"beacon_block": {Min: 50, Max: 110},
					"beacon_state": {Min: 200, Max: 330},
				},
			},
		},
		{
			name: "overlapping intervals handled correctly",
			records: []IncrementalTableRecord{
				{Table: "beacon_block", Position: 100, Interval: 100},
				{Table: "beacon_block", Position: 150, Interval: 10},
			},
			expected: &BoundsData{
				Tables: map[string]TableBounds{
					"beacon_block": {Min: 100, Max: 200}, // 100 + 100 = 200
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logrus.New()
			logger.SetOutput(io.Discard)

			svc := &Service{
				logger: logger,
			}

			result := svc.calculateBounds(tt.records)

			require.NotNil(t, result)
			assert.Equal(t, tt.expected.Tables, result.Tables)
			assert.False(t, result.LastUpdated.IsZero())
		})
	}
}

func TestService_fetchBoundsForNetwork(t *testing.T) {
	tests := []struct {
		name          string
		networkConfig config.NetworkConfig
		mockResponse  func(w http.ResponseWriter, r *http.Request)
		expectError   bool
		errorContains string
		validateData  func(t *testing.T, data *BoundsData)
	}{
		{
			name: "single page response",
			networkConfig: config.NetworkConfig{
				Name:      "mainnet",
				TargetURL: "", // Will be set by test server
			},
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				resp := AdminCBTIncrementalResponse{
					AdminCBTIncremental: []IncrementalTableRecord{
						{Table: "beacon_block", Position: 100, Interval: 10},
						{Table: "beacon_block", Position: 200, Interval: 20},
					},
					NextPageToken: "",
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck //test
			},
			expectError: false,
			validateData: func(t *testing.T, data *BoundsData) {
				t.Helper()

				require.NotNil(t, data)
				assert.Contains(t, data.Tables, "beacon_block")
				assert.Equal(t, int64(100), data.Tables["beacon_block"].Min)
				assert.Equal(t, int64(220), data.Tables["beacon_block"].Max)
			},
		},
		{
			name: "multiple pages handled correctly",
			networkConfig: config.NetworkConfig{
				Name:      "mainnet",
				TargetURL: "",
			},
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				pageToken := r.URL.Query().Get("page_token")

				switch pageToken {
				case "":
					// First page
					resp := AdminCBTIncrementalResponse{
						AdminCBTIncremental: []IncrementalTableRecord{
							{Table: "beacon_block", Position: 100, Interval: 10},
						},
						NextPageToken: "page2",
					}

					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(resp) //nolint:errcheck //test
				case "page2":
					// Second page
					resp := AdminCBTIncrementalResponse{
						AdminCBTIncremental: []IncrementalTableRecord{
							{Table: "beacon_block", Position: 200, Interval: 20},
						},
						NextPageToken: "",
					}

					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(resp) //nolint:errcheck //test
				}
			},
			expectError: false,
			validateData: func(t *testing.T, data *BoundsData) {
				t.Helper()

				require.NotNil(t, data)
				assert.Contains(t, data.Tables, "beacon_block")
				// Should have both records
				assert.Equal(t, int64(100), data.Tables["beacon_block"].Min)
				assert.Equal(t, int64(220), data.Tables["beacon_block"].Max)
			},
		},
		{
			name: "empty response returns empty bounds",
			networkConfig: config.NetworkConfig{
				Name:      "mainnet",
				TargetURL: "",
			},
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				resp := AdminCBTIncrementalResponse{
					AdminCBTIncremental: []IncrementalTableRecord{},
					NextPageToken:       "",
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck //test
			},
			expectError: false,
			validateData: func(t *testing.T, data *BoundsData) {
				t.Helper()

				require.NotNil(t, data)
				assert.Empty(t, data.Tables)
			},
		},
		{
			name: "HTTP error returns error",
			networkConfig: config.NetworkConfig{
				Name:      "mainnet",
				TargetURL: "",
			},
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Internal Server Error")) //nolint:errcheck // test.
			},
			expectError:   true,
			errorContains: "unexpected status 500",
		},
		{
			name: "invalid JSON returns error",
			networkConfig: config.NetworkConfig{
				Name:      "mainnet",
				TargetURL: "",
			},
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("invalid json")) //nolint:errcheck // test.
			},
			expectError:   true,
			errorContains: "parse JSON",
		},
		{
			name: "missing target URL returns error",
			networkConfig: config.NetworkConfig{
				Name:      "mainnet",
				TargetURL: "",
			},
			mockResponse:  nil,
			expectError:   true,
			errorContains: "no target_url configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server

			if tt.mockResponse != nil {
				server = httptest.NewServer(http.HandlerFunc(tt.mockResponse))
				defer server.Close()

				tt.networkConfig.TargetURL = server.URL
			}

			cfg := &config.Config{
				Bounds: config.BoundsConfig{
					RequestTimeout: 10 * time.Second,
				},
			}

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			svc := &Service{
				config:     cfg,
				logger:     logger,
				httpClient: cfg.Bounds.HTTPClient(),
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result, err := svc.fetchBoundsForNetwork(ctx, tt.networkConfig)

			if tt.expectError {
				require.Error(t, err)

				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}

				return
			}

			require.NoError(t, err)

			if tt.validateData != nil {
				tt.validateData(t, result)
			}
		})
	}
}

func TestService_fetchBoundsForNetwork_HybridMode(t *testing.T) {
	// External server returns bounds for fct_block and fct_attestation
	externalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := AdminCBTIncrementalResponse{
			AdminCBTIncremental: []IncrementalTableRecord{
				{Table: "fct_block", Position: 100, Interval: 10},
				{Table: "fct_attestation", Position: 200, Interval: 20},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck //test
	}))
	defer externalServer.Close()

	// Local server returns bounds for fct_block only (different values)
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := AdminCBTIncrementalResponse{
			AdminCBTIncremental: []IncrementalTableRecord{
				{Table: "fct_block", Position: 50, Interval: 5},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck //test
	}))
	defer localServer.Close()

	cfg := &config.Config{
		Bounds: config.BoundsConfig{
			RequestTimeout: 10 * time.Second,
		},
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := &Service{
		config:     cfg,
		logger:     logger,
		httpClient: cfg.Bounds.HTTPClient(),
	}

	ctx := context.Background()

	network := config.NetworkConfig{
		Name:      "mainnet",
		TargetURL: externalServer.URL,
		LocalOverrides: &config.LocalOverridesConfig{
			TargetURL: localServer.URL,
			Tables:    []string{"fct_block"},
		},
	}

	result, err := svc.fetchBoundsForNetwork(ctx, network)
	require.NoError(t, err)
	require.NotNil(t, result)

	// fct_block should come from local (overridden table)
	assert.Equal(t, int64(50), result.Tables["fct_block"].Min)
	assert.Equal(t, int64(55), result.Tables["fct_block"].Max)

	// fct_attestation should come from external
	assert.Equal(t, int64(200), result.Tables["fct_attestation"].Min)
	assert.Equal(t, int64(220), result.Tables["fct_attestation"].Max)
}

func TestService_fetchBoundsForNetwork_HybridLocalFailsGracefully(t *testing.T) {
	// External server works fine
	externalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := AdminCBTIncrementalResponse{
			AdminCBTIncremental: []IncrementalTableRecord{
				{Table: "fct_block", Position: 100, Interval: 10},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck //test
	}))
	defer externalServer.Close()

	// Local server is down
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer localServer.Close()

	cfg := &config.Config{
		Bounds: config.BoundsConfig{
			RequestTimeout: 10 * time.Second,
		},
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	svc := &Service{
		config:     cfg,
		logger:     logger,
		httpClient: cfg.Bounds.HTTPClient(),
	}

	ctx := context.Background()

	network := config.NetworkConfig{
		Name:      "mainnet",
		TargetURL: externalServer.URL,
		LocalOverrides: &config.LocalOverridesConfig{
			TargetURL: localServer.URL,
			Tables:    []string{"fct_block"},
		},
	}

	// Should succeed with cached external bounds when local fails
	result, err := svc.fetchBoundsForNetwork(ctx, network)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int64(100), result.Tables["fct_block"].Min)
}

func TestService_FetchBounds(t *testing.T) {
	tests := []struct {
		name              string
		cartoNetworks     map[string]*cartographoor.Network
		configNetworks    []config.NetworkConfig
		mockServers       map[string]func(w http.ResponseWriter, r *http.Request)
		expectedNetworks  []string
		expectPartialData bool
	}{
		{
			name: "all networks succeed",
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {Name: "mainnet", DisplayName: "Mainnet"},
			},
			configNetworks: []config.NetworkConfig{},
			mockServers: map[string]func(w http.ResponseWriter, r *http.Request){
				"mainnet": func(w http.ResponseWriter, r *http.Request) {
					resp := AdminCBTIncrementalResponse{
						AdminCBTIncremental: []IncrementalTableRecord{
							{Table: "beacon_block", Position: 100, Interval: 10},
						},
						NextPageToken: "",
					}

					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(resp) //nolint:errcheck //test
				},
			},
			expectedNetworks:  []string{"mainnet"},
			expectPartialData: false,
		},
		{
			name: "some networks fail returns partial data",
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {Name: "mainnet", DisplayName: "Mainnet"},
				"sepolia": {Name: "sepolia", DisplayName: "Sepolia"},
			},
			configNetworks: []config.NetworkConfig{},
			mockServers: map[string]func(w http.ResponseWriter, r *http.Request){
				"mainnet": func(w http.ResponseWriter, r *http.Request) {
					resp := AdminCBTIncrementalResponse{
						AdminCBTIncremental: []IncrementalTableRecord{
							{Table: "beacon_block", Position: 100, Interval: 10},
						},
						NextPageToken: "",
					}

					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(resp) //nolint:errcheck //test
				},
				"sepolia": func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				},
			},
			expectedNetworks:  []string{"mainnet"},
			expectPartialData: true,
		},
		{
			name:              "no enabled networks returns empty map",
			cartoNetworks:     map[string]*cartographoor.Network{},
			configNetworks:    []config.NetworkConfig{},
			mockServers:       map[string]func(w http.ResponseWriter, r *http.Request){},
			expectedNetworks:  []string{},
			expectPartialData: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Setup mock servers
			servers := make(map[string]*httptest.Server)
			for network, handler := range tt.mockServers {
				servers[network] = httptest.NewServer(http.HandlerFunc(handler))
				defer servers[network].Close()
			}

			// Setup cartographoor mock
			var mockProvider cartographoor.Provider

			if len(tt.cartoNetworks) > 0 {
				mock := cartomocks.NewMockProvider(ctrl)

				// Set target URLs from test servers
				networksWithURLs := make(map[string]*cartographoor.Network)

				for name, network := range tt.cartoNetworks {
					netCopy := *network
					if server, ok := servers[name]; ok {
						netCopy.TargetURL = server.URL
					}

					networksWithURLs[name] = &netCopy
				}

				mock.EXPECT().
					GetActiveNetworks(gomock.Any()).
					Return(networksWithURLs).
					AnyTimes()
				mockProvider = mock
			}

			cfg := &config.Config{
				Networks: tt.configNetworks,
				Bounds: config.BoundsConfig{
					RequestTimeout: 10 * time.Second,
				},
			}

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			svc := &Service{
				config:                cfg,
				cartographoorProvider: mockProvider,
				logger:                logger,
				httpClient:            cfg.Bounds.HTTPClient(),
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result, err := svc.FetchBounds(ctx)

			require.NoError(t, err)
			require.NotNil(t, result)

			// Verify expected networks are present
			for _, expectedNetwork := range tt.expectedNetworks {
				assert.Contains(t, result, expectedNetwork,
					fmt.Sprintf("expected network %s not found in results", expectedNetwork))
			}

			// Verify result size matches expected
			assert.Equal(t, len(tt.expectedNetworks), len(result),
				"result should contain exactly the expected networks")
		})
	}
}
