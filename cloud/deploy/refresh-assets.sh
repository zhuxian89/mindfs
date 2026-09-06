#!/bin/sh
set -eu

# Resolve the existing Compose project from this script's directory, including
# when an operator's scheduler starts the command in another working directory.
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

docker compose config --quiet
docker compose run --rm --no-deps asset-sync
docker compose run --rm --no-deps relay check-assets /var/lib/mindfs-assets
