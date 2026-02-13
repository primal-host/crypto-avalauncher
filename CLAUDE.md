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
- Traefik middleware: `noknok-auth@docker` (AT Protocol OAuth via noknok)
- DNS: `192.168.147.53` (infra CoreDNS)
- Docker socket mounted for container management
- SSH keys (ro) and agent socket mounted for remote host access

## Authentication

Two auth methods, checked in order by `checkBearer()`:

1. **noknok role header** — `X-User-Role: admin` set by Traefik forwardAuth (via noknok). Users with an `admin` grant in noknok get full API access automatically.
2. **Bearer token** — `Authorization: Bearer <ADMIN_KEY>` for direct API access (fallback).

The dashboard detects auth state from `/api/status` response. When authenticated via noknok, the user's Bluesky handle appears in the header badge and no manual key entry is needed.

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/health` | No | Health check |
| `GET` | `/` | No | Dashboard |
| `GET` | `/api/status` | No | Card counts + node summaries (auth for full details) |
| `POST` | `/api/nodes` | Yes | Create and start a node |
| `GET` | `/api/nodes` | Yes | List all nodes |
| `GET` | `/api/nodes/:id` | Yes | Get node details |
| `POST` | `/api/nodes/:id/start` | Yes | Start a stopped node |
| `POST` | `/api/nodes/:id/stop` | Yes | Stop a running node |
| `DELETE` | `/api/nodes/:id` | Yes | Remove node (?remove_volumes=true) |
| `GET` | `/api/nodes/:id/logs` | Yes | Container logs (?tail=50) |
| `GET` | `/api/events` | Yes | Audit event log (?limit=50) |
| `GET` | `/api/hosts` | Yes | List all hosts |
| `POST` | `/api/hosts` | Yes | Add remote host (name, ssh_addr) |
| `DELETE` | `/api/hosts/:id` | Yes | Remove host (no nodes) |
| `POST` | `/api/l1s` | Yes | Create L1 (name, vm, subnet_id, blockchain_id) |
| `GET` | `/api/l1s` | Yes | List L1s with validator counts |
| `GET` | `/api/l1s/:id` | Yes | Get L1 with validators |
| `DELETE` | `/api/l1s/:id` | Yes | Delete L1 (no validators) |
| `POST` | `/api/l1s/:id/validators` | Yes | Add validator (node_id, weight) |
| `DELETE` | `/api/l1s/:id/validators/:nodeId` | Yes | Remove validator |

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
- Host poller (2x health interval) pings remote hosts, auto-reconnects on failure
- Multi-host: nodes can target any connected host, port uniqueness scoped per host

## L1 Lifecycle

```
POST /api/l1s → pending (no subnet_id)
             → configured (with subnet_id) → active (Phase 4b)
```

- L1s start as `pending` until a subnet_id is assigned
- `configured` L1s trigger container reconfiguration when validators are added/removed
- Adding a validator to a configured L1 recreates the node's container with `AVAGO_TRACK_SUBNETS`
- Removing a validator also reconfigures the container (updates tracked subnets)
- Nodes cannot be deleted while they have L1 validator assignments
- L1s cannot be deleted while they have validators

## AvalancheGo Containers

- Container naming: `avax-<name>` (e.g., `avax-mainnet-1`)
- Volumes: `avax-<name>-db`, `avax-<name>-staking`, `avax-<name>-logs`
- Networks: `avax` (bridge) + `infra` (for Traefik routing)
- Staking port published to `0.0.0.0` for P2P
- HTTP API (9650) routed via Traefik with basic auth
- Labels: `managed-by=avalauncher`, `avalauncher.node-name=<name>`, Traefik labels

## Traefik RPC Routing

AvalancheGo node RPC endpoints are exposed via Traefik with basic auth:

- **HTTPS**: `https://<node-name>.avax.primal.host` (Let's Encrypt DNS challenge)
- **Local**: `http://<node-name>.avax.localhost`
- **Auth**: Basic auth (user/pass from `AVAGO_TRAEFIK_AUTH`)
- **Port**: Routes to container port 9650 (AvalancheGo HTTP API)

Config env vars:
- `AVAGO_TRAEFIK_DOMAIN` — Domain suffix (e.g., `avax.primal.host`). Empty disables routing.
- `AVAGO_TRAEFIK_NETWORK` — Docker network Traefik can reach (default: `infra`)
- `AVAGO_TRAEFIK_AUTH` — htpasswd entry for basicauth (e.g., `user:$2y$05$...`)

**DNS requirement**: Add `*.avax` wildcard A/CNAME record on Namecheap pointing to `primal.host`.

## Remote Hosts

- SSH-based Docker client via `connhelper` (github.com/docker/cli)
- Remote host must have Docker 18.09+ and SSH key auth
- Host info (hostname, OS, CPU, memory, Docker version) stored in `hosts.labels` JSONB
- Remote host key must be in `~/.ssh/known_hosts`
