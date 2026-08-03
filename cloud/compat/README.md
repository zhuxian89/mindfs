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

Each run uses temporary Cloud data, Node configuration, static assets, root
directory, binaries, and loopback ports. The Node process receives invalid
HTTP proxies for non-loopback traffic and a restricted `PATH`, so the scenario
does not contact hosted services or start local agent programs.

Failures identify the compatibility stage and include only a short redacted
process-log tail. Pairing secrets, bind codes, device tokens, E2EE keys, proofs,
ciphertext, authorization values, and protected payloads are not logged.
