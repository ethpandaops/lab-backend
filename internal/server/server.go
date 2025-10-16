package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/api"
	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/ethpandaops/lab-backend/internal/frontend"
	"github.com/ethpandaops/lab-backend/internal/handlers"
	"github.com/ethpandaops/lab-backend/internal/middleware"
	"github.com/ethpandaops/lab-backend/internal/proxy"
)

// Server represents the HTTP server.
type Server struct {
	httpServer *http.Server
	proxy      *proxy.Proxy
	logger     logrus.FieldLogger
}

// New creates a new HTTP server with all routes and middleware.
func New(
	logger logrus.FieldLogger,
	cfg *config.Config,
	cartographoorSvc *cartographoor.Service,
) (*Server, error) {
	mux := http.NewServeMux()

	// Health endpoint (no middleware needed for simple health check)
	mux.HandleFunc("GET /health", handlers.Health())
	logger.Info("Registered route: GET /health")

	// Metrics endpoint (Prometheus format)
	mux.Handle("GET /metrics", promhttp.Handler())
	logger.Info("Registered route: GET /metrics")

	// Config API (must come before wildcard proxy route)
	var provider cartographoor.Provider
	if cartographoorSvc != nil {
		provider = cartographoorSvc
	}

	configHandler := api.NewConfigHandler(cfg, provider)
	mux.Handle("GET /api/v1/config", configHandler)
	logger.Info("Registered route: GET /api/v1/config")

	// Network-based proxy for all other API routes
	if cartographoorSvc != nil {
		provider = cartographoorSvc
	}

	proxyHandler, err := proxy.New(logger.WithField("component", "proxy"), cfg, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy: %w", err)
	}

	mux.Handle("/api/v1/", proxyHandler)
	logger.WithField("networks", proxyHandler.NetworkCount()).Info("Registered proxy routes")

	// Build config data for frontend injection (use same logic as API endpoint)
	configData := configHandler.GetConfigData()

	// Frontend handler (catch-all for non-API routes)
	frontendHandler, err := frontend.New(configData, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create frontend handler: %w", err)
	}

	// Mount frontend as catch-all (must be last)
	mux.Handle("/", frontendHandler)
	logger.Info("Registered route: /")

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
		proxy:      proxyHandler,
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

	// Shutdown proxy first (stops periodic sync)
	if s.proxy != nil {
		if err := s.proxy.Shutdown(); err != nil {
			s.logger.WithError(err).Error("Error shutting down proxy")
		}
	}

	return s.httpServer.Shutdown(ctx)
}
