package cartographoor

import "time"

const (
	NetworkStatusActive   = "active"
	NetworkStatusInactive = "inactive"
)

// CartographoorResponse represents the top-level JSON structure from networks.json.
type CartographoorResponse struct {
	Networks        map[string]RawNetwork      `json:"networks"`
	NetworkMetadata map[string]NetworkMetadata `json:"networkMetadata"`
	Clients         map[string]Client          `json:"clients"`
	LastUpdate      time.Time                  `json:"lastUpdate"`
	Duration        float64                    `json:"duration"` // Duration in milliseconds
	Providers       []DataProvider             `json:"providers"`
}

// DataProvider represents a data provider in cartographoor.
type DataProvider struct {
	Name string `json:"name"`
}

// RawNetwork represents a network entry in the cartographoor JSON.
// Note: The network name is the map KEY, not the "name" field.
type RawNetwork struct {
	Name          string                 `json:"name"`       // Short name (e.g., "devnet-3") - not used, we use map key
	Repository    string                 `json:"repository"` // Optional - only on devnets
	Path          string                 `json:"path"`       // Optional - only on devnets
	URL           string                 `json:"url"`        // Optional - only on devnets
	Status        string                 `json:"status"`
	ChainID       int64                  `json:"chainId"` // Integer, not string
	LastUpdated   time.Time              `json:"lastUpdated"`
	Description   string                 `json:"description"`
	GenesisConfig map[string]interface{} `json:"genesisConfig"`
	ServiceURLs   map[string]string      `json:"serviceUrls"` // Kept for completeness, not used for CBT URL
	Forks         map[string]interface{} `json:"forks"`
	SelfHostedDNS bool                   `json:"selfHostedDns"`
}

// NetworkMetadata contains display information for networks.
type NetworkMetadata struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

// Client represents an Ethereum client implementation.
type Client struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "execution" or "consensus"
	Repository  string `json:"repository"`
	Description string `json:"description"`
}

// Network is the processed network data used internally.
type Network struct {
	Name        string
	DisplayName string
	Description string
	Status      string
	ChainID     int64  // Integer chain ID
	TargetURL   string // CBT API URL constructed from network name
	LastUpdated time.Time
}

// Provider defines the interface for network data providers.
// This abstraction allows for multiple implementations (in-memory, Redis, etc.).
type Provider interface {
	GetNetworks() map[string]*Network
	GetActiveNetworks() map[string]*Network
	GetNetwork(name string) (*Network, bool)
}
