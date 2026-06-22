package incus

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestClient(t *testing.T, transport roundTripFunc) *Client {
	t.Helper()

	return &Client{
		base: "http://unix",
		http: &http.Client{
			Transport: transport,
		},
	}
}

func TestExecuteForwardsGETWithoutBody(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/1.0/instances" {
			t.Fatalf("path = %s, want /1.0/instances", req.URL.Path)
		}
		if req.Body != nil {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			if len(body) != 0 {
				t.Fatalf("body = %q, want empty", string(body))
			}
		}
		if req.Header.Get("Content-Type") != "" {
			t.Fatalf("content-type = %q, want empty", req.Header.Get("Content-Type"))
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"metadata":[]}`)),
			Header:     make(http.Header),
		}, nil
	})

	resp := client.Execute(context.Background(), &ProxyRequest{
		ID:     "m1-get",
		Method: http.MethodGet,
		Path:   "/1.0/instances",
	})

	if resp.ID != "m1-get" {
		t.Fatalf("id = %q, want m1-get", resp.ID)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if string(resp.Body) != `{"metadata":[]}` {
		t.Fatalf("response body = %s, want raw Incus body", resp.Body)
	}
	if resp.Error != "" {
		t.Fatalf("error = %q, want empty", resp.Error)
	}
}

func TestExecuteForwardsJSONBody(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", req.Method)
		}
		if req.URL.Path != "/1.0/instances/web/state" {
			t.Fatalf("path = %s", req.URL.Path)
		}
		if req.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content-type = %q, want application/json", req.Header.Get("Content-Type"))
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if string(body) != `{"action":"start"}` {
			t.Fatalf("body = %s", body)
		}

		return &http.Response{
			StatusCode: http.StatusAccepted,
			Body:       io.NopCloser(strings.NewReader(`{"operation":"/1.0/operations/abc"}`)),
			Header:     make(http.Header),
		}, nil
	})

	resp := client.Execute(context.Background(), &ProxyRequest{
		ID:     "req-1",
		Method: http.MethodPost,
		Path:   "/1.0/instances/web/state",
		Body:   json.RawMessage(`{"action":"start"}`),
	})

	if resp.ID != "req-1" {
		t.Fatalf("id = %q", resp.ID)
	}
	if resp.Status != http.StatusAccepted {
		t.Fatalf("status = %d", resp.Status)
	}
	if string(resp.Body) != `{"operation":"/1.0/operations/abc"}` {
		t.Fatalf("response body = %s", resp.Body)
	}
}

func TestExecutePreservesRequestID(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"type":"sync"}`)),
			Header:     make(http.Header),
		}, nil
	})

	resp := client.Execute(context.Background(), &ProxyRequest{
		ID:     "opaque-client-id-123",
		Method: http.MethodGet,
		Path:   "/1.0",
	})

	if resp.ID != "opaque-client-id-123" {
		t.Fatalf("id = %q, want opaque-client-id-123", resp.ID)
	}
}

func TestExecutePreservesIncusStatus(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{"error":"missing"}`)),
			Header:     make(http.Header),
		}, nil
	})

	resp := client.Execute(context.Background(), &ProxyRequest{
		ID:     "missing-instance",
		Method: http.MethodGet,
		Path:   "/1.0/instances/missing",
	})

	if resp.Status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.Status)
	}
}

func TestExecutePreservesIncusResponseBody(t *testing.T) {
	rawBody := `{"type":"sync","status":"Success","metadata":{"unknown":{"nested":[1,true,"x"]}}}`
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(rawBody)),
			Header:     make(http.Header),
		}, nil
	})

	resp := client.Execute(context.Background(), &ProxyRequest{
		ID:     "raw-body",
		Method: http.MethodGet,
		Path:   "/1.0",
	})

	if string(resp.Body) != rawBody {
		t.Fatalf("body = %s, want %s", resp.Body, rawBody)
	}
}

func TestExecutePreservesMalformedIncusResponseBody(t *testing.T) {
	rawBody := `{"type":"sync"`
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(rawBody)),
			Header:     make(http.Header),
		}, nil
	})

	resp := client.Execute(context.Background(), &ProxyRequest{
		ID:     "malformed-body",
		Method: http.MethodGet,
		Path:   "/1.0",
	})

	if string(resp.Body) != rawBody {
		t.Fatalf("body = %s, want %s", resp.Body, rawBody)
	}
}

func TestExecuteReturnsInternalServerErrorOnResponseReadError(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       errReadCloser{err: errors.New("read failed")},
			Header:     make(http.Header),
		}, nil
	})

	resp := client.Execute(context.Background(), &ProxyRequest{
		ID:     "read-error",
		Method: http.MethodGet,
		Path:   "/1.0",
	})

	if resp.ID != "read-error" {
		t.Fatalf("id = %q, want read-error", resp.ID)
	}
	if resp.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.Status)
	}
	if resp.Error == "" {
		t.Fatal("error is empty, want read error message")
	}
	if resp.Body != nil {
		t.Fatalf("body = %s, want nil", resp.Body)
	}
}

func TestExecuteReturnsServiceUnavailableOnIncusTransportError(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial unix /var/lib/incus/unix.socket: connect: no such file or directory")
	})

	resp := client.Execute(context.Background(), &ProxyRequest{
		ID:     "socket-missing",
		Method: http.MethodGet,
		Path:   "/1.0",
	})

	if resp.ID != "socket-missing" {
		t.Fatalf("id = %q, want socket-missing", resp.ID)
	}
	if resp.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.Status)
	}
	if resp.Error == "" {
		t.Fatal("error is empty, want transport error message")
	}
	if resp.Body != nil {
		t.Fatalf("body = %s, want nil", resp.Body)
	}
}

func TestProxyResponseOmitsSuccessErrorAndIncludesNullErrorBody(t *testing.T) {
	success, err := json.Marshal(ProxyResponse{
		ID:     "ok",
		Status: http.StatusOK,
		Body:   json.RawMessage(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("marshal success: %v", err)
	}
	if string(success) != `{"id":"ok","status":200,"body":{"ok":true}}` {
		t.Fatalf("success json = %s", success)
	}

	failure, err := json.Marshal(ProxyResponse{
		ID:     "fail",
		Status: http.StatusInternalServerError,
		Error:  "safe error",
	})
	if err != nil {
		t.Fatalf("marshal failure: %v", err)
	}
	if string(failure) != `{"id":"fail","status":500,"body":null,"error":"safe error"}` {
		t.Fatalf("failure json = %s", failure)
	}
}

type errReadCloser struct {
	err error
}

func (r errReadCloser) Read(p []byte) (int, error) {
	return 0, r.err
}

func (r errReadCloser) Close() error {
	return nil
}
