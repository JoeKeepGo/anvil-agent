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
			name:    "unknown agent api path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/agent/v1/unknown"},
			wantErr: false,
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
			name:    "accepts incus instances list read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances"},
			wantErr: false,
		},
		{
			name:    "accepts incus images list read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/images"},
			wantErr: false,
		},
		{
			name:    "accepts incus images recursion read query",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/images?recursion=1"},
			wantErr: false,
		},
		{
			name:    "accepts incus operations list read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/operations"},
			wantErr: false,
		},
		{
			name:    "accepts incus encoded instance detail read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances/demo%20name"},
			wantErr: false,
		},
		{
			name:    "accepts incus image detail read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/images/fingerprint"},
			wantErr: false,
		},
		{
			name:    "accepts incus operation detail read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/operations/op-1"},
			wantErr: false,
		},
		{
			name:    "rejects unsupported incus read path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/certificates"},
			wantErr: true,
		},
		{
			name:    "rejects unsupported instance snapshots read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances/vm/snapshots"},
			wantErr: true,
		},
		{
			name:    "rejects unsupported instance state read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances/vm/state"},
			wantErr: true,
		},
		{
			name:    "rejects unsupported instance logs read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances/vm/logs"},
			wantErr: true,
		},
		{
			name:    "rejects unsupported image export read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/images/fp/export"},
			wantErr: true,
		},
		{
			name:    "rejects unsupported operation wait read",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/operations/op/wait"},
			wantErr: true,
		},
		{
			name:    "rejects arbitrary root query",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0?recursion=1"},
			wantErr: true,
		},
		{
			name:    "rejects unsupported instances recursion query",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances?recursion=1"},
			wantErr: true,
		},
		{
			name:    "rejects unsupported images query",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/images?project=default"},
			wantErr: true,
		},
		{
			name:    "rejects unsupported operations recursion query",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/operations?recursion=1"},
			wantErr: true,
		},
		{
			name:    "rejects detail query strings",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances/vm?recursion=1"},
			wantErr: true,
		},
		{
			name:    "rejects encoded slash in detail segment",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances/vm%2Fstate"},
			wantErr: true,
		},
		{
			name:    "rejects dot detail segment",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances/."},
			wantErr: true,
		},
		{
			name:    "rejects dot dot detail segment",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/1.0/instances/.."},
			wantErr: true,
		},
		{
			name:    "accepts agent state path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/agent/v1/state"},
			wantErr: false,
		},
		{
			name:    "accepts agent network state path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "GET", Path: "/agent/v1/network/state"},
			wantErr: false,
		},
		{
			name:    "accepts agent network apply path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "POST", Path: "/agent/v1/network/apply"},
			wantErr: false,
		},
		{
			name:    "accepts lifecycle create path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "POST", Path: "/agent/v1/lifecycle/instances/create"},
			wantErr: false,
		},
		{
			name:    "accepts lifecycle start path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "POST", Path: "/agent/v1/lifecycle/instances/vm/start"},
			wantErr: false,
		},
		{
			name:    "accepts lifecycle stop path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "POST", Path: "/agent/v1/lifecycle/instances/vm/stop"},
			wantErr: false,
		},
		{
			name:    "accepts lifecycle restart path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "POST", Path: "/agent/v1/lifecycle/instances/vm/restart"},
			wantErr: false,
		},
		{
			name:    "accepts lifecycle delete path",
			req:     incus.ProxyRequest{ID: "req-1", Method: "POST", Path: "/agent/v1/lifecycle/instances/vm/delete"},
			wantErr: false,
		},
		{
			name:    "rejects raw incus post write",
			req:     incus.ProxyRequest{ID: "req-1", Method: "POST", Path: "/1.0/instances/web/state"},
			wantErr: true,
		},
		{
			name:    "rejects raw incus put write",
			req:     incus.ProxyRequest{ID: "req-1", Method: "PUT", Path: "/1.0/profiles/default"},
			wantErr: true,
		},
		{
			name:    "rejects raw incus patch write",
			req:     incus.ProxyRequest{ID: "req-1", Method: "PATCH", Path: "/1.0/instances/web"},
			wantErr: true,
		},
		{
			name:    "rejects raw incus delete write",
			req:     incus.ProxyRequest{ID: "req-1", Method: "DELETE", Path: "/1.0/instances/web"},
			wantErr: true,
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
