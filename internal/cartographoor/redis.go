package cartographoor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ethpandaops/lab-backend/internal/leader"
	"github.com/ethpandaops/lab-backend/internal/redis"
	"github.com/sirupsen/logrus"
)

// Compile-time interface compliance check.
var _ Provider = (*RedisProvider)(nil)

const redisNetworksKey = "lab:config:networks"

// RedisProvider implements Provider interface using Redis as storage.
type RedisProvider struct {
	log        logrus.FieldLogger
	cfg        Config
	redis      redis.Client
	elector    leader.Elector
	upstream   *Service
	done       chan struct{}
	notifyChan chan struct{} // Signals when network data has been updated
	wg         sync.WaitGroup
}

// NewRedisProvider creates a Redis-backed cartographoor provider.
func NewRedisProvider(
	log logrus.FieldLogger,
	cfg Config,
	redisClient redis.Client,
	elector leader.Elector,
	upstream *Service,
) Provider {
	return &RedisProvider{
		log:        log.WithField("component", "cartographoor_redis"),
		cfg:        cfg,
		redis:      redisClient,
		elector:    elector,
		upstream:   upstream,
		done:       make(chan struct{}),
		notifyChan: make(chan struct{}, 1), // Buffered so we don't block
	}
}

// Start initializes the provider and starts background refresh loop.
// Blocks until Redis has data or timeout is reached (fail-fast on timeout).
func (r *RedisProvider) Start(ctx context.Context) error {
	r.log.Info("Starting cartographoor provider")

	// Start background refresh loop
	r.wg.Add(1)

	go r.refreshLoop(ctx)

	// Wait for Redis to be populated (readiness check)
	// This ensures we never start serving requests with empty data
	r.log.Info("Waiting for cartographoor data")

	readinessTimeout := 30 * time.Second

	readinessCtx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-readinessCtx.Done():
			return fmt.Errorf(
				"readiness timeout: no cartographoor data in Redis after %v",
				readinessTimeout,
			)
		case <-ticker.C:
			networks := r.GetNetworks(ctx)
			if len(networks) > 0 {
				r.log.WithField("network_count", len(networks)).Info(
					"Cartographoor data ready",
				)

				return nil
			}

			r.log.Debug("Waiting for cartographoor data...")
		}
	}
}

// Stop stops the provider.
func (r *RedisProvider) Stop() error {
	r.log.Info("Stopping cartographoor provider")
	close(r.done)
	r.wg.Wait()

	return nil
}

// GetNetworks returns all networks by reading directly from Redis.
func (r *RedisProvider) GetNetworks(ctx context.Context) map[string]*Network {
	data, err := r.redis.Get(ctx, redisNetworksKey)
	if err != nil {
		r.log.WithError(err).Debug("Failed to get networks from Redis")

		return make(map[string]*Network)
	}

	var networks map[string]*Network
	if err := json.Unmarshal([]byte(data), &networks); err != nil {
		r.log.WithError(err).Error("Failed to unmarshal networks")

		return make(map[string]*Network)
	}

	return networks
}

// GetActiveNetworks returns only active networks by reading directly from Redis.
func (r *RedisProvider) GetActiveNetworks(
	ctx context.Context,
) map[string]*Network {
	allNetworks := r.GetNetworks(ctx)

	result := make(map[string]*Network)

	for name, network := range allNetworks {
		if network.Status == NetworkStatusActive {
			result[name] = network
		}
	}

	return result
}

// GetNetwork returns a specific network by reading directly from Redis.
func (r *RedisProvider) GetNetwork(
	ctx context.Context,
	name string,
) (*Network, bool) {
	allNetworks := r.GetNetworks(ctx)
	network, ok := allNetworks[name]

	return network, ok
}

// NotifyChannel returns a channel that signals when network data has been updated.
func (r *RedisProvider) NotifyChannel() <-chan struct{} {
	return r.notifyChan
}

func (r *RedisProvider) refreshLoop(ctx context.Context) {
	defer r.wg.Done()

	ticker := time.NewTicker(r.cfg.RefreshInterval)
	defer ticker.Stop()

	// Give leader election a moment to settle (it tries immediately on boot)
	time.Sleep(100 * time.Millisecond)

	// Immediate refresh on startup if leader
	if r.elector.IsLeader() {
		r.refreshData(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.done:
			return
		case <-ticker.C:
			// Only leader refreshes from upstream
			if r.elector.IsLeader() {
				r.refreshData(ctx)
			}
			// Followers do nothing - they read directly from Redis on API calls
		}
	}
}

