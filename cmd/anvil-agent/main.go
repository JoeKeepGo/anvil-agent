package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/JoeKeepGo/anvil-agent/internal/config"
	"github.com/JoeKeepGo/anvil-agent/internal/incus"
	"github.com/JoeKeepGo/anvil-agent/internal/network"
	"github.com/JoeKeepGo/anvil-agent/internal/proxy"
	"github.com/JoeKeepGo/anvil-agent/internal/state"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	incusClient := incus.NewUnixClient(cfg.IncusSocket)
	identity, err := state.LoadIdentity(cfg.StateDir)
	if err != nil {
		log.Fatalf("identity error: %v", err)
	}
	detector := network.NewDetector(cfg.ManagedInterfacePrefix, nil)
	reporter := state.NewReporter(state.ReporterOptions{
		Identity:  identity,
		Version:   state.DefaultVersion,
		Incus:     incusClient,
		WireGuard: detector,
	})
	networkState := network.NewStateReporter(detector, network.AgentSummary{
		ID:                 identity.ID,
		StateSchemaVersion: identity.StateSchemaVersion,
	})
	networkApplier := network.NewApplier(cfg.ManagedInterfacePrefix)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	server := proxy.NewServerWithNetwork(cfg, incusClient, reporter, networkState, networkApplier)
	if err := server.Start(ctx); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
