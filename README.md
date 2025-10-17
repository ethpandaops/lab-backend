# lab-backend

API gateway and frontend server for routing requests to network-specific CBT API backends.

## Overview

lab-backend is a reverse proxy that routes API requests to different CBT API backends based on network (mainnet, sepolia, holesky, hoodi, etc). It also serves the Lab frontend with runtime configuration injection.

**What it does:**
- Routes `/api/v1/{network}/*` requests to the appropriate CBT API backend
- Serves frontend static assets with embedded filesystem
- Injects runtime configuration into the frontend (network metadata, experiment flags)
- Provides health and metrics endpoints for observability

## Quick Start

```bash
# 1. Configure networks and backends
cp config.example.yaml config.yaml
# Edit config.yaml with your CBT API backend URLs

# 2. Build and run (downloads latest frontend from GitHub releases)
make run
```

Visit `http://localhost:8080` to access the full Lab application.

### Using a Local Frontend

If you're developing the frontend locally:

```bash
# Use local frontend source (requires built dist/ directory)
FRONTEND_SOURCE=../lab make run
```

### Using a Specific Branch

```bash
# Download frontend from a specific branch's latest release
FRONTEND_BRANCH=develop make run
```

## Make Commands

| Command | Description |
|---------|-------------|
| `make help` | Show available commands |
| `make build` | Download frontend from GitHub releases and build the lab-backend binary |
| `make run` | Build and run the server (starts Redis automatically) |
| `make redis` | Start Redis container for local development |
| `make stop-redis` | Stop and remove Redis container |
| `make clean` | Remove all build artifacts, frontend directory, and stop Redis |
| `make test` | Run tests |

**Environment Variables:**
- `FRONTEND_SOURCE` - Path to local frontend source (uses `dist/` directory)
- `FRONTEND_BRANCH` - Download from specific branch's latest release (e.g., `develop`)
- `GITHUB_REPO` - GitHub repository for frontend releases (default: `ethpandaops/lab`)

## Configuration

Copy `config.example.yaml` to `config.yaml` and configure:

### Server Settings

```yaml
server:
  port: 8080
  host: "0.0.0.0"
  read_timeout: 30s
  write_timeout: 30s
  shutdown_timeout: 10s
  log_level: "info"
```

### Network Configuration

```yaml
networks:
  - name: mainnet
    enabled: true
    target_url: "https://cbt-api-mainnet.primary.production.platform.ethpandaops.io/api/v1"

  - name: sepolia
    enabled: true
    target_url: "https://cbt-api-sepolia.primary.production.platform.ethpandaops.io/api/v1"
```

**For Kubernetes deployments**, use internal cluster DNS:

```yaml
target_url: "http://cbt-api-mainnet.xatu.svc.cluster.local:8080/api/v1"
```

### Proxy Routing

Requests to `/api/v1/{network}/*` are proxied to the configured backend:

```bash
# Routes to mainnet CBT API
GET /api/v1/mainnet/fct_block?slot_eq=1000

# Routes to sepolia CBT API
GET /api/v1/sepolia/fct_attestation_correctness_by_validator_head
```

**Error responses:**
- `400` - Invalid path format
- `404` - Network not found in configuration
- `503` - Network disabled (set `enabled: false` in config)

### Frontend

```bash
GET /          # Serves index.html with injected config
GET /app/*     # SPA routing (falls back to index.html)
GET /static/*  # Static assets with 1-year cache headers
```

## How It Works

### Request Flow

```
Browser Request
  ↓
Lab Backend
  ├─ /api/v1/{network}/*  → Extract network → Proxy to CBT API backend
  ├─ /api/v1/config       → Return config JSON
  ├─ /health, /metrics    → Health/observability endpoints
  └─ /* (everything else) → Serve frontend (index.html or static assets)
```
