package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	cartomocks "github.com/ethpandaops/lab-backend/internal/cartographoor/mocks"
	"github.com/ethpandaops/lab-backend/internal/config"
)

func TestProxy_AddNetwork(t *testing.T) {
	tests := []struct {
		name        string
		network     config.NetworkConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid network added successfully",
			network: config.NetworkConfig{
				Name:      "mainnet",
				TargetURL: "http://localhost:8080",
			},
			expectError: false,
		},
		{
			name: "network with https URL",
			network: config.NetworkConfig{
				Name:      "sepolia",
				TargetURL: "https://example.com/api",
			},
			expectError: false,
		},
		{
			name: "invalid target URL rejected",
			network: config.NetworkConfig{
				Name:      "invalid",
				TargetURL: "://invalid-url",
			},
			expectError: true,
			errorMsg:    "invalid target URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logrus.New()
			logger.SetOutput(io.Discard)

			cfg := &config.Config{}

			p := &Proxy{
				config:    cfg,
				proxies:   make(map[string]*httputil.ReverseProxy),
				proxyURLs: make(map[string]string),
				logger:    logger,
			}

			err := p.AddNetwork(tt.network)

			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Contains(t, p.proxies, tt.network.Name)
				assert.Equal(t, tt.network.TargetURL, p.proxyURLs[tt.network.Name])
			}
		})
	}
}

func TestProxy_RemoveNetwork(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := &config.Config{}

	p := &Proxy{
		config:    cfg,
		proxies:   make(map[string]*httputil.ReverseProxy),
		proxyURLs: make(map[string]string),
		logger:    logger,
	}

	// Add a network first
	network := config.NetworkConfig{
		Name:      "mainnet",
		TargetURL: "http://localhost:8080",
	}

	err := p.AddNetwork(network)
	require.NoError(t, err)
	require.Contains(t, p.proxies, "mainnet")

	// Remove the network
	p.RemoveNetwork("mainnet")

	// Verify it's gone
	assert.NotContains(t, p.proxies, "mainnet")
	assert.NotContains(t, p.proxyURLs, "mainnet")
}

func TestProxy_UpdateNetwork(t *testing.T) {
	tests := []struct {
		name         string
		initialURL   string
		updatedURL   string
		expectUpdate bool
		expectError  bool
	}{
		{
			name:         "URL change updates proxy",
			initialURL:   "http://localhost:8080",
			updatedURL:   "http://localhost:9090",
			expectUpdate: true,
			expectError:  false,
		},
		{
			name:         "same URL skips update",
			initialURL:   "http://localhost:8080",
			updatedURL:   "http://localhost:8080",
			expectUpdate: false,
			expectError:  false,
		},
		{
			name:         "invalid URL returns error",
			initialURL:   "http://localhost:8080",
			updatedURL:   "://invalid",
			expectUpdate: false,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logrus.New()
			logger.SetOutput(io.Discard)

			cfg := &config.Config{}

			p := &Proxy{
				config:    cfg,
				proxies:   make(map[string]*httputil.ReverseProxy),
				proxyURLs: make(map[string]string),
				logger:    logger,
			}

			// Add initial network
			initial := config.NetworkConfig{
				Name:      "mainnet",
				TargetURL: tt.initialURL,
			}

			err := p.AddNetwork(initial)
			require.NoError(t, err)

			// Update network
			updated := config.NetworkConfig{
				Name:      "mainnet",
				TargetURL: tt.updatedURL,
			}

			err = p.UpdateNetwork(updated)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				if tt.expectUpdate {
					assert.Equal(t, tt.updatedURL, p.proxyURLs["mainnet"])
				} else {
					assert.Equal(t, tt.initialURL, p.proxyURLs["mainnet"])
				}
			}
		})
	}
}

func TestProxy_ServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		requestPath    string
		networkExists  bool
		backendStatus  int
		backendBody    string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "valid request proxied correctly",
			requestPath:    "/api/v1/mainnet/bounds",
			networkExists:  true,
			backendStatus:  http.StatusOK,
			backendBody:    `{"min":100,"max":200}`,
			expectedStatus: http.StatusOK,
			expectedBody:   `{"min":100,"max":200}`,
		},
		{
			name:           "network not found returns 404",
			requestPath:    "/api/v1/nonexistent/bounds",
			networkExists:  false,
			expectedStatus: http.StatusNotFound,
			expectedBody:   "network not found",
		},
		{
			name:           "invalid path returns 400",
			requestPath:    "/invalid",
			networkExists:  false,
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "invalid path format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logrus.New()
			logger.SetOutput(io.Discard)

			cfg := &config.Config{}

			// Create backend server if network exists
			var backend *httptest.Server
			if tt.networkExists {
				backend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.backendStatus)
					w.Write([]byte(tt.backendBody)) //nolint:errcheck // test
				}))
				defer backend.Close()
			}

			p := &Proxy{
				config:    cfg,
				proxies:   make(map[string]*httputil.ReverseProxy),
				proxyURLs: make(map[string]string),
				logger:    logger,
			}

			// Add network if it should exist
			if tt.networkExists {
				network := config.NetworkConfig{
					Name:      "mainnet",
					TargetURL: backend.URL,
				}
				err := p.AddNetwork(network)
				require.NoError(t, err)
			}

			// Create request
			req := httptest.NewRequest(http.MethodGet, tt.requestPath, http.NoBody)
			rec := httptest.NewRecorder()

			// Serve request
			p.ServeHTTP(rec, req)

			// Assert status code
			assert.Equal(t, tt.expectedStatus, rec.Code)

			// Assert body contains expected content
			if tt.expectedBody != "" {
				assert.Contains(t, rec.Body.String(), tt.expectedBody)
			}
		})
	}
}

