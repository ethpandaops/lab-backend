package bounds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/sirupsen/logrus"
)

// Service manages periodic bounds data fetching and caching.
type Service struct {
	config                *config.Config
	cartographoorProvider cartographoor.Provider
	logger                logrus.FieldLogger
	httpClient            *http.Client

	// Data management
	mu         sync.RWMutex
	boundsData map[string]*BoundsData // Key is network name
	lastFetch  time.Time

	// Lifecycle management
	refreshTicker *time.Ticker
	stopChan      chan struct{}
	wg            sync.WaitGroup
}

// New creates a new bounds service.
func New(
	logger logrus.FieldLogger,
	cfg *config.Config,
	cartographoorProvider cartographoor.Provider,
) (*Service, error) {
	if err := cfg.Bounds.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		config:                cfg,
		cartographoorProvider: cartographoorProvider,
		logger:                logger.WithField("component", "bounds"),
		httpClient:            cfg.Bounds.HTTPClient(),
		boundsData:            make(map[string]*BoundsData),
		stopChan:              make(chan struct{}),
	}, nil
}

// Start begins the background refresh cycle.
func (s *Service) Start(ctx context.Context) error {
	s.logger.Info("Starting bounds service")

	// Initial fetch (non-blocking on error)
	if err := s.fetchAndUpdateAll(ctx); err != nil {
		s.logger.WithError(err).Error("Initial bounds fetch failed")
	}

	// Start background refresh
	s.refreshTicker = time.NewTicker(s.config.Bounds.RefreshInterval)
	s.wg.Add(1)

	go s.refreshLoop(ctx)

	s.logger.WithField("refresh_interval", s.config.Bounds.RefreshInterval).Info("Bounds service started")

	return nil
}

// Stop gracefully shuts down the service.
func (s *Service) Stop() error {
	s.logger.Info("Stopping bounds service")

	close(s.stopChan)

	if s.refreshTicker != nil {
		s.refreshTicker.Stop()
	}

	s.wg.Wait()

	s.logger.Info("Bounds service stopped")

	return nil
}

// GetBounds retrieves bounds for a specific network (Provider interface).
func (s *Service) GetBounds(network string) (*BoundsData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bounds, exists := s.boundsData[network]
	if !exists {
		return nil, false
	}

	// Deep copy to prevent external mutation
	boundsCopy := *bounds

	return &boundsCopy, true
}

// GetAllBounds retrieves bounds for all networks (Provider interface).
func (s *Service) GetAllBounds() map[string]*BoundsData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*BoundsData, len(s.boundsData))

	for k, v := range s.boundsData {
		boundsCopy := *v
		result[k] = &boundsCopy
	}

	return result
}

