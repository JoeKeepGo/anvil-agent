package lifecycle

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

// --- Test doubles -----------------------------------------------------------

type fakeIncus struct {
	calls []*incus.ProxyRequest
	resp  *incus.ProxyResponse
	resps map[string]*incus.ProxyResponse // keyed by request path
}

func (f *fakeIncus) Execute(ctx context.Context, req *incus.ProxyRequest) *incus.ProxyResponse {
	f.calls = append(f.calls, req)
	if f.resps != nil {
		if r, ok := f.resps[req.Path]; ok {
			return r
		}
	}
	if f.resp != nil {
		return f.resp
	}
	return &incus.ProxyResponse{Status: http.StatusOK, Body: json.RawMessage(`{"type":"sync"}`)}
}

func syncOK() *incus.ProxyResponse {
	return &incus.ProxyResponse{Status: http.StatusOK, Body: json.RawMessage(`{"type":"sync"}`)}
}

func asyncOK(opID string) *incus.ProxyResponse {
	return &incus.ProxyResponse{
		Status: http.StatusAccepted,
		Body:   json.RawMessage(`{"type":"async","operation":"/1.0/operations/` + opID + `"}`),
	}
}

func operationWait(statusCode int, status string, errMessage string) *incus.ProxyResponse {
	metadata := map[string]interface{}{
		"id":          "op-1",
		"status":      status,
		"status_code": statusCode,
	}
	if errMessage != "" {
		metadata["err"] = errMessage
	}
	raw, err := json.Marshal(map[string]interface{}{
		"type":     "sync",
		"metadata": metadata,
	})
	if err != nil {
		panic(err)
	}
	return &incus.ProxyResponse{
		Status: http.StatusOK,
		Body:   raw,
	}
}

func operationWaitSuccess() *incus.ProxyResponse {
	return operationWait(200, "Success", "")
}

func operationWaitFailure(message string) *incus.ProxyResponse {
	return operationWait(400, "Failure", message)
}

func mustJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// --- Path / capabilities ----------------------------------------------------

func TestCapabilitiesAdvertisesAllowlistedActions(t *testing.T) {
	s := NewService(&fakeIncus{})
	caps := s.Capabilities()
	want := []string{"create", "start", "stop", "restart", "delete"}
	if len(caps.SupportedActions) != len(want) {
		t.Fatalf("supported actions = %v, want %v", caps.SupportedActions, want)
	}
	for i, a := range want {
		if caps.SupportedActions[i] != a {
			t.Fatalf("action[%d] = %q, want %q", i, caps.SupportedActions[i], a)
		}
	}
	if !caps.OperationNormalization {
		t.Fatal("operationNormalization = false, want true")
	}
}

func TestUnknownLifecyclePathReturns404DELETED(t *testing.T) {
	s := NewService(&fakeIncus{})
	r := s.Handle(context.Background(), http.MethodGet, "/agent/v1/lifecycle/instances/x/snapshot", nil)
	if r.Err == nil || r.Err.Status != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", r.Err)
	}
	if r.Err.Code != "UNKNOWN_LIFECYCLE_PATH" {
		t.Fatalf("code = %q", r.Err.Code)
	}
}

func TestSnapshotPathRejected(t *testing.T) {
	s := NewService(&fakeIncus{})
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/foo/snapshot", nil)
	if r.Err == nil || r.Err.Status != http.StatusNotFound {
		t.Fatalf("snapshot rejected: err=%v", r.Err)
	}
}

func TestUnsupportedStateSegmentRejected(t *testing.T) {
	s := NewService(&fakeIncus{})
	for _, seg := range []string{"exec", "console", "files", "state", "migrate", "snapshots"} {
		r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/foo/"+seg, nil)
		if r.Err == nil || r.Err.Status != http.StatusNotFound {
			t.Fatalf("segment %q rejected: err=%v", seg, r.Err)
		}
	}
}

func TestPathTraversalRejected(t *testing.T) {
	s := NewService(&fakeIncus{})
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/..%2f..%2fetc/start", nil)
	if r.Err == nil {
		t.Fatal("path traversal accepted")
	}
}

// --- Name validation --------------------------------------------------------

