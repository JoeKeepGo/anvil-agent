package proxy

import (
	"testing"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

func TestValidateProxyRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     incus.ProxyRequest
		wantErr bool
	}{
		{
			name:    "missing id",
			req:     incus.ProxyRequest{Method: "GET", Path: "/1.0"},
			wantErr: true,
		},
		{
			name:    "missing method",
			req:     incus.ProxyRequest{ID: "req-1", Path: "/1.0"},
			wantErr: true,
		},
		{
			name:    "unsupported method",
			req:     incus.ProxyRequest{ID: "req-1", Method: "TRACE", Path: "/1.0"},
			wantErr: true,
		},
		{
			name:    "missing path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET"},
			wantErr: true,
		},
		{
			name:    "path outside incus api",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/not-incus"},
			wantErr: true,
		},
		{
			name:    "path prefix collision",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0foo"},
			wantErr: true,
		},
		{
			name:    "accepts incus root path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0"},
			wantErr: false,
		},
		{
			name:    "accepts incus nested path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances"},
			wantErr: false,
		},
		{
			name:    "accepts post",
			req:     incus.ProxyRequest{ID: "req-1", Method: "POST", Path: "/1.0/instances/web/state"},
			wantErr: false,
		},
		{
			name:    "accepts put",
			req:     incus.ProxyRequest{ID: "req-1", Method: "PUT", Path: "/1.0/profiles/default"},
			wantErr: false,
		},
		{
			name:    "accepts patch",
			req:     incus.ProxyRequest{ID: "req-1", Method: "PATCH", Path: "/1.0/instances/web"},
			wantErr: false,
		},
		{
			name:    "accepts delete",
			req:     incus.ProxyRequest{ID: "req-1", Method: "DELETE", Path: "/1.0/instances/web"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProxyRequest(tt.req)
			if tt.wantErr && err == nil {
				t.Fatal("validateProxyRequest returned nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateProxyRequest returned error: %v", err)
			}
		})
	}
}
