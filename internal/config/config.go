package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port        int
	Host        string
	IncusSocket string
	AuthToken   string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:        9090,
		Host:        "127.0.0.1",
		IncusSocket: "/var/lib/incus/unix.socket",
	}

	if v := os.Getenv("PROXY_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid PROXY_PORT %q: %w", v, err)
		}
		cfg.Port = port
	}
	if v := os.Getenv("PROXY_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("INCUS_SOCKET"); v != "" {
		cfg.IncusSocket = v
	}
	if v := os.Getenv("PROXY_AUTH_TOKEN"); v != "" {
		cfg.AuthToken = v
	}

	return cfg, nil
}

func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
