# tunnelto-client

Public CLI for opening outbound tunnels to tunnel.to relays.

## Build

```bash
make build
```

## Use

```bash
tunnelto 3000
```

The default relay is Toronto and returns URLs under `https://<name>.tunnel.to`.

Select another region through the control plane:

```bash
tunnelto 3000 --region us-west
```

Or connect directly to a relay:

```bash
tunnelto 3000 --relay https://sfo1.tunnel.to
```

Supported region values include `ca-toronto`, `us-new-york`, `us-west`, and `eu-frankfurt`; common aliases like `tor`, `nyc`, `sfo`, `west`, and `fra` are accepted by the API.

Rewrite the upstream `Host` header for local apps that validate hostnames:

```bash
tunnelto 3000 --host-header localhost
tunnelto 127.0.0.1:3000 --host-header rewrite
tunnelto 3000 --upstream-host internal.example
```

The public tunnel host is still forwarded through `X-Forwarded-Host`; the client also forwards `X-Forwarded-For` and `X-Forwarded-Proto`, and derives a standard `Forwarded` header when possible. WebSocket upgrades preserve the browser `Origin`, public `Host`, and application-level headers such as `Sec-WebSocket-Protocol` by default; connection-specific WebSocket key/version/extension headers are regenerated for the local upstream handshake.

OpenClaw users still need to add the public tunnel origin to OpenClaw's normal Control UI allowlist. See [docs/openclaw.md](docs/openclaw.md) for the helper script and exact origin examples.

## Test

```bash
make test
```
