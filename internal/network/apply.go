package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// ApplyMode is the requested apply mode. DRY_RUN must not mutate host state.
// APPLY validates and renders the authoritative plan; host execution through
// a managed service is owned by Phase 5, so Phase 3 never runs arbitrary
// shell text to mutate interfaces.
type ApplyMode string

const (
	ModeDryRun ApplyMode = "DRY_RUN"
	ModeApply  ApplyMode = "APPLY"
)

// ApplyStatus is the lifecycle status returned by the apply protocol.
const (
	StatusValidated    = "VALIDATED"
	StatusApplyPlanned = "APPLY_PLANNED"
)

// Limits keep untrusted request payloads bounded.
const (
	maxPeers         = 256
	maxAddresses     = 32
	maxAllowedIps    = 64
	maxFieldBytes    = 1 << 14 // 16 KiB per string field
	maxPeerPublicKey = 64
)

// InterfaceSpec is the desired Anvil-managed interface state in a request.
type InterfaceSpec struct {
	Name       string   `json:"name"`
	ListenPort int      `json:"listenPort"`
	Addresses  []string `json:"addresses"`
}

// PeerSpec is a requested WireGuard peer. The optional PresharedKey is
// accepted (server-side decrypted before send) but is NEVER echoed in the
// response.
type PeerSpec struct {
	PublicKey    string   `json:"publicKey"`
	PresharedKey string   `json:"presharedKey,omitempty"`
	AllowedIps   []string `json:"allowedIps"`
}

// RoutingSpec is the desired kernel forwarding state.
type RoutingSpec struct {
	IPv4Forwarding bool `json:"ipv4Forwarding"`
	IPv6Forwarding bool `json:"ipv6Forwarding"`
}

// ApplyRequest is the body of POST /agent/v1/network/apply.
type ApplyRequest struct {
	Mode      ApplyMode     `json:"mode"`
	Interface InterfaceSpec `json:"interface"`
	Peers     []PeerSpec    `json:"peers"`
	Routing   RoutingSpec   `json:"routing"`
}

// ApplyPeerSummary is the browser-safe peer echo. It never includes the
// preshared key.
type ApplyPeerSummary struct {
	PublicKey  string   `json:"publicKey"`
	AllowedIps []string `json:"allowedIps"`
}

// ApplyInterfaceSummary is the browser-safe interface echo.
type ApplyInterfaceSummary struct {
	Name       string   `json:"name"`
	ListenPort int      `json:"listenPort"`
	Addresses  []string `json:"addresses"`
}

// ApplyResponse is the body of a successful apply response.
type ApplyResponse struct {
	Mode      ApplyMode             `json:"mode"`
	Status    string                `json:"status"`
	Interface ApplyInterfaceSummary `json:"interface"`
	PeerCount int                   `json:"peerCount"`
	Routing   RoutingSpec           `json:"routing"`
	Summary   string                `json:"summary"`
}

// ApplyError is returned for validation failures. It carries a safe, non-
// secret error summary only.
type ApplyError struct {
	Status  int
	Message string
}

func (e *ApplyError) Error() string { return e.Message }

// NewApplyError constructs a validation error with an HTTP-style status.
func NewApplyError(status int, message string) *ApplyError {
	return &ApplyError{Status: status, Message: message}
}

// Applier validates and plans Anvil-managed network apply requests. It never
// executes arbitrary shell text and never mutates host state in Phase 3.
type Applier struct {
	prefix string
}

// NewApplier returns an Applier bound to the given Anvil-managed interface
// prefix. A blank prefix defaults to "anvilwg".
func NewApplier(prefix string) *Applier {
	if strings.TrimSpace(prefix) == "" {
		prefix = "anvilwg"
	}
	return &Applier{prefix: prefix}
}

// Validate parses a raw request body and returns a validated ApplyRequest or
// a safe ApplyError.
func (a *Applier) Validate(raw json.RawMessage) (ApplyRequest, error) {
	if len(raw) == 0 {
		return ApplyRequest{}, NewApplyError(400, "missing request body")
	}
	var req ApplyRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return ApplyRequest{}, NewApplyError(400, "invalid request body: "+err.Error())
	}
	if err := a.validateRequest(req); err != nil {
		return ApplyRequest{}, err
	}
	return req, nil
}