func TestValidateInstanceNameRejectsTraversalAndShell(t *testing.T) {
	bad := []string{"", "foo/../bar", "a/b", "a$b", "UPPER", "with space", "-leading", strings.Repeat("a", maxInstanceLen+1)}
	for _, name := range bad {
		if err := ValidateInstanceName(name); err == nil {
			t.Fatalf("name %q accepted, want rejection", name)
		}
	}
}

func TestValidateInstanceNameAcceptsBoundedDNSLabel(t *testing.T) {
	good := []string{"a", "vm-1", "anvil-instance-42", strings.Repeat("a", maxInstanceLen)}
	for _, name := range good {
		if err := ValidateInstanceName(name); err != nil {
			t.Fatalf("name %q rejected: %v", name, err)
		}
	}
}

// --- Create validation ------------------------------------------------------

func TestCreateRejectsMissingBody(t *testing.T) {
	s := NewService(&fakeIncus{})
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/create", nil)
	if r.Err == nil || r.Err.Code != "MISSING_BODY" {
		t.Fatalf("err = %v, want MISSING_BODY", r.Err)
	}
}

func TestCreateRejectsInvalidName(t *testing.T) {
	s := NewService(&fakeIncus{})
	body := mustJSON(t, CreateInstanceRequest{Name: "BAD NAME", Image: "ubuntu/24.04", CPUCount: 1, MemoryBytes: 1024, RootDiskBytes: 1024})
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/create", body)
	if r.Err == nil || r.Err.Code != "INVALID_INSTANCE_NAME" {
		t.Fatalf("err = %v, want INVALID_INSTANCE_NAME", r.Err)
	}
	// Error message must not echo the submitted name.
	if strings.Contains(strings.ToLower(r.Err.Message), "bad name") {
		t.Fatalf("error echoed submitted name: %q", r.Err.Message)
	}
}

func TestCreateRejectsInvalidLimits(t *testing.T) {
	s := NewService(&fakeIncus{})
	cases := []CreateInstanceRequest{
		{Name: "vm-1", Image: "ubuntu/24.04", CPUCount: 0, MemoryBytes: 1024, RootDiskBytes: 1024},
		{Name: "vm-1", Image: "ubuntu/24.04", CPUCount: 1, MemoryBytes: 0, RootDiskBytes: 1024},
		{Name: "vm-1", Image: "ubuntu/24.04", CPUCount: 1, MemoryBytes: 1024, RootDiskBytes: 0},
		{Name: "vm-1", Image: "ubuntu/24.04", CPUCount: maxCPUCount + 1, MemoryBytes: 1024, RootDiskBytes: 1024},
		{Name: "vm-1", Image: "ubuntu/24.04", CPUCount: 1, MemoryBytes: maxMemoryBytes + 1, RootDiskBytes: 1024},
	}
	for _, c := range cases {
		r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/create", mustJSON(t, c))
		if r.Err == nil || r.Err.Code != "INVALID_LIMITS" {
			t.Fatalf("limits %d/%d/%d accepted: err=%v", c.CPUCount, c.MemoryBytes, c.RootDiskBytes, r.Err)
		}
	}
}

func TestCreateRejectsUnknownFields(t *testing.T) {
	s := NewService(&fakeIncus{})
	body := json.RawMessage(`{"name":"vm-1","image":"ubuntu/24.04","cpuCount":1,"memoryBytes":1024,"rootDiskBytes":1024,"shellCommand":"rm -rf /"}`)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/create", body)
	if r.Err == nil || r.Err.Code != "INVALID_BODY" {
		t.Fatalf("err = %v, want INVALID_BODY", r.Err)
	}
	if strings.Contains(r.Err.Message, "shellCommand") {
		t.Fatalf("error echoed unknown field name: %q", r.Err.Message)
	}
}

