package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/JoeKeepGo/anvil-agent/internal/config"
	"github.com/JoeKeepGo/anvil-agent/internal/incus"
	"github.com/JoeKeepGo/anvil-agent/internal/lifecycle"
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
	lifecycleSvc := lifecycle.NewService(incusClient)
	vmDetector := alwaysAvailable{}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Rebuild the reporter so the VMLifecycle capability reflects the
	// wired lifecycle service (always available in the default runtime).
	reporter = state.NewReporter(state.ReporterOptions{
		Identity:  identity,
		Version:   state.DefaultVersion,
		Incus:     incusClient,
		WireGuard: detector,
		VMLife:    vmDetector,
	})

	server := proxy.NewServerWithLifecycle(cfg, incusClient, reporter, networkState, networkApplier, lifecycleSvc)
	if err := server.Start(ctx); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// alwaysAvailable is a VMLifecycleDetector that reports readiness whenever
// the lifecycle service is wired into the agent.
type alwaysAvailable struct{}

func (alwaysAvailable) VMLifecycleAvailable(context.Context) (bool, error) { return true, nil }
