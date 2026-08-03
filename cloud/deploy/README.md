# Cloud Relay Deployment

Build and run from this directory after creating `.env` from `.env.example`.
Generate `MINDFS_CLOUD_TOKEN_KEY` as an unpadded base64url encoding of 32 random
bytes, set a public DNS name, and point that name at the host.

```bash
docker compose build
docker compose run --rm relay validate
docker compose run --rm relay migrate
docker compose up -d
docker compose ps
```

Caddy terminates HTTPS/WSS and forwards the original Host, scheme, and
WebSocket upgrade headers. The Cloud container listens only inside the Compose
network. SQLite lives in `relay-data`; generated backups live in
`relay-backups`. The application binary and Web asset bundle are read-only and
the final container runs as UID/GID 65532.

Create an online SQLite snapshot without stopping Relay:

```bash
docker compose exec relay mindfs-relay backup /backups/mindfs-cloud-$(date +%Y%m%d-%H%M%S).db
```

Copy a backup out of the named volume before storing it elsewhere:

```bash
docker compose cp relay:/backups/mindfs-cloud-YYYYMMDD-HHMMSS.db ./
```

Before restoring, stop the stack and preserve the current data volume. Restore
only a backup produced by `mindfs-relay backup`; direct copies of a live
`mindfs-cloud.db`, WAL, or SHM file are not supported.

Operational endpoints:

```text
GET /healthz   process liveness
GET /readyz    SQLite and Web asset readiness
GET /metrics   Prometheus text metrics
```
