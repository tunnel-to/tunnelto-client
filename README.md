# tunnelto-client

Public CLI for opening outbound tunnels to tunnel.to relays.

## Build

```bash
make build
```

## Use

```bash
tunnelto expose http://localhost:3000 --name claw --relay https://relay.tunnel.to
```

During local development, the relay defaults to `http://localhost:8080`.

## Test

```bash
make test
```
