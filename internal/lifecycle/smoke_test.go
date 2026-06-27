package lifecycle

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

// TestSmokeLifecycle is a fake-transport local smoke for the trusted VM
// lifecycle protocol, per docs/M13/03 agent-incus-lifecycle-protocol.md
// "Runtime Smoke" which permits fake Incus server/transport first.
func TestSmokeLifecycle(t *testing.T) {
	// Fake Incus that returns an async operation for stop and sync-ok for the
	// rest, and asserts every dispatched request is an allowlisted path.
	fake := &smokeIncus{resps: map[string]*incus.ProxyResponse{
		"/1.0/instances":            syncOK200(),
		"/1.0/instances/vm-1/state": {Status: http.StatusAccepted, Body: json.RawMessage(`{"type":"async","operation":"/1.0/operations/op-7"}`)},
		"/1.0/operations/op-7/wait": operationWaitSuccess(),
		"/1.0/instances/vm-1":       syncOK200(),
	}}
	s := NewService(fake)

	// Create
	c1 := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/create",
		json.RawMessage(`{"name":"vm-1","image":"ubuntu/24.04","cpuCount":2,"memoryBytes":5368709120,"rootDiskBytes":10737418240}`))
	requireOK(t, c1, "create")

	// Start
	c2 := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/start", nil)
	requireOK(t, c2, "start")

	// Stop -> async operation completed by the agent before returning.
	c3 := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/stop", nil)
	requireOK(t, c3, "stop")
	var resp Response
	json.Unmarshal(c3.Body, &resp)
	if resp.OperationKind != "async" || resp.OperationID != "op-7" || resp.Status != "operation-completed" {
		t.Fatalf("stop normalize = %v", resp)
	}

	// Restart
	c4 := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/restart", nil)
	requireOK(t, c4, "restart")

	// Delete with confirmation
	c5 := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/delete", json.RawMessage(`{"confirm":true}`))
	requireOK(t, c5, "delete")

	// Rejected: snapshot path, exec, unknown fields, unconfirmed delete, bad name.
	for _, p := range []string{
		"/agent/v1/lifecycle/instances/vm-1/snapshot",
		"/agent/v1/lifecycle/instances/vm-1/exec",
		"/agent/v1/lifecycle/instances/vm-1/files",
		"/agent/v1/lifecycle/instances/vm-1/console",
		"/agent/v1/lifecycle/instances/vm-1/migrate",
	} {
		if r := s.Handle(context.Background(), http.MethodPost, p, nil); r.Err == nil {
			t.Fatalf("disallowed segment accepted: %s", p)
		}
	}

	if r := s.Handle(context.Background(), http.MethodPost,
		"/agent/v1/lifecycle/instances/create",
		json.RawMessage(`{"name":"vm-1","image":"ubuntu/24.04","cpuCount":2,"memoryBytes":5368709120,"rootDiskBytes":10737418240,"shellCommand":"rm -rf /"}`)); r.Err == nil {
		t.Fatal("unknown field accepted")
	}
	if r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/delete", nil); r.Err == nil {
		t.Fatal("unconfirmed delete accepted")
	}
	if r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm 1/start", nil); r.Err == nil {
		t.Fatal("invalid name accepted")
	}

	// Confirm only allowlisted Incus paths were dispatched.
	for _, c := range fake.calls {
		switch c.Path {
		case "/1.0/instances", "/1.0/instances/vm-1/state", "/1.0/operations/op-7/wait", "/1.0/instances/vm-1":
		default:
			t.Fatalf("disallowed Incus path dispatched: %q", c.Path)
		}
		if strings.Contains(string(c.Body), "shell") || strings.Contains(string(c.Body), "snapshot") {
			t.Fatalf("forbidden field leaked to Incus: %s", c.Body)
		}
	}
}

func requireOK(t *testing.T, r Result, label string) {
	t.Helper()
	if r.Err != nil {
		t.Fatalf("%s failed: code=%s status=%d msg=%q", label, r.Err.Code, r.Err.Status, r.Err.Message)
	}
	if r.Status < 200 || r.Status >= 300 {
		t.Fatalf("%s status = %d", label, r.Status)
	}
	if len(r.Body) == 0 {
		t.Fatalf("%s body empty", label)
	}
}

func syncOK200() *incus.ProxyResponse {
	return &incus.ProxyResponse{Status: http.StatusOK, Body: json.RawMessage(`{"type":"sync"}`)}
}

type smokeIncus struct {
	calls []*incus.ProxyRequest
	resps map[string]*incus.ProxyResponse
}

func (f *smokeIncus) Execute(ctx context.Context, req *incus.ProxyRequest) *incus.ProxyResponse {
	f.calls = append(f.calls, req)
	if r, ok := f.resps[req.Path]; ok {
		return r
	}
	return syncOK200()
}
