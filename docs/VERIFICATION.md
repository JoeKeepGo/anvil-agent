# M1 Verification

## Local Anvil Agent Tests

```bash
GOCACHE=/tmp/anvil-go-cache GOMODCACHE=/tmp/anvil-go-modcache go test ./...
```

## Remote Anvil Agent Tests

```bash
rsync -az -e 'ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' --delete --exclude '.git' --exclude 'tmp' --exclude 'docs/superpowers' /Users/joeyang/Documents/Projects/anvil-agent/ root@47.74.37.12:/opt/anvil-agent/
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 'cd /opt/anvil-agent; go test ./...'
```

When verifying an unmerged feature worktree, replace the source and destination paths with the worktree under test, for example:

```bash
rsync -az -e 'ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' --delete --exclude '.git' --exclude 'tmp' --exclude 'docs/superpowers' /Users/joeyang/Documents/Projects/anvil-agent/.worktrees/m1-phase-4-test-harness/ root@47.74.37.12:/opt/anvil-agent-m1-phase-4/
ssh -i ~/.ssh/anvil_dev_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@47.74.37.12 'cd /opt/anvil-agent-m1-phase-4; go test ./...'
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

## Dashboard Guard Checks

```bash
cd /Users/joeyang/Documents/Projects/anvil
pnpm --filter @anvil/server typecheck
pnpm --filter @anvil/web typecheck
```

Anvil control-plane checks are guards only. M1 Anvil Agent behavior remains owned by this repository.
