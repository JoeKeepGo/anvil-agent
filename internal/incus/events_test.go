package incus

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestListenEventsUsesVerifiedEndpoint(t *testing.T) {
	var requestedPath string
	client := newWebSocketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}

		go func() {
			defer conn.Close(websocket.StatusNormalClosure, "done")
			writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := conn.Write(writeCtx, websocket.MessageText, []byte(`{"type":"lifecycle","data":{"action":"instance-started"}}`)); err != nil {
				t.Errorf("write event: %v", err)
			}
		}()
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	events := make(chan Event, 1)

	err := client.ListenEvents(ctx, events)
	if err == nil {
		t.Fatal("ListenEvents returned nil, want close error after server closes websocket")
	}
	if requestedPath != "/1.0/events" {
		t.Fatalf("path = %q, want /1.0/events", requestedPath)
	}
}

func TestListenEventsDecodesEventMessages(t *testing.T) {
	client := newWebSocketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}

		go func() {
			defer conn.Close(websocket.StatusNormalClosure, "done")
			writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := conn.Write(writeCtx, websocket.MessageText, []byte(`{"type":"lifecycle","data":{"action":"instance-started","metadata":{"name":"web"}}}`)); err != nil {
				t.Errorf("write event: %v", err)
			}
		}()
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	events := make(chan Event, 1)

	err := client.ListenEvents(ctx, events)
	if err == nil {
		t.Fatal("ListenEvents returned nil, want close error after server closes websocket")
	}

	select {
	case event := <-events:
		if event.Type != "lifecycle" {
			t.Fatalf("event type = %q, want lifecycle", event.Type)
		}
		var data map[string]json.RawMessage
		if err := json.Unmarshal(event.Data, &data); err != nil {
			t.Fatalf("unmarshal event data: %v", err)
		}
		if string(data["action"]) != `"instance-started"` {
			t.Fatalf("action = %s, want instance-started", data["action"])
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}

func TestListenEventsReturnsHandshakeErrors(t *testing.T) {
	client := newWebSocketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no websocket", http.StatusBadRequest)
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := client.ListenEvents(ctx, make(chan Event, 1))
	if err == nil {
		t.Fatal("ListenEvents returned nil, want handshake error")
	}
}

func newWebSocketTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	return &Client{
		base: "http://unix",
		http: &http.Client{
			Transport: websocketTestTransport{handler: handler},
		},
	}
}

type websocketTestTransport struct {
	handler http.HandlerFunc
}

func (t websocketTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clientConn, serverConn := net.Pipe()
	recorder := httptest.NewRecorder()
	hijacker := testHijacker{
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

type testHijacker struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (h testHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.conn, bufio.NewReadWriter(bufio.NewReader(h.conn), bufio.NewWriter(h.conn)), nil
}
