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

// mustMarshal is a helper to marshal test data.
func mustMarshal(t *testing.T, v interface{}) string {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err)

	return string(data)
}
