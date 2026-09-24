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

The client automatically registers an anonymous ephemeral tunnel with the
tunnel.to control plane, obtains a short-lived connect token, and returns a URL
under `https://<name>.tunnel.to`. No account or token setup is required.

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

## Streaming and SSE

tunnel.to supports Server-Sent Events (SSE), ordinary streaming HTTP, and WebSockets through anonymous, registered, and custom-domain tunnels. No client-side SSE mode is required.

For a local endpoint at `http://localhost:3000/events`:

```bash
tunnelto 3000
curl -N https://<generated-host>.tunnel.to/events
```

`curl -N` disables curl's own output buffering so each event is visible as it arrives. SSE responses should use `Content-Type: text/event-stream`; periodic comment heartbeats such as `: keepalive` are forwarded unchanged.

OpenClaw users still need to add the public tunnel origin to OpenClaw's normal Control UI allowlist. See [docs/openclaw.md](docs/openclaw.md) for the helper script and exact origin examples.

## Test

```bash
make test
```

## License

Licensed under the [Apache License, Version 2.0](LICENSE) (SPDX: `Apache-2.0`).
