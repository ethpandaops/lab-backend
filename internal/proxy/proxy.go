package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/config"
)

// Proxy manages network-based reverse proxying.
type Proxy struct {
	config  *config.Config
	proxies map[string]*httputil.ReverseProxy
	logger  logrus.FieldLogger
	mu      sync.RWMutex
}

// New creates a new proxy service with pre-configured ReverseProxy instances.
func New(cfg *config.Config, logger logrus.FieldLogger) (*Proxy, error) {
	p := &Proxy{
		config:  cfg,
		proxies: make(map[string]*httputil.ReverseProxy, len(cfg.Networks)),
		logger:  logger.WithField("component", "proxy"),
	}

	// Create reverse proxy for each enabled network
	for _, network := range cfg.Networks {
		if !network.Enabled {
			p.logger.WithFields(logrus.Fields{
				"network": network.Name,
				"reason":  "disabled",
			}).Debug("Skipping disabled network")

			continue
		}

		proxy, err := p.createReverseProxy(network.TargetURL, network.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to create proxy for network %s: %w", network.Name, err)
		}

		p.proxies[network.Name] = proxy
		p.logger.WithFields(logrus.Fields{
			"network":    network.Name,
			"target_url": network.TargetURL,
		}).Info("Network proxy created")
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

	// Use RLock for reading proxy map (allows concurrent reads)
	p.mu.RLock()
	proxy, exists := p.proxies[network]
	p.mu.RUnlock()

	if !exists {
		// Check if network is configured but disabled
		networkCfg, err := p.config.GetNetworkByName(network)
		if err == nil && !networkCfg.Enabled {
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
func (p *Proxy) createReverseProxy(targetURL string, networkName string) (*httputil.ReverseProxy, error) {
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

// === Dynamic Network Management Methods (Phase 2 Ready) ===
// These methods enable runtime network updates for future cartographer integration.
// Phase 1 implementation: These methods are implemented but not called.
// Phase 2 implementation: cartographer will call these when networks change.

// AddNetwork dynamically adds a new network proxy at runtime.
// Used by cartographer in Phase 2 when new devnets are discovered.
func (p *Proxy) AddNetwork(network config.NetworkConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create reverse proxy for this network
	proxy, err := p.createReverseProxy(network.TargetURL, network.Name)
	if err != nil {
		return fmt.Errorf("failed to create proxy for %s: %w", network.Name, err)
	}

	p.proxies[network.Name] = proxy
	p.logger.WithFields(logrus.Fields{
		"network":    network.Name,
		"target_url": network.TargetURL,
	}).Info("Network proxy added")

	return nil
}

// RemoveNetwork dynamically removes a network proxy at runtime.
// Used by cartographer in Phase 2 when devnets are retired.
func (p *Proxy) RemoveNetwork(networkName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.proxies, networkName)

	p.logger.WithField("network", networkName).Info("Network proxy removed")
}

// UpdateNetwork dynamically updates a network proxy at runtime.
// Used by cartographer in Phase 2 when network URLs change.
func (p *Proxy) UpdateNetwork(network config.NetworkConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create new reverse proxy
	proxy, err := p.createReverseProxy(network.TargetURL, network.Name)
	if err != nil {
		return fmt.Errorf("failed to update proxy for %s: %w", network.Name, err)
	}

	p.proxies[network.Name] = proxy
	p.logger.WithFields(logrus.Fields{
		"network":    network.Name,
		"target_url": network.TargetURL,
	}).Info("Network proxy updated")

	return nil
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