// Apply validates the request and renders the authoritative plan. DRY_RUN
// performs no host mutation; APPLY in Phase 3 renders the same plan and
// defers execution to the managed-service integration owned by Phase 5.
// No shell text, raw file paths, or unmanaged interface names are accepted.
func (a *Applier) Apply(ctx context.Context, raw json.RawMessage) (ApplyResponse, error) {
	if err := ctx.Err(); err != nil {
		return ApplyResponse{}, NewApplyError(503, "request cancelled")
	}
	req, err := a.Validate(raw)
	if err != nil {
		return ApplyResponse{}, err
	}

	peerCount := len(req.Peers)
	status := StatusValidated
	summary := fmt.Sprintf(
		"validated %s with %d peer(s); dry-run, no host mutation",
		req.Interface.Name, peerCount,
	)
	if req.Mode == ModeApply {
		status = StatusApplyPlanned
		summary = fmt.Sprintf(
			"validated and planned %s with %d peer(s); apply execution deferred to managed service",
			req.Interface.Name, peerCount,
		)
	}

	addresses := req.Interface.Addresses
	if addresses == nil {
		addresses = []string{}
	}
	return ApplyResponse{
		Mode:   req.Mode,
		Status: status,
		Interface: ApplyInterfaceSummary{
			Name:       req.Interface.Name,
			ListenPort: req.Interface.ListenPort,
			Addresses:  addresses,
		},
		PeerCount: peerCount,
		Routing:   req.Routing,
		Summary:   summary,
	}, nil
}

func (a *Applier) validateRequest(req ApplyRequest) error {
	if req.Mode != ModeDryRun && req.Mode != ModeApply {
		return NewApplyError(400, "unsupported mode")
	}
	if err := a.validateInterface(req.Interface); err != nil {
		return err
	}
	if err := validatePeers(req.Peers); err != nil {
		return err
	}
	return nil
}

func (a *Applier) validateInterface(spec InterfaceSpec) error {
	if !isManagedInterfaceName(a.prefix, spec.Name) {
		return NewApplyError(400, "interface name is not Anvil-managed")
	}
	if spec.ListenPort < 1 || spec.ListenPort > 65535 {
		return NewApplyError(400, "listenPort out of range")
	}
	if len(spec.Addresses) > maxAddresses {
		return NewApplyError(400, "too many interface addresses")
	}
	for _, addr := range spec.Addresses {
		if err := validateCidr(addr); err != nil {
			return NewApplyError(400, fmt.Sprintf("interface address %q is not a valid CIDR: %v", addr, err))
		}
	}
	return nil
}

func validatePeers(peers []PeerSpec) error {
	if len(peers) > maxPeers {
		return NewApplyError(400, "too many peers")
	}
	seen := make(map[string]struct{}, len(peers))
	for i, peer := range peers {
		if peer.PublicKey == "" {
			return NewApplyError(400, fmt.Sprintf("peer %d missing public key", i))
		}
		if len(peer.PublicKey) > maxPeerPublicKey {
			return NewApplyError(400, fmt.Sprintf("peer %d public key too long", i))
		}
		if _, dup := seen[peer.PublicKey]; dup {
			return NewApplyError(400, "duplicate peer public key")
		}
		seen[peer.PublicKey] = struct{}{}
		if len(peer.AllowedIps) > maxAllowedIps {
			return NewApplyError(400, fmt.Sprintf("peer %d has too many allowed IPs", i))
		}
		for _, cidr := range peer.AllowedIps {
			if err := validateCidr(cidr); err != nil {
				return NewApplyError(400, fmt.Sprintf("peer %d allowed IP %q is not a valid CIDR: %v", i, cidr, err))
			}
		}
	}
	return nil
}

// isManagedInterfaceName reports whether name matches the Anvil-managed
// allowlist: it must start with the configured prefix and the remainder must
// be alphanumeric only. This rejects unmanaged names such as wg0, eth0, and
// any path-like or shell-injectable value.
func isManagedInterfaceName(prefix, name string) bool {
	if name == "" || !strings.HasPrefix(name, prefix) {
		return false
	}
	rest := name[len(prefix):]
	matched, _ := regexp.MatchString(`^[0-9a-zA-Z]+$`, rest)
	return matched
}

// validateCidr validates that a string is a parseable IPv4/IPv6 CIDR in the
// form <address>/<prefix>. Interface addresses and peer allowed IPs are host
// addresses with a prefix length (e.g. 10.42.0.1/24), so a host address is
// valid here; only structural/prefix validity is enforced.
func validateCidr(value string) error {
	if len(value) > maxFieldBytes {
		return fmt.Errorf("CIDR too long")
	}
	if _, _, err := net.ParseCIDR(value); err != nil {
		return err
	}
	return nil
}
