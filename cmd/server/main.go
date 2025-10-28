package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/bounds"
	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/ethpandaops/lab-backend/internal/leader"
	"github.com/ethpandaops/lab-backend/internal/redis"
	"github.com/ethpandaops/lab-backend/internal/server"
	"github.com/ethpandaops/lab-backend/internal/version"
	"github.com/ethpandaops/lab-backend/internal/wallclock"
)

// infrastructure holds core infrastructure components.
type infrastructure struct {
	redisClient redis.Client
	elector     leader.Elector
}

// services holds application services.
type services struct {
	cartographoorSvc      *cartographoor.Service
	cartographoorProvider cartographoor.Provider
	upstreamBounds        *bounds.Service
	boundsProvider        bounds.Provider
	wallclockSvc          *wallclock.Service
}

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Setup logger
	logger := setupLogger()

	// Create application context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load and validate configuration
	cfg, err := loadAndValidateConfig(ctx, logger, *configPath)
	if err != nil {
		logger.WithError(err).Fatal("Configuration error")
	}

	// Setup infrastructure (redis, leader election, etc)
	infra, err := setupInfrastructure(ctx, logger, cfg)
	if err != nil {
		logger.WithError(err).Fatal("Infrastructure setup failed")
	}

	// Setup services (cartographoor, bounds)
	svc, err := setupServices(ctx, logger, cfg, infra)
	if err != nil {
		logger.WithError(err).Fatal("Service setup failed")
	}

	// Start HTTP server
	srv, err := startServer(cfg, logger, infra, svc)
	if err != nil {
		logger.WithError(err).Fatal("Server startup failed")
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	sig := <-sigChan

	logger.WithField("signal", sig.String()).Info("Received shutdown signal")

	// Cancel application context to signal all services to stop
	cancel()

	// Perform graceful shutdown
	shutdownGracefully(logger, cfg, srv, svc, infra)
}

// setupLogger creates and configures the application logger.
func setupLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		ForceColors:   true,
		FullTimestamp: true,
	})

	logger.WithFields(logrus.Fields{
		"version":    version.Short(),
		"git_commit": version.GitCommit,
		"build_date": version.BuildDate,
	}).Info("Starting...")

	return logger
}

// loadAndValidateConfig loads the configuration file and validates it.
func loadAndValidateConfig(
	_ context.Context,
	logger *logrus.Logger,
	configPath string,
) (*config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// Set log level from config
	level, parseErr := logrus.ParseLevel(cfg.Server.LogLevel)
	if parseErr != nil {
		logger.WithError(parseErr).Warn("Invalid log level, using info")

		level = logrus.InfoLevel
	}

	logger.SetLevel(level)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"port":      cfg.Server.Port,
		"log_level": cfg.Server.LogLevel,
	}).Info("Configuration loaded")

	return cfg, nil
}

// setupInfrastructure initializes Redis and leader election.
func setupInfrastructure(
	ctx context.Context,
	logger *logrus.Logger,
	cfg *config.Config,
) (*infrastructure, error) {
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

	if err := redisClient.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start Redis client: %w", err)
	}

	// Initialize leader election
	elector := leader.NewElector(logger, leader.Config{
		LockKey:       cfg.Leader.LockKey,
		LockTTL:       cfg.Leader.LockTTL,
		RenewInterval: cfg.Leader.RenewInterval,
		RetryInterval: cfg.Leader.RetryInterval,
	}, redisClient)

	if err := elector.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start leader election: %w", err)
	}

	return &infrastructure{
		redisClient: redisClient,
		elector:     elector,
	}, nil
}

