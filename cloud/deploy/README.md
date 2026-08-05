# Cloud Relay Deployment

This document is the deployment contract for human operators and AI agents.
Run all commands from `cloud/deploy/` unless a step says otherwise. Do not
improvise a different upgrade sequence.

## Mandatory Safety Rules

1. Never run `docker compose down -v`.
2. Never remove or recreate the `relay-data`, `relay-assets`, or
   `relay-backups` volumes during a normal deployment or upgrade.
3. Never run `docker system prune --volumes` on the Relay host.
4. Never skip `asset-sync` after building a new Relay image.
5. Stop the deployment immediately if `asset-sync`, `validate`, `migrate`, a
   health check, or an asset verification command fails.
6. Keep `cloud/deploy/.env` only on the server. Never commit it or print its
   secrets in logs, chat, issue reports, or deployment summaries.
7. Do not change the existing DNS, Cloudflare, 1Panel, OpenResty, Caddy, or
   Docker-network arrangement during a routine application upgrade.
8. Never use `git reset --hard`, `git checkout --`, or file deletion to make a
   failed `git pull` succeed. Stop and report unexpected tracked changes.

`relay-data` contains the SQLite database. `relay-assets` contains the merged
Web assets used by all supported official MindFS client versions.
`relay-backups` contains generated database backups. Deleting volumes is not a
deployment step.

## Configuration

Create `.env` from `.env.example`. Generate `MINDFS_CLOUD_TOKEN_KEY` as an
unpadded base64url encoding of 32 random bytes, set the public Relay URL, and
configure the QQ SMTP authorization code in `MINDFS_CLOUD_SMTP_PASSWORD`. The
authorization code is not the QQ mailbox password.

The Relay accepts registrations only for exact `@qq.com` addresses.
Registration uses email, a user-defined Relay password, and an email code.
Later logins use email and the Relay password without another code.

Restrict the configuration file to the service administrator:

```bash
chmod 600 .env
```

`MINDFS_CLOUD_BOOTSTRAP_EMAIL` owns nodes migrated from a V0 database. That QQ
address must complete normal registration before those nodes become visible.

Before every deployment, verify that Compose can read the configuration:

```bash
docker compose config --quiet
```

Do not continue if this command fails.

## First Deployment

Use this sequence for a new server or a server on which the named volumes have
already been created intentionally:

```bash
set -euo pipefail
docker compose build --pull
docker compose run --rm asset-sync
docker compose run --rm relay validate
docker compose run --rm relay migrate
docker compose up -d
docker compose ps
```

`relay` must be healthy and `asset-sync` must exit successfully. If either is
not true, inspect the logs and stop. Do not delete volumes and retry from an
empty state.

## Required Upgrade Procedure

For every application upgrade, run exactly this sequence:

```bash
set -euo pipefail
git status --short
test -z "$(git status --porcelain --untracked-files=no)"
docker compose exec relay mindfs-relay backup \
  /backups/mindfs-cloud-pre-upgrade-$(date +%Y%m%d-%H%M%S).db
git pull --ff-only
docker compose config --quiet
docker compose build --pull --no-cache relay
docker compose run --rm asset-sync
docker compose run --rm relay validate
docker compose run --rm relay migrate
docker compose up -d --force-recreate relay
docker compose ps
```

Review `git status --short` before continuing. Untracked `.env`-style secrets
must remain untracked; unexpected changes to tracked files require operator
review. Do not automatically discard them.

Do not run `docker compose down` before upgrading. Recreating the `relay`
container does not require removing the Compose stack or its volumes.

The `relay` and `asset-sync` services use the same image tag. Building the
`relay` service updates the image that the following `asset-sync` command
uses. `asset-sync` must finish successfully before the Relay is recreated.

## How Historical Web Asset Sync Works

The Docker image contains only the Web bundle from the currently checked-out
source. `asset-sync` merges that bundle into the persistent `relay-assets`
volume and queries the official `a9gent/mindfs` GitHub Releases API for stable
releases from `v0.1.8` onward.

The sync is incremental:

