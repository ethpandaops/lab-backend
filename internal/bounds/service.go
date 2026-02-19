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

// FetchBounds fetches bounds data for all enabled networks and returns it.
func (s *Service) FetchBounds(
	ctx context.Context,
) (map[string]*BoundsData, error) {
	s.logger.Debug("Fetching bounds data for all networks")

	// Build merged network list (cartographoor + config overrides)
	mergedNetworks := config.BuildMergedNetworkList(
		ctx,
		s.logger,
		s.config,
		s.cartographoorProvider,
	)

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
		"total":   len(networks),
		"errors":  errorCount,
	}

	if errorCount > 0 {
		s.logger.WithFields(logFields).Warn("Fetched bounds data with errors")
	} else {
		s.logger.WithFields(logFields).Debug("Fetched bounds data")
	}

	return boundsData, nil
}

// fetchBoundsForNetwork fetches bounds for a single network with pagination support.
// In hybrid mode (LocalOverrides set), it fetches from both external and local
// sources, then merges: local bounds for overridden tables, external for everything else.
func (s *Service) fetchBoundsForNetwork(
	ctx context.Context,
	network config.NetworkConfig,
) (*BoundsData, error) {
	if network.TargetURL == "" {
		return nil, fmt.Errorf("network %s has no target_url configured", network.Name)
	}

	// If no local overrides, fetch from primary source only.
	if network.LocalOverrides == nil {
		return s.fetchBoundsFromURL(ctx, network.TargetURL, network.Name)
	}

	// Hybrid mode: fetch both external and local, merge results.
	externalBounds, externalErr := s.fetchBoundsFromURL(
		ctx, network.TargetURL, network.Name,
	)
	if externalErr != nil {
		s.logger.WithFields(logrus.Fields{
			"network": network.Name,
			"error":   externalErr,
		}).Warn("External bounds fetch failed")
	}

	localBounds, localErr := s.fetchBoundsFromURL(
		ctx, network.LocalOverrides.TargetURL, network.Name,
	)
	if localErr != nil {
		s.logger.WithFields(logrus.Fields{
			"network":   network.Name,
			"local_url": network.LocalOverrides.TargetURL,
			"error":     localErr,
		}).Warn("Local bounds fetch failed")
	}

	// Handle failures gracefully.
	if externalErr != nil && localErr != nil {
		return nil, fmt.Errorf(
			"both fetches failed: external: %w, local: %v",
			externalErr, localErr,
		)
	}

	if externalErr != nil {
		return localBounds, nil //nolint:nilerr // Graceful degradation: use local when external fails.
	}

	if localErr != nil {
		return externalBounds, nil //nolint:nilerr // Graceful degradation: use external when local fails.
	}

	// Both succeeded — merge: local bounds for overridden tables, external for rest.
	localTableSet := make(map[string]bool, len(network.LocalOverrides.Tables))
	for _, table := range network.LocalOverrides.Tables {
		localTableSet[table] = true
	}

	merged := &BoundsData{
		Tables:      make(map[string]TableBounds, len(externalBounds.Tables)),
		LastUpdated: time.Now(),
	}

	for table, bounds := range externalBounds.Tables {
		merged.Tables[table] = bounds
	}

	for table, bounds := range localBounds.Tables {
		if localTableSet[table] {
			merged.Tables[table] = bounds
		}
	}

	s.logger.WithFields(logrus.Fields{
		"network":      network.Name,
		"external":     len(externalBounds.Tables),
		"local":        len(localBounds.Tables),
		"merged":       len(merged.Tables),
		"local_tables": network.LocalOverrides.Tables,
	}).Debug("Merged external and local bounds for hybrid mode")

	return merged, nil
}

// fetchBoundsFromURL fetches bounds from a single cbt-api URL with pagination.
func (s *Service) fetchBoundsFromURL(
	ctx context.Context,
	targetURL string,
	networkName string,
) (*BoundsData, error) {
	var (
		allRecords    = make([]IncrementalTableRecord, 0)
		nextPageToken = ""
		pageCount     = 0
	)

	for {
		reqURL := fmt.Sprintf(
			"%s/admin_cbt_incremental?database_eq=%s&page_size=10000",
			targetURL,
			networkName,
		)

		if nextPageToken != "" {
			reqURL = fmt.Sprintf("%s&page_token=%s", reqURL, nextPageToken)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
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

		allRecords = append(allRecords, apiResp.AdminCBTIncremental...)
		pageCount++

		if apiResp.NextPageToken == "" {
			break
		}

		nextPageToken = apiResp.NextPageToken

		s.logger.WithFields(logrus.Fields{
			"network": networkName,
			"url":     targetURL,
			"page":    pageCount,
			"records": len(apiResp.AdminCBTIncremental),
		}).Debug("Fetched page of admin_cbt_incremental data")
	}

	s.logger.WithFields(logrus.Fields{
		"network":       networkName,
		"url":           targetURL,
		"total_pages":   pageCount,
		"total_records": len(allRecords),
	}).Debug("Completed fetching bounds from URL")

	return s.calculateBounds(allRecords), nil
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
