package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/anvil/proxy/internal/config"
	"github.com/anvil/proxy/internal/incus"
	"github.com/anvil/proxy/internal/proxy"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	incusClient := incus.NewUnixClient(cfg.IncusSocket)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	server := proxy.NewServer(cfg, incusClient)
	if err := server.Start(ctx); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