func TestCreateConstructsAllowlistedIncusRequest(t *testing.T) {
	fb := &fakeIncus{}
	s := NewService(fb)
	body := mustJSON(t, CreateInstanceRequest{Name: "vm-1", Image: "ubuntu/24.04", CPUCount: 2, MemoryBytes: 1 << 30, RootDiskBytes: 1 << 32})
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/create", body)
	if r.Err != nil {
		t.Fatalf("create failed: %v", r.Err)
	}
	if len(fb.calls) != 2 {
		t.Fatalf("incus calls = %d, want 2", len(fb.calls))
	}
	call := fb.calls[0]
	if call.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", call.Method)
	}
	if call.Path != "/1.0/instances" {
		t.Fatalf("path = %q, want /1.0/instances", call.Path)
	}
	if fb.calls[1].Method != http.MethodGet || fb.calls[1].Path != "/1.0/instances/vm-1" {
		t.Fatalf("verify call = %s %s, want GET /1.0/instances/vm-1", fb.calls[1].Method, fb.calls[1].Path)
	}
	var sent map[string]interface{}
	if err := json.Unmarshal(call.Body, &sent); err != nil {
		t.Fatalf("unmarshal sent body: %v", err)
	}
	if sent["type"].(string) != "virtual-machine" {
		t.Fatalf("type = %v, want virtual-machine", sent["type"])
	}
	raw := string(call.Body)
	if strings.Contains(raw, "shellCommand") || strings.Contains(raw, "hookCommand") {
		t.Fatalf("sent body leaked forbidden field: %s", raw)
	}
}

// --- State actions ----------------------------------------------------------

func TestStartRequiresPOST(t *testing.T) {
	s := NewService(&fakeIncus{})
	r := s.Handle(context.Background(), http.MethodGet, "/agent/v1/lifecycle/instances/test/start", nil)
	if r.Err == nil || r.Err.Code != "METHOD_NOT_ALLOWED" {
		t.Fatalf("err = %v, want METHOD_NOT_ALLOWED", r.Err)
	}
}

func TestRestartConstructsStateRequest(t *testing.T) {
	fb := &fakeIncus{}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/restart", nil)
	if r.Err != nil {
		t.Fatalf("restart failed: %v", r.Err)
	}
	if len(fb.calls) != 1 {
		t.Fatalf("incus calls = %d, want 1", len(fb.calls))
	}
	call := fb.calls[0]
	if call.Method != http.MethodPut {
		t.Fatalf("method = %s, want PUT", call.Method)
	}
	if call.Path != "/1.0/instances/vm-1/state" {
		t.Fatalf("path = %q", call.Path)
	}
	if !strings.Contains(string(call.Body), `"action":"restart"`) {
		t.Fatalf("body missing action: %s", call.Body)
	}
}

func TestStopSendsForceParamDELETED(t *testing.T) {
	fb := &fakeIncus{}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/stop", mustJSON(t, StateRequest{Force: true}))
	if r.Err != nil {
		t.Fatalf("stop failed: %v", r.Err)
	}
	if !strings.Contains(string(fb.calls[0].Body), `"force":true`) {
		t.Fatalf("body missing force: %s", fb.calls[0].Body)
	}
}

func TestStateRejectsUnknownFields(t *testing.T) {
	s := NewService(&fakeIncus{})
	body := json.RawMessage(`{"force":false,"shell":"rm -rf /"}`)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/start", body)
	if r.Err == nil || r.Err.Code != "INVALID_BODY" {
		t.Fatalf("err = %v, want INVALID_BODY", r.Err)
	}
}

// --- Delete -----------------------------------------------------------------

func TestDeleteRequiresConfirmation(t *testing.T) {
	s := NewService(&fakeIncus{})
	if r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/delete", nil); r.Err == nil || r.Err.Code != "DELETE_NOT_CONFIRMED" {
		t.Fatalf("no-body err = %v", r.Err)
	}
	if r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/delete", mustJSON(t, DeleteRequest{Confirm: false})); r.Err == nil || r.Err.Code != "DELETE_NOT_CONFIRMED" {
		t.Fatalf("confirm=false err = %v", r.Err)
	}
}

func TestDeleteConstructsIncusRequest(t *testing.T) {
	fb := &fakeIncus{}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/delete", mustJSON(t, DeleteRequest{Confirm: true}))
	if r.Err != nil {
		t.Fatalf("delete failed: %v", r.Err)
	}
	call := fb.calls[0]
	if call.Method != http.MethodDelete {
		t.Fatalf("method = %s, want DELETE", call.Method)
	}
	if call.Path != "/1.0/instances/vm-1" {
		t.Fatalf("path = %q", call.Path)
	}
}

// --- Response normalization ------------------------------------------------

