package state

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

const DefaultVersion = "dev"

type Reporter interface {
	Report(context.Context) (Report, error)
}

type ReporterOptions struct {
	Identity  AgentIdentity
	Version   string
	StartedAt time.Time
	Hostname  func() (string, error)
	Now       func() time.Time
	Incus     incusBackend
	WireGuard WireGuardDetector
	VMLife    VMLifecycleDetector
}

type Report struct {
	Agent        AgentSummary      `json:"agent"`
	Host         HostSummary       `json:"host"`
	Incus        IncusSummary      `json:"incus"`
	Capabilities CapabilitySummary `json:"capabilities"`
	Snapshot     SnapshotSummary   `json:"snapshot"`
}

type AgentSummary struct {
	ID                 string    `json:"id"`
	Version            string    `json:"version"`
	StateSchemaVersion int       `json:"stateSchemaVersion"`
	StartedAt          time.Time `json:"startedAt"`
	ReportedAt         time.Time `json:"reportedAt"`
}

type HostSummary struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

type IncusSummary struct {
	Available     bool   `json:"available"`
	StatusCode    int    `json:"statusCode"`
	ServerVersion string `json:"serverVersion"`
	APIVersion    string `json:"apiVersion"`
}

type CapabilitySummary struct {
	IncusProxy  bool `json:"incusProxy"`
	Events      bool `json:"events"`
	StateReport bool `json:"stateReport"`
	WireGuard   bool `json:"wireGuard"`
	VMLifecycle bool `json:"vmLifecycle"`
}

type SnapshotSummary struct {
	InstancesTotal  int `json:"instancesTotal"`
	ImagesTotal     int `json:"imagesTotal"`
	OperationsTotal int `json:"operationsTotal"`
}

type reporter struct {
	identity  AgentIdentity
	version   string
	startedAt time.Time
	hostname  func() (string, error)
	now       func() time.Time
	incus     incusBackend
	wireGuard WireGuardDetector
	vmLife    VMLifecycleDetector
}

type staticReporter struct {
	report Report
}

type incusBackend interface {
	Execute(context.Context, *incus.ProxyRequest) *incus.ProxyResponse
}

// WireGuardDetector reports whether the host has WireGuard readiness. It is
// kept narrow so the state report stays decoupled from the network package.
type WireGuardDetector interface {
	WireGuardAvailable(context.Context) (bool, error)
}

// VMLifecycleDetector reports whether the connected agent exposes the trusted
// VM lifecycle protocol. It is kept narrow so the state report stays
// decoupled from the lifecycle package.
type VMLifecycleDetector interface {
	VMLifecycleAvailable(context.Context) (bool, error)
}

func NewReporter(opts ReporterOptions) Reporter {
	version := opts.Version
	if version == "" {
		version = DefaultVersion
	}
	hostname := opts.Hostname
	if hostname == nil {
		hostname = os.Hostname
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	startedAt := opts.StartedAt
	if startedAt.IsZero() {
		startedAt = now()
	}
	return &reporter{
		identity:  opts.Identity,
		version:   opts.Version,
		startedAt: startedAt.UTC(),
		hostname:  hostname,
		now:       now,
		incus:     opts.Incus,
		wireGuard: opts.WireGuard,
		vmLife:    opts.VMLife,
	}
}

func NewStaticReporter(report Report) Reporter {
	return &staticReporter{report: report}
}

func (r *staticReporter) Report(ctx context.Context) (Report, error) {
	return r.report, nil
}

func (r *reporter) Report(ctx context.Context) (Report, error) {
	hostname, err := r.hostname()
	if err != nil {
		return Report{}, fmt.Errorf("read host name: %w", err)
	}

	report := Report{
		Agent: AgentSummary{
			ID:                 r.identity.ID,
			Version:            r.version,
			StateSchemaVersion: r.identity.StateSchemaVersion,
			StartedAt:          r.startedAt,
			ReportedAt:         r.now().UTC(),
		},
		Host: HostSummary{
			Hostname: hostname,
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
		},
		Capabilities: CapabilitySummary{
			IncusProxy:  true,
			Events:      true,
			StateReport: true,
			WireGuard:   false,
			VMLifecycle: false,
		},
	}

	if r.wireGuard != nil {
		if available, err := r.wireGuard.WireGuardAvailable(ctx); err == nil {
			report.Capabilities.WireGuard = available
		}
	}

	if r.vmLife != nil {
		if available, err := r.vmLife.VMLifecycleAvailable(ctx); err == nil {
			report.Capabilities.VMLifecycle = available
		}
	}

	if r.incus == nil {
		return report, nil
	}

	incusRoot := r.incus.Execute(ctx, &incus.ProxyRequest{ID: "agent-state-incus-root", Method: http.MethodGet, Path: "/1.0"})
	report.Incus = summarizeIncusRoot(incusRoot)
	if !report.Incus.Available {
		return report, nil
	}

	report.Snapshot.InstancesTotal = r.countList(ctx, "/1.0/instances")
	report.Snapshot.ImagesTotal = r.countList(ctx, "/1.0/images")
	report.Snapshot.OperationsTotal = r.countOperations(ctx)
	return report, nil
}

func (r *reporter) countList(ctx context.Context, path string) int {
	resp := r.incus.Execute(ctx, &incus.ProxyRequest{ID: "agent-state-count", Method: http.MethodGet, Path: path})
	if resp == nil || resp.Status < 200 || resp.Status >= 300 {
		return 0
	}
	var body struct {
		Metadata []json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return 0
	}
	return len(body.Metadata)
}

func (r *reporter) countOperations(ctx context.Context) int {
	resp := r.incus.Execute(ctx, &incus.ProxyRequest{ID: "agent-state-operations", Method: http.MethodGet, Path: "/1.0/operations"})
	if resp == nil || resp.Status < 200 || resp.Status >= 300 {
		return 0
	}
	var body struct {
		Metadata map[string][]json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return 0
	}
	total := 0
	for _, operations := range body.Metadata {
		total += len(operations)
	}
	return total
}

func summarizeIncusRoot(resp *incus.ProxyResponse) IncusSummary {
	if resp == nil {
		return IncusSummary{Available: false, StatusCode: http.StatusServiceUnavailable}
	}
	summary := IncusSummary{
		Available:  resp.Status >= 200 && resp.Status < 300,
		StatusCode: resp.Status,
	}
	if !summary.Available {
		return summary
	}

	var body struct {
		Metadata struct {
			Environment struct {
				ServerVersion string `json:"server_version"`
			} `json:"environment"`
			APIVersion string `json:"api_version"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return summary
	}
	summary.ServerVersion = body.Metadata.Environment.ServerVersion
	summary.APIVersion = body.Metadata.APIVersion
	return summary
}
