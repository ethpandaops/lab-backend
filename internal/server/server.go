package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/api"
	"github.com/ethpandaops/lab-backend/internal/bounds"
	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/ethpandaops/lab-backend/internal/frontend"
	"github.com/ethpandaops/lab-backend/internal/handlers"
	"github.com/ethpandaops/lab-backend/internal/middleware"
	"github.com/ethpandaops/lab-backend/internal/proxy"
	"github.com/ethpandaops/lab-backend/internal/ratelimit"
	"github.com/ethpandaops/lab-backend/internal/redis"
)

// Server represents the HTTP server.
type Server struct {
	httpServer            *http.Server
	proxy                 *proxy.Proxy
	frontend              *frontend.Frontend
	rateLimiter           ratelimit.Service
	logger                logrus.FieldLogger
	cartographoorProvider cartographoor.Provider
	boundsProvider        bounds.Provider
}

// New creates a new HTTP server with all routes and middleware.
func New(
	logger logrus.FieldLogger,
	cfg *config.Config,
	redisClient redis.Client,
	cartographoorProvider cartographoor.Provider,
	boundsProvider bounds.Provider,
) (*Server, error) {
	mux := http.NewServeMux()

	// Health endpoint (no middleware needed for simple health check)
	mux.HandleFunc("GET /health", handlers.Health())
	logger.WithField("route", "GET /health").Info("Registered route")

	// Metrics endpoint (Prometheus format)
	mux.Handle("GET /metrics", promhttp.Handler())
	logger.WithField("route", "GET /metrics").Info("Registered route")

	// Config API (must come before wildcard proxy route)
	configHandler := api.NewConfigHandler(logger, cfg, cartographoorProvider)
	mux.Handle("GET /api/v1/config", configHandler)
	logger.WithField("route", "GET /api/v1/config").Info("Registered route")

	// Network-scoped bounds endpoint (must come before wildcard proxy)
	boundsHandler := api.NewBoundsHandler(boundsProvider, logger)
	mux.Handle("GET /api/v1/{network}/bounds", boundsHandler)
	logger.WithField("route", "GET /api/v1/{network}/bounds").Info("Registered route")

	// Network-based proxy for all other API routes
	proxyHandler, err := proxy.New(logger.WithField("component", "proxy"), cfg, cartographoorProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy: %w", err)
	}

	mux.Handle("/api/v1/", proxyHandler)
	logger.WithField("networks", proxyHandler.NetworkCount()).Info("Registered proxy routes")

	// Frontend handler (catch-all for non-API routes)
	// Pass providers so frontend can refresh its cache when data updates
	frontendHandler, err := frontend.New(logger, configHandler, boundsProvider, cartographoorProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create frontend handler: %w", err)
	}

	// Mount frontend as catch-all (must be last)
	mux.Handle("/", frontendHandler)
	logger.WithField("route", "GET /").Info("Registered route")

	// Create rate limiter service if enabled
	var rateLimiter ratelimit.Service
	if cfg.RateLimiting.Enabled {
		rateLimiter = ratelimit.NewService(
			logger,
			redisClient.GetClient(),
			cfg.RateLimiting.FailureMode,
		)

		logger.Info("Rate limiting enabled")
	}

	// Apply middleware chain: Logging → Metrics → CORS → RateLimit → Recovery
	handler := middleware.Logging(logger)(mux)
	handler = middleware.Metrics()(handler)
	handler = middleware.CORS()(handler)

	// Add rate limiting AFTER CORS but BEFORE recovery
	if cfg.RateLimiting.Enabled {
		handler = middleware.RateLimit(logger, cfg.RateLimiting, rateLimiter)(handler)
	}

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
		httpServer:            httpServer,
		proxy:                 proxyHandler,
		frontend:              frontendHandler,
		rateLimiter:           rateLimiter,
		logger:                logger,
		cartographoorProvider: cartographoorProvider,
		boundsProvider:        boundsProvider,
	}, nil
}

// Start starts the HTTP server (blocking call).
func (s *Server) Start() error {
	// Start rate limiter if enabled
	if s.rateLimiter != nil {
		if err := s.rateLimiter.Start(context.Background()); err != nil {
			return fmt.Errorf("failed to start rate limiter: %w", err)
		}
	}

	// Start frontend cache refresh loop
	if err := s.frontend.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start frontend: %w", err)
	}

	s.logger.WithField("addr", s.httpServer.Addr).Info("Starting HTTP server")

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down HTTP server")

	// Shutdown frontend cache refresh loop
	if s.frontend != nil {
		if err := s.frontend.Stop(); err != nil {
			s.logger.WithError(err).Error("Error shutting down frontend")
		}
	}

	// Shutdown proxy (stops periodic sync)
	if s.proxy != nil {
		if err := s.proxy.Shutdown(); err != nil {
			s.logger.WithError(err).Error("Error shutting down proxy")
		}
	}

	// Shutdown rate limiter
	if s.rateLimiter != nil {
		if err := s.rateLimiter.Stop(); err != nil {
			s.logger.WithError(err).Error("Error shutting down rate limiter")
		}
	}

	return s.httpServer.Shutdown(ctx)
}
