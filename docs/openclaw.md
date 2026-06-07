# OpenClaw Through tunnel.to

OpenClaw should work through tunnel.to with the default browser-facing tunnel behavior:

```bash
tunnelto 18789
```

or, explicitly:

```bash
tunnelto 18789 --host-header preserve
```

The tunnel preserves the browser `Origin`, public `Host`, and standard forwarding headers. OpenClaw still requires the public tunnel origin to be listed in `gateway.controlUi.allowedOrigins`.

## Allow The Tunnel Origin

Use the helper script from this repository:

```bash
scripts/manage-openclaw-origins.sh add https://claw.tunnel.to
```

For local relay testing with `nip.io`, use the exact local origin:

```bash
scripts/manage-openclaw-origins.sh add http://claw.127.0.0.1.nip.io:8080
```

Origins must be exact `scheme://host[:port]` values. Do not include a path, query string, fragment, or wildcard.

To inspect or remove origins:

```bash
scripts/manage-openclaw-origins.sh list
scripts/manage-openclaw-origins.sh remove https://claw.tunnel.to
```

The helper edits `~/.openclaw/openclaw.json` by default and saves a timestamped backup next to it. Set `OPENCLAW_CONFIG` or pass `--config` if your config lives somewhere else:

```bash
scripts/manage-openclaw-origins.sh --config /path/to/openclaw.json add https://claw.tunnel.to
```

Restart or reload the OpenClaw gateway after changing the allowlist.

You should not need `gateway.controlUi.dangerouslyAllowHostHeaderOriginFallback` for tunnel.to. That fallback is only for proxies that strip the browser `Origin` header.
