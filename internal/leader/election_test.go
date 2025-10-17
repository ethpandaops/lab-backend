package leader

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	redismocks "github.com/ethpandaops/lab-backend/internal/redis/mocks"
)

func TestElector_AcquireLeadership(t *testing.T) {
	tests := []struct {
		name           string
		setNXResult    bool
		setNXError     error
		expectedLeader bool
	}{
		{
			name:           "successfully acquires leadership",
			setNXResult:    true,
			setNXError:     nil,
			expectedLeader: true,
		},
		{
			name:           "fails to acquire leadership (lock held by another)",
			setNXResult:    false,
			setNXError:     nil,
			expectedLeader: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRedis := redismocks.NewMockClient(ctrl)

			// Mock SetNX call
			mockRedis.EXPECT().
				SetNX(gomock.Any(), "test-lock", gomock.Any(), 10*time.Second).
				Return(tt.setNXResult, tt.setNXError).
				Times(1)

			// If acquisition fails, expect Get call for current leader ID logging
			if !tt.setNXResult && tt.setNXError == nil {
				mockRedis.EXPECT().
					Get(gomock.Any(), "test-lock").
					Return("other-instance-id", nil).
					Times(1)
			}

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			cfg := Config{
				LockKey:       "test-lock",
				LockTTL:       10 * time.Second,
				RenewInterval: 3 * time.Second,
				RetryInterval: 2 * time.Second,
			}

			elector := NewElector(logger, cfg, mockRedis).(*elector) //nolint:errcheck // type assertion in test

			// Manually call tryAcquireLeadership to test it
			ctx := context.Background()
			elector.tryAcquireLeadership(ctx)

			assert.Equal(t, tt.expectedLeader, elector.IsLeader())
		})
	}
}

func TestElector_LeadershipRenewal(t *testing.T) {
	tests := []struct {
		name                string
		currentHolder       string
		getError            error
		setError            error
		expectedLeaderAfter bool
	}{
		{
			name:                "successfully renews leadership",
			currentHolder:       "same-id",
			getError:            nil,
			setError:            nil,
			expectedLeaderAfter: true,
		},
		{
			name:                "loses leadership (different holder)",
			currentHolder:       "different-id",
			getError:            nil,
			setError:            nil,
			expectedLeaderAfter: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRedis := redismocks.NewMockClient(ctrl)

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			cfg := Config{
				LockKey:       "test-lock",
				LockTTL:       10 * time.Second,
				RenewInterval: 3 * time.Second,
				RetryInterval: 2 * time.Second,
			}

			elector := NewElector(logger, cfg, mockRedis).(*elector) //nolint:errcheck // type assertion in test

			// Set elector as leader and capture its ID
			elector.isLeader = true
			instanceID := elector.id

			// Determine what ID Get should return
			getID := tt.currentHolder
			if tt.currentHolder == "same-id" {
				getID = instanceID
			}

			// Mock Get call
			mockRedis.EXPECT().
				Get(gomock.Any(), "test-lock").
				Return(getID, tt.getError).
				Times(1)

			// If same holder and no errors, expect Set call
			if tt.currentHolder == "same-id" && tt.getError == nil && tt.setError == nil {
				mockRedis.EXPECT().
					Set(gomock.Any(), "test-lock", instanceID, 10*time.Second).
					Return(tt.setError).
					Times(1)
			}

			ctx := context.Background()
			elector.renewLeadership(ctx)

			assert.Equal(t, tt.expectedLeaderAfter, elector.IsLeader())
		})
	}
}

func TestElector_RenewalFailures(t *testing.T) {
	tests := []struct {
		name                string
		getError            error
		setError            error
		expectedLeaderAfter bool
	}{
		{
			name:                "loses leadership on Get error",
			getError:            assert.AnError,
			setError:            nil,
			expectedLeaderAfter: false,
		},
		{
			name:                "loses leadership on Set error",
			getError:            nil,
			setError:            assert.AnError,
			expectedLeaderAfter: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRedis := redismocks.NewMockClient(ctrl)

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			cfg := Config{
				LockKey:       "test-lock",
				LockTTL:       10 * time.Second,
				RenewInterval: 3 * time.Second,
				RetryInterval: 2 * time.Second,
			}

			elector := NewElector(logger, cfg, mockRedis).(*elector) //nolint:errcheck // type assertion in test
			elector.isLeader = true
			instanceID := elector.id

			// Mock Get call
			mockRedis.EXPECT().
				Get(gomock.Any(), "test-lock").
				Return(instanceID, tt.getError).
				Times(1)

			// If Get succeeds, expect Set call with error
			if tt.getError == nil {
				mockRedis.EXPECT().
					Set(gomock.Any(), "test-lock", instanceID, 10*time.Second).
					Return(tt.setError).
					Times(1)
			}

			ctx := context.Background()
			elector.renewLeadership(ctx)

			assert.Equal(t, tt.expectedLeaderAfter, elector.IsLeader())
		})
	}
}

