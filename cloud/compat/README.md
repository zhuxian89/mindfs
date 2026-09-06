# MindFS Relay Compatibility Suite

This package runs the Cloud Relay against an unmodified MindFS CLI binary as
real subprocesses. The heavy scenario is opt-in:

```bash
cd cloud
MINDFS_RUN_COMPAT=1 go test ./compat -run TestUnmodifiedNodeRelayCompatibility -v
```

To test a prebuilt client binary instead of the current checkout:

```bash
MINDFS_RUN_COMPAT=1 \
MINDFS_COMPAT_NODE_BINARY=/path/to/mindfs \
go test ./compat -run TestUnmodifiedNodeRelayCompatibility -v
```

For the current Node feature contract (v0.5.0 or newer), also run:

```bash
MINDFS_RUN_COMPAT=1 go test ./compat -run TestCurrentNodeAPICompatibility -v
```

This verifies memory/release endpoints with an empty AgentPool, preference
round trips, prompt deletion, and the error responses of Codex rate-limit
endpoints. It does not consume real credits or run an Agent. E2EE requests with
encoded tool-call IDs are compared with direct Node requests; Relay must
preserve the exact path and return the same encrypted business response.
Use only the core suite for older binaries whose APIs predate this contract.

`MINDFS_COMPAT_NODE_BINARY` applies to both suites. For release validation,
download the official platform archive, verify its GitHub SHA-256 digest, and
pass the extracted binary. Do not point the tests at a wrapper that injects
real user configuration. Source builds and binary tests use a static fixture;
they do not prove that a production asset volume contains the release bundle.
Run the deployment `refresh-assets.sh` and public asset checks separately.

The asset importer also has an opt-in check against a downloaded official
Linux AMD64 archive. Save the corresponding GitHub release metadata as a JSON
array containing that one release, then run from `cloud/`:

```bash
MINDFS_RUN_COMPAT=1 \
MINDFS_COMPAT_RELEASE_ARCHIVE=/path/to/mindfs_v0.5.0_linux_amd64.tar.gz \
MINDFS_COMPAT_RELEASE_METADATA=/path/to/release.json \
go test ./internal/assetsync -run TestOfficialReleaseAssetCompatibility -v
```

It serves the downloaded archive on loopback, retains GitHub's original size
and SHA-256 for importer validation, compares every extracted asset with the
imported copy, and verifies coverage and an incremental second sync. It does
not access production assets or download other versions.

Each run uses temporary Cloud data, Node configuration, static assets, root
directory, binaries, and loopback ports. The Node process receives invalid
HTTP proxies for non-loopback traffic and a restricted `PATH`, so the scenario
does not contact hosted services or start local agent programs. Before starting
the real Relay process, the harness registers a temporary QQ-only Cloud User
through the Identity Service and then uses the public email/password login API.

Failures identify the compatibility stage and include only a short redacted
process-log tail. Pairing secrets, bind codes, device tokens, E2EE keys, proofs,
ciphertext, authorization values, and protected payloads are not logged.
