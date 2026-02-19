package wallclock

import (
	"context"
	"sync"
	"time"

	"github.com/ethpandaops/ethwallclock"
	"github.com/sirupsen/logrus"
)

// Service manages wallclock instances for multiple networks.
type Service struct {
	log      logrus.FieldLogger
	networks map[string]*Network
	mu       sync.RWMutex
}

// Network represents a single network's wallclock.
type Network struct {
	Name      string
	wallclock *ethwallclock.EthereumBeaconChain
	mu        sync.Mutex
}

// NetworkConfig represents wallclock configuration for a network.
type NetworkConfig struct {
	Name           string
	GenesisTime    time.Time
	SecondsPerSlot uint64 // Defaults to 12 if not specified
}

// New creates a new wallclock service.
func New(log logrus.FieldLogger) *Service {
	return &Service{
		log:      log.WithField("service", "wallclock"),
		networks: make(map[string]*Network),
	}
}

// Start initializes wallclock service (no-op as networks are added dynamically).
func (s *Service) Start(ctx context.Context) error {
	s.log.Info("Wallclock service started")

	return nil
}

// Stop stops all wallclock instances.
func (s *Service) Stop() error {
	s.log.Info("Stopping wallclock service")

	s.mu.Lock()
	defer s.mu.Unlock()

	for name, network := range s.networks {
		if network.wallclock != nil {
			network.wallclock.Stop()
			s.log.WithField("network", name).Debug("Stopped network wallclock")
		}
	}

	s.log.Info("Wallclock service stopped")

	return nil
}

// Name returns the service name.
func (s *Service) Name() string {
	return "wallclock"
}

// AddNetwork dynamically adds or updates a network wallclock.
func (s *Service) AddNetwork(config NetworkConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Default seconds per slot to 12 if not specified
	secondsPerSlot := config.SecondsPerSlot
	if secondsPerSlot == 0 {
		secondsPerSlot = 12
	}

	// Check if network already exists
	if _, exists := s.networks[config.Name]; exists {
		// Network already exists, no need to recreate
		s.log.WithFields(logrus.Fields{
			"network": config.Name,
			"genesis": config.GenesisTime.Format(time.RFC3339),
		}).Debug("Network wallclock already exists")

		return nil
	}

	// Create network wallclock
	network := &Network{
		Name: config.Name,
	}

	// Create the wallclock
	slotDuration := time.Second * time.Duration(secondsPerSlot)
	network.wallclock = ethwallclock.NewEthereumBeaconChain(
		config.GenesisTime,
		slotDuration,
		32, // 32 slots per epoch is constant for Ethereum
	)

	s.networks[config.Name] = network

	s.log.WithFields(logrus.Fields{
		"network":        config.Name,
		"genesis":        config.GenesisTime.Format(time.RFC3339),
		"secondsPerSlot": secondsPerSlot,
	}).Info("Initialized network wallclock")

	return nil
}

// RemoveNetwork removes a network wallclock.
func (s *Service) RemoveNetwork(networkName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, exists := s.networks[networkName]
	if !exists {
		return
	}

	if network.wallclock != nil {
		network.wallclock.Stop()
	}

	delete(s.networks, networkName)

	s.log.WithField("network", networkName).Info("Removed network wallclock")
}

// GetWallclock returns the wallclock for a specific network.
// Returns nil if the network is not found.
func (s *Service) GetWallclock(networkName string) *ethwallclock.EthereumBeaconChain {
	network := s.getNetwork(networkName)
	if network == nil {
		return nil
	}

	return network.GetWallclock()
}

// getNetwork returns the network for a specific name.
func (s *Service) getNetwork(networkName string) *Network {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.networks[networkName]
}

// CalculateSlotStartTime calculates slot_start_time for a given slot.
// Returns 0 if wallclock unavailable (caller should handle gracefully).
func (s *Service) CalculateSlotStartTime(networkName string, slot uint64) uint32 {
	wc := s.GetWallclock(networkName)
	if wc == nil {
		s.log.WithFields(logrus.Fields{
			"network": networkName,
			"slot":    slot,
		}).Debug("Wallclock not available for network")

		return 0
	}

	// Calculate slot start time using wallclock
	slotObj := wc.Slots().FromNumber(slot)
	startTime := slotObj.TimeWindow().Start()
	slotStartTimeUnix := startTime.Unix()
	slotStartTime := uint32(slotStartTimeUnix) //nolint:gosec // Safe for slot times

	s.log.WithFields(logrus.Fields{
		"network":       networkName,
		"slot":          slot,
		"slotStartTime": slotStartTime,
	}).Debug("Calculated slot start time")

	return slotStartTime
}

// GetWallclock returns the network's wallclock.
func (n *Network) GetWallclock() *ethwallclock.EthereumBeaconChain {
	n.mu.Lock()
	defer n.mu.Unlock()

	return n.wallclock
}
