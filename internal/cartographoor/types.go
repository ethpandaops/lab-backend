package cartographoor

//go:generate mockgen -package mocks -destination mocks/mock_provider.go github.com/ethpandaops/lab-backend/internal/cartographoor Provider

import (
	"context"
	"time"
)

const (
	NetworkStatusActive   = "active"
	NetworkStatusInactive = "inactive"
)

// CartographoorResponse represents the top-level JSON structure from networks.json.
// Only parses fields we actually use - ignores clients, providers, timestamps, etc.
type CartographoorResponse struct {
	Networks        map[string]RawNetwork      `json:"networks"`
	NetworkMetadata map[string]NetworkMetadata `json:"networkMetadata"`
}

// RawNetwork represents a network entry in the cartographoor JSON.
// Only parses essential fields - network name comes from map key.
type RawNetwork struct {
	Status        string        `json:"status"`
	ChainID       int64         `json:"chainId"`
	LastUpdated   time.Time     `json:"lastUpdated"`
	GenesisConfig GenesisConfig `json:"genesisConfig"`
	Forks         Forks         `json:"forks"`
}

// GenesisConfig contains genesis configuration.
type GenesisConfig struct {
	GenesisTime  int64 `json:"genesisTime"`  // Unix timestamp
	GenesisDelay int64 `json:"genesisDelay"` // Genesis delay in seconds
}

// Forks contains fork information for a network.
type Forks struct {
	Consensus map[string]Fork `json:"consensus"` // Map of fork name to fork info
}

// Fork represents a single fork with epoch and minimum client versions.
type Fork struct {
	Epoch             int64             `json:"epoch"`
	MinClientVersions map[string]string `json:"minClientVersions"` // Map of client name to version (camelCase to match cartographoor JSON)
}

// NetworkMetadata contains display information for networks.
type NetworkMetadata struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

// Network is the processed network data used internally.
type Network struct {
	Name         string
	DisplayName  string
	Description  string
	Status       string
	ChainID      int64  // Integer chain ID
	GenesisTime  int64  // Unix timestamp
	GenesisDelay int64  // Genesis delay in seconds
	Forks        Forks  // Fork information
	TargetURL    string // CBT API URL constructed from network name
	LastUpdated  time.Time
}

// Provider defines the interface for network data providers.
// This abstraction allows for multiple implementations (in-memory, Redis, etc.).
type Provider interface {
	Start(ctx context.Context) error
	Stop() error
	GetNetworks(ctx context.Context) map[string]*Network
	GetActiveNetworks(ctx context.Context) map[string]*Network
	GetNetwork(ctx context.Context, name string) (*Network, bool)
	// NotifyChannel returns a channel that signals when network data has been updated.
	// Consumers should listen on this channel to refresh cached data.
	NotifyChannel() <-chan struct{}
}
