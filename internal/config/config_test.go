package config

import "testing"

func TestLoadUsesAnvilAgentEnvironment(t *testing.T) {
	t.Setenv("ANVIL_AGENT_HOST", "0.0.0.0")
	t.Setenv("ANVIL_AGENT_PORT", "19090")
	t.Setenv("ANVIL_AGENT_AUTH_TOKEN", "secret")
	t.Setenv("INCUS_SOCKET", "/tmp/incus.sock")

	cfg := Load()

	if cfg.Host != "0.0.0.0" {
		t.Fatalf("host = %q, want 0.0.0.0", cfg.Host)
	}
	if cfg.Port != 19090 {
		t.Fatalf("port = %d, want 19090", cfg.Port)
	}
	if cfg.AuthToken != "secret" {
		t.Fatalf("auth token = %q, want secret", cfg.AuthToken)
	}
	if cfg.IncusSocket != "/tmp/incus.sock" {
		t.Fatalf("incus socket = %q, want /tmp/incus.sock", cfg.IncusSocket)
	}
}

func TestLoadIgnoresLegacyProxyEnvironment(t *testing.T) {
	t.Setenv("PROXY_HOST", "0.0.0.0")
	t.Setenv("PROXY_PORT", "19090")
	t.Setenv("PROXY_AUTH_TOKEN", "legacy")

	cfg := Load()

	if cfg.Host != "127.0.0.1" {
		t.Fatalf("host = %q, want default host", cfg.Host)
	}
	if cfg.Port != 9090 {
		t.Fatalf("port = %d, want default port", cfg.Port)
	}
	if cfg.AuthToken != "" {
		t.Fatalf("auth token = %q, want empty token", cfg.AuthToken)
	}
}
