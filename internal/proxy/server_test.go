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

	"github.com/anvil/proxy/internal/incus"
	"github.com/coder/websocket"
)

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
	recorder := httptest.NewRecorder()
	hijacker := proxyTestHijacker{
		ResponseRecorder: recorder,
		conn:             serverConn,
	}

	t.handler.ServeHTTP(hijacker, r)

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
}

func (h proxyTestHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.conn, bufio.NewReadWriter(bufio.NewReader(h.conn), bufio.NewWriter(h.conn)), nil
}