func TestNormalizeSyncResponse(t *testing.T) {
	fb := &fakeIncus{resp: syncOK()}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/start", nil)
	if r.Err != nil {
		t.Fatalf("start failed: %v", r.Err)
	}
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d", r.Status)
	}
	var resp Response
	json.Unmarshal(r.Body, &resp)
	if resp.Action != ActionStart {
		t.Fatalf("action = %q", resp.Action)
	}
	if resp.Instance != "vm-1" {
		t.Fatalf("instance = %q", resp.Instance)
	}
	if resp.Status != "sync-ok" {
		t.Fatalf("status = %q, want sync-ok", resp.Status)
	}
	if resp.OperationKind != "sync" || resp.OperationID != "" {
		t.Fatalf("operation = %q/%q, want sync/empty", resp.OperationKind, resp.OperationID)
	}
}

func TestAsyncCreateWaitsForOperationCompletionBeforeReturningSuccess(t *testing.T) {
	fb := &fakeIncus{resps: map[string]*incus.ProxyResponse{
		"/1.0/instances":               asyncOK("abc-123"),
		"/1.0/operations/abc-123/wait": operationWaitSuccess(),
		"/1.0/instances/vm-1":          syncOK(),
	}}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/create", mustJSON(t, CreateInstanceRequest{
		Name: "vm-1", Image: "ubuntu/24.04", CPUCount: 1, MemoryBytes: 1024, RootDiskBytes: 1024,
	}))
	if r.Err != nil {
		t.Fatalf("create failed: %v", r.Err)
	}
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", r.Status)
	}
	var resp Response
	json.Unmarshal(r.Body, &resp)
	if resp.OperationKind != "async" || resp.OperationID != "abc-123" {
		t.Fatalf("operation = %q/%q, want async/abc-123", resp.OperationKind, resp.OperationID)
	}
	if resp.Status != "operation-completed" {
		t.Fatalf("status = %q, want operation-completed", resp.Status)
	}
	if len(fb.calls) != 3 {
		t.Fatalf("calls = %d, want create + wait + instance verify", len(fb.calls))
	}
}

func TestAsyncCreateFailsWhenOperationDisappears(t *testing.T) {
	fb := &fakeIncus{resps: map[string]*incus.ProxyResponse{
		"/1.0/instances":                  asyncOK("missing-op"),
		"/1.0/operations/missing-op/wait": {Status: http.StatusNotFound},
	}}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/create", mustJSON(t, CreateInstanceRequest{
		Name: "vm-1", Image: "ubuntu/24.04", CPUCount: 1, MemoryBytes: 1024, RootDiskBytes: 1024,
	}))
	if r.Err == nil || r.Err.Code != "INCUS_OPERATION_MISSING" {
		t.Fatalf("err = %v, want INCUS_OPERATION_MISSING", r.Err)
	}
	if r.Err.Status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", r.Err.Status)
	}
}

func TestAsyncCreateFailsWhenOperationFails(t *testing.T) {
	fb := &fakeIncus{resps: map[string]*incus.ProxyResponse{
		"/1.0/instances":                 asyncOK("failed-op"),
		"/1.0/operations/failed-op/wait": operationWaitFailure("boom"),
	}}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/create", mustJSON(t, CreateInstanceRequest{
		Name: "vm-1", Image: "ubuntu/24.04", CPUCount: 1, MemoryBytes: 1024, RootDiskBytes: 1024,
	}))
	if r.Err == nil || r.Err.Code != "INCUS_OPERATION_FAILED" {
		t.Fatalf("err = %v, want INCUS_OPERATION_FAILED", r.Err)
	}
	raw := r.Err.Error()
	if strings.Contains(raw, "boom") {
		t.Fatalf("operation failure leaked raw Incus error: %q", raw)
	}
}

func TestAsyncCreateFailsWhenCompletedInstanceIsMissing(t *testing.T) {
	fb := &fakeIncus{resps: map[string]*incus.ProxyResponse{
		"/1.0/instances":                    asyncOK("completed-op"),
		"/1.0/operations/completed-op/wait": operationWaitSuccess(),
		"/1.0/instances/vm-1":               {Status: http.StatusNotFound},
	}}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/create", mustJSON(t, CreateInstanceRequest{
		Name: "vm-1", Image: "ubuntu/24.04", CPUCount: 1, MemoryBytes: 1024, RootDiskBytes: 1024,
	}))
	if r.Err == nil || r.Err.Code != "INCUS_INSTANCE_MISSING" {
		t.Fatalf("err = %v, want INCUS_INSTANCE_MISSING", r.Err)
	}
	if r.Err.Status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", r.Err.Status)
	}
}

