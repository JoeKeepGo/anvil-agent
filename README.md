# Anvil Agent

Anvil Agent is the lightweight host-side agent for Anvil. It lets the Anvil control plane manage Incus hosts without exposing the Incus remote API by default.

It runs on each managed machine, talks to the local Incus Unix socket, and exposes a small WebSocket interface for trusted control-plane clients such as Anvil.

## Why Anvil

Incus already provides a REST API. For local access, that API is available through a Unix socket; for remote access, it can be exposed over HTTPS with Incus authentication.

Anvil Agent exists for deployments that want the control plane to avoid direct Incus remote API exposure. Instead of configuring remote TLS access on every Incus host, the Anvil backend can connect to a small host-side agent that keeps Incus access local.

## Use Cases

- Manage Incus hosts from the central Anvil backend.
- Keep Incus Unix socket access local to each host.
- Avoid placing Incus credentials or certificates in a browser client.
- Provide one small host protocol for fleet management.
- Run behind SSH tunnels, private networking, or a future secure agent transport.

## Architecture

```text
Anvil backend
  -> Anvil Agent
    -> Incus Unix socket
      -> Incus daemon
```

The agent persists only a host-local identity and reports browser-safe host state for trusted control-plane clients. Product-level concerns such as users, teams, authorization, audit logs, tenants, projects, endpoint inventory, and multi-host policy belong in the Anvil control-plane backend.

## Configuration

| Variable | Default | Description |
|---|---:|---|
| `ANVIL_AGENT_HOST` | `127.0.0.1` | Host address to bind |
| `ANVIL_AGENT_PORT` | `9090` | WebSocket port |
| `INCUS_SOCKET` | `/var/lib/incus/unix.socket` | Incus Unix socket path |
| `ANVIL_AGENT_STATE_DIR` | `/var/lib/anvil-agent` | Directory for the persisted host-local agent identity |
| `ANVIL_AGENT_AUTH_TOKEN` | empty | Optional bearer token for WebSocket access |

## Run

```bash
go run ./cmd/anvil-agent
```

Then connect to:

```text
ws://127.0.0.1:9090/ws
```

Health check:

```bash
curl http://127.0.0.1:9090/health
```

## Protocol Example

Incus proxy request:

Request:

```json
{"id":"1","method":"GET","path":"/1.0/instances"}
```

Response:

```json
{"id":"1","status":200,"body":{"type":"sync","status":"Success","metadata":[]}}
```

Agent state request:

```json
{"id":"state-1","method":"GET","path":"/agent/v1/state"}
```

Response:

```json
{"id":"state-1","status":200,"body":{"agent":{"id":"11111111-1111-4111-8111-111111111111","version":"dev","stateSchemaVersion":1,"startedAt":"2026-06-22T00:00:00Z","reportedAt":"2026-06-22T00:00:00Z"},"host":{"hostname":"anvil-local-vm","os":"linux","arch":"arm64"},"incus":{"available":true,"statusCode":200,"serverVersion":"6.x","apiVersion":"1.0"},"capabilities":{"incusProxy":true,"events":true,"stateReport":true,"wireGuard":false,"vmLifecycle":false},"snapshot":{"instancesTotal":0,"imagesTotal":0,"operationsTotal":0}}}
```

## Security Model

By default, Anvil Agent binds to `127.0.0.1` and does not expose Incus to the network. For remote development or deployment, place it behind an explicit secure path such as SSH tunneling, private networking, a reverse proxy, or a future authenticated transport.

## License

MIT
