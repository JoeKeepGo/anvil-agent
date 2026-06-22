package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JoeKeepGo/anvil-agent/internal/config"
	"github.com/JoeKeepGo/anvil-agent/internal/incus"
	"github.com/JoeKeepGo/anvil-agent/internal/state"
	"github.com/coder/websocket"
)

func TestWebSocketAllowsConnectionWhenTokenUnset(t *testing.T) {
	server := newWebSocketAuthTestServer("")

	conn, resp, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.CloseNow()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
}

func TestWebSocketRejectsMissingBearerTokenWhenConfigured(t *testing.T) {
	server := newWebSocketAuthTestServer("secret")

	_, resp, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
	})
	if err == nil {
		t.Fatal("dial websocket succeeded, want auth failure")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", responseStatus(resp))
	}
}

func TestWebSocketRejectsWrongBearerToken(t *testing.T) {
	server := newWebSocketAuthTestServer("secret")
	header := http.Header{}
	header.Set("Authorization", "Bearer wrong")

	_, resp, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
		HTTPHeader: header,
	})
	if err == nil {
		t.Fatal("dial websocket succeeded, want auth failure")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", responseStatus(resp))
	}
}

func TestWebSocketAcceptsCorrectBearerToken(t *testing.T) {
	server := newWebSocketAuthTestServer("secret")
	header := http.Header{}
	header.Set("Authorization", "Bearer secret")

	conn, resp, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
		HTTPHeader: header,
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.CloseNow()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
}

