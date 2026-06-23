package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port                   int
	Host                   string
	IncusSocket            string
	StateDir               string
	AuthToken              string
	ManagedInterfacePrefix string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                   9090,
		Host:                   "127.0.0.1",
		IncusSocket:            "/var/lib/incus/unix.socket",
		StateDir:               "/var/lib/anvil-agent",
		ManagedInterfacePrefix: defaultManagedInterfacePrefix,
	}

	if v := os.Getenv("ANVIL_AGENT_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid ANVIL_AGENT_PORT %q: %w", v, err)
		}
		cfg.Port = port
	}
	if v := os.Getenv("ANVIL_AGENT_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("INCUS_SOCKET"); v != "" {
		cfg.IncusSocket = v
	}
	if v := os.Getenv("ANVIL_AGENT_STATE_DIR"); v != "" {
		cfg.StateDir = v
	}
	if v := os.Getenv("ANVIL_AGENT_AUTH_TOKEN"); v != "" {
		cfg.AuthToken = v
	}
	if v := os.Getenv("ANVIL_AGENT_MANAGED_INTERFACE_PREFIX"); v != "" {
		cfg.ManagedInterfacePrefix = v
	}

	return cfg, nil
}

const defaultManagedInterfacePrefix = "anvilwg"

func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