// setupServices initializes cartographoor and bounds services.
// Providers.Start() used here will block until redis has data to give us
// a guarantee we can boot.
func setupServices(
	ctx context.Context,
	logger *logrus.Logger,
	cfg *config.Config,
	infra *infrastructure,
) (*services, error) {
	svc := &services{}

	// Create cartographoor service
	var err error

	// Create upstream service (fetches from Cartographoor API)
	svc.cartographoorSvc, err = cartographoor.New(&cfg.Cartographoor, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create cartographoor service: %w", err)
	}

	// Wrap with Redis provider
	svc.cartographoorProvider = cartographoor.NewRedisProvider(
		logger,
		cfg.Cartographoor,
		infra.redisClient,
		infra.elector,
		svc.cartographoorSvc,
	)

	err = svc.cartographoorProvider.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start cartographoor provider: %w", err)
	}

	logger.Info("Cartographoor service started")

	// Create upstream bounds service
	svc.upstreamBounds, err = bounds.New(logger, cfg, svc.cartographoorProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create bounds service: %w", err)
	}

	// Wrap with Redis provider
	svc.boundsProvider = bounds.NewRedisProvider(
		logger,
		bounds.Config{
			RefreshInterval: cfg.Bounds.RefreshInterval,
			PageSize:        500,
			BoundsTTL:       cfg.Bounds.BoundsTTL,
		},
		infra.redisClient,
		infra.elector,
		svc.upstreamBounds,
	)

	err = svc.boundsProvider.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start bounds provider: %w", err)
	}

	logger.Info("Bounds service started")

	// Initialize wallclock service
	svc.wallclockSvc = wallclock.New(logger)

	err = svc.wallclockSvc.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start wallclock service: %w", err)
	}

	// Populate wallclocks from cartographoor networks
	networks := svc.cartographoorProvider.GetActiveNetworks(ctx)
	for name, network := range networks {
		genesisTimeWithDelay := time.Unix(network.GenesisTime+network.GenesisDelay, 0)

		if err := svc.wallclockSvc.AddNetwork(wallclock.NetworkConfig{
			Name:           name,
			GenesisTime:    genesisTimeWithDelay,
			SecondsPerSlot: 12,
		}); err != nil {
			logger.WithFields(logrus.Fields{
				"network": name,
				"error":   err.Error(),
			}).Warn("Failed to add wallclock for network")
		}
	}

	logger.WithField("networks", len(networks)).Info("Wallclock service started")

	// Sync wallclocks when cartographoor updates
	go func() {
		notifyChan := svc.cartographoorProvider.NotifyChannel()

		for {
			select {
			case <-notifyChan:
				logger.Debug("Cartographoor updated, syncing wallclocks")

				networks := svc.cartographoorProvider.GetActiveNetworks(ctx)

				for name, network := range networks {
					genesisTime := time.Unix(network.GenesisTime, 0)

					if err := svc.wallclockSvc.AddNetwork(wallclock.NetworkConfig{
						Name:           name,
						GenesisTime:    genesisTime,
						SecondsPerSlot: 12,
					}); err != nil {
						logger.WithFields(logrus.Fields{
							"network": name,
							"error":   err.Error(),
						}).Warn("Failed to update wallclock for network")
					}
				}

				logger.Debug("Wallclocks synced with cartographoor")
			case <-ctx.Done():
				return
			}
		}
	}()

	return svc, nil
}

// startServer creates and starts the HTTP server.
func startServer(
	cfg *config.Config,
	logger *logrus.Logger,
	infra *infrastructure,
	svc *services,
) (*server.Server, error) {
	srv, err := server.New(logger, cfg, infra.redisClient, svc.cartographoorProvider, svc.boundsProvider, svc.wallclockSvc)
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}

	// Start server in goroutine
	go func() {
		logger.WithField("port", cfg.Server.Port).Info("HTTP server starting")

		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("HTTP server error")
		}
	}()

	return srv, nil
}

// shutdownGracefully performs graceful shutdown of all services.
// Shutdown order:
// 1. HTTP server (stop accepting requests).
// 2. Providers (stop background loops that use Redis).
// 3. Leader election (release leadership lock).
// 4. Redis client (close connections).
func shutdownGracefully(
	logger *logrus.Logger,
	cfg *config.Config,
	srv *server.Server,
	svc *services,
	infra *infrastructure,
) {
	logger.Info("Initiating graceful shutdown...")

	// Create a timeout context for the shutdown process
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	// Stop HTTP server
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Error("Error during server shutdown")
	}

	// Stop providers
	if svc.cartographoorProvider != nil {
		if err := svc.cartographoorProvider.Stop(); err != nil {
			logger.WithError(err).Error("Error stopping cartographoor provider")
		}
	}

	if svc.boundsProvider != nil {
		if err := svc.boundsProvider.Stop(); err != nil {
			logger.WithError(err).Error("Error stopping bounds provider")
		}
	}

	// Stop wallclock service
	if svc.wallclockSvc != nil {
		if err := svc.wallclockSvc.Stop(); err != nil {
			logger.WithError(err).Error("Error stopping wallclock service")
		}
	}

	// Stop leader election (releases lock)
	if err := infra.elector.Stop(); err != nil {
		logger.WithError(err).Error("Error stopping leader election")
	}

	// Stop Redis client (closes connections)
	if err := infra.redisClient.Stop(); err != nil {
		logger.WithError(err).Error("Error stopping Redis client")
	}

	logger.Info("Server stopped gracefully")
}
