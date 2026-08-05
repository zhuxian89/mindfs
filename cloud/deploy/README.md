# Cloud Relay Deployment

Build and run from this directory after creating `.env` from `.env.example`.
Generate `MINDFS_CLOUD_TOKEN_KEY` as an unpadded base64url encoding of 32 random
bytes, set a public DNS name, and point that name at the host. Configure the QQ
SMTP authorization code in `MINDFS_CLOUD_SMTP_PASSWORD`; it is not the QQ
mailbox password. The Relay accepts registrations only for exact `@qq.com`
addresses. Registration uses email, a user-defined Relay password, and an
email code; later logins use email and the Relay password without a code.

Keep `cloud/deploy/.env` only on the server and restrict it to the service
administrator:

```bash
chmod 600 .env
```

`MINDFS_CLOUD_BOOTSTRAP_EMAIL` owns nodes migrated from a V0 database. That QQ
address must complete normal registration before those nodes become visible.

```bash
docker compose build
docker compose run --rm asset-sync
docker compose run --rm relay validate
docker compose run --rm relay migrate
docker compose up -d
docker compose ps
```

`asset-sync` merges the Web bundle baked into the current image with every
official MindFS release from `v0.1.8` onward. The merged content-hashed files
live in the persistent `relay-assets` volume: existing files are never deleted
or overwritten, and a same-name/different-content collision fails the sync.
Relay mounts this volume read-only and continues to serve the client contract
at `/mindfs-assets/{file}`.

Upgrade the Relay without dropping assets required by older Nodes:

```bash
git pull --ff-only
docker compose build --pull --no-cache relay
docker compose run --rm asset-sync
docker compose up -d --force-recreate relay
docker compose ps
```

After an upgrade, verify both the current entry asset and a historical asset
return `200` before considering the deployment complete:

```bash
curl -I https://relay.example.com/mindfs-assets/index-DeNebQ9q.js
curl -I https://relay.example.com/mindfs-assets/index-B4USfphH.js
```

Caddy terminates HTTPS/WSS and forwards the original Host, scheme, and
WebSocket upgrade headers. The Cloud container listens only inside the Compose
network. SQLite lives in `relay-data`; the immutable multi-release Web asset
set lives in `relay-assets`; generated backups live in `relay-backups`. The
Relay application mounts the Web assets read-only and the final container runs
as UID/GID 65532.

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

The reverse proxy must preserve the original Host and scheme and pass WebSocket
upgrade headers for `/ws/connector` and `/n/{node-id}/...` routes.
