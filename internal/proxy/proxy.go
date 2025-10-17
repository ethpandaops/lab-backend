package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
)

// Proxy manages network-based reverse proxying.
type Proxy struct {
	config    *config.Config
	proxies   map[string]*httputil.ReverseProxy
	proxyURLs map[string]string
	logger    logrus.FieldLogger
	mu        sync.RWMutex
	provider  cartographoor.Provider

	// Periodic sync lifecycle
	syncTicker *time.Ticker
	stopChan   chan struct{}
	wg         sync.WaitGroup
}

// New creates a new proxy service with pre-configured ReverseProxy instances.
func New(
	logger logrus.FieldLogger,
	cfg *config.Config,
	provider cartographoor.Provider,
) (*Proxy, error) {
	p := &Proxy{
		config:    cfg,
		proxies:   make(map[string]*httputil.ReverseProxy),
		proxyURLs: make(map[string]string),
		logger:    logger.WithField("component", "proxy"),
		provider:  provider,
		stopChan:  make(chan struct{}),
	}

	// Initial sync: build merged network list and create proxies
	// Uses cartographoor-first, config-overlay approach.
	if err := p.SyncNetworks(context.Background()); err != nil {
		// Don't error - proxy still usable with whatever loaded
		p.logger.WithError(err).Warn("Initial network sync failed")
	}

	// Start periodic sync if provider available
	if provider != nil {
		p.startPeriodicSync()
	}

	return p, nil
}

// ServeHTTP implements http.Handler interface.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract network from path
	network, _, err := ExtractNetwork(r.URL.Path)
	if err != nil {
		p.logger.WithFields(logrus.Fields{
			"path":  r.URL.Path,
			"error": err.Error(),
		}).Warn("Invalid path format")

		p.writeJSONError(w, http.StatusBadRequest, "invalid path format", "")

		return
	}

	p.mu.RLock()
	proxy, exists := p.proxies[network]
	p.mu.RUnlock()

	if !exists {
		// Check if network is configured but disabled
		networkCfg, err := p.config.GetNetworkByName(network)
		if err == nil && networkCfg.Enabled != nil && !*networkCfg.Enabled {
			p.logger.WithField("network", network).Debug("Network is disabled")

			p.writeJSONError(w, http.StatusServiceUnavailable, "network disabled", network)

			return
		}

		// Network not found in config
		p.logger.WithField("network", network).Debug("Network not found")

		p.writeJSONError(w, http.StatusNotFound, "network not found", network)

		return
	}

	// Log proxy request
	p.logger.WithFields(logrus.Fields{
		"method":  r.Method,
		"network": network,
		"path":    r.URL.Path,
	}).Debug("Proxying request")

	// Forward request to backend
	proxy.ServeHTTP(w, r)
}

// createReverseProxy creates and configures a ReverseProxy for a target URL.
func (p *Proxy) createReverseProxy(
	targetURL string,
	networkName string,
) (*httputil.ReverseProxy, error) {
	// Parse target URL
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}

	// Create custom Transport with connection pooling
	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Create ReverseProxy with Rewrite function
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			// Set target URL
			r.SetURL(target)
			// Enable X-Forwarded-* headers
			r.SetXForwarded()

			// Rewrite path to remove network segment
			rewrittenPath, err := RewritePath(r.In.URL.Path)
			if err != nil {
				p.logger.WithFields(logrus.Fields{
					"network": networkName,
					"path":    r.In.URL.Path,
					"error":   err.Error(),
				}).Error("Failed to rewrite path")

				return
			}

			r.Out.URL.Path = rewrittenPath

			// Preserve query parameters (already handled by SetURL)
			r.Out.URL.RawQuery = r.In.URL.RawQuery
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			p.logger.WithFields(logrus.Fields{
				"network":     networkName,
				"target_url":  target.String(),
				"error":       err.Error(),
				"method":      r.Method,
				"path":        r.URL.Path,
				"remote_addr": r.RemoteAddr,
			}).Error("Backend error")

			p.writeJSONError(w, http.StatusBadGateway, "backend unavailable", networkName)
		},
	}

	return proxy, nil
}

// startPeriodicSync starts the background sync goroutine.
func (p *Proxy) startPeriodicSync() {
	// Use cartographoor refresh interval for proxy sync
	// Default to 5 minutes if not configured
	interval := p.config.Cartographoor.RefreshInterval
	if interval == 0 {
		interval = 5 * time.Minute
	}

	p.syncTicker = time.NewTicker(interval)
	p.wg.Add(1)

	go p.syncLoop()

	p.logger.WithField("refresh_interval", interval).Info("Started periodic network sync")
}

// stopPeriodicSync stops the background sync goroutine.
func (p *Proxy) stopPeriodicSync() {
	if p.syncTicker != nil {
		close(p.stopChan)

		p.syncTicker.Stop()
		p.wg.Wait()

		p.logger.Info("Stopped periodic network sync")
	}
}

// syncLoop runs the periodic sync in background.
func (p *Proxy) syncLoop() {
	defer p.wg.Done()

	// Sync immediately on startup to get initial state from Redis
	if err := p.SyncNetworks(context.Background()); err != nil {
		p.logger.WithError(err).Error("Initial network sync failed")
	}

	for {
		select {
		case <-p.syncTicker.C:
			if err := p.SyncNetworks(context.Background()); err != nil {
				p.logger.WithError(err).Error("Periodic network sync failed")
			}
		case <-p.stopChan:
			return
		}
	}
}

