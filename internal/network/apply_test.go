package network

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func validApplyBody(mode, ifaceName string, peers int) []byte {
	peerList := []PeerSpec{}
	for i := 0; i < peers; i++ {
		peerList = append(peerList, PeerSpec{
			PublicKey:  "peer-public-key-" + string(rune('a'+i)),
			AllowedIps: []string{"10.42.0.2/32"},
		})
	}
	body, _ := json.Marshal(ApplyRequest{
		Mode: ApplyMode(mode),
		Interface: InterfaceSpec{
			Name:       ifaceName,
			ListenPort: 51820,
			Addresses:  []string{"10.42.0.1/24", "fd42:42:42::1/64"},
		},
		Peers:   peerList,
		Routing: RoutingSpec{IPv4Forwarding: true, IPv6Forwarding: true},
	})
	return body
}

func TestApplyDryRunValidatesAndDoesNotMutateHostState(t *testing.T) {
	applier := NewApplier("anvilwg")
	resp, err := applier.Apply(context.Background(), validApplyBody("DRY_RUN", "anvilwg0", 1))
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if resp.Mode != ModeDryRun {
		t.Fatalf("mode = %q, want DRY_RUN", resp.Mode)
	}
	if resp.Status != StatusValidated {
		t.Fatalf("status = %q, want VALIDATED", resp.Status)
	}
	if resp.Interface.Name != "anvilwg0" {
		t.Fatalf("interface name = %q", resp.Interface.Name)
	}
	if resp.PeerCount != 1 {
		t.Fatalf("peer count = %d, want 1", resp.PeerCount)
	}
	if !strings.Contains(resp.Summary, "dry-run") {
		t.Fatalf("summary = %q, want dry-run mention", resp.Summary)
	}
}

func TestApplyApplyModePlansAndDefersExecution(t *testing.T) {
	applier := NewApplier("anvilwg")
	resp, err := applier.Apply(context.Background(), validApplyBody("APPLY", "anvilwg0", 2))
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if resp.Mode != ModeApply {
		t.Fatalf("mode = %q, want APPLY", resp.Mode)
	}
	if resp.Status != StatusApplyPlanned {
		t.Fatalf("status = %q, want APPLY_PLANNED", resp.Status)
	}
	if !strings.Contains(resp.Summary, "deferred to managed service") {
		t.Fatalf("summary = %q, want deferred mention", resp.Summary)
	}
}

func TestApplyRejectsUnmanagedInterfaceNames(t *testing.T) {
	applier := NewApplier("anvilwg")
	for _, name := range []string{"wg0", "eth0", "anvilwg0/../../etc/passwd", "anvil wg0", "anvilwg0;rm -rf /", "", "ANVILWG0"} {
		_, err := applier.Apply(context.Background(), validApplyBody("DRY_RUN", name, 0))
		if err == nil {
			t.Fatalf("expected error for unmanaged interface name %q", name)
		}
		applyErr, ok := err.(*ApplyError)
		if !ok || applyErr.Status != 400 {
			t.Fatalf("interface %q error = %v, want 400 ApplyError", name, err)
		}
	}
}

func TestApplyAcceptsOnlyAnvilManagedPrefix(t *testing.T) {
	applier := NewApplier("anvilwg")
	resp, err := applier.Apply(context.Background(), validApplyBody("DRY_RUN", "anvilwg123", 0))
	if err != nil {
		t.Fatalf("expected anvilwg123 to be accepted, got %v", err)
	}
	if resp.Interface.Name != "anvilwg123" {
		t.Fatalf("interface name = %q", resp.Interface.Name)
	}
}

func TestApplyRejectsUnsupportedMode(t *testing.T) {
	applier := NewApplier("anvilwg")
	body, _ := json.Marshal(ApplyRequest{
		Mode:      "EXEC",
		Interface: InterfaceSpec{Name: "anvilwg0", ListenPort: 51820, Addresses: []string{"10.42.0.1/24"}},
	})
	_, err := applier.Apply(context.Background(), body)
	if err == nil {
		t.Fatal("expected error for unsupported mode")
	}
	if e, ok := err.(*ApplyError); !ok || e.Status != 400 {
		t.Fatalf("error = %v, want 400 ApplyError", err)
	}
}

func TestApplyRejectsInvalidListenPort(t *testing.T) {
	applier := NewApplier("anvilwg")
	for _, port := range []int{0, -1, 70000, 65536} {
		body, _ := json.Marshal(ApplyRequest{
			Mode:      ModeDryRun,
			Interface: InterfaceSpec{Name: "anvilwg0", ListenPort: port, Addresses: []string{"10.42.0.1/24"}},
		})
		_, err := applier.Apply(context.Background(), body)
		if err == nil {
			t.Fatalf("expected error for port %d", port)
		}
	}
}