func TestRejectsInvalidJSON(t *testing.T) {
	clientConn, incusCalls := newRequestValidationClient(t)
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{`))
	resp := readProxyResponse(t, clientConn)

	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
	if resp.ID != "" {
		t.Fatalf("id = %q, want empty", resp.ID)
	}
	if *incusCalls != 0 {
		t.Fatalf("incus calls = %d, want 0", *incusCalls)
	}
}

func TestRejectsMissingID(t *testing.T) {
	clientConn, incusCalls := newRequestValidationClient(t)
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"method":"GET","path":"/1.0"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
	if resp.ID != "" {
		t.Fatalf("id = %q, want empty", resp.ID)
	}
	if *incusCalls != 0 {
		t.Fatalf("incus calls = %d, want 0", *incusCalls)
	}
}

func TestRejectsMissingMethod(t *testing.T) {
	clientConn, incusCalls := newRequestValidationClient(t)
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"id":"bad-method","path":"/1.0"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
	if resp.ID != "bad-method" {
		t.Fatalf("id = %q, want bad-method", resp.ID)
	}
	if *incusCalls != 0 {
		t.Fatalf("incus calls = %d, want 0", *incusCalls)
	}
}

func TestRejectsUnsupportedMethod(t *testing.T) {
	clientConn, incusCalls := newRequestValidationClient(t)
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"id":"bad-method","method":"TRACE","path":"/1.0"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
	if resp.ID != "bad-method" {
		t.Fatalf("id = %q, want bad-method", resp.ID)
	}
	if *incusCalls != 0 {
		t.Fatalf("incus calls = %d, want 0", *incusCalls)
	}
}

func TestRejectsMissingPath(t *testing.T) {
	clientConn, incusCalls := newRequestValidationClient(t)
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"id":"bad-path","method":"GET"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
	if resp.ID != "bad-path" {
		t.Fatalf("id = %q, want bad-path", resp.ID)
	}
	if *incusCalls != 0 {
		t.Fatalf("incus calls = %d, want 0", *incusCalls)
	}
}

func TestRejectsPathOutsideIncusAPI(t *testing.T) {
	clientConn, incusCalls := newRequestValidationClient(t)
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"id":"bad-path","method":"GET","path":"/not-incus"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
	if resp.ID != "bad-path" {
		t.Fatalf("id = %q, want bad-path", resp.ID)
	}
	if *incusCalls != 0 {
		t.Fatalf("incus calls = %d, want 0", *incusCalls)
	}
}

func TestAcceptsIncusRootPath(t *testing.T) {
	clientConn, incusCalls := newRequestValidationClient(t)
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"id":"ok-root","method":"GET","path":"/1.0"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if resp.ID != "ok-root" {
		t.Fatalf("id = %q, want ok-root", resp.ID)
	}
	if *incusCalls != 1 {
		t.Fatalf("incus calls = %d, want 1", *incusCalls)
	}
}

func TestAcceptsIncusNestedPath(t *testing.T) {
	clientConn, incusCalls := newRequestValidationClient(t)
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"id":"ok-nested","method":"GET","path":"/1.0/instances"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if resp.ID != "ok-nested" {
		t.Fatalf("id = %q, want ok-nested", resp.ID)
	}
	if *incusCalls != 1 {
		t.Fatalf("incus calls = %d, want 1", *incusCalls)
	}
}

func TestValidRequestWritesProxyResponse(t *testing.T) {
	clientConn, incusCalls := newRequestValidationClient(t)
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"id":"write-ok","method":"GET","path":"/1.0"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.ID != "write-ok" {
		t.Fatalf("id = %q, want write-ok", resp.ID)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if string(resp.Body) != `{"type":"sync","metadata":{}}` {
		t.Fatalf("body = %s, want fake backend body", resp.Body)
	}
	if resp.Error != "" {
		t.Fatalf("error = %q, want empty", resp.Error)
	}
	if *incusCalls != 1 {
		t.Fatalf("incus calls = %d, want 1", *incusCalls)
	}
}

func TestAgentStateRequestReturnsReportWithoutIncusProxyExecution(t *testing.T) {
	clientConn, incusCalls := newStateRequestClient(t, "")
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"id":"state-ok","method":"GET","path":"/agent/v1/state"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.ID != "state-ok" {
		t.Fatalf("id = %q, want state-ok", resp.ID)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if resp.Error != "" {
		t.Fatalf("error = %q, want empty", resp.Error)
	}
	if *incusCalls != 0 {
		t.Fatalf("incus proxy calls = %d, want 0", *incusCalls)
	}

	var report state.Report
	if err := json.Unmarshal(resp.Body, &report); err != nil {
		t.Fatalf("unmarshal state report: %v", err)
	}
	if report.Agent.ID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("agent id = %q", report.Agent.ID)
	}
	if !report.Capabilities.StateReport {
		t.Fatal("stateReport capability = false, want true")
	}
}

func TestAgentStateRequestRequiresGET(t *testing.T) {
	clientConn, incusCalls := newStateRequestClient(t, "")
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"id":"state-bad-method","method":"POST","path":"/agent/v1/state"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.ID != "state-bad-method" {
		t.Fatalf("id = %q, want state-bad-method", resp.ID)
	}
	if resp.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Status)
	}
	if resp.Error == "" {
		t.Fatal("error is empty, want safe error")
	}
	if *incusCalls != 0 {
		t.Fatalf("incus proxy calls = %d, want 0", *incusCalls)
	}
}

func TestUnknownAgentPathReturnsSafeErrorWithoutIncusProxyExecution(t *testing.T) {
	clientConn, incusCalls := newStateRequestClient(t, "")
	defer clientConn.CloseNow()

	writeWebSocketMessage(t, clientConn, []byte(`{"id":"state-unknown","method":"GET","path":"/agent/v1/unknown"}`))
	resp := readProxyResponse(t, clientConn)

	if resp.ID != "state-unknown" {
		t.Fatalf("id = %q, want state-unknown", resp.ID)
	}
	if resp.Status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.Status)
	}
	if resp.Error == "" {
		t.Fatal("error is empty, want safe error")
	}
	if *incusCalls != 0 {
		t.Fatalf("incus proxy calls = %d, want 0", *incusCalls)
	}
}

func TestAgentStateRouteUsesExistingBearerAuth(t *testing.T) {
	server := newStateAuthTestServer("secret")

	_, resp, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
	})
	if err == nil {
		t.Fatal("dial websocket succeeded, want auth failure")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", responseStatus(resp))
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer secret")
	conn, resp, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
		HTTPHeader: header,
	})
	if err != nil {
		t.Fatalf("dial websocket with token: %v", err)
	}
	defer conn.CloseNow()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}

	writeWebSocketMessage(t, conn, []byte(`{"id":"state-auth","method":"GET","path":"/agent/v1/state"}`))
	stateResp := readProxyResponse(t, conn)
	if stateResp.Status != http.StatusOK {
		t.Fatalf("state status = %d, want 200", stateResp.Status)
	}
}

func TestHandleRequestReturnsWhenClientDisconnectsBeforeResponse(t *testing.T) {
	backend := &fakeIncusBackend{}
	server := &Server{incus: backend}
	clientConn, serverConn := websocketPipe(t)
	clientConn.CloseNow()
	defer serverConn.CloseNow()

	done := make(chan struct{})
	go func() {
		server.handleRequest(&client{conn: serverConn, ctx: context.Background()}, &incus.ProxyRequest{
			ID:     "disconnected",
			Method: http.MethodGet,
			Path:   "/1.0",
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleRequest did not return after client disconnected")
	}
}

func TestForwardEventsBroadcastsEventToConnectedClient(t *testing.T) {
	server, clientConn, serverConn := newEventForwardingTestServer(t)
	defer clientConn.CloseNow()
	defer serverConn.CloseNow()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.forwardEvents(ctx)

	server.eventCh <- incus.Event{
		Type: "lifecycle",
		Data: json.RawMessage(`{"action":"instance-started"}`),
	}

	readCtx, readCancel := context.WithTimeout(context.Background(), time.Second)
	defer readCancel()
	_, msg, err := clientConn.Read(readCtx)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if string(msg) != `{"type":"lifecycle","data":{"action":"instance-started"}}` {
		t.Fatalf("message = %s", msg)
	}
}

func TestBroadcastEventContinuesWhenOneClientWriteFails(t *testing.T) {
	oldEventWriteTimeout := eventWriteTimeout
	eventWriteTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		eventWriteTimeout = oldEventWriteTimeout
	})

	server := &Server{
		clients: make(map[*client]struct{}),
		eventCh: make(chan incus.Event, 1),
	}

	goodClientConn, goodServerConn := websocketPipe(t)
	defer goodClientConn.CloseNow()
	defer goodServerConn.CloseNow()

	badClientConn, badServerConn := websocketPipe(t)
	badClientConn.CloseNow()
	badServerConn.CloseNow()

	server.clients[&client{conn: badServerConn, ctx: context.Background()}] = struct{}{}
	server.clients[&client{conn: goodServerConn, ctx: context.Background()}] = struct{}{}

	server.broadcastEvent(incus.Event{
		Type: "lifecycle",
		Data: json.RawMessage(`{"action":"instance-started"}`),
	})

	readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, msg, err := goodClientConn.Read(readCtx)
	if err != nil {
		t.Fatalf("read event from good client: %v", err)
	}
	if string(msg) != `{"type":"lifecycle","data":{"action":"instance-started"}}` {
		t.Fatalf("message = %s", msg)
	}
}

func newWebSocketAuthTestServer(token string) *Server {
	return NewServer(&config.Config{AuthToken: token}, nil)
}

func websocketAuthTestClient(server *Server) *http.Client {
	return &http.Client{
		Transport: websocketPipeTransport{
			handler: server.handleWebSocket,
		},
	}
}

func responseStatus(resp *http.Response) interface{} {
	if resp == nil {
		return nil
	}
	return resp.StatusCode
}

func newRequestValidationClient(t *testing.T) (*websocket.Conn, *int) {
	t.Helper()

	backend := &fakeIncusBackend{}
	server := &Server{
		cfg:      &config.Config{},
		incus:    backend,
		clients:  make(map[*client]struct{}),
		eventCh:  make(chan incus.Event, 1),
		upgrader: websocket.AcceptOptions{InsecureSkipVerify: true},
	}

	conn, _, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn, &backend.calls
}

func newStateRequestClient(t *testing.T, token string) (*websocket.Conn, *int) {
	t.Helper()

	backend := &fakeIncusBackend{}
	server := newStateAuthTestServer(token)
	server.incus = backend

	conn, _, err := websocket.Dial(context.Background(), "ws://example.com/ws", &websocket.DialOptions{
		HTTPClient: websocketAuthTestClient(server),
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn, &backend.calls
}

func newStateAuthTestServer(token string) *Server {
	reporter := state.NewStaticReporter(state.Report{
		Agent: state.AgentSummary{
			ID:                 "11111111-1111-4111-8111-111111111111",
			Version:            "test",
			StateSchemaVersion: 1,
			StartedAt:          time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
			ReportedAt:         time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		},
		Host: state.HostSummary{
			Hostname: "test-host",
			OS:       "linux",
			Arch:     "arm64",
		},
		Incus: state.IncusSummary{
			Available:  true,
			StatusCode: http.StatusOK,
		},
		Capabilities: state.CapabilitySummary{
			IncusProxy:  true,
			Events:      true,
			StateReport: true,
			WireGuard:   false,
			VMLifecycle: false,
		},
		Snapshot: state.SnapshotSummary{},
	})
	return NewServerWithReporter(&config.Config{AuthToken: token}, &fakeIncusBackend{}, reporter)
}

func writeWebSocketMessage(t *testing.T, conn *websocket.Conn, msg []byte) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatalf("write websocket message: %v", err)
	}
}

func readProxyResponse(t *testing.T, conn *websocket.Conn) incus.ProxyResponse {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read websocket message: %v", err)
	}

	var resp incus.ProxyResponse
	if err := json.Unmarshal(msg, &resp); err != nil {
		t.Fatalf("unmarshal proxy response: %v", err)
	}
	return resp
}

type fakeIncusBackend struct {
	calls int
}

func (f *fakeIncusBackend) Execute(ctx context.Context, req *incus.ProxyRequest) *incus.ProxyResponse {
	f.calls++
	return &incus.ProxyResponse{
		ID:     req.ID,
		Status: http.StatusOK,
		Body:   json.RawMessage(`{"type":"sync","metadata":{}}`),
	}
}

func (f *fakeIncusBackend) ListenEvents(ctx context.Context, ch chan<- incus.Event) error {
	<-ctx.Done()
	return ctx.Err()
}

func newEventForwardingTestServer(t *testing.T) (*Server, *websocket.Conn, *websocket.Conn) {
	t.Helper()

	clientConn, serverConn := websocketPipe(t)
	server := &Server{
		clients: make(map[*client]struct{}),
		eventCh: make(chan incus.Event, 1),
	}
	server.clients[&client{conn: serverConn, ctx: context.Background()}] = struct{}{}
	return server, clientConn, serverConn
}

func websocketPipe(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()

	var serverConn *websocket.Conn
	transport := websocketPipeTransport{
		handler: func(w http.ResponseWriter, r *http.Request) {
			var err error
			serverConn, err = websocket.Accept(w, r, nil)
			if err != nil {
				t.Errorf("accept websocket: %v", err)
			}
		},
	}

	clientConn, _, err := websocket.Dial(context.Background(), "ws://example.com", &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("dial websocket pipe: %v", err)
	}
	if serverConn == nil {
		t.Fatal("server websocket was not accepted")
	}
	return clientConn, serverConn
}

type websocketPipeTransport struct {
	handler http.HandlerFunc
}

func (t websocketPipeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clientConn, serverConn := net.Pipe()
	done := make(chan struct{})
	recorder := httptest.NewRecorder()
	hijacker := proxyTestHijacker{
		ResponseRecorder: recorder,
		conn:             serverConn,
		done:             done,
	}

	go func() {
		t.handler.ServeHTTP(hijacker, r)
		closeDone(done)
	}()
	<-done

	resp := recorder.Result()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		resp.Body = clientConn
	} else {
		clientConn.Close()
		serverConn.Close()
	}
	return resp, nil
}

type proxyTestHijacker struct {
	*httptest.ResponseRecorder
	conn net.Conn
	done chan struct{}
}

func (h proxyTestHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	closeDone(h.done)
	return h.conn, bufio.NewReadWriter(bufio.NewReader(h.conn), bufio.NewWriter(h.conn)), nil
}

func closeDone(done chan struct{}) {
	select {
	case <-done:
	default:
		close(done)
	}
}
