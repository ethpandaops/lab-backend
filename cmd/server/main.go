package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/bounds"
	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/ethpandaops/lab-backend/internal/leader"
	"github.com/ethpandaops/lab-backend/internal/redis"
	"github.com/ethpandaops/lab-backend/internal/server"
	"github.com/ethpandaops/lab-backend/internal/version"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Setup logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		ForceColors:   true,
		FullTimestamp: true,
	})

	// Log startup with version info
	logger.WithFields(logrus.Fields{
		"version":    version.Short(),
		"git_commit": version.GitCommit,
		"build_date": version.BuildDate,
	}).Info("Starting...")

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.WithError(err).Fatal("Failed to load configuration")
	}

	// Set log level from config
	level, parseErr := logrus.ParseLevel(cfg.Server.LogLevel)
	if parseErr != nil {
		logger.WithError(parseErr).Warn("Invalid log level, using info")

		level = logrus.InfoLevel
	}

	logger.SetLevel(level)

	// Validate configuration
	if verr := cfg.Validate(); verr != nil {
		logger.WithError(verr).Fatal("Invalid configuration")
	}

	logger.WithFields(logrus.Fields{
		"port":      cfg.Server.Port,
		"log_level": cfg.Server.LogLevel,
	}).Info("Configuration loaded")

	// Create a cancellable context for the application lifecycle
	// This will be cancelled on shutdown to signal all services to stop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Redis client
	redisClient := redis.NewClient(logger, redis.Config{
		Address:      cfg.Redis.Address,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  cfg.Redis.DialTimeout,
		ReadTimeout:  cfg.Redis.ReadTimeout,
		WriteTimeout: cfg.Redis.WriteTimeout,
		PoolSize:     cfg.Redis.PoolSize,
	})

	if startErr := redisClient.Start(ctx); startErr != nil {
		logger.WithError(startErr).Fatal("Failed to start Redis client")
	}

	// Initialize leader election
	elector := leader.NewElector(logger, leader.Config{
		LockKey:       cfg.Leader.LockKey,
		LockTTL:       cfg.Leader.LockTTL,
		RenewInterval: cfg.Leader.RenewInterval,
		RetryInterval: cfg.Leader.RetryInterval,
	}, redisClient)

	if startErr := elector.Start(ctx); startErr != nil {
		logger.WithError(startErr).Fatal("Failed to start leader election")
	}

	// Create cartographoor service
	var cartographoorProvider cartographoor.Provider

	var cartographoorRedisProvider *cartographoor.RedisProvider

	var cartographoorSvc *cartographoor.Service

	if cfg.Cartographoor.Enabled {
		var cerr error

		// Create upstream service (fetches from Cartographoor API)
		cartographoorSvc, cerr = cartographoor.New(&cfg.Cartographoor, logger)
		if cerr != nil {
			logger.WithError(cerr).Fatal("Failed to create cartographoor service")
		}

		// Start upstream service
		if serr := cartographoorSvc.Start(ctx); serr != nil {
			logger.WithError(serr).Fatal("Failed to start cartographoor service")
		}

		// Wrap with Redis provider (mandatory)
		provider := cartographoor.NewRedisProvider(
			logger,
			cfg.Cartographoor,
			redisClient,
			elector,
			cartographoorSvc,
		)

		var ok bool

		cartographoorRedisProvider, ok = provider.(*cartographoor.RedisProvider)
		if !ok {
			logger.Fatal("Failed to assert cartographoor provider type")
		}

		if startErr := cartographoorRedisProvider.Start(ctx); startErr != nil {
			logger.WithError(startErr).Fatal("Failed to start Redis cartographoor provider")
		}

		cartographoorProvider = cartographoorRedisProvider

		logger.Info("Cartographoor service started")
	}

	// Create upstream bounds service
	upstreamBounds, err := bounds.New(logger, cfg, cartographoorProvider)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create bounds service")
	}

	if startErr := upstreamBounds.Start(ctx); startErr != nil {
		logger.WithError(startErr).Fatal("Failed to start bounds service")
	}

	// Wrap with Redis provider
	boundsProv := bounds.NewRedisProvider(
		logger,
		bounds.Config{
			RefreshInterval: cfg.Bounds.RefreshInterval,
			PageSize:        500, // Hardcoded for now, can be made configurable later
			BoundsTTL:       cfg.Bounds.BoundsTTL,
		},
		redisClient,
		elector,
		upstreamBounds,
	)

	boundsRedisProvider, ok := boundsProv.(*bounds.RedisProvider)
	if !ok {
		logger.Fatal("Failed to assert bounds provider type")
	}

	if startErr := boundsRedisProvider.Start(ctx); startErr != nil {
		logger.WithError(startErr).Fatal("Failed to start Redis bounds provider")
	}

	var boundsProvider bounds.Provider = boundsRedisProvider

	logger.Info("Bounds service started")

	// Note: RedisProvider.Start() blocks until Redis has data (with 30s timeout)
	// If we reach here, Redis is guaranteed to be populated
	// If timeout occurred, Start() would have returned error and we'd have Fatal'd above

	// Create server
	srv, err := server.New(logger, cfg, cartographoorProvider, boundsProvider)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create server")
	}

	// Start server in goroutine
	go func() {
		logger.WithField("port", cfg.Server.Port).Info("HTTP server starting")

		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("HTTP server error")
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	sig := <-sigChan

	logger.WithField("signal", sig.String()).Info("Received shutdown signal")

	// Graceful shutdown
	logger.Info("Initiating graceful shutdown...")

	// Cancel the application context to signal all services to stop
	// This triggers ctx.Done() in all service refresh loops
	cancel()

	// Create a timeout context for the shutdown process itself
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	// Shutdown order (reverse of startup dependencies):
	// 1. HTTP server (stop accepting requests)
	// 2. Redis providers (stop background loops that use Redis)
	// 3. Upstream services (stop their background loops)
	// 4. Leader election (release leadership lock)
	// 5. Redis client (close connections)

	// Stop HTTP server
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Error("Error during server shutdown")
	}

	// Stop Redis providers
	if cartographoorRedisProvider != nil {
		if err := cartographoorRedisProvider.Stop(); err != nil {
			logger.WithError(err).Error("Error stopping cartographoor Redis provider")
		}
	}

	if err := boundsRedisProvider.Stop(); err != nil {
		logger.WithError(err).Error("Error stopping bounds Redis provider")
	}

	// Stop upstream services
	if cartographoorSvc != nil {
		if err := cartographoorSvc.Stop(); err != nil {
			logger.WithError(err).Error("Error stopping cartographoor service")
		}
	}

	if err := upstreamBounds.Stop(); err != nil {
		logger.WithError(err).Error("Error stopping bounds service")
	}

	// Stop leader election (releases lock)
	if err := elector.Stop(); err != nil {
		logger.WithError(err).Error("Error stopping leader election")
	}

	// Stop Redis client (closes connections)
	if err := redisClient.Stop(); err != nil {
		logger.WithError(err).Error("Error stopping Redis client")
	}

	logger.Info("Server stopped gracefully")
}
