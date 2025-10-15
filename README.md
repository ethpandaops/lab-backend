# Lab Backend

API router and frontend server for the Ethereum Lab application.

## Overview

Lab Backend serves as a unified gateway that:
- Routes API requests to network-specific CBT API backends
- Serves the Lab frontend with optimized caching
- Provides configuration endpoint for network metadata

## Project Status

**Current Phase**: Plan 2 - Infrastructure (Complete)

This project is being built incrementally following the Ultrathink methodology:
- ✅ **Plan 1 - Foundation**: Go module, config package, version package, directory structure
- ✅ **Plan 2 - Infrastructure**: Middleware, server setup, health handlers, graceful shutdown
- ⏳ **Plan 3 - API Proxy**: Proxy logic, API routing, config endpoint
- ⏳ **Plan 4 - Frontend**: Frontend serving, static assets, config injection
- ⏳ **Plan 5 - Deployment**: Docker build, Kubernetes manifests, production config

## Project Structure

```
lab-backend/
├── cmd/
│   └── server/              # ✅ Main entry point with graceful shutdown
│       └── main.go
├── internal/
│   ├── config/              # ✅ Configuration loading and validation
│   │   ├── config.go
│   │   └── networks.go
│   ├── version/             # ✅ Version information
│   │   └── version.go
│   ├── middleware/          # ✅ HTTP middleware (logging, metrics, CORS, recovery)
│   │   ├── logging.go
│   │   ├── metrics.go
│   │   ├── cors.go
│   │   └── recovery.go
│   ├── server/              # ✅ Server setup with middleware chain
│   │   └── server.go
│   ├── handlers/            # ✅ HTTP handlers (health check)
│   │   └── health.go
│   ├── proxy/               # ⏳ Proxy logic (Plan 3)
│   ├── frontend/            # ⏳ Frontend serving (Plan 4)
│   └── api/                 # ⏳ API handlers (Plan 3)
├── web/                     # ⏳ Static assets (Plan 4)
├── config.example.yaml      # ✅ Configuration template
├── config.yaml              # Local config (gitignored, copy from example)
├── Makefile                 # ✅ Build automation
├── go.mod                   # ✅ Go module definition
└── README.md                # ✅ This file
```

## Configuration

The service is configured via `config.yaml` in the project root. Example:

```yaml
server:
  port: 8080
  host: "0.0.0.0"
  log_level: "info"

networks:
  - name: mainnet
    enabled: true
    target_url: "https://cbt-api-mainnet.primary.production.platform.ethpandaops.io/api/v1"
```

### Configuration Strategy

- **Local Development**: Uses external URLs (default in `config.yaml`)
- **Kubernetes**: ConfigMap can override with internal DNS for performance
  - Pattern: `http://{service}.{namespace}.svc.cluster.local:8080/api/v1`
  - Example: `http://cbt-api-mainnet.xatu.svc.cluster.local:8080/api/v1`

## Development

### Prerequisites

- Go 1.23.0+
- Access to CBT API backends

### Current Status

Plans 1 & 2 are complete. The HTTP server is fully operational with:
- ✅ Configuration loading and validation
- ✅ Version information with build-time injection
- ✅ HTTP server with middleware stack (logging, metrics, CORS, recovery)
- ✅ Health check endpoint at `/health`
- ✅ Prometheus metrics at `/metrics`
- ✅ Graceful shutdown handling

### Running the Server

```bash
# First-time setup: Copy config template
cp config.example.yaml config.yaml

# Build the server
make build

# Run the server
make run

# Or run directly
./bin/lab-backend -config config.yaml
```

### Testing Endpoints

```bash
# Health check
curl http://localhost:8080/health
# {"status":"healthy","version":"dev","timestamp":1697395200}

# Prometheus metrics
curl http://localhost:8080/metrics
```

### Testing Configuration

```go
package main

import (
    "fmt"
    "log"

    "github.com/ethpandaops/lab-backend/internal/config"
    "github.com/ethpandaops/lab-backend/internal/version"
)

func main() {
    // Load config
    cfg, err := config.Load("config.yaml")
    if err != nil {
        log.Fatal(err)
    }

    // Validate config
    if err := cfg.Validate(); err != nil {
        log.Fatal(err)
    }

    // Print version
    fmt.Printf("Version: %s\n", version.Full())

    // Print enabled networks
    for _, net := range cfg.GetEnabledNetworks() {
        fmt.Printf("Network: %s -> %s\n", net.Name, net.TargetURL)
    }
}
```

## Next Steps

See `ai_plans/lab-backend/03-api-proxy.md` for the next implementation phase, which will add:
- Network-based API routing
- Reverse proxy to CBT API backends
- Configuration API endpoint

## Architecture

```
Lab Frontend → Lab Backend → CBT API (mainnet/sepolia/holesky/hoodi)
```

## License

MIT License