func TestNormalizeMalformedResponseIsSafeError(t *testing.T) {
	fb := &fakeIncus{resp: &incus.ProxyResponse{Status: http.StatusOK, Body: json.RawMessage(`{not json`)}}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/start", nil)
	// Malformed body must degrade to sync-ok (never leak raw bytes).
	var resp Response
	json.Unmarshal(r.Body, &resp)
	if resp.Status != "sync-ok" {
		t.Fatalf("status = %q, want sync-ok on malformed body", resp.Status)
	}
	raw := string(r.Body)
	if strings.Contains(raw, "not json") {
		t.Fatalf("response leaked malformed upstream body: %s", raw)
	}
}

func TestAcceptedResponseWithoutOperationIsSafeError(t *testing.T) {
	fb := &fakeIncus{resp: &incus.ProxyResponse{Status: http.StatusAccepted, Body: json.RawMessage(`{"type":"async"}`)}}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/start", nil)
	if r.Err == nil || r.Err.Code != "INCUS_OPERATION_MALFORMED" {
		t.Fatalf("err = %v, want INCUS_OPERATION_MALFORMED", r.Err)
	}
	if r.Err.Status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", r.Err.Status)
	}
}

func TestAcceptedResponseWithMalformedBodyIsSafeError(t *testing.T) {
	fb := &fakeIncus{resp: &incus.ProxyResponse{Status: http.StatusAccepted, Body: json.RawMessage(`{not json`)}}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/start", nil)
	if r.Err == nil || r.Err.Code != "INCUS_OPERATION_MALFORMED" {
		t.Fatalf("err = %v, want INCUS_OPERATION_MALFORMED", r.Err)
	}
}

func TestIncusErrorMapsToSafeError(t *testing.T) {
	fb := &fakeIncus{resp: &incus.ProxyResponse{Status: http.StatusNotFound}}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/ghost/start", nil)
	if r.Err == nil || r.Err.Code != "INSTANCE_NOT_FOUND" {
		t.Fatalf("err = %v, want INSTANCE_NOT_FOUND", r.Err)
	}
	if r.Err.Status != http.StatusNotFound {
		t.Fatalf("status = %d", r.Err.Status)
	}
}

func TestIncusUnavailable(t *testing.T) {
	fb := &nilIncus{}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/start", nil)
	if r.Err == nil || r.Err.Code != "INCUS_UNAVAILABLE" {
		t.Fatalf("err = %v", r.Err)
	}
}

type nilIncus struct{}

func (nilIncus) Execute(context.Context, *incus.ProxyRequest) *incus.ProxyResponse { return nil }

// --- BuildIncusRequest URL-encoding -----------------------------------------

func TestBuildInsertsEncInstanceNameInPath(t *testing.T) {
	req, err := BuildIncusRequest(ActionStart, "vm-1", StateRequest{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(req.Path, "/1.0/instances/vm-1/state") {
		t.Fatalf("path = %q", req.Path)
	}
}

// --- No-leak sweep ----------------------------------------------------------

func TestNoResponseLeaksRawIncusOrPath(t *testing.T) {
	fb := &fakeIncus{resps: map[string]*incus.ProxyResponse{
		"/1.0/instances/vm-1/state": {
			Status: http.StatusAccepted,
			Body:   json.RawMessage(`{"type":"async","operation":"/1.0/operations/op-1","metadata":{"secret":"MUST-NOT-LEAK","config":{"user.shell":"/bin/bash"},"socket":"/var/lib/incus/unix.socket"}}`),
		},
		"/1.0/operations/op-1/wait": operationWaitSuccess(),
	}}
	s := NewService(fb)
	r := s.Handle(context.Background(), http.MethodPost, "/agent/v1/lifecycle/instances/vm-1/start", nil)
	if r.Err != nil {
		t.Fatalf("start failed: %v", r.Err)
	}
	raw := string(r.Body)
	lower := strings.ToLower(raw)
	for _, bad := range []string{"must-not-leak", "socket", "secret", "/var/lib/incus", "/bin/bash", "config"} {
		if strings.Contains(lower, strings.ToLower(bad)) {
			t.Fatalf("response leaked %q: %s", bad, raw)
		}
	}
}
