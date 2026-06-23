package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
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
	if cfg.StateDir != "/var/lib/anvil-agent" {
		t.Fatalf("state dir = %q, want /var/lib/anvil-agent", cfg.StateDir)
	}
	if cfg.AuthToken != "" {
		t.Fatalf("auth token = %q, want empty", cfg.AuthToken)
	}
}

func TestLoadUsesAnvilAgentEnvironment(t *testing.T) {
	t.Setenv("ANVIL_AGENT_HOST", "0.0.0.0")
	t.Setenv("ANVIL_AGENT_PORT", "19090")
	t.Setenv("ANVIL_AGENT_AUTH_TOKEN", "secret")
	t.Setenv("ANVIL_AGENT_STATE_DIR", "/tmp/anvil-agent-state")
	t.Setenv("INCUS_SOCKET", "/tmp/incus.sock")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ListenAddr() != "0.0.0.0:19090" {
		t.Fatalf("listen addr = %q, want 0.0.0.0:19090", cfg.ListenAddr())
	}
	if cfg.IncusSocket != "/tmp/incus.sock" {
		t.Fatalf("incus socket = %q, want /tmp/incus.sock", cfg.IncusSocket)
	}
	if cfg.StateDir != "/tmp/anvil-agent-state" {
		t.Fatalf("state dir = %q, want /tmp/anvil-agent-state", cfg.StateDir)
	}
	if cfg.AuthToken != "secret" {
		t.Fatalf("auth token = %q, want secret", cfg.AuthToken)
	}
}

func TestLoadRejectsInvalidAnvilAgentPort(t *testing.T) {
	t.Setenv("ANVIL_AGENT_PORT", "not-a-port")

	cfg, err := Load()
	if err == nil {
		t.Fatalf("Load returned nil error and config %+v, want invalid port error", cfg)
	}
}

func TestLoadIgnoresLegacyProxyEnvironment(t *testing.T) {
	t.Setenv("PROXY_HOST", "0.0.0.0")
	t.Setenv("PROXY_PORT", "19090")
	t.Setenv("PROXY_AUTH_TOKEN", "legacy")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

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

func TestLoadDefaultsManagedInterfacePrefix(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ManagedInterfacePrefix != "anvilwg" {
		t.Fatalf("managed interface prefix = %q, want anvilwg", cfg.ManagedInterfacePrefix)
	}
}

func TestLoadUsesManagedInterfacePrefixEnvironment(t *testing.T) {
	t.Setenv("ANVIL_AGENT_MANAGED_INTERFACE_PREFIX", "anvilmesh")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ManagedInterfacePrefix != "anvilmesh" {
		t.Fatalf("managed interface prefix = %q, want anvilmesh", cfg.ManagedInterfacePrefix)
	}
}
