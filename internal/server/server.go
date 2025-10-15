package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/api"
	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/ethpandaops/lab-backend/internal/frontend"
	"github.com/ethpandaops/lab-backend/internal/handlers"
	"github.com/ethpandaops/lab-backend/internal/middleware"
	"github.com/ethpandaops/lab-backend/internal/proxy"
)

// Server represents the HTTP server.
type Server struct {
	httpServer *http.Server
	logger     logrus.FieldLogger
}

// New creates a new HTTP server with all routes and middleware.
func New(cfg *config.Config, logger logrus.FieldLogger) (*Server, error) {
	mux := http.NewServeMux()

	// Health endpoint (no middleware needed for simple health check)
	mux.HandleFunc("GET /health", handlers.Health())
	logger.Info("Registered route: GET /health")

	// Metrics endpoint (Prometheus format)
	mux.Handle("GET /metrics", promhttp.Handler())
	logger.Info("Registered route: GET /metrics")

	// Config API (must come before wildcard proxy route)
	configHandler := api.NewConfigHandler(cfg)
	mux.Handle("GET /api/v1/config", configHandler)
	logger.Info("Registered route: GET /api/v1/config")

	// Network-based proxy for all other API routes
	proxyHandler, err := proxy.New(cfg, logger.WithField("component", "proxy"))
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy: %w", err)
	}

	mux.Handle("/api/v1/", proxyHandler)
	logger.WithField("networks", len(cfg.Networks)).Info("Registered proxy routes")

	// Build config data for frontend injection
	configData := buildConfigData(cfg)

	// Frontend handler (catch-all for non-API routes)
	frontendHandler, err := frontend.New(configData, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create frontend handler: %w", err)
	}

	// Mount frontend as catch-all (must be last)
	mux.Handle("/", frontendHandler)
	logger.Info("Registered route: / (frontend catch-all)")

	// Apply middleware chain: Logging → Metrics → CORS → Recovery
	handler := middleware.Logging(logger)(mux)
	handler = middleware.Metrics()(handler)
	handler = middleware.CORS()(handler)
	handler = middleware.Recovery(logger)(handler)

	// Create HTTP server
	httpServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       120 * time.Second,
	}

	return &Server{
		httpServer: httpServer,
		logger:     logger,
	}, nil
}

// Start starts the HTTP server (blocking call).
func (s *Server) Start() error {
	s.logger.WithField("addr", s.httpServer.Addr).Info("Starting HTTP server")

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down HTTP server")

	return s.httpServer.Shutdown(ctx)
}

// buildConfigData converts Config to frontend config format.
// This is what gets injected into index.html as window.__CONFIG__.
func buildConfigData(cfg *config.Config) interface{} {
	networks := make([]map[string]interface{}, 0, len(cfg.Networks))
	for _, net := range cfg.Networks {
		networks = append(networks, map[string]interface{}{
			"name":    net.Name,
			"enabled": net.Enabled,
		})
	}

	experiments := make(map[string]interface{})
	for name, exp := range cfg.Experiments.Experiments {
		experiments[name] = map[string]interface{}{
			"enabled":  exp.Enabled,
			"networks": exp.Networks,
		}
	}

	return map[string]interface{}{
		"networks":    networks,
		"experiments": experiments,
	}
}
