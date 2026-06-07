# Scripts

This directory contains small helper scripts for tunnel.to users and maintainers.
Scripts here should be safe to run directly from the repository root and should
include `--help` or clear usage output when they accept arguments.

## manage-openclaw-origins

`manage-openclaw-origins.sh` updates OpenClaw's Control UI origin allowlist for
tunnel.to URLs. OpenClaw intentionally requires each browser origin to be listed
in `gateway.controlUi.allowedOrigins`; this helper edits that list without
enabling host-header fallback or relaxing OpenClaw's gateway checks.

Examples:

```bash
scripts/manage-openclaw-origins.sh list
scripts/manage-openclaw-origins.sh add https://claw.tunnel.to
scripts/manage-openclaw-origins.sh remove https://claw.tunnel.to
```

For local relay testing:

```bash
scripts/manage-openclaw-origins.sh add http://claw.127.0.0.1.nip.io:8080
```

By default the helper edits `~/.openclaw/openclaw.json` and writes a timestamped
backup next to it. To use another config path:

```bash
scripts/manage-openclaw-origins.sh --config /path/to/openclaw.json add https://claw.tunnel.to
```

Restart or reload the OpenClaw gateway after changing the allowlist.

See `docs/openclaw.md` for the full OpenClaw tunnel.to workflow.