func TestProxy_SyncNetworks(t *testing.T) {
	tests := []struct {
		name             string
		initialNetworks  []string
		cartoNetworks    map[string]*cartographoor.Network
		expectedNetworks []string
	}{
		{
			name:            "add new networks",
			initialNetworks: []string{},
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:      "mainnet",
					TargetURL: "http://mainnet.example.com",
					Status:    cartographoor.NetworkStatusActive,
				},
				"sepolia": {
					Name:      "sepolia",
					TargetURL: "http://sepolia.example.com",
					Status:    cartographoor.NetworkStatusActive,
				},
			},
			expectedNetworks: []string{"mainnet", "sepolia"},
		},
		{
			name:            "remove networks no longer in config",
			initialNetworks: []string{"mainnet", "sepolia", "old-network"},
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:      "mainnet",
					TargetURL: "http://mainnet.example.com",
					Status:    cartographoor.NetworkStatusActive,
				},
			},
			expectedNetworks: []string{"mainnet"},
		},
		{
			name:            "update existing network URLs",
			initialNetworks: []string{"mainnet"},
			cartoNetworks: map[string]*cartographoor.Network{
				"mainnet": {
					Name:      "mainnet",
					TargetURL: "http://new-mainnet.example.com",
					Status:    cartographoor.NetworkStatusActive,
				},
			},
			expectedNetworks: []string{"mainnet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			mockProvider := cartomocks.NewMockProvider(ctrl)

			// Setup mock to return cartographoor networks
			mockProvider.EXPECT().
				GetActiveNetworks(gomock.Any()).
				Return(tt.cartoNetworks).
				Times(1)

			cfg := &config.Config{}

			p := &Proxy{
				config:    cfg,
				proxies:   make(map[string]*httputil.ReverseProxy),
				proxyURLs: make(map[string]string),
				logger:    logger,
				provider:  mockProvider,
			}

			// Add initial networks
			for _, name := range tt.initialNetworks {
				network := config.NetworkConfig{
					Name:      name,
					TargetURL: "http://" + name + ".example.com",
				}
				err := p.AddNetwork(network)
				require.NoError(t, err)
			}

			// Sync networks
			ctx := context.Background()
			err := p.SyncNetworks(ctx)
			require.NoError(t, err)

			// Verify expected networks exist
			for _, expectedName := range tt.expectedNetworks {
				assert.Contains(t, p.proxies, expectedName,
					"expected network %s not found", expectedName)
			}

			// Verify only expected networks exist
			assert.Equal(t, len(tt.expectedNetworks), len(p.proxies),
				"proxy should have exactly %d networks", len(tt.expectedNetworks))
		})
	}
}

func TestProxy_NetworkCount(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := &config.Config{}

	p := &Proxy{
		config:    cfg,
		proxies:   make(map[string]*httputil.ReverseProxy),
		proxyURLs: make(map[string]string),
		logger:    logger,
	}

	// Initially empty
	assert.Equal(t, 0, p.NetworkCount())

	// Add networks
	for i := 0; i < 3; i++ {
		network := config.NetworkConfig{
			Name:      "network-" + string(rune('a'+i)),
			TargetURL: "http://localhost:8080",
		}
		err := p.AddNetwork(network)
		require.NoError(t, err)
	}

	assert.Equal(t, 3, p.NetworkCount())

	// Remove one
	p.RemoveNetwork("network-a")
	assert.Equal(t, 2, p.NetworkCount())
}

func TestProxy_ConcurrentAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Create backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck // test
	}))
	defer backend.Close()

	cfg := &config.Config{}

	p := &Proxy{
		config:    cfg,
		proxies:   make(map[string]*httputil.ReverseProxy),
		proxyURLs: make(map[string]string),
		logger:    logger,
	}

	// Add network
	network := config.NetworkConfig{
		Name:      "mainnet",
		TargetURL: backend.URL,
	}
	err := p.AddNetwork(network)
	require.NoError(t, err)

	// Spawn multiple concurrent requests
	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/mainnet/bounds", http.NoBody)
			rec := httptest.NewRecorder()

			p.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)

			done <- true
		}()
	}

	// Wait for all requests
	for i := 0; i < 100; i++ {
		<-done
	}
}
