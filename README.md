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

## Test

```bash
make test
```
