package wallclock

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_New(t *testing.T) {
	logger := logrus.New()
	svc := New(logger)

	assert.NotNil(t, svc)
	assert.NotNil(t, svc.log)
	assert.NotNil(t, svc.networks)
	assert.Equal(t, 0, len(svc.networks))
}

func TestService_Start(t *testing.T) {
	logger := logrus.New()
	svc := New(logger)

	ctx := context.Background()
	err := svc.Start(ctx)

	require.NoError(t, err)
}

func TestService_AddNetwork(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise in tests
	svc := New(logger)

	// Mainnet genesis time: Dec 1, 2020
	genesisTime := time.Unix(1606824023, 0)

	err := svc.AddNetwork(NetworkConfig{
		Name:           "mainnet",
		GenesisTime:    genesisTime,
		SecondsPerSlot: 12,
	})

	require.NoError(t, err)
	assert.Equal(t, 1, len(svc.networks))

	// Verify network exists
	network := svc.getNetwork("mainnet")
	assert.NotNil(t, network)
	assert.Equal(t, "mainnet", network.Name)
	assert.NotNil(t, network.wallclock)
}

func TestService_AddNetwork_DefaultSecondsPerSlot(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := New(logger)

	genesisTime := time.Unix(1606824023, 0)

	err := svc.AddNetwork(NetworkConfig{
		Name:        "mainnet",
		GenesisTime: genesisTime,
		// SecondsPerSlot not specified, should default to 12
	})

	require.NoError(t, err)

	// Verify wallclock was created
	wc := svc.GetWallclock("mainnet")
	assert.NotNil(t, wc)
}

func TestService_AddNetwork_Duplicate(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := New(logger)

	genesisTime := time.Unix(1606824023, 0)

	// Add network twice
	err := svc.AddNetwork(NetworkConfig{
		Name:           "mainnet",
		GenesisTime:    genesisTime,
		SecondsPerSlot: 12,
	})
	require.NoError(t, err)

	err = svc.AddNetwork(NetworkConfig{
		Name:           "mainnet",
		GenesisTime:    genesisTime,
		SecondsPerSlot: 12,
	})
	require.NoError(t, err)

	// Should only have one network
	assert.Equal(t, 1, len(svc.networks))
}

func TestService_RemoveNetwork(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := New(logger)

	genesisTime := time.Unix(1606824023, 0)

	err := svc.AddNetwork(NetworkConfig{
		Name:           "mainnet",
		GenesisTime:    genesisTime,
		SecondsPerSlot: 12,
	})
	require.NoError(t, err)

	// Remove network
	svc.RemoveNetwork("mainnet")

	assert.Equal(t, 0, len(svc.networks))

	// Verify network no longer exists
	network := svc.getNetwork("mainnet")
	assert.Nil(t, network)
}

func TestService_RemoveNetwork_NotFound(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := New(logger)

	// Remove network that doesn't exist (should not panic)
	svc.RemoveNetwork("nonexistent")

	assert.Equal(t, 0, len(svc.networks))
}

func TestService_GetWallclock(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := New(logger)

	genesisTime := time.Unix(1606824023, 0)

	err := svc.AddNetwork(NetworkConfig{
		Name:           "mainnet",
		GenesisTime:    genesisTime,
		SecondsPerSlot: 12,
	})
	require.NoError(t, err)

	// Get wallclock
	wc := svc.GetWallclock("mainnet")
	assert.NotNil(t, wc)
}

func TestService_GetWallclock_NotFound(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := New(logger)

	// Get wallclock for non-existent network
	wc := svc.GetWallclock("nonexistent")
	assert.Nil(t, wc)
}

func TestService_CalculateSlotStartTime(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := New(logger)

	// Mainnet genesis time: Dec 1, 2020, 12:00:23 UTC
	genesisTime := time.Unix(1606824023, 0)

	err := svc.AddNetwork(NetworkConfig{
		Name:           "mainnet",
		GenesisTime:    genesisTime,
		SecondsPerSlot: 12,
	})
	require.NoError(t, err)

	tests := []struct {
		name             string
		slot             uint64
		expectedUnixTime int64
	}{
		{
			name:             "slot 0",
			slot:             0,
			expectedUnixTime: 1606824023, // Genesis time
		},
		{
			name:             "slot 1",
			slot:             1,
			expectedUnixTime: 1606824035, // Genesis + 12 seconds
		},
		{
			name:             "slot 100",
			slot:             100,
			expectedUnixTime: 1606825223, // Genesis + 1200 seconds
		},
		{
			name:             "slot 1000",
			slot:             1000,
			expectedUnixTime: 1606836023, // Genesis + 12000 seconds
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slotStartTime := svc.CalculateSlotStartTime("mainnet", tt.slot)

			assert.Equal(t, uint32(tt.expectedUnixTime), slotStartTime)
		})
	}
}

func TestService_CalculateSlotStartTime_NetworkNotFound(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := New(logger)

	// Calculate for non-existent network (should return 0)
	slotStartTime := svc.CalculateSlotStartTime("nonexistent", 1000)

	assert.Equal(t, uint32(0), slotStartTime)
}

func TestService_Stop(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := New(logger)

	genesisTime := time.Unix(1606824023, 0)

	err := svc.AddNetwork(NetworkConfig{
		Name:           "mainnet",
		GenesisTime:    genesisTime,
		SecondsPerSlot: 12,
	})
	require.NoError(t, err)

	// Stop service
	err = svc.Stop()
	require.NoError(t, err)
}

func TestService_Concurrent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := New(logger)

	genesisTime := time.Unix(1606824023, 0)

	// Add initial network
	err := svc.AddNetwork(NetworkConfig{
		Name:           "mainnet",
		GenesisTime:    genesisTime,
		SecondsPerSlot: 12,
	})
	require.NoError(t, err)

	// Test concurrent access
	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_ = svc.GetWallclock("mainnet")
			_ = svc.CalculateSlotStartTime("mainnet", 1000)
		}()
	}

	// Concurrent network additions
	for i := 0; i < 5; i++ {
		wg.Add(1)

		networkName := "testnet"

		go func(name string) {
			defer wg.Done()

			_ = svc.AddNetwork(NetworkConfig{
				Name:           name,
				GenesisTime:    genesisTime,
				SecondsPerSlot: 12,
			})
		}(networkName)
	}

	wg.Wait()

	// Verify no races occurred
	assert.NotNil(t, svc.GetWallclock("mainnet"))
}

func TestService_Name(t *testing.T) {
	logger := logrus.New()
	svc := New(logger)

	assert.Equal(t, "wallclock", svc.Name())
}
