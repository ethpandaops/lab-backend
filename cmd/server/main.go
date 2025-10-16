package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
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
	}).Info("Starting Lab Backend")

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.WithError(err).Fatal("Failed to load configuration")
	}

	// Set log level from config
	level, err := logrus.ParseLevel(cfg.Server.LogLevel)
	if err != nil {
		logger.WithError(err).Warn("Invalid log level, using info")

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

	// Create cartographoor service
	var cartographoorSvc *cartographoor.Service

	if cfg.Cartographoor.Enabled {
		var cerr error

		cartographoorSvc, cerr = cartographoor.New(&cfg.Cartographoor, logger)
		if cerr != nil {
			logger.WithError(cerr).Fatal("Failed to create cartographoor service")
		}

		// Start cartographoor service
		ctx := context.Background()
		if serr := cartographoorSvc.Start(ctx); serr != nil {
			logger.WithError(serr).Fatal("Failed to start cartographoor service")
		}

		logger.Info("Cartographoor service started")
	}

	// Create server
	srv, err := server.New(logger, cfg, cartographoorSvc)
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

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	// Stop cartographoor service
	if cartographoorSvc != nil {
		if err := cartographoorSvc.Stop(); err != nil {
			logger.WithError(err).Error("Error stopping cartographoor service")
		}
	}

	if err := srv.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("Error during shutdown")

		os.Exit(1)
	}

	logger.Info("Server stopped gracefully")
}
