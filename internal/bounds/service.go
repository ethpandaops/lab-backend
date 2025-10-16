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

// Service is a stateless fetcher that retrieves bounds data from Xatu CBT APIs.
// It does NOT implement the Provider interface - that's RedisProvider's job.
// RedisProvider wraps this Service and controls all caching and refresh timing.
type Service struct {
	config                *config.Config
	cartographoorProvider cartographoor.Provider
	logger                logrus.FieldLogger
	httpClient            *http.Client
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
	}, nil
}

// Start begins the bounds service.
// When wrapped by RedisProvider, this does minimal initialization.
// RedisProvider controls all fetching (including initial fetch) via its loop.
func (s *Service) Start(ctx context.Context) error {
	s.logger.Info("Bounds service started")

	return nil
}

// Stop gracefully shuts down the service.
func (s *Service) Stop() error {
	s.logger.Info("Stopping bounds service")

	return nil
}

// FetchBounds fetches bounds data for all enabled networks and returns it.
// Does NOT cache - returns data directly to caller (RedisProvider).
func (s *Service) FetchBounds(
	ctx context.Context,
) (map[string]*BoundsData, error) {
	s.logger.Debug("Fetching bounds data for all networks")

	// Build merged network list (cartographoor + config overrides)
	mergedNetworks := config.BuildMergedNetworkList(ctx, s.config, s.cartographoorProvider)

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

		return make(map[string]*BoundsData), nil
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
		boundsData   = make(map[string]*BoundsData, len(networks))
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

		boundsData[res.network] = res.bounds
		successCount++
	}

	// Log at appropriate level based on errors
	logFields := logrus.Fields{
		"success": successCount,
		"errors":  errorCount,
		"total":   len(networks),
	}

	if errorCount > 0 {
		s.logger.WithFields(logFields).Warn("Fetched bounds data with errors")
	} else {
		s.logger.WithFields(logFields).Debug("Fetched bounds data")
	}

	// Return data directly - no caching
	if errorCount > 0 {
		return boundsData, fmt.Errorf("failed to fetch bounds for %d/%d networks", errorCount, len(networks))
	}

	return boundsData, nil
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
func (s *Service) calculateBounds(
	records []IncrementalTableRecord,
) *BoundsData {
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