func TestApplyRejectsMalformedCidr(t *testing.T) {
	applier := NewApplier("anvilwg")
	for _, addr := range []string{"not-a-cidr", "10.42.0.1/33", "fd42:42:42::1/129", "10.42.0.1"} {
		body, _ := json.Marshal(ApplyRequest{
			Mode:      ModeDryRun,
			Interface: InterfaceSpec{Name: "anvilwg0", ListenPort: 51820, Addresses: []string{addr}},
		})
		_, err := applier.Apply(context.Background(), body)
		if err == nil {
			t.Fatalf("expected error for CIDR %q", addr)
		}
	}
}

func TestApplyRejectsDuplicatePeerPublicKeys(t *testing.T) {
	applier := NewApplier("anvilwg")
	body, _ := json.Marshal(ApplyRequest{
		Mode:      ModeDryRun,
		Interface: InterfaceSpec{Name: "anvilwg0", ListenPort: 51820, Addresses: []string{"10.42.0.1/24"}},
		Peers: []PeerSpec{
			{PublicKey: "same-key", AllowedIps: []string{"10.42.0.2/32"}},
			{PublicKey: "same-key", AllowedIps: []string{"10.42.0.3/32"}},
		},
	})
	_, err := applier.Apply(context.Background(), body)
	if err == nil {
		t.Fatal("expected duplicate peer public key error")
	}
	if e, ok := err.(*ApplyError); !ok || e.Status != 400 {
		t.Fatalf("error = %v, want 400 ApplyError", err)
	}
}

func TestApplyRejectsHugePeerListAndOversizedFields(t *testing.T) {
	applier := NewApplier("anvilwg")
	peers := make([]PeerSpec, maxPeers+1)
	for i := range peers {
		peers[i] = PeerSpec{PublicKey: "key", AllowedIps: []string{"10.42.0.2/32"}}
	}
	body, _ := json.Marshal(ApplyRequest{
		Mode:      ModeDryRun,
		Interface: InterfaceSpec{Name: "anvilwg0", ListenPort: 51820, Addresses: []string{"10.42.0.1/24"}},
		Peers:     peers,
	})
	_, err := applier.Apply(context.Background(), body)
	if err == nil {
		t.Fatal("expected error for too many peers")
	}

	huge := strings.Repeat("x", maxPeerPublicKey+1)
	body2, _ := json.Marshal(ApplyRequest{
		Mode:      ModeDryRun,
		Interface: InterfaceSpec{Name: "anvilwg0", ListenPort: 51820, Addresses: []string{"10.42.0.1/24"}},
		Peers:     []PeerSpec{{PublicKey: huge, AllowedIps: []string{"10.42.0.2/32"}}},
	})
	_, err = applier.Apply(context.Background(), body2)
	if err == nil {
		t.Fatal("expected error for oversized public key")
	}
}

func TestApplyNeverEchoesPresharedKey(t *testing.T) {
	applier := NewApplier("anvilwg")
	body, _ := json.Marshal(ApplyRequest{
		Mode:      ModeDryRun,
		Interface: InterfaceSpec{Name: "anvilwg0", ListenPort: 51820, Addresses: []string{"10.42.0.1/24"}},
		Peers: []PeerSpec{
			{PublicKey: "peer-public-key", PresharedKey: "preshared-key-must-not-leak", AllowedIps: []string{"10.42.0.2/32"}},
		},
		Routing: RoutingSpec{IPv4Forwarding: true, IPv6Forwarding: true},
	})
	resp, err := applier.Apply(context.Background(), body)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	serialized, _ := json.Marshal(resp)
	lower := strings.ToLower(string(serialized))
	for _, forbidden := range []string{
		"preshared-key-must-not-leak",
		"preshared",
		"psk",
		"private",
		"token",
		"cookie",
		"authorization",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("apply response leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestApplyRejectsMissingBody(t *testing.T) {
	applier := NewApplier("anvilwg")
	_, err := applier.Apply(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for missing body")
	}
}

func TestApplyRejectsMalformedJSON(t *testing.T) {
	applier := NewApplier("anvilwg")
	_, err := applier.Apply(context.Background(), json.RawMessage(`{`))
	if err == nil {
		t.Fatal("expected error for malformed json")
	}
	if e, ok := err.(*ApplyError); !ok || e.Status != 400 {
		t.Fatalf("error = %v, want 400 ApplyError", err)
	}
}

func TestApplyResponsePreservesRoutingFlags(t *testing.T) {
	applier := NewApplier("anvilwg")
	resp, err := applier.Apply(context.Background(), validApplyBody("DRY_RUN", "anvilwg0", 0))
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !resp.Routing.IPv4Forwarding || !resp.Routing.IPv6Forwarding {
		t.Fatalf("routing = %+v, want both true", resp.Routing)
	}
}
