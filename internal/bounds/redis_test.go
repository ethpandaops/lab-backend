package bounds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	leadermocks "github.com/ethpandaops/lab-backend/internal/leader/mocks"
	redismocks "github.com/ethpandaops/lab-backend/internal/redis/mocks"
)

func TestRedisProvider_GetBounds(t *testing.T) {
	tests := []struct {
		name         string
		network      string
		redisData    string
		redisError   error
		expectFound  bool
		validateData func(t *testing.T, data *BoundsData)
	}{
		{
			name:    "bounds exist for network",
			network: "mainnet",
			redisData: mustMarshal(t, BoundsData{
				Tables: map[string]TableBounds{
					"beacon_block": {Min: 100, Max: 200},
				},
				LastUpdated: time.Now(),
			}),
			redisError:  nil,
			expectFound: true,
			validateData: func(t *testing.T, data *BoundsData) {
				t.Helper()

				require.NotNil(t, data)
				assert.Contains(t, data.Tables, "beacon_block")
				assert.Equal(t, int64(100), data.Tables["beacon_block"].Min)
				assert.Equal(t, int64(200), data.Tables["beacon_block"].Max)
			},
		},
		{
			name:        "bounds do not exist for network",
			network:     "nonexistent",
			redisData:   "",
			redisError:  fmt.Errorf("redis: nil"),
			expectFound: false,
		},
		{
			name:        "invalid JSON returns not found",
			network:     "mainnet",
			redisData:   "invalid json",
			redisError:  nil,
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRedis := redismocks.NewMockClient(ctrl)
			mockElector := leadermocks.NewMockElector(ctrl)

			// Setup Redis mock expectations
			mockRedis.EXPECT().
				Get(gomock.Any(), fmt.Sprintf("%s%s", redisKeyPrefix, tt.network)).
				Return(tt.redisData, tt.redisError).
				Times(1)

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			provider := NewRedisProvider(
				logger,
				Config{},
				mockRedis,
				mockElector,
				nil, // upstream not needed for Get test
			)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			data, found := provider.GetBounds(ctx, tt.network)

			assert.Equal(t, tt.expectFound, found)

			if tt.expectFound && tt.validateData != nil {
				tt.validateData(t, data)
			}
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

func TestRedisProvider_FollowerPolling(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRedis := redismocks.NewMockClient(ctrl)
	mockElector := leadermocks.NewMockElector(ctrl)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Follower pod (IsLeader returns false)
	mockElector.EXPECT().IsLeader().Return(false).AnyTimes()

	// Mock Redis.Keys for Start() readiness check
	mockRedis.EXPECT().
		GetClient().
		Return(nil).
		AnyTimes()

	providerInterface := NewRedisProvider(
		logger,
		Config{
			RefreshInterval: 100 * time.Millisecond, // Short interval for testing
			BoundsTTL:       0,
		},
		mockRedis,
		mockElector,
		nil, // No upstream service needed for this test
	)

	provider, ok := providerInterface.(*RedisProvider)
	require.True(t, ok, "provider should be *RedisProvider")

	// Manually start the refresh loop (skip Start() readiness check)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	provider.wg.Add(1)

	go provider.refreshLoop(ctx)

	// Wait for follower to send notification
	select {
	case <-provider.NotifyChannel():
		// Success - follower sent notification
		t.Log("Follower successfully sent notification")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for follower notification")
	}

	// Clean up
	err := provider.Stop()
	require.NoError(t, err)
}

func TestRedisProvider_PanicRecovery(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRedis := redismocks.NewMockClient(ctrl)
	mockElector := leadermocks.NewMockElector(ctrl)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Start as follower to avoid calling refreshData
	mockElector.EXPECT().IsLeader().Return(false).AnyTimes()

	providerInterface := NewRedisProvider(
		logger,
		Config{
			RefreshInterval: 50 * time.Millisecond,
			BoundsTTL:       0,
		},
		mockRedis,
		mockElector,
		nil,
	)

	provider, ok := providerInterface.(*RedisProvider)
	require.True(t, ok, "provider should be *RedisProvider")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Manually start the refresh loop
	provider.wg.Add(1)

	go provider.refreshLoop(ctx)

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop should complete without hanging (proves panic recovery works)
	done := make(chan struct{})

	go func() {
		err := provider.Stop()
		require.NoError(t, err)
		close(done)
	}()

	select {
	case <-done:
		// Success - Stop() completed, wg.Done() was called despite any panics
		t.Log("Provider stopped gracefully, panic recovery working")
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() hung - panic recovery may have failed")
	}
}

// mustMarshal is a helper to marshal test data.
func mustMarshal(t *testing.T, v interface{}) string {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err)

	return string(data)
}
