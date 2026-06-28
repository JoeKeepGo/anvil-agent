package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

func TestReadinessDetectorReportsFalseWhenKVMUnavailable(t *testing.T) {
	fb := &fakeIncus{}
	detector := NewReadinessDetector(fb)
	detector.kvmAvailable = func(string) (bool, error) { return false, nil }

	available, err := detector.VMLifecycleAvailable(context.Background())
	if err != nil {
		t.Fatalf("VMLifecycleAvailable error: %v", err)
	}
	if available {
		t.Fatal("vm lifecycle available = true, want false without KVM")
	}
	if len(fb.calls) != 0 {
		t.Fatalf("incus calls = %d, want 0 when KVM is unavailable", len(fb.calls))
	}
}

func TestReadinessDetectorReturnsKVMProbeError(t *testing.T) {
	want := errors.New("probe failed")
	detector := NewReadinessDetector(&fakeIncus{})
	detector.kvmAvailable = func(string) (bool, error) { return false, want }

	available, err := detector.VMLifecycleAvailable(context.Background())
	if err != want {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if available {
		t.Fatal("vm lifecycle available = true, want false on probe error")
	}
}

func TestReadinessDetectorReportsFalseWhenProfileRootDiskUnsafe(t *testing.T) {
	fb := &fakeIncus{resps: map[string]*incus.ProxyResponse{
		"/1.0/profiles/default": profileWithRoot(map[string]map[string]string{
			"root": {"type": "disk", "path": "/"},
		}),
	}}
	detector := NewReadinessDetector(fb)
	detector.kvmAvailable = func(string) (bool, error) { return true, nil }

	available, err := detector.VMLifecycleAvailable(context.Background())
	if err != nil {
		t.Fatalf("VMLifecycleAvailable error: %v", err)
	}
	if available {
		t.Fatal("vm lifecycle available = true, want false with unsafe profile root disk")
	}
	if len(fb.calls) != 1 || fb.calls[0].Method != http.MethodGet || fb.calls[0].Path != "/1.0/profiles/default" {
		t.Fatalf("profile calls = %+v, want one default profile read", fb.calls)
	}
}

func TestReadinessDetectorReportsTrueWhenKVMAndProfileRootDiskAvailable(t *testing.T) {
	fb := &fakeIncus{resps: map[string]*incus.ProxyResponse{
		"/1.0/profiles/default": defaultProfileRoot(),
	}}
	detector := NewReadinessDetector(fb)
	detector.kvmAvailable = func(string) (bool, error) { return true, nil }

	available, err := detector.VMLifecycleAvailable(context.Background())
	if err != nil {
		t.Fatalf("VMLifecycleAvailable error: %v", err)
	}
	if !available {
		t.Fatal("vm lifecycle available = false, want true")
	}
}