// refreshLoop runs the periodic fetch cycle.
func (s *Service) refreshLoop(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-s.refreshTicker.C:
			if err := s.fetchAndUpdateAll(ctx); err != nil {
				s.logger.WithError(err).Error("Failed to refresh bounds data")
			}
		case <-s.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// fetchAndUpdateAll fetches bounds for all enabled networks concurrently.
func (s *Service) fetchAndUpdateAll(ctx context.Context) error {
	s.logger.Debug("Fetching bounds data for all networks")

	// Build merged network list (cartographoor + config overrides)
	mergedNetworks := config.BuildMergedNetworkList(s.config, s.cartographoorProvider)

	// Convert map to slice of enabled networks only
	networks := make([]config.NetworkConfig, 0, len(mergedNetworks))

	for _, network := range mergedNetworks {
		// Only include enabled networks
		if network.Enabled == nil || *network.Enabled {
			networks = append(networks, network)
		}
	}

	if len(networks) == 0 {
		s.logger.Warn("No enabled networks found")

		return nil
	}

	// Concurrent fetching with goroutines
	type result struct {
		network string
		bounds  *BoundsData
		err     error
	}

	resultsChan := make(chan result, len(networks))

	var fetchWg sync.WaitGroup

	// Launch goroutine for each network
	for _, network := range networks {
		fetchWg.Add(1)

		go func(net config.NetworkConfig) {
			defer fetchWg.Done()

			bounds, err := s.fetchBoundsForNetwork(ctx, net)
			resultsChan <- result{
				network: net.Name,
				bounds:  bounds,
				err:     err,
			}
		}(network)
	}

	// Wait for all goroutines to complete and close channel
	go func() {
		fetchWg.Wait()
		close(resultsChan)
	}()

	// Collect results
	var (
		newBounds    = make(map[string]*BoundsData, len(networks))
		successCount = 0
		errorCount   = 0
	)

	for res := range resultsChan {
		if res.err != nil {
			s.logger.WithFields(logrus.Fields{
				"network": res.network,
				"error":   res.err,
			}).Error("Failed to fetch bounds for network")

			errorCount++

			continue
		}

		newBounds[res.network] = res.bounds
		successCount++
	}

	// Update cache atomically
	s.mu.Lock()
	s.boundsData = newBounds
	s.lastFetch = time.Now()
	s.mu.Unlock()

	s.logger.WithFields(logrus.Fields{
		"success": successCount,
		"errors":  errorCount,
		"total":   len(networks),
	}).Info("Updated bounds data")

	if errorCount > 0 {
		return fmt.Errorf("failed to fetch bounds for %d/%d networks", errorCount, len(networks))
	}

	return nil
}

// fetchBoundsForNetwork fetches bounds for a single network with pagination support.
func (s *Service) fetchBoundsForNetwork(
	ctx context.Context,
	network config.NetworkConfig,
) (*BoundsData, error) {
	if network.TargetURL == "" {
		return nil, fmt.Errorf("network %s has no target_url configured", network.Name)
	}

	// Accumulate all records across pages
	var (
		allRecords    = make([]IncrementalTableRecord, 0)
		nextPageToken = ""
		pageCount     = 0
	)

	for {
		// Construct URL with database filter, page size, and optional page token
		url := fmt.Sprintf(
			"%s/admin_cbt_incremental?database_eq=%s&page_size=500",
			network.TargetURL,
			network.Name,
		)

		if nextPageToken != "" {
			url = fmt.Sprintf("%s&page_token=%s", url, nextPageToken)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch data: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		var apiResp AdminCBTIncrementalResponse
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}

		// Accumulate records from this page
		allRecords = append(allRecords, apiResp.AdminCBTIncremental...)
		pageCount++

		// Check if there are more pages
		if apiResp.NextPageToken == "" {
			break
		}

		nextPageToken = apiResp.NextPageToken

		s.logger.WithFields(logrus.Fields{
			"network": network.Name,
			"page":    pageCount,
			"records": len(apiResp.AdminCBTIncremental),
		}).Debug("Fetched page of admin_cbt_incremental data")
	}

	s.logger.WithFields(logrus.Fields{
		"network":       network.Name,
		"total_pages":   pageCount,
		"total_records": len(allRecords),
	}).Debug("Completed fetching all bounds for network")

	// Calculate bounds from all accumulated records
	bounds := s.calculateBounds(allRecords)

	return bounds, nil
}

// calculateBounds computes per-table min/max from incremental table records.
func (s *Service) calculateBounds(records []IncrementalTableRecord) *BoundsData {
	if len(records) == 0 {
		return &BoundsData{
			Tables:      make(map[string]TableBounds),
			LastUpdated: time.Now(),
		}
	}

	// Group records by table name
	tableGroups := make(map[string][]IncrementalTableRecord)

	for _, record := range records {
		tableGroups[record.Table] = append(tableGroups[record.Table], record)
	}

	// Calculate bounds for each table
	tableBounds := make(map[string]TableBounds, len(tableGroups))

	for tableName, tableRecords := range tableGroups {
		var (
			minPos = int64(math.MaxInt64)
			maxPos = int64(0)
		)

		for _, record := range tableRecords {
			if record.Position < minPos {
				minPos = record.Position
			}

			endPos := record.Position + record.Interval
			if endPos > maxPos {
				maxPos = endPos
			}
		}

		tableBounds[tableName] = TableBounds{
			Min: minPos,
			Max: maxPos,
		}
	}

	return &BoundsData{
		Tables:      tableBounds,
		LastUpdated: time.Now(),
	}
}
