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

### Option 1: Quick Start (Placeholder Frontend)

```bash
# 1. Configure networks and backends
cp config.example.yaml config.yaml
# Edit config.yaml with your CBT API backend URLs

# 2. Build and run
make run
```

Visit `http://localhost:8080` to see a placeholder page with server status.

### Option 2: Full Frontend (w/ Lab Application)

```bash
# 1. Configure networks and backends
cp config.example.yaml config.yaml

# 2. Build with full frontend (clones and builds lab frontend), pass FRONTEND_REF=release/v1.0.0 to switch branch/hash used.
make run-all
```

Visit `http://localhost:8080` to access the full Lab application.

**Note:** Requires Node.js 20+ and npm installed.

## Make Commands

| Command | Description |
|---------|-------------|
| `make help` | Show available commands |
| `make build` | Build the lab-backend binary (with placeholder frontend) |
| `make build-all` | Clone lab frontend, build it, and embed in backend |
| `make run` | Build and run the server (placeholder frontend, starts Redis automatically) |
| `make run-all` | Build with full frontend and run the server (starts Redis automatically) |
| `make redis` | Start Redis container for local development |
| `make stop-redis` | Stop and remove Redis container |
| `make clean` | Remove all build artifacts, cloned frontend, and stop Redis |
| `make test` | Run tests |

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
