# Avalauncher

Avalanche L1 chain management dashboard.

## Project Structure

- `cmd/avalauncher/` — Entry point
- `internal/config/` — Environment + cluster.yaml config
- `internal/database/` — pgx pool, schema bootstrap
- `internal/docker/` — Docker SDK wrapper, AvalancheGo container config
- `internal/manager/` — Node lifecycle, health polling, event logging
- `internal/server/` — Echo HTTP server, routes, dashboard

## Build & Run

```bash
go build -o avalauncher ./cmd/avalauncher
go vet ./...

# Local run (needs postgres + docker)
DB_USER=dba_avalauncher DB_PASSWORD=xxx DB_HOST=localhost DB_PORT=5433 ADMIN_KEY=dev ./avalauncher

# Docker
./.launch.sh
```

## Conventions

- Go module: `github.com/primal-host/avalauncher`
- HTTP framework: Echo v4
- Database: pgx v5 on infra-postgres, database `avalauncher`
- Container name: `crypto-avalauncher`
- Schema auto-bootstraps via `CREATE TABLE IF NOT EXISTS`
- Config uses env vars with `_FILE` suffix support for Docker secrets

## Database

Postgres on `infra-postgres:5432` (host port 5433), database `avalauncher`, user `dba_avalauncher`.

Tables: `hosts`, `nodes`, `l1s`, `l1_validators`, `events`.

## Docker

- Image/container: `crypto-avalauncher`
- Networks: `infra` (postgres/traefik), `avax` (AvalancheGo nodes)
- Port: 4321
- Traefik: `avalauncher.primal.host` / `avalauncher.localhost`
- DNS: `192.168.147.53` (infra CoreDNS)
- Docker socket mounted for container management

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/health` | No | Health check |
| `GET` | `/` | No | Dashboard |
| `GET` | `/api/status` | No | Card counts + node summaries (auth for nodes) |
| `POST` | `/api/nodes` | Bearer | Create and start a node |
| `GET` | `/api/nodes` | Bearer | List all nodes |
| `GET` | `/api/nodes/:id` | Bearer | Get node details |
| `POST` | `/api/nodes/:id/start` | Bearer | Start a stopped node |
| `POST` | `/api/nodes/:id/stop` | Bearer | Stop a running node |
| `DELETE` | `/api/nodes/:id` | Bearer | Remove node (?remove_volumes=true) |
| `GET` | `/api/nodes/:id/logs` | Bearer | Container logs (?tail=50) |
| `GET` | `/api/events` | Bearer | Audit event log (?limit=50) |

## Node Lifecycle

```
POST /api/nodes → creating → running ⇄ stopped → DELETE
                      |           |
                      v           v
                   failed     unhealthy
```

- Image pull, container create, and start happen in a background goroutine
- Health poller (default 30s) checks running nodes via AvalancheGo JSON-RPC
- Node ID discovered automatically on first healthy check
- Startup reconciliation syncs DB status with actual Docker container states

## AvalancheGo Containers

- Container naming: `avax-<name>` (e.g., `avax-mainnet-1`)
- Volumes: `avax-<name>-db`, `avax-<name>-staking`, `avax-<name>-logs`
- Network: `avax` (bridge)
- Staking port published to `0.0.0.0` for P2P
- HTTP API (9650) internal to `avax` network only
- Labels: `managed-by=avalauncher`, `avalauncher.node-name=<name>`