func (r *RedisProvider) refreshData(ctx context.Context) {
	r.log.Debug("Refreshing cartographoor data from upstream")

	// Fetch fresh data from upstream (no caching, just HTTP call)
	allNetworks, err := r.upstream.FetchNetworks(ctx)
	if err != nil {
		r.log.WithError(err).Error("Failed to fetch networks from upstream")

		return
	}

	// Filter for active networks only
	activeNetworks := make(map[string]*Network)

	for name, network := range allNetworks {
		if network.Status == NetworkStatusActive {
			activeNetworks[name] = network
		}
	}

	if len(activeNetworks) == 0 {
		r.log.Warn("No active networks found in upstream data")

		return
	}

	// Filter for healthy networks only (health check each backend)
	healthyNetworks := r.filterHealthyNetworks(activeNetworks)

	if len(healthyNetworks) == 0 {
		r.log.Warn("No healthy networks found after health checks")

		return
	}

	r.log.WithFields(logrus.Fields{
		"total":   len(activeNetworks),
		"healthy": len(healthyNetworks),
	}).Debug("Filtered networks by health")

	// Serialize to JSON
	data, err := json.Marshal(healthyNetworks)
	if err != nil {
		r.log.WithError(err).Error("Failed to marshal networks")

		return
	}

	// Store in Redis with configured TTL
	ttl := r.cfg.NetworksTTL // 0 = no TTL (configurable)
	if err := r.redis.Set(ctx, redisNetworksKey, string(data), ttl); err != nil {
		r.log.WithError(err).Error("Failed to store networks in Redis")

		return
	}

	// Notify listeners that network data has been updated (non-blocking)
	select {
	case r.notifyChan <- struct{}{}:
		r.log.Debug("Notified listeners of cartographoor update")
	default:
		// Channel already has a pending notification, skip
	}
}

// filterHealthyNetworks performs concurrent health checks on all networks.
// Only returns networks that pass health checks.
func (r *RedisProvider) filterHealthyNetworks(networks map[string]*Network) map[string]*Network {
	type healthCheckResult struct {
		name    string
		network *Network
		healthy bool
		reason  string
	}

	// Launch concurrent health checks
	resultsChan := make(chan healthCheckResult, len(networks))

	var wg sync.WaitGroup

	for name, network := range networks {
		wg.Add(1)

		go func(n string, net *Network) {
			defer wg.Done()

			healthy, reason := r.checkNetworkHealth(net.TargetURL)
			resultsChan <- healthCheckResult{
				name:    n,
				network: net,
				healthy: healthy,
				reason:  reason,
			}
		}(name, network)
	}

	// Close channel when all health checks complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	healthyNetworks := make(map[string]*Network)

	for result := range resultsChan {
		if !result.healthy {
			r.log.WithFields(logrus.Fields{
				"network":    result.name,
				"target_url": result.network.TargetURL,
				"reason":     result.reason,
			}).Warn("Network failed health check, skipping")

			continue
		}

		healthyNetworks[result.name] = result.network
	}

	return healthyNetworks
}

// checkNetworkHealth checks if a backend is healthy by hitting its /health endpoint.
// Returns (healthy bool, reason string).
func (r *RedisProvider) checkNetworkHealth(targetURL string) (bool, string) {
	if targetURL == "" {
		return false, "no target URL"
	}

	// Parse target URL to construct health endpoint
	baseURL, err := url.Parse(targetURL)
	if err != nil {
		return false, fmt.Sprintf("invalid URL: %v", err)
	}

	// Build health check URL (replace /api/v1 path with /health)
	healthURL := &url.URL{
		Scheme: baseURL.Scheme,
		Host:   baseURL.Host,
		Path:   "/health",
	}

	// Create HTTP client with short timeout for health checks
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Perform health check
	resp, err := client.Get(healthURL.String())
	if err != nil {
		return false, fmt.Sprintf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	// Check for 200 OK status
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("health check returned %d", resp.StatusCode)
	}

	return true, ""
}
