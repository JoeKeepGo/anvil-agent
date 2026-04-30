# M1 Verification

## Local Host-Agent Tests

```bash
GOCACHE=/tmp/anvil-go-cache GOMODCACHE=/tmp/anvil-go-modcache go test ./...
```

## Remote Host-Agent Tests

```bash
rsync -az -e 'ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' --delete --exclude '.git' --exclude 'tmp' --exclude 'docs/superpowers' /Users/joeyang/Documents/Projects/anvil/ root@47.74.37.12:/opt/anvil/
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 'cd /opt/anvil; go test ./...'
```

When verifying an unmerged feature worktree, replace the source and destination paths with the worktree under test, for example:

```bash
rsync -az -e 'ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' --delete --exclude '.git' --exclude 'tmp' --exclude 'docs/superpowers' /Users/joeyang/Documents/Projects/anvil/.worktrees/m1-phase-4-test-harness/ root@47.74.37.12:/opt/anvil-m1-phase-4/
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 'cd /opt/anvil-m1-phase-4; go test ./...'
```

## Runtime Build And Service

Build the host-agent binary on the development server:

```bash
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 'cd /opt/anvil; go build -o /usr/local/bin/anvil-host-agent ./cmd/anvil-proxy'
```

Install or update the development systemd service:

```bash
scp -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null deploy/systemd/anvil-host-agent.service root@47.74.37.12:/etc/systemd/system/anvil-host-agent.service
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 'systemctl daemon-reload && systemctl enable anvil-host-agent.service && systemctl restart anvil-host-agent.service'
```

Inspect service status and logs:

```bash
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 'systemctl status anvil-host-agent.service --no-pager -l'
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 'journalctl -u anvil-host-agent.service -n 100 --no-pager'
```

Expected log facts:

- `Anvil proxy listening on 127.0.0.1:9090`
- `Incus socket: /var/lib/incus/unix.socket`

Verify the service is active and locally bound:

```bash
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 'systemctl is-active anvil-host-agent.service'
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 "ss -ltnp | grep '127.0.0.1:9090'"
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 'curl http://127.0.0.1:9090/health'
```

Expected health response:

```json
{"status":"ok"}
```

## Tunnel Health Smoke

```bash
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -N -L 19090:127.0.0.1:9090 root@47.74.37.12
curl http://127.0.0.1:19090/health
```

Expected response:

```json
{"status":"ok"}
```

## WebSocket Smoke Requests

Send these JSON messages to `ws://127.0.0.1:19090/ws`:

```json
{"id":"m1-smoke-root","method":"GET","path":"/1.0"}
{"id":"m1-smoke-instances","method":"GET","path":"/1.0/instances"}
```

Expected behavior:

- response `id` equals request `id`
- response `status` is `200`
- response `body` is the raw Incus response JSON
- clients must match responses by request `id`, because event messages can arrive on the same WebSocket

## Dashboard Guard Checks

```bash
cd /Users/joeyang/Documents/Projects/anvil-dashboard
pnpm --filter @anvil/server typecheck
pnpm --filter @anvil/web typecheck
```

Dashboard checks are guards only. M1 host-agent behavior remains owned by this repository.