func TestElector_IsLeader(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRedis := redismocks.NewMockClient(ctrl)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := Config{
		LockKey:       "test-lock",
		LockTTL:       10 * time.Second,
		RenewInterval: 3 * time.Second,
		RetryInterval: 2 * time.Second,
	}

	elector := NewElector(logger, cfg, mockRedis).(*elector) //nolint:errcheck // type assertion in test

	// Initially not leader
	assert.False(t, elector.IsLeader())

	// Become leader
	elector.mu.Lock()
	elector.isLeader = true
	elector.mu.Unlock()

	assert.True(t, elector.IsLeader())

	// Lose leadership
	elector.mu.Lock()
	elector.isLeader = false
	elector.mu.Unlock()

	assert.False(t, elector.IsLeader())
}

func TestElector_StartStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRedis := redismocks.NewMockClient(ctrl)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := Config{
		LockKey:       "test-lock",
		LockTTL:       10 * time.Second,
		RenewInterval: 100 * time.Millisecond,
		RetryInterval: 100 * time.Millisecond,
	}

	// Mock initial SetNX attempt (fails)
	mockRedis.EXPECT().
		SetNX(gomock.Any(), "test-lock", gomock.Any(), gomock.Any()).
		Return(false, nil).
		AnyTimes()

	// Mock Get for logging current leader
	mockRedis.EXPECT().
		Get(gomock.Any(), "test-lock").
		Return("other-id", nil).
		AnyTimes()

	elector := NewElector(logger, cfg, mockRedis)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start elector
	err := elector.Start(ctx)
	require.NoError(t, err)

	// Wait a bit to let election loop run
	time.Sleep(300 * time.Millisecond)

	// Stop elector
	err = elector.Stop()
	require.NoError(t, err)
}

func TestElector_StopWhileLeader(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRedis := redismocks.NewMockClient(ctrl)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := Config{
		LockKey:       "test-lock",
		LockTTL:       10 * time.Second,
		RenewInterval: 100 * time.Millisecond,
		RetryInterval: 100 * time.Millisecond,
	}

	// Expect Del call when stopping as leader
	mockRedis.EXPECT().
		Del(gomock.Any(), "test-lock").
		Return(nil).
		Times(1)

	e := NewElector(logger, cfg, mockRedis).(*elector) //nolint:errcheck // type assertion in test

	// Manually set as leader (without starting election loop)
	e.mu.Lock()
	e.isLeader = true
	e.mu.Unlock()

	// Verify is leader
	assert.True(t, e.IsLeader())

	// Stop elector (should release lock)
	err := e.Stop()
	require.NoError(t, err)

	// Verify no longer leader
	assert.False(t, e.IsLeader())
}

func TestElector_ConcurrentAccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRedis := redismocks.NewMockClient(ctrl)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := Config{
		LockKey:       "test-lock",
		LockTTL:       10 * time.Second,
		RenewInterval: 3 * time.Second,
		RetryInterval: 2 * time.Second,
	}

	elector := NewElector(logger, cfg, mockRedis).(*elector) //nolint:errcheck // type assertion in test

	// Set as leader
	elector.isLeader = true

	// Concurrently read IsLeader multiple times
	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func() {
			// This should not race
			_ = elector.IsLeader()

			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}
}

func TestElector_LeadershipTransitions(t *testing.T) {
	tests := []struct {
		name           string
		setNXResults   []bool // Sequence of SetNX return values
		expectedStates []bool // Expected IsLeader() values after each attempt
	}{
		{
			name:           "acquire then maintain leadership",
			setNXResults:   []bool{true, true, true},
			expectedStates: []bool{true, true, true},
		},
		{
			name:           "fail to acquire leadership",
			setNXResults:   []bool{false, false, false},
			expectedStates: []bool{false, false, false},
		},
		{
			name:           "gain leadership after retries",
			setNXResults:   []bool{false, false, true},
			expectedStates: []bool{false, false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockRedis := redismocks.NewMockClient(ctrl)

			logger := logrus.New()
			logger.SetOutput(io.Discard)

			cfg := Config{
				LockKey:       "test-lock",
				LockTTL:       10 * time.Second,
				RenewInterval: 3 * time.Second,
				RetryInterval: 2 * time.Second,
			}

			elector := NewElector(logger, cfg, mockRedis).(*elector) //nolint:errcheck // type assertion in test

			ctx := context.Background()

			// Track whether we've had a failure yet (Get call only happens on FIRST failure)
			hadFailure := false

			for i, setNXResult := range tt.setNXResults {
				// Mock SetNX call
				mockRedis.EXPECT().
					SetNX(gomock.Any(), "test-lock", gomock.Any(), 10*time.Second).
					Return(setNXResult, nil).
					Times(1)

				// If acquisition fails AND this is the first failure since last success,
				// expect Get call for logging (due to loggedFollower flag)
				if !setNXResult && !hadFailure {
					mockRedis.EXPECT().
						Get(gomock.Any(), "test-lock").
						Return("other-instance-id", nil).
						Times(1)

					hadFailure = true
				} else if setNXResult {
					// Reset failure flag on success
					hadFailure = false
				}

				// Attempt to acquire leadership
				elector.tryAcquireLeadership(ctx)

				// Check expected state
				assert.Equal(t, tt.expectedStates[i], elector.IsLeader(),
					"state mismatch at attempt %d", i)
			}
		})
	}
}
