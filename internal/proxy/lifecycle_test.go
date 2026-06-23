package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/JoeKeepGo/anvil-agent/internal/config"
	"github.com/JoeKeepGo/anvil-agent/internal/incus"
	"github.com/JoeKeepGo/anvil-agent/internal/lifecycle"
	"github.com/JoeKeepGo/anvil-agent/internal/state"
	"github.com/coder/websocket"
)

func newLifecycleServer(t *testing.T, fake lifecycle.IncusBackend) *Server {
	t.Helper()
	reporter := state.NewStaticReporter(state.Report{
		Agent: state.AgentSummary{ID: "11111111-1111-4111-8111-111111111111", StateSchemaVersion: 1},
	})
	return NewServerWithLifecycle(
		&config.Config{},
		&fakeIncusBackend{},
		reporter,
		nil, nil,
		lifecycle.NewService(fake),
	)
}

func dialLifecycle(t *testing.T, token string, fake lifecycle.IncusBackend) (*websocket.Conn, *Server) {
	t.Helper()
	server := newLifecycleServer(t, fake)
	server.cfg.AuthToken = token
	conn, _, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn, server
}

func TestLifecycleCapabilitiesRoute(t *testing.T) {
	conn, _ := dialLifecycle(t, "", &fakeLifecycleIncus{})
	defer conn.CloseNow()

	writeWebSocketMessage(t, conn, []byte(`{"id":"caps","method":"GET","path":"/agent/v1/lifecycle/capabilities"}`))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	var caps lifecycle.CapabilitiesResponse
	if err := json.Unmarshal(resp.Body, &caps); err != nil {
		t.Fatalf("unmarshal caps: %v", err)
	}
	if len(caps.SupportedActions) != 5 {
		t.Fatalf("actions = %d, want 5", len(caps.SupportedActions))
	}
}

func TestLifecycleCapabilitiesRejectsNonGET(t *testing.T) {
	conn, _ := dialLifecycle(t, "", &fakeLifecycleIncus{})
	defer conn.CloseNow()

	writeWebSocketMessage(t, conn, []byte(`{"id":"caps-post","method":"POST","path":"/agent/v1/lifecycle/capabilities"}`))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
}

func TestLifecycleNotConfiguredReturns503(t *testing.T) {
	server := NewServerWithLifecycle(&config.Config{}, &fakeIncusBackend{}, nil, nil, nil, nil)
	server.upgrader = websocket.AcceptOptions{InsecureSkipVerify: true}
	conn, _, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: websocketPipeTransport{handler: server.handleWebSocket}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	writeWebSocketMessage(t, conn, []byte(`{"id":"cap-unset","method":"GET","path":"/agent/v1/lifecycle/capabilities"}`))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.Status)
	}
}

func TestLifecycleUnknownPathReturns404(t *testing.T) {
	conn, _ := dialLifecycle(t, "", &fakeLifecycleIncus{})
	defer conn.CloseNow()

	writeWebSocketMessage(t, conn, []byte(`{"id":"unknown","method":"POST","path":"/agent/v1/lifecycle/instances/vm/snapshot"}`))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.Status)
	}
}

func TestLifecycleCreateHappyPath(t *testing.T) {
	fake := &fakeLifecycleIncus{}
	conn, _ := dialLifecycle(t, "", fake)
	defer conn.CloseNow()

	body := `{"id":"create","method":"POST","path":"/agent/v1/lifecycle/instances/create","body":{"name":"vm-1","image":"ubuntu/24.04","cpuCount":1,"memoryBytes":1024,"rootDiskBytes":1024}}`
	writeWebSocketMessage(t, conn, []byte(body))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("incus calls = %d, want 1", len(fake.calls))
	}
	if fake.calls[0].Path != "/1.0/instances" {
		t.Fatalf("path = %q", fake.calls[0].Path)
	}
	if !strings.Contains(string(fake.calls[0].Body), `"virtual-machine"`) {
		t.Fatalf("body not allowlisted: %s", fake.calls[0].Body)
	}
}

func TestLifecycleCreateInvalidPayloadRejected(t *testing.T) {
	fake := &fakeLifecycleIncus{}
	conn, _ := dialLifecycle(t, "", fake)
	defer conn.CloseNow()

	body := `{"id":"create-bad","method":"POST","path":"/agent/v1/lifecycle/instances/create","body":{"name":"BAD NAME","image":"ubuntu/24.04","cpuCount":1,"memoryBytes":1024,"rootDiskBytes":1024}}`
	writeWebSocketMessage(t, conn, []byte(body))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
	if strings.Contains(strings.ToLower(resp.Error), "bad name") {
		t.Fatalf("error echoed submitted name: %q", resp.Error)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("incus calls = %d, want 0", len(fake.calls))
	}
}

