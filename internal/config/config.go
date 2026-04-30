package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port        int
	Host        string
	IncusSocket string
	AuthToken   string
}

func Load() *Config {
	cfg := &Config{
		Port:        9090,
		Host:        "127.0.0.1",
		IncusSocket: "/var/lib/incus/unix.socket",
	}

	if v := os.Getenv("ANVIL_AGENT_PORT"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.Port)
	}
	if v := os.Getenv("ANVIL_AGENT_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("INCUS_SOCKET"); v != "" {
		cfg.IncusSocket = v
	}
	if v := os.Getenv("ANVIL_AGENT_AUTH_TOKEN"); v != "" {
		cfg.AuthToken = v
	}

	return cfg
}

func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
