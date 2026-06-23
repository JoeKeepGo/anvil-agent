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
| `ANVIL_AGENT_MANAGED_INTERFACE_PREFIX` | `anvilwg` | Prefix for Anvil-managed WireGuard interfaces (e.g. `anvilwg0`) |

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
{"id":"state-1","status":200,"body":{"agent":{"id":"11111111-1111-4111-8111-111111111111","version":"dev","stateSchemaVersion":1,"startedAt":"2026-06-22T00:00:00Z","reportedAt":"2026-06-22T00:00:00Z"},"host":{"hostname":"anvil-local-vm","os":"linux","arch":"arm64"},"incus":{"available":true,"statusCode":200,"serverVersion":"6.x","apiVersion":"1.0"},"capabilities":{"incusProxy":true,"events":true,"stateReport":true,"wireGuard":false,"vmLifecycle":true},"snapshot":{"instancesTotal":0,"imagesTotal":0,"operationsTotal":0}}}
```

## Network Capability And Managed Apply

The agent reports Anvil-managed WireGuard/network readiness and accepts a narrow, allowlisted dry-run/apply protocol for Anvil-managed interfaces only. It never executes arbitrary shell text, never takes over unmanaged interfaces such as `wg0`/`eth0`, never mutates Incus instance NICs, and never exposes WireGuard private keys or preshared keys.

Network state request:

```json
{"id":"net-state","method":"GET","path":"/agent/v1/network/state"}
```

Response (browser-safe; private keys and preshared keys are never included):

```json
{"id":"net-state","status":200,"body":{"agent":{"id":"11111111-1111-4111-8111-111111111111","stateSchemaVersion":1},"network":{"wireGuardAvailable":true,"ipCommandAvailable":true,"iptablesAvailable":true,"ip6tablesAvailable":true,"forwarding":{"ipv4":true,"ipv6":true},"managedInterfaces":[]}}}
```

Network apply request (DRY_RUN performs no host mutation; APPLY validates and renders a plan deferred to the managed-service integration):

```json
{"id":"net-apply","method":"POST","path":"/agent/v1/network/apply","body":{"mode":"DRY_RUN","interface":{"name":"anvilwg0","listenPort":51820,"addresses":["10.42.0.1/24"]},"peers":[{"publicKey":"<peer-public-key>","allowedIps":["10.42.0.2/32"]}],"routing":{"ipv4Forwarding":true,"ipv6Forwarding":true}}}
```

Interface names must match the Anvil-managed prefix (`anvilwg` by default); requests for unmanaged interfaces, unsupported modes, malformed CIDRs, duplicate peer public keys, or oversized payloads are rejected with a safe validation error.

## VM Lifecycle Protocol

The agent exposes a narrow, trusted backend-to-agent VM lifecycle protocol. It dispatches only the allowlisted Incus instance operations `create`, `start`, `stop`, `restart`, and `delete`, and never exposes arbitrary Incus write paths, shell execution, snapshots, migration, console, or file operations. The agent owns lifecycle response normalization and never echoes raw Incus output, the Incus Unix socket path, tokens, host private config, or product state.

Endpoint shape (trusted WS messages, not browser-public):

```text
GET  /agent/v1/lifecycle/capabilities
POST /agent/v1/lifecycle/instances/create
POST /agent/v1/lifecycle/instances/{name}/start
POST /agent/v1/lifecycle/instances/{name}/stop
POST /agent/v1/lifecycle/instances/{name}/restart
POST /agent/v1/lifecycle/instances/{name}/delete
```

Instance names must match a DNS-label-safe allowlist and are URL-encoded into the Incus path. Create requests mirror the M13 backend VM lifecycle policy contract (`cpuCount`/`memoryBytes`/`rootDiskBytes`), fix the Incus instance type to `virtual-machine`, and emit bounded, validated limits only. Delete requires an explicit `confirm` field. Unknown JSON fields, path traversal, shell metacharacters, disallowed operation segments (e.g. `snapshot`/`exec`/`console`/`files`/`migrate`), and oversized payloads are rejected with agent-owned safe error codes. Async Incus operations are normalized to an `operationId` + `operationKind: "async"` echo with no raw Incus bytes.

## Security Model

By default, Anvil Agent binds to `127.0.0.1` and does not expose Incus to the network. For remote development or deployment, place it behind an explicit secure path such as SSH tunneling, private networking, a reverse proxy, or a future authenticated transport.

## License

MIT