func TestLifecycleStartURLPath(t *testing.T) {
	fake := &fakeLifecycleIncus{}
	conn, _ := dialLifecycle(t, "", fake)
	defer conn.CloseNow()

	writeWebSocketMessage(t, conn, []byte(`{"id":"start","method":"POST","path":"/agent/v1/lifecycle/instances/started-vm/start"}`))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if fake.calls[0].Path != "/1.0/instances/started-vm/state" {
		t.Fatalf("path = %q", fake.calls[0].Path)
	}
}

func TestLifecycleDeleteRequiresConfirm(t *testing.T) {
	fake := &fakeLifecycleIncus{}
	conn, _ := dialLifecycle(t, "", fake)
	defer conn.CloseNow()

	writeWebSocketMessage(t, conn, []byte(`{"id":"del","method":"POST","path":"/agent/v1/lifecycle/instances/vm-1/delete","body":{"confirm":false}}`))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("incus calls = %d, want 0", len(fake.calls))
	}
}

func TestLifecycleDeleteConfirmExecutes(t *testing.T) {
	fake := &fakeLifecycleIncus{}
	conn, _ := dialLifecycle(t, "", fake)
	defer conn.CloseNow()

	writeWebSocketMessage(t, conn, []byte(`{"id":"del-ok","method":"POST","path":"/agent/v1/lifecycle/instances/vm-1/delete","body":{"confirm":true}}`))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if fake.calls[0].Path != "/1.0/instances/vm-1" {
		t.Fatalf("path = %q", fake.calls[0].Path)
	}
}

// --- no generic Incus write proxy -------------------------------------------

func TestLifecycleDoesNotForwardArbitraryIncusWrites(t *testing.T) {
	fake := &fakeLifecycleIncus{}
	conn, _ := dialLifecycle(t, "", fake)
	defer conn.CloseNow()

	// Unsupported state segment must not reach Incus.
	writeWebSocketMessage(t, conn, []byte(`{"id":"exec-reject","method":"POST","path":"/agent/v1/lifecycle/instances/vm/exec"}`))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.Status)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("incus calls = %d, want 0", len(fake.calls))
	}
}

func TestLifecycleRejectsUnknownBodyFieldOverWebSocket(t *testing.T) {
	fake := &fakeLifecycleIncus{}
	conn, _ := dialLifecycle(t, "", fake)
	defer conn.CloseNow()

	body := `{"id":"snap-field","method":"POST","path":"/agent/v1/lifecycle/instances/create","body":{"name":"vm-1","image":"ubuntu/24.04","cpuCount":1,"memoryBytes":1,"rootDiskBytes":1,"snapshot":true}}`
	writeWebSocketMessage(t, conn, []byte(body))
	resp := readProxyResponse(t, conn)
	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("incus calls = %d, want 0", len(fake.calls))
	}
}

func TestLifecycleAuthPreserved(t *testing.T) {
	fake := &fakeLifecycleIncus{}
	server := newLifecycleServer(t, fake)
	server.cfg.AuthToken = "secret"

	_, resp, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
	})
	if err == nil {
		t.Fatal("dial succeeded, want auth failure")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", responseStatus(resp))
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer secret")
	conn, _, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
		HTTPHeader: header,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	writeWebSocketMessage(t, conn, []byte(`{"id":"caps-auth","method":"GET","path":"/agent/v1/lifecycle/capabilities"}`))
	r := readProxyResponse(t, conn)
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", r.Status)
	}
}

func TestLifecycleResponseNoLeak(t *testing.T) {
	fake := &fakeLifecycleIncus{resp: &incus.ProxyResponse{
		Status: http.StatusAccepted,
		Body:   json.RawMessage(`{"operation":"/1.0/operations/op-1","metadata":{"user_data":"MUST-NOT-LEAK"}}`),
	}}
	conn, _ := dialLifecycle(t, "", fake)
	defer conn.CloseNow()

	writeWebSocketMessage(t, conn, []byte(`{"id":"leak","method":"POST","path":"/agent/v1/lifecycle/instances/vm-1/start"}`))
	resp := readProxyResponse(t, conn)
	raw := string(resp.Body) + resp.Error
	lower := strings.ToLower(raw)
	for _, bad := range []string{"must-not-leak", "user_data", "/1.0/operations"} {
		if strings.Contains(lower, strings.ToLower(bad)) {
			t.Fatalf("response leaked %q: %s", bad, raw)
		}
	}
}

func TestLifecycleUnusedImportGuard(t *testing.T) {
	// Sanity: ensure time import is used elsewhere; this keeps the file's
	// imports stable across go versions.
	_ = time.Time{}
	// Sanity: ensure strings import is retained for the leak sweeps.
	_ = strings.Contains
}

// --- fakes ------------------------------------------------------------------

type fakeLifecycleIncus struct {
	calls []*incus.ProxyRequest
	resp  *incus.ProxyResponse
}

func (f *fakeLifecycleIncus) Execute(ctx context.Context, req *incus.ProxyRequest) *incus.ProxyResponse {
	f.calls = append(f.calls, req)
	if f.resp != nil {
		return f.resp
	}
	return &incus.ProxyResponse{Status: http.StatusOK, Body: json.RawMessage(`{"type":"sync"}`)}
}
