package cartographoor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Service manages cartographoor network data and implements the Provider interface.
// In the Redis phase, a RedisProvider will implement the same interface.
type Service struct {
	config     *Config
	logger     logrus.FieldLogger
	httpClient *http.Client

	// Data management
	mu        sync.RWMutex
	networks  map[string]*Network // Key is network name
	lastFetch time.Time

	// Lifecycle management
	refreshTicker *time.Ticker
	stopChan      chan struct{}
	wg            sync.WaitGroup
}

// New creates a new cartographoor service.
func New(cfg *Config, logger logrus.FieldLogger) (*Service, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		config:     cfg,
		logger:     logger.WithField("component", "cartographoor"),
		httpClient: cfg.HTTPClient(),
		networks:   make(map[string]*Network),
		stopChan:   make(chan struct{}),
	}, nil
}

// Start starts the cartographoor service.
func (s *Service) Start(ctx context.Context) error {
	if !s.config.Enabled {
		s.logger.Info("Cartographoor service disabled")

		return nil
	}

	s.logger.Info("Starting cartographoor service")

	// Initial fetch
	if err := s.fetchAndUpdate(ctx); err != nil {
		s.logger.WithError(err).Error("Initial cartographoor fetch failed")
		// Don't return error - continue with empty networks
	}

	// Start background refresh
	s.refreshTicker = time.NewTicker(s.config.RefreshInterval)
	s.wg.Add(1)

	go s.refreshLoop(ctx)

	s.logger.WithFields(logrus.Fields{
		"source_url":       s.config.SourceURL,
		"refresh_interval": s.config.RefreshInterval,
	}).Info("Cartographoor service started")

	return nil
}

// Stop stops the cartographoor service.
func (s *Service) Stop() error {
	if !s.config.Enabled {
		return nil
	}

	s.logger.Info("Stopping cartographoor service")

	close(s.stopChan)

	if s.refreshTicker != nil {
		s.refreshTicker.Stop()
	}

	s.wg.Wait()

	s.logger.Info("Cartographoor service stopped")

	return nil
}

// GetNetworks returns all networks (active and inactive).
func (s *Service) GetNetworks() map[string]*Network {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Deep copy to prevent external mutation
	result := make(map[string]*Network, len(s.networks))

	for k, v := range s.networks {
		networkCopy := *v
		result[k] = &networkCopy
	}

	return result
}

// GetActiveNetworks returns only active networks.
func (s *Service) GetActiveNetworks() map[string]*Network {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*Network)

	for k, v := range s.networks {
		if v.Status == NetworkStatusActive {
			networkCopy := *v
			result[k] = &networkCopy
		}
	}

	return result
}

// GetNetwork returns a specific network by name.
func (s *Service) GetNetwork(name string) (*Network, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	network, exists := s.networks[name]
	if !exists {
		return nil, false
	}

	networkCopy := *network

	return &networkCopy, true
}

// fetchAndUpdate fetches data from cartographoor and updates internal state.
func (s *Service) fetchAndUpdate(ctx context.Context) error {
	s.logger.Debug("Fetching cartographoor data")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.SourceURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var rawResponse CartographoorResponse
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}

	// Process networks
	networks := s.processNetworks(&rawResponse)

	// Update state
	s.mu.Lock()
	s.networks = networks
	s.lastFetch = time.Now()
	s.mu.Unlock()

	s.logger.WithFields(logrus.Fields{
		"total_networks":  len(networks),
		"active_networks": s.countActive(networks),
		"last_update":     rawResponse.LastUpdate,
	}).Info("Updated cartographoor data")

	return nil
}

// processNetworks converts raw cartographoor data to Network structs.
func (s *Service) processNetworks(response *CartographoorResponse) map[string]*Network {
	networks := make(map[string]*Network, len(response.Networks))

	for networkName, rawNet := range response.Networks {
		// Get metadata if available
		var displayName, description string
		if meta, exists := response.NetworkMetadata[networkName]; exists {
			displayName = meta.DisplayName
			description = meta.Description
		}

		// Fallback display name
		if displayName == "" {
			displayName = s.formatDisplayName(networkName)
		}

		// Construct CBT API URL from network name
		targetURL := s.constructTargetURL(networkName)

		networks[networkName] = &Network{
			Name:        networkName,
			DisplayName: displayName,
			Description: description,
			Status:      rawNet.Status,
			ChainID:     rawNet.ChainID,
			TargetURL:   targetURL,
			LastUpdated: rawNet.LastUpdated,
		}
	}

	return networks
}

// constructTargetURL builds the CBT API URL for a network.
func (s *Service) constructTargetURL(networkName string) string {
	// Construct from network name using standard pattern
	// Network names in cartographoor JSON are already clean (e.g., "mainnet", "fusaka-devnet-3")
	return fmt.Sprintf("https://cbt-api-%s.primary.production.platform.ethpandaops.io/api/v1", networkName)
}

// formatDisplayName creates a display name from network name.
func (s *Service) formatDisplayName(name string) string {
	// Capitalize first letter of network name
	if len(name) > 0 {
		return strings.ToUpper(name[:1]) + name[1:]
	}

	return name
}

// countActive counts active networks in a map.
func (s *Service) countActive(networks map[string]*Network) int {
	count := 0

	for _, net := range networks {
		if net.Status == NetworkStatusActive {
			count++
		}
	}

	return count
}

// refreshLoop runs the background refresh cycle.
func (s *Service) refreshLoop(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-s.refreshTicker.C:
			if err := s.fetchAndUpdate(ctx); err != nil {
				s.logger.WithError(err).Error("Failed to refresh cartographoor data")
			}
		case <-s.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}
