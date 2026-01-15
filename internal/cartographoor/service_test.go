package cartographoor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_FetchNetworks(t *testing.T) {
	tests := []struct {
		name          string
		mockResponse  func(w http.ResponseWriter, r *http.Request)
		expectError   bool
		errorContains string
		validateData  func(t *testing.T, networks map[string]*Network)
	}{
		{
			name: "successful fetch with valid JSON",
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				resp := CartographoorResponse{
					Networks: map[string]RawNetwork{
						"mainnet": {
							Status:  NetworkStatusActive,
							ChainID: 1,
							GenesisConfig: GenesisConfig{
								GenesisTime:  1606824000,
								GenesisDelay: 0,
							},
							Forks:       Forks{Consensus: map[string]ConsensusFork{}},
							LastUpdated: time.Unix(1706824000, 0),
						},
					},
					NetworkMetadata: map[string]NetworkMetadata{
						"mainnet": {
							DisplayName: "Ethereum Mainnet",
							Description: "Production Ethereum network",
						},
					},
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck // test.
			},
			expectError: false,
			validateData: func(t *testing.T, networks map[string]*Network) {
				t.Helper()

				require.NotNil(t, networks)
				assert.Contains(t, networks, "mainnet")

				mainnet := networks["mainnet"]
				assert.Equal(t, "mainnet", mainnet.Name)
				assert.Equal(t, "Ethereum Mainnet", mainnet.DisplayName)
				assert.Equal(t, "Production Ethereum network", mainnet.Description)
				assert.Equal(t, NetworkStatusActive, mainnet.Status)
				assert.Equal(t, int64(1), mainnet.ChainID)
				assert.Equal(t, int64(1606824000), mainnet.GenesisTime)
				assert.Contains(t, mainnet.TargetURL, "mainnet")
			},
		},
		{
			name: "fetch with missing metadata uses fallback display name",
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				resp := CartographoorResponse{
					Networks: map[string]RawNetwork{
						"sepolia": {
							Status:  NetworkStatusActive,
							ChainID: 11155111,
							GenesisConfig: GenesisConfig{
								GenesisTime: 1655733600,
							},
						},
					},
					NetworkMetadata: map[string]NetworkMetadata{},
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck // test.
			},
			expectError: false,
			validateData: func(t *testing.T, networks map[string]*Network) {
				t.Helper()

				require.Contains(t, networks, "sepolia")
				assert.Equal(t, "Sepolia", networks["sepolia"].DisplayName)
			},
		},
		{
			name: "empty response returns empty map",
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				resp := CartographoorResponse{
					Networks:        map[string]RawNetwork{},
					NetworkMetadata: map[string]NetworkMetadata{},
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck // test.
			},
			expectError: false,
			validateData: func(t *testing.T, networks map[string]*Network) {
				t.Helper()

				assert.Empty(t, networks)
			},
		},
		{
			name: "HTTP error returns error",
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectError:   true,
			errorContains: "unexpected status code: 500",
		},
		{
			name: "invalid JSON returns error",
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("invalid json")) //nolint:errcheck // test.
			},
			expectError:   true,
			errorContains: "parse JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.mockResponse))
			defer server.Close()

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			cfg := &Config{
				SourceURL:      server.URL,
				RequestTimeout: 10 * time.Second,
			}

			svc, err := New(cfg, logger)
			require.NoError(t, err)

			ctx := context.Background()
			result, err := svc.FetchNetworks(ctx)

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

func TestService_formatDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "formats single word",
			input:    "mainnet",
			expected: "Mainnet",
		},
		{
			name:     "formats hyphenated name preserves hyphens",
			input:    "ethereum-mainnet",
			expected: "Ethereum-mainnet",
		},
		{
			name:     "handles empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "already capitalized",
			input:    "Sepolia",
			expected: "Sepolia",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logrus.New()
			logger.SetOutput(io.Discard)

			svc := &Service{
				logger: logger,
			}

			result := svc.formatDisplayName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestService_constructTargetURL(t *testing.T) {
	tests := []struct {
		name        string
		networkName string
		expected    string
	}{
		{
			name:        "standard network name",
			networkName: "mainnet",
			expected:    "https://cbt-api-mainnet.analytics.production.platform.ethpandaops.io/api/v1",
		},
		{
			name:        "hyphenated network name",
			networkName: "fusaka-devnet-3",
			expected:    "https://cbt-api-fusaka-devnet-3.analytics.production.platform.ethpandaops.io/api/v1",
		},
		{
			name:        "sepolia testnet",
			networkName: "sepolia",
			expected:    "https://cbt-api-sepolia.analytics.production.platform.ethpandaops.io/api/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logrus.New()
			logger.SetOutput(io.Discard)

			svc := &Service{
				logger: logger,
			}

			result := svc.constructTargetURL(tt.networkName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestService_countActive(t *testing.T) {
	tests := []struct {
		name     string
		networks map[string]*Network
		expected int
	}{
		{
			name: "all networks active",
			networks: map[string]*Network{
				"mainnet": {Status: NetworkStatusActive},
				"sepolia": {Status: NetworkStatusActive},
			},
			expected: 2,
		},
		{
			name: "some networks inactive",
			networks: map[string]*Network{
				"mainnet": {Status: NetworkStatusActive},
				"sepolia": {Status: NetworkStatusInactive},
				"holesky": {Status: NetworkStatusActive},
			},
			expected: 2,
		},
		{
			name: "no networks active",
			networks: map[string]*Network{
				"mainnet": {Status: NetworkStatusInactive},
				"sepolia": {Status: NetworkStatusInactive},
			},
			expected: 0,
		},
		{
			name:     "empty networks map",
			networks: map[string]*Network{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logrus.New()
			logger.SetOutput(io.Discard)

			svc := &Service{
				logger: logger,
			}

			result := svc.countActive(tt.networks)
			assert.Equal(t, tt.expected, result)
		})
	}
}