- Every run fetches the release list, but it does not download every release
  archive again.
- A valid `.releases/{tag}.complete` marker and its verified files cause that
  release to be skipped.
- A newly published release downloads only its Linux AMD64 archive.
- A missing marker or missing/corrupt historical file causes only the affected
  release to be downloaded and repaired.
- Existing content-hashed files are never deleted or overwritten.
- A same-name/different-content collision fails the sync and therefore blocks
  deployment.

Because the project does not modify the MindFS frontend, the official GitHub
Releases remain the recovery source for historical frontend assets. The
`relay-assets` volume is still preserved during routine upgrades so that sync
stays incremental and deployment does not depend on downloading every release
again.

The Relay mounts `relay-assets` read-only and serves these files at
`/mindfs-assets/{file}`. Compose also prevents Relay startup when the
`asset-sync` initialization service fails.

## Post-Deployment Verification

The deployment is complete only after all of the following checks pass.
Replace `https://relay.example.com` with the configured public URL.

```bash
set -euo pipefail
BASE_URL=https://relay.example.com
curl --fail --silent --show-error "$BASE_URL/healthz"
curl --fail --silent --show-error "$BASE_URL/readyz"
docker compose ps
docker compose logs --tail=100 asset-sync relay
```

`/readyz` verifies that every current JS/CSS entry referenced by the deployed
`index.html` exists in `relay-assets`. To verify those same files through the
public reverse proxy, first copy the index out of the container and inspect the
actual content-hashed names:

```bash
docker compose cp relay:/var/lib/mindfs-assets/index.html \
  /tmp/mindfs-relay-index.html
ASSETS="$(grep -oE 'assets/[^"[:space:]<>]+\.(js|css)' \
  /tmp/mindfs-relay-index.html | sed 's#^assets/##' | sort -u)"
test -n "$ASSETS"
printf '%s\n' "$ASSETS" | while IFS= read -r asset; do
  curl --fail --silent --show-error --output /dev/null \
    "$BASE_URL/mindfs-assets/$asset" || exit 1
done
```

The commands must exit successfully. An empty asset list is a failure.

Finally, verify at least one known historical content-hashed asset also
returns HTTP `200`:

```bash
curl --fail --silent --show-error --output /dev/null \
  --write-out '%{http_code}\n' \
  "$BASE_URL/mindfs-assets/index-B4USfphH.js"
```

An HTTP `404`, a non-healthy Relay container, or an `asset-sync` error means
the deployment failed. Do not report success and do not delete volumes. Keep
the current state for diagnosis.

## Reverse Proxy Boundary

The repository includes a Caddy example that terminates HTTPS/WSS and proxies
to `relay:8080`. Production may instead use the existing Cloudflare plus
1Panel/OpenResty arrangement. In either case, the reverse proxy must:

- preserve the original `Host` and scheme;
- pass WebSocket upgrade headers;
- forward `/ws/connector`, `/n/{node-id}/...`, authentication routes, and
  `/mindfs-assets/...` to the same Relay service;
- keep the Relay application configured with its public HTTPS URL;
- avoid exposing container port `8080` publicly.

When 1Panel/OpenResty is already working, a routine Relay upgrade changes only
the Relay image and containers. It must not replace or reconfigure the existing
reverse proxy, Cloudflare DNS, TLS settings, or Docker network.

## Database Backup

Create an online SQLite snapshot without stopping Relay:

```bash
docker compose exec relay mindfs-relay backup \
  /backups/mindfs-cloud-$(date +%Y%m%d-%H%M%S).db
```

Copy the backup out of the named volume before storing it elsewhere:

```bash
docker compose cp \
  relay:/backups/mindfs-cloud-YYYYMMDD-HHMMSS.db ./
```

Before restoring, stop the Relay and preserve the current data volume. Restore
only a backup produced by `mindfs-relay backup`. Direct copies of a live
`mindfs-cloud.db`, WAL, or SHM file are not supported.

## Operational Endpoints

```text
GET /healthz   process liveness
GET /readyz    SQLite and Web asset readiness
GET /metrics   Prometheus text metrics
```
