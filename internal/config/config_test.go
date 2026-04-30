package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("PROXY_PORT", "")
	t.Setenv("PROXY_HOST", "")
	t.Setenv("INCUS_SOCKET", "")
	t.Setenv("PROXY_AUTH_TOKEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Host != "127.0.0.1" {
		t.Fatalf("host = %q, want 127.0.0.1", cfg.Host)
	}
	if cfg.Port != 9090 {
		t.Fatalf("port = %d, want 9090", cfg.Port)
	}
	if cfg.IncusSocket != "/var/lib/incus/unix.socket" {
		t.Fatalf("incus socket = %q, want default socket", cfg.IncusSocket)
	}
	if cfg.AuthToken != "" {
		t.Fatalf("auth token = %q, want empty", cfg.AuthToken)
	}
}

func TestLoadUsesEnvironmentOverrides(t *testing.T) {
	t.Setenv("PROXY_PORT", "19090")
	t.Setenv("PROXY_HOST", "127.0.0.2")
	t.Setenv("INCUS_SOCKET", "/tmp/incus.sock")
	t.Setenv("PROXY_AUTH_TOKEN", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ListenAddr() != "127.0.0.2:19090" {
		t.Fatalf("listen addr = %q, want 127.0.0.2:19090", cfg.ListenAddr())
	}
	if cfg.IncusSocket != "/tmp/incus.sock" {
		t.Fatalf("incus socket = %q, want /tmp/incus.sock", cfg.IncusSocket)
	}
	if cfg.AuthToken != "secret" {
		t.Fatalf("auth token = %q, want secret", cfg.AuthToken)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("PROXY_PORT", "not-a-port")

	cfg, err := Load()
	if err == nil {
		t.Fatalf("Load returned nil error and config %+v, want invalid port error", cfg)
	}
}
