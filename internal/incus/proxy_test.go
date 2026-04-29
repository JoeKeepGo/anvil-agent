package incus

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestExecuteForwardsJSONBody(t *testing.T) {
	client := &Client{
		base: "http://unix",
		http: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
			}),
		},
	}

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
