package incus

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	http *http.Client
	base string
}

func NewUnixClient(socketPath string) *Client {
	return &Client{
		http: newUnixHTTPClient(socketPath),
		base: "http://unix",
	}
}

func newUnixHTTPClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 30 * time.Second,
	}
}

func (c *Client) Do(ctx context.Context, method string, path string, body io.Reader) (*http.Response, error) {
	url := c.base + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "no such file or directory") {
			return nil, fmt.Errorf("incus socket not found — is Incus running? %w", err)
		}
		if strings.Contains(err.Error(), "connection refused") {
			return nil, fmt.Errorf("incus socket connection refused — is Incus running? %w", err)
		}
		return nil, fmt.Errorf("incus request failed: %w", err)
	}
	return resp, nil
}

func ReadBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
