package state

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

func TestReporterReturnsStateReportShape(t *testing.T) {
	startedAt := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 22, 1, 2, 3, 0, time.UTC)
	reporter := NewReporter(ReporterOptions{
		Identity: AgentIdentity{
			ID:                 "11111111-1111-4111-8111-111111111111",
			CreatedAt:          startedAt,
			StateSchemaVersion: 1,
		},
		Version:   "test-version",
		StartedAt: startedAt,
		Hostname:  func() (string, error) { return "anvil-local-vm", nil },
		Now:       func() time.Time { return now },
		Incus: &fakeReportIncus{
			responses: map[string]*incus.ProxyResponse{
				"/1.0": {
					Status: http.StatusOK,
					Body:   json.RawMessage(`{"metadata":{"environment":{"server_version":"6.12"},"api_version":"1.0"}}`),
				},
				"/1.0/instances": {
					Status: http.StatusOK,
					Body:   json.RawMessage(`{"metadata":["/1.0/instances/demo"]}`),
				},
				"/1.0/images": {
					Status: http.StatusOK,
					Body:   json.RawMessage(`{"metadata":["/1.0/images/fingerprint"]}`),
				},
				"/1.0/operations": {
					Status: http.StatusOK,
					Body:   json.RawMessage(`{"metadata":{"running":["/1.0/operations/one"],"success":["/1.0/operations/two"],"failure":[]}}`),
				},
			},
		},
	})

	report, err := reporter.Report(context.Background())
	if err != nil {
		t.Fatalf("Report returned error: %v", err)
	}

	if report.Agent.ID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("agent id = %q", report.Agent.ID)
	}
	if report.Agent.Version != "test-version" {
		t.Fatalf("agent version = %q", report.Agent.Version)
	}
	if report.Agent.StateSchemaVersion != 1 {
		t.Fatalf("state schema version = %d, want 1", report.Agent.StateSchemaVersion)
	}
	if !report.Agent.StartedAt.Equal(startedAt) {
		t.Fatalf("started at = %s, want %s", report.Agent.StartedAt, startedAt)
	}
	if !report.Agent.ReportedAt.Equal(now) {
		t.Fatalf("reported at = %s, want %s", report.Agent.ReportedAt, now)
	}
	if report.Host.Hostname != "anvil-local-vm" {
		t.Fatalf("hostname = %q", report.Host.Hostname)
	}
	if report.Host.OS == "" {
		t.Fatal("host os is empty")
	}
	if report.Host.Arch == "" {
		t.Fatal("host arch is empty")
	}
	if !report.Incus.Available {
		t.Fatal("incus available = false, want true")
	}
	if report.Incus.StatusCode != http.StatusOK {
		t.Fatalf("incus status = %d, want 200", report.Incus.StatusCode)
	}
	if report.Incus.ServerVersion != "6.12" {
		t.Fatalf("server version = %q, want 6.12", report.Incus.ServerVersion)
	}
	if report.Incus.APIVersion != "1.0" {
		t.Fatalf("api version = %q, want 1.0", report.Incus.APIVersion)
	}
	if !report.Capabilities.IncusProxy || !report.Capabilities.Events || !report.Capabilities.StateReport {
		t.Fatalf("capabilities = %+v, want incusProxy/events/stateReport true", report.Capabilities)
	}
	if report.Capabilities.WireGuard {
		t.Fatal("wireGuard capability = true, want false")
	}
	if report.Capabilities.VMLifecycle {
		t.Fatal("vmLifecycle capability = true, want false")
	}
	if report.Snapshot.InstancesTotal != 1 {
		t.Fatalf("instances total = %d, want 1", report.Snapshot.InstancesTotal)
	}
	if report.Snapshot.ImagesTotal != 1 {
		t.Fatalf("images total = %d, want 1", report.Snapshot.ImagesTotal)
	}
	if report.Snapshot.OperationsTotal != 2 {
		t.Fatalf("operations total = %d, want 2", report.Snapshot.OperationsTotal)
	}
}

func TestReporterHandlesIncusUnavailable(t *testing.T) {
	reporter := NewReporter(ReporterOptions{
		Identity:  AgentIdentity{ID: "11111111-1111-4111-8111-111111111111", StateSchemaVersion: 1},
		StartedAt: time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		Hostname:  func() (string, error) { return "host", nil },
		Now:       func() time.Time { return time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC) },
		Incus: &fakeReportIncus{
			responses: map[string]*incus.ProxyResponse{
				"/1.0": {Status: http.StatusServiceUnavailable, Error: "incus unavailable"},
			},
		},
	})

	report, err := reporter.Report(context.Background())
	if err != nil {
		t.Fatalf("Report returned error: %v", err)
	}
	if report.Incus.Available {
		t.Fatal("incus available = true, want false")
	}
	if report.Incus.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("incus status = %d, want 503", report.Incus.StatusCode)
	}
	if report.Snapshot.InstancesTotal != 0 || report.Snapshot.ImagesTotal != 0 || report.Snapshot.OperationsTotal != 0 {
		t.Fatalf("snapshot = %+v, want zero totals", report.Snapshot)
	}
}

func TestReporterSerializationExcludesSecretsAndProductState(t *testing.T) {
	reporter := NewReporter(ReporterOptions{
		Identity:  AgentIdentity{ID: "11111111-1111-4111-8111-111111111111", StateSchemaVersion: 1},
		Version:   "secret-scan-version",
		StartedAt: time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		Hostname:  func() (string, error) { return "host", nil },
		Now:       func() time.Time { return time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC) },
		Incus: &fakeReportIncus{
			responses: map[string]*incus.ProxyResponse{
				"/1.0": {
					Status: http.StatusOK,
					Body:   json.RawMessage(`{"metadata":{"environment":{"server_version":"6.12","server_pid":1234,"server_name":"secret-host"},"api_version":"1.0","config":{"core.trust_password":"do-not-return"}}}`),
				},
				"/1.0/instances": {
					Status: http.StatusOK,
					Body:   json.RawMessage(`{"metadata":[]}`),
				},
				"/1.0/images": {
					Status: http.StatusOK,
					Body:   json.RawMessage(`{"metadata":[]}`),
				},
				"/1.0/operations": {
					Status: http.StatusOK,
					Body:   json.RawMessage(`{"metadata":{"running":[]}}`),
				},
			},
		},
	})

	report, err := reporter.Report(context.Background())
	if err != nil {
		t.Fatalf("Report returned error: %v", err)
	}
	serialized, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	lower := strings.ToLower(string(serialized))
	for _, forbidden := range []string{
		"token",
		"authorization",
		"cookie",
		"password",
		"private",
		"unix.socket",
		"/var/lib/incus",
		"tenant",
		"project",
		"user",
		"rbac",
		"audit",
		"do-not-return",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("serialized report contains forbidden %q: %s", forbidden, serialized)
		}
	}
}

type fakeReportIncus struct {
	responses map[string]*incus.ProxyResponse
}

func (f *fakeReportIncus) Execute(ctx context.Context, req *incus.ProxyRequest) *incus.ProxyResponse {
	if resp, ok := f.responses[req.Path]; ok {
		resp.ID = req.ID
		return resp
	}
	return &incus.ProxyResponse{ID: req.ID, Status: http.StatusNotFound, Error: "not found"}
}

func (f *fakeReportIncus) ListenEvents(ctx context.Context, ch chan<- incus.Event) error {
	<-ctx.Done()
	return ctx.Err()
}
