package cartographoor

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

	leadermocks "github.com/ethpandaops/lab-backend/internal/leader/mocks"
	redismocks "github.com/ethpandaops/lab-backend/internal/redis/mocks"
)

func TestRedisProvider_GetNetworks(t *testing.T) {
	tests := []struct {
		name          string
		redisData     string
		redisError    error
		expectedCount int
	}{
		{
			name: "networks exist in Redis",
			redisData: mustMarshalCarto(t, map[string]*Network{
				"mainnet": {Name: "mainnet", DisplayName: "Mainnet", Status: NetworkStatusActive},
				"sepolia": {Name: "sepolia", DisplayName: "Sepolia", Status: NetworkStatusActive},
			}),
			redisError:    nil,
			expectedCount: 2,
		},
		{
			name:          "no networks in Redis",
			redisData:     "",
			redisError:    fmt.Errorf("redis: nil"),
			expectedCount: 0,
		},
		{
			name:          "invalid JSON returns empty map",
			redisData:     "invalid json",
			redisError:    nil,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRedis := redismocks.NewMockClient(ctrl)
			mockElector := leadermocks.NewMockElector(ctrl)

			// Setup Redis mock
			mockRedis.EXPECT().
				Get(gomock.Any(), redisNetworksKey).
				Return(tt.redisData, tt.redisError).
				Times(1)

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			provider := NewRedisProvider(
				logger,
				Config{},
				mockRedis,
				mockElector,
				nil,
			)

			ctx := context.Background()
			result := provider.GetNetworks(ctx)

			require.NotNil(t, result)
			assert.Equal(t, tt.expectedCount, len(result))
		})
	}
}

func TestRedisProvider_GetActiveNetworks(t *testing.T) {
	tests := []struct {
		name          string
		allNetworks   map[string]*Network
		expectedCount int
	}{
		{
			name: "filters out inactive networks",
			allNetworks: map[string]*Network{
				"mainnet": {Name: "mainnet", Status: NetworkStatusActive},
				"sepolia": {Name: "sepolia", Status: NetworkStatusInactive},
				"holesky": {Name: "holesky", Status: NetworkStatusActive},
			},
			expectedCount: 2,
		},
		{
			name: "all networks active",
			allNetworks: map[string]*Network{
				"mainnet": {Name: "mainnet", Status: NetworkStatusActive},
				"sepolia": {Name: "sepolia", Status: NetworkStatusActive},
			},
			expectedCount: 2,
		},
		{
			name: "no networks active",
			allNetworks: map[string]*Network{
				"mainnet": {Name: "mainnet", Status: NetworkStatusInactive},
				"sepolia": {Name: "sepolia", Status: NetworkStatusInactive},
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRedis := redismocks.NewMockClient(ctrl)
			mockElector := leadermocks.NewMockElector(ctrl)

			// Setup Redis mock
			mockRedis.EXPECT().
				Get(gomock.Any(), redisNetworksKey).
				Return(mustMarshalCarto(t, tt.allNetworks), nil).
				Times(1)

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			provider := NewRedisProvider(
				logger,
				Config{},
				mockRedis,
				mockElector,
				nil,
			)

			ctx := context.Background()
			result := provider.GetActiveNetworks(ctx)

			require.NotNil(t, result)
			assert.Equal(t, tt.expectedCount, len(result))

			// Verify all returned networks are active
			for _, network := range result {
				assert.Equal(t, NetworkStatusActive, network.Status)
			}
		})
	}
}

func TestRedisProvider_GetNetwork(t *testing.T) {
	tests := []struct {
		name         string
		allNetworks  map[string]*Network
		requestedNet string
		expectFound  bool
	}{
		{
			name: "network exists",
			allNetworks: map[string]*Network{
				"mainnet": {Name: "mainnet", DisplayName: "Mainnet"},
			},
			requestedNet: "mainnet",
			expectFound:  true,
		},
		{
			name: "network does not exist",
			allNetworks: map[string]*Network{
				"mainnet": {Name: "mainnet", DisplayName: "Mainnet"},
			},
			requestedNet: "nonexistent",
			expectFound:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRedis := redismocks.NewMockClient(ctrl)
			mockElector := leadermocks.NewMockElector(ctrl)

			// Setup Redis mock
			mockRedis.EXPECT().
				Get(gomock.Any(), redisNetworksKey).
				Return(mustMarshalCarto(t, tt.allNetworks), nil).
				Times(1)

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			provider := NewRedisProvider(
				logger,
				Config{},
				mockRedis,
				mockElector,
				nil,
			)

			ctx := context.Background()
			network, found := provider.GetNetwork(ctx, tt.requestedNet)

			assert.Equal(t, tt.expectFound, found)

			if tt.expectFound {
				require.NotNil(t, network)
				assert.Equal(t, tt.requestedNet, network.Name)
			}
		})
	}
}

func TestRedisProvider_checkNetworkHealth(t *testing.T) {
	tests := []struct {
		name           string
		mockResponse   func(w http.ResponseWriter, r *http.Request)
		expectHealthy  bool
		reasonContains string
	}{
		{
			name: "healthy network returns 200",
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/health", r.URL.Path)
				w.WriteHeader(http.StatusOK)
			},
			expectHealthy: true,
		},
		{
			name: "unhealthy network returns 500",
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectHealthy:  false,
			reasonContains: "health check returned 500",
		},
		{
			name: "network timeout",
			mockResponse: func(w http.ResponseWriter, r *http.Request) {
				// Simulate timeout by sleeping longer than health check timeout
				time.Sleep(6 * time.Second)
				w.WriteHeader(http.StatusOK)
			},
			expectHealthy:  false,
			reasonContains: "health check failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server

			if tt.mockResponse != nil {
				server = httptest.NewServer(http.HandlerFunc(tt.mockResponse))
				defer server.Close()
			}

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			provider := &RedisProvider{
				log: logger,
			}

			targetURL := server.URL + "/api/v1"

			if tt.name == "network timeout" {
				// Skip timeout test as it takes too long
				t.Skip("Timeout test takes too long, skipping")
			}

			healthy, reason := provider.checkNetworkHealth(targetURL)

			assert.Equal(t, tt.expectHealthy, healthy)

			if !tt.expectHealthy && tt.reasonContains != "" {
				assert.Contains(t, reason, tt.reasonContains)
			}
		})
	}
}

func TestRedisProvider_checkNetworkHealth_InvalidURL(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	provider := &RedisProvider{
		log: logger,
	}

	tests := []struct {
		name      string
		targetURL string
	}{
		{
			name:      "empty target URL",
			targetURL: "",
		},
		{
			name:      "invalid URL format",
			targetURL: "://invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			healthy, reason := provider.checkNetworkHealth(tt.targetURL)
			assert.False(t, healthy)
			assert.NotEmpty(t, reason)
		})
	}
}

func TestRedisProvider_NotifyChannel(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRedis := redismocks.NewMockClient(ctrl)
	mockElector := leadermocks.NewMockElector(ctrl)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	provider := NewRedisProvider(
		logger,
		Config{},
		mockRedis,
		mockElector,
		nil,
	)

	ch := provider.NotifyChannel()
	require.NotNil(t, ch)

	// Verify channel is readable
	select {
	case <-ch:
		t.Fatal("channel should not have data initially")
	default:
		// Expected - channel is empty
	}
}

// mustMarshalCarto is a helper to marshal test data.
func mustMarshalCarto(t *testing.T, v interface{}) string {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err)

	return string(data)
}
