# Cloud Relay Deployment

This document is the deployment contract for human operators and AI agents.
Run all commands from `cloud/deploy/` unless a step says otherwise. Do not
improvise a different upgrade sequence.

## Mandatory Safety Rules

1. Never run `docker compose down -v`.
2. Never remove or recreate the `relay-data`, `relay-assets`, or
   `relay-backups` volumes during a normal deployment or upgrade.
3. Never run `docker system prune --volumes` on the Relay host.
4. Never skip `refresh-assets.sh` (sync and coverage verification) after building a new Relay image or before upgrading a Node to a new official release.
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

SQLite uses WAL with `synchronous=FULL`, one writer connection and up to four
read-only connections for node lookups/lists. The database directory must remain
on a local filesystem that supports SQLite locking and writable WAL/SHM sidecar
files. Keep the existing `relay-data` volume; do not move it to a network share.
Use the backup command below for a complete snapshot, including committed data
that has not yet been checkpointed from WAL into the main database file.

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

### Client IP and resource limits

`MINDFS_CLOUD_TRUSTED_PROXIES` accepts comma-separated IP CIDRs. The default is
empty: Relay uses the TCP peer address and ignores client-IP headers. When
behind a proxy, configure only the actual trusted proxy addresses/subnets; do
not use `0.0.0.0/0` or `::/0`. Relay walks `X-Forwarded-For` from right to left
until it reaches an untrusted address. It never trusts `CF-Connecting-IP`
directly, and the supplied Caddy example removes that header.

Before upgrading a proxied deployment, identify its current proxy chain and
configure the trusted CIDRs. An empty list is safe, but all clients arriving
through the same proxy will share its login quota. With Cloudflare or another
edge in front of Caddy/OpenResty, the edge and proxy must sanitize forwarded
headers and preserve a verifiable chain; adding a trusted CIDR does not make
arbitrary incoming headers trustworthy. Keep the existing network arrangement.

Password hashing and verification share a two-operation concurrency limit
(about 128 MiB of Argon2 working memory with default password parameters).
Excess work returns HTTP 429. Binding polls allow 600 requests per source per
minute; new challenges additionally have a global rolling limit of 120 per
minute and a 10,000-record storage cap. Existing challenges remain pollable at
the creation/storage caps. Oversized binding codes or device IDs are rejected.

Expired/revoked challenges are retained until 24 hours after their original
expiry, then removed in batches of at most 1,000 each cleanup pass. Device tokens
are stored separately and remain valid. Old databases exceeding the cap can
require several minute-spaced cleanup passes to reclaim their terminal rows;
active bindings are never removed to make room. SQLite can reuse the freed
pages; file size does not necessarily shrink immediately.

The Cloud build now requires Go 1.26.6 or newer. The supplied Dockerfile pins
Go 1.26.6 so a rebuild includes the reviewed standard-library security fixes.
Source changes alone do not update an already deployed binary.

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
./refresh-assets.sh
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
./refresh-assets.sh
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
uses. Both asset sync and coverage verification must finish successfully before
the Relay is recreated. The script runs one-off commands with `--no-deps`; it
does not start, stop, recreate, or migrate the running Relay.

## Node Upgrades Without a Cloud Upgrade

A Node can install a release newer than the running Cloud image. The one-shot
Compose `asset-sync` service does not poll for new releases. Before upgrading
any Node, refresh the existing asset volume using the already built image:

```bash
./refresh-assets.sh
```

This imports new official releases, retains historical hashed assets, then
checks the current entry and the immutable files recorded for every supported
release against the current GitHub catalog. The check reports the latest
covered tag and fails if a release is missing, a marker/file is invalid, the
release list is empty, or GitHub is unavailable. Failure leaves the running
Relay in place; investigate the reported error before upgrading the Node.

Once an image containing `check-assets` has been built, the same script can be
run from the existing 1Panel/host scheduler, for example daily, using its
absolute path. Configure the scheduler to report nonzero exit status and avoid
overlapping runs or application deployments. No scheduler is installed by this
repository. A daily refresh can lag a just-published release, so still run the
script immediately before a Node upgrade.

For a read-only coverage check, without syncing or restarting anything:

```bash
docker compose run --rm --no-deps relay check-assets /var/lib/mindfs-assets
```

This explicitly checks GitHub and hashes local recorded immutable assets; it
does not download archives or write files and does not require SMTP/database
initialization. It is a maintenance command, not part of `/readyz` or the Relay
request path. It verifies the mounted files, while the public HTTP checks below
verify reverse-proxy access. It cannot certify arbitrary development builds or
every historical version of shared, non-hashed assets.

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
- A missing marker or missing historical hashed file causes only the affected
  release to be downloaded and repaired.
- A corrupt existing hashed file is detected, but its differing content is an
  immutable-name collision and fails the sync; it is not silently overwritten.
- Existing content-hashed files are never deleted or overwritten.
- A same-name/different-content collision fails the sync and therefore blocks
  deployment.

Because the project does not modify the MindFS frontend, the official GitHub
Releases remain the recovery source for historical frontend assets. The
`relay-assets` volume is still preserved during routine upgrades so that sync
stays incremental and deployment does not depend on downloading every release
again.

`check-assets` uses the same release selection as `sync-assets` (stable releases
from `v0.1.8`, including all paginated results). Neither command treats an empty
supported-release list as successful verification.

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

`/readyz` does not check release freshness. It verifies that every current JS/CSS entry referenced by the deployed
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