// SyncNetworks syncs proxy networks using cartographoor-first, config-overlay approach.
func (p *Proxy) SyncNetworks(ctx context.Context) error {
	// Build merged network list (cartographoor + config overlay)
	desiredNetworks := config.BuildMergedNetworkList(ctx, p.config, p.provider)

	p.logger.WithField("count", len(desiredNetworks)).Debug("Syncing networks from merged config")

	// Track which networks should exist
	desiredNames := make(map[string]bool)

	// Add or update networks from merged list
	for name, networkCfg := range desiredNetworks {
		// Only process enabled networks
		if networkCfg.Enabled != nil && !*networkCfg.Enabled {
			p.logger.WithField("network", name).Debug("Network disabled, skipping")

			continue
		}

		desiredNames[name] = true

		// Check if network already exists
		p.mu.RLock()
		_, exists := p.proxies[name]
		p.mu.RUnlock()

		if exists {
			// Update existing network
			if err := p.UpdateNetwork(networkCfg); err != nil {
				p.logger.WithError(err).WithField("network", name).Error("Failed to update network")
			}
		} else {
			// Add new network
			if err := p.AddNetwork(networkCfg); err != nil {
				p.logger.WithError(err).WithField("network", name).Error("Failed to add network")
			}
		}
	}

	// Remove networks that are no longer desired
	p.mu.RLock()

	currentNetworks := make([]string, 0, len(p.proxies))
	for name := range p.proxies {
		currentNetworks = append(currentNetworks, name)
	}

	p.mu.RUnlock()

	for _, name := range currentNetworks {
		if !desiredNames[name] {
			p.logger.WithField("network", name).Info("Removing network no longer in config")
			p.RemoveNetwork(name)
		}
	}

	return nil
}

// NetworkCount returns the number of active network proxies.
func (p *Proxy) NetworkCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return len(p.proxies)
}

// Shutdown stops the proxy and cleans up resources.
func (p *Proxy) Shutdown() error {
	p.logger.Info("Shutting down proxy")
	p.stopPeriodicSync()

	return nil
}

// AddNetwork dynamically adds a new network proxy at runtime.
// Used by cartographoor when new devnets are discovered.
func (p *Proxy) AddNetwork(network config.NetworkConfig) error {
	// Check backend health before adding
	healthy, _ := p.checkHealth(network.TargetURL)
	if !healthy {
		p.logger.WithFields(logrus.Fields{
			"network":    network.Name,
			"target_url": network.TargetURL,
		}).Warn("Backend unhealthy for network")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Create reverse proxy for this network
	proxy, err := p.createReverseProxy(network.TargetURL, network.Name)
	if err != nil {
		return fmt.Errorf("failed to create proxy for %s: %w", network.Name, err)
	}

	p.proxies[network.Name] = proxy
	p.proxyURLs[network.Name] = network.TargetURL

	healthStatus := "healthy"
	if !healthy {
		healthStatus = "unhealthy"
	}

	p.logger.WithFields(logrus.Fields{
		"network":       network.Name,
		"target_url":    network.TargetURL,
		"health_status": healthStatus,
	}).Info("Network proxy added")

	return nil
}

// RemoveNetwork dynamically removes a network proxy at runtime.
// Used by cartographer in Phase 2 when devnets are retired.
func (p *Proxy) RemoveNetwork(networkName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.proxies, networkName)
	delete(p.proxyURLs, networkName)

	p.logger.WithField("network", networkName).Info("Network proxy removed")
}

// UpdateNetwork dynamically updates a network proxy at runtime.
// Used by cartographer in Phase 2 when network URLs change.
func (p *Proxy) UpdateNetwork(network config.NetworkConfig) error {
	p.mu.RLock()
	currentURL, exists := p.proxyURLs[network.Name]
	p.mu.RUnlock()

	// Only update if URL changed
	if exists && currentURL == network.TargetURL {
		p.logger.WithFields(logrus.Fields{
			"network":    network.Name,
			"target_url": network.TargetURL,
		}).Debug("Network proxy unchanged, skipping update")

		return nil
	}

	// Check backend health before updating
	healthy, _ := p.checkHealth(network.TargetURL)
	if !healthy {
		p.logger.WithFields(logrus.Fields{
			"network":    network.Name,
			"target_url": network.TargetURL,
		}).Warn("Backend unhealthy for network")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Create new reverse proxy
	proxy, err := p.createReverseProxy(network.TargetURL, network.Name)
	if err != nil {
		return fmt.Errorf("failed to update proxy for %s: %w", network.Name, err)
	}

	p.proxies[network.Name] = proxy
	p.proxyURLs[network.Name] = network.TargetURL

	healthStatus := "healthy"
	if !healthy {
		healthStatus = "unhealthy"
	}

	p.logger.WithFields(logrus.Fields{
		"network":       network.Name,
		"target_url":    network.TargetURL,
		"health_status": healthStatus,
	}).Info("Network proxy updated")

	return nil
}

// checkHealth checks if a backend is healthy by hitting its /health endpoint.
func (p *Proxy) checkHealth(targetURL string) (bool, error) {
	// Parse target URL to construct health endpoint
	baseURL, err := url.Parse(targetURL)
	if err != nil {
		return false, fmt.Errorf("invalid target URL: %w", err)
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
		return false, fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for 200 OK status
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}

	return true, nil
}

// writeJSONError writes a JSON error response.
func (p *Proxy) writeJSONError(w http.ResponseWriter, statusCode int, message string, network string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]string{
		"error": message,
	}

	if network != "" {
		response["network"] = network
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		p.logger.WithFields(logrus.Fields{
			"error":       err.Error(),
			"status_code": statusCode,
		}).Error("Failed to encode error response")
	}
}
