# Eve Industry Planner

A modular-monolith Go codebase with multiple deployable services in a single repository and single Go module. Shared business logic and infrastructure code live under `internal/` and are imported by service entrypoints in `cmd/`.

## Overview

- Single Go module: shared code in `internal/...`; services in `cmd/...` each build to their own binary
- Nginx serves the frontend and proxies API and WebSocket traffic
- Infra services: NATS, Redis, MongoDB, MinIO, Keycloak
- Docker-first: all services built and run with Docker Compose

## Repository Layout

```
Eve-Industry-Planner-Standalone/
├── go.mod                      # Single Go module for entire repo
├── docker-compose.yml          # Orchestrates all services
├── configs/                    # Per-service environment files
│   ├── api.env
│   ├── worker.env
│   └── push.env
├── cmd/                        # Service entrypoints (each => its own container)
│   ├── api/                    # REST API service (port 8080)
│   │   └── Dockerfile
│   ├── worker/                 # Background job processor
│   │   └── Dockerfile
│   ├── push/                   # WebSocket push service (port 8090)
│   │   └── Dockerfile
│   └── web/                    # Frontend (built with Node, served by nginx)
│       └── Dockerfile
├── internal/                   # Shared code (not importable outside this module)
│   ├── core/                   # Clients + config (Mongo, NATS, Redis, Keycloak, MinIO)
│   ├── domain/                 # Business logic (industry, market, etc.)
│   ├── jobs/                   # Worker jobs
│   ├── messaging/              # NATS subjects + handlers
│   ├── ws/                     # WebSocket hub + broadcast helpers
│   └── shared/                 # Logger, utilities, helpers
├── nginx/                      # Reverse proxy config + Dockerfile
└── legacy/                     # Preserved previous implementation
```

## Services

- api (`cmd/api`)
  - REST API for the frontend
  - Uses shared logic from `internal/...`
  - Exposes port 8080 (proxied by nginx)
- worker (`cmd/worker`)
  - Background jobs and event-driven processing
  - Subscribes to NATS subjects; uses Mongo/MinIO/Redis as needed
- push (`cmd/push`)
  - WebSocket server for real-time updates and notifications
  - Exposes port 8090 (proxied by nginx at `/ws`)
- web (`cmd/web`)
  - Builds the frontend; final static assets are served by nginx
- nginx
  - Serves frontend static files
  - Proxies `/api/*` to `api:8080`
  - Proxies `/ws/*` to `push:8090` with upgrade headers

## Infrastructure

- NATS: lightweight message broker for inter-service events
- Redis: caching and session storage
- MongoDB: primary database
- MinIO: S3-compatible object storage
- Keycloak: auth provider (OIDC)

## Docker Compose (key points)

- Builds each service from the repo root so all can import `internal/...`
- Uses per-service Dockerfiles: `cmd/<service>/Dockerfile`
- Latest stable images pinned where appropriate
- Volumes for Mongo/MinIO data persistence

## Configuration

Service configuration is provided via per-service env files under `configs/`:

- `configs/api.env`
- `configs/worker.env`
- `configs/push.env`

Example keys (actual values set in your env files):

```
NATS_URL=nats://nats:4222
MONGO_URI=mongodb://mongo:27017/eve_industry
REDIS_URL=redis://redis:6379
KEYCLOAK_URL=http://keycloak:8080
MINIO_ENDPOINT=http://minio:9000
MINIO_ACCESS_KEY=minio
MINIO_SECRET_KEY=minio123
PORT=8080
```

`internal/core/config.go` loads env vars at runtime with sensible defaults for local compose.

## Build & Run

Prerequisites: Docker, Docker Compose.

- Start everything:

```bash
docker compose up --build
```

- Access:
  - Frontend via nginx: http://localhost:3000
  - API (proxied): http://localhost:3000/api/ (direct: http://localhost:8080)
  - WebSocket (proxied): ws://localhost:3000/ws/
  - Keycloak admin: http://localhost:8081
  - MinIO console: http://localhost:9001

## Local Go builds (optional)

You can build services directly with Go (still one module):

```bash
go mod tidy

go build ./cmd/api
go build ./cmd/worker
go build ./cmd/push
```

## Request Flows

- Frontend → API:
  1) Browser requests `http://localhost:3000`
  2) nginx serves static React build
  3) SPA calls `/api/...`
  4) nginx proxies to `api:8080`

- Frontend ↔ WebSocket:
  1) SPA connects to `ws://localhost:3000/ws/`
  2) nginx upgrades and proxies to `push:8090`
  3) `push` publishes/consumes via NATS for real-time events

- Background processing:
  1) `api` emits events to NATS (or schedules work)
  2) `worker` subscribes and executes domain logic
  3) Results saved to Mongo/MinIO and/or new events are published

## Coding Guidelines

- Single module: import shared code as `eve-industry-planner/internal/...`
- Keep domain logic in `internal/domain` and call from services
- Define event subjects/types in `internal/messaging`
- Wrap external services (Mongo/NATS/Redis/etc.) in `internal/core`
- Keep service `main.go` files small: wire config/clients, register routes/handlers, start server

## Production Notes

- Scale services independently via Compose replicas or an orchestrator
- Keep `container_name` unset in production for easier scaling
- Add health endpoints and readiness checks per service
- Persist Mongo/MinIO volumes and manage backups
- Secure Keycloak, MinIO, and inter-service networks for production

## Legacy

The prior implementation remains under `legacy/` for reference and gradual migration.
