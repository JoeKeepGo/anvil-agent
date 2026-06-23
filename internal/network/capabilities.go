// Package network implements Anvil-managed WireGuard/network capability
// detection and the trusted network state report consumed by the Anvil
// control plane through the agent's trusted WebSocket transport.
//
// This package is deliberately host-local and stateless. It never stores
// product state, never executes arbitrary shell text, and never exposes
// WireGuard private keys or preshared keys. Managed interface reporting is
// limited to interfaces whose names match the configured Anvil-managed
// prefix (e.g. anvilwg0).
package network

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ForwardingState reports kernel IP forwarding readiness.
type ForwardingState struct {
	IPv4 bool `json:"ipv4"`
	IPv6 bool `json:"ipv6"`
}

// Capabilities summarizes the host network prerequisites observed by the
// agent. Fields beyond the trusted network state contract are kept internal
// to the agent and used for capability decisions.
type Capabilities struct {
	WireGuardAvailable bool            `json:"-"`
	IPCommandAvailable bool            `json:"-"`
	IPTablesAvailable  bool            `json:"-"`
	IP6TablesAvailable bool            `json:"-"`
	SysctlAvailable    bool            `json:"-"`
	TunAvailable       bool            `json:"-"`
	Forwarding         ForwardingState `json:"-"`
}

// Peer is a browser-safe WireGuard peer summary. It never includes the
// peer's preshared key; only public material and counters are exposed.
type Peer struct {
	PublicKey         string     `json:"publicKey"`
	LatestHandshakeAt *time.Time `json:"latestHandshakeAt"`
	RxBytes           int64      `json:"rxBytes"`
	TxBytes           int64      `json:"txBytes"`
}

// ManagedInterface is a browser-safe summary of an Anvil-managed WireGuard
// interface. It exposes only public material and address/routing state; the
// interface private key is never included.
type ManagedInterface struct {
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	PublicKey  string   `json:"publicKey"`
	ListenPort int      `json:"listenPort"`
	Addresses  []string `json:"addresses"`
	Peers      []Peer   `json:"peers"`
}

// NetworkSummary is the browser-safe network portion of the trusted report.
type NetworkSummary struct {
	WireGuardAvailable bool               `json:"wireGuardAvailable"`
	IPCommandAvailable bool               `json:"ipCommandAvailable"`
	IPTablesAvailable  bool               `json:"iptablesAvailable"`
	IP6TablesAvailable bool               `json:"ip6tablesAvailable"`
	Forwarding         ForwardingState    `json:"forwarding"`
	ManagedInterfaces  []ManagedInterface `json:"managedInterfaces"`
}

// NetworkState is the trusted /agent/v1/network/state response body.
type NetworkState struct {
	Agent   AgentSummary   `json:"agent"`
	Network NetworkSummary `json:"network"`
}

// AgentSummary mirrors only the identity fields the control plane needs to
// correlate a network state report with the host's accepted identity.
type AgentSummary struct {
	ID                 string `json:"id"`
	StateSchemaVersion int    `json:"stateSchemaVersion"`
}

// Probe is the host interaction boundary used by the Detector. It is an
// interface so detection can be unit-tested without a real host.
type Probe interface {
	// LookPath reports whether the named binary is available on PATH.
	LookPath(file string) (string, error)
	// Stat reports file metadata (used to detect /dev/net/tun).
	Stat(name string) (os.FileInfo, error)
	// ReadFile reads a file (used to read /proc/sys forwarding knobs).
	ReadFile(name string) ([]byte, error)
	// Run executes a named binary with fixed arguments and returns stdout.
	// It must never accept caller-supplied shell text.
	Run(name string, args ...string) ([]byte, error)
}

// hostProbe is the default Probe using the real host filesystem and exec.
type hostProbe struct{}

func (hostProbe) LookPath(file string) (string, error)  { return exec.LookPath(file) }
func (hostProbe) Stat(name string) (os.FileInfo, error) { return os.Stat(name) }
func (hostProbe) ReadFile(name string) ([]byte, error)  { return os.ReadFile(name) }
func (hostProbe) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// Detector probes the host for WireGuard/network readiness and reports
// Anvil-managed interfaces only.
type Detector struct {
	probe  Probe
	prefix string
}

// NewDetector returns a Detector bound to the given Anvil-managed interface
// prefix. A blank prefix defaults to "anvilwg".
func NewDetector(prefix string, probe Probe) *Detector {
	if strings.TrimSpace(prefix) == "" {
		prefix = "anvilwg"
	}
	if probe == nil {
		probe = hostProbe{}
	}
	return &Detector{probe: probe, prefix: prefix}
}

// ManagedPrefix returns the configured Anvil-managed interface prefix.
func (d *Detector) ManagedPrefix() string { return d.prefix }

// Detect observes the host network prerequisites. It never returns a private
// key or preshared key; callers consume only the boolean forwarding/capability
// summary.
func (d *Detector) Detect(ctx context.Context) (Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}

	caps := Capabilities{
		WireGuardAvailable: d.binaryAvailable("wg"),
		IPCommandAvailable: d.binaryAvailable("ip"),
		IPTablesAvailable:  d.binaryAvailable("iptables"),
		IP6TablesAvailable: d.binaryAvailable("ip6tables"),
		SysctlAvailable:    d.binaryAvailable("sysctl"),
		TunAvailable:       d.fileExists("/dev/net/tun"),
		Forwarding:         d.detectForwarding(),
	}
	return caps, nil
}

// NetworkState builds the trusted network state report. Managed interface
// reporting requires both `wg` and `ip`; when either is unavailable the
// managed interface list is empty and the corresponding capability flag is
// false, which is an honest absence rather than a fallback.
func (d *Detector) NetworkState(ctx context.Context, identity AgentSummary) (NetworkState, error) {
	caps, err := d.Detect(ctx)
	if err != nil {
		return NetworkState{}, err
	}

	summary := NetworkSummary{
		WireGuardAvailable: caps.WireGuardAvailable,
		IPCommandAvailable: caps.IPCommandAvailable,
		IPTablesAvailable:  caps.IPTablesAvailable,
		IP6TablesAvailable: caps.IP6TablesAvailable,
		Forwarding:         caps.Forwarding,
		ManagedInterfaces:  []ManagedInterface{},
	}

	if caps.WireGuardAvailable {
		interfaces, err := d.listManagedInterfaces(ctx)
		if err != nil {
			return NetworkState{}, fmt.Errorf("list managed wireguard interfaces: %w", err)
		}
		summary.ManagedInterfaces = interfaces
	}

	return NetworkState{Agent: identity, Network: summary}, nil
}

func (d *Detector) binaryAvailable(name string) bool {
	_, err := d.probe.LookPath(name)
	return err == nil
}

func (d *Detector) fileExists(path string) bool {
	_, err := d.probe.Stat(path)
	return err == nil
}

func (d *Detector) detectForwarding() ForwardingState {
	return ForwardingState{
		IPv4: d.readBoolFile("/proc/sys/net/ipv4/ip_forward"),
		IPv6: d.readBoolFile("/proc/sys/net/ipv6/conf/all/forwarding"),
	}
}

func (d *Detector) readBoolFile(path string) bool {
	data, err := d.probe.ReadFile(path)
	if err != nil {
		return false
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	return value != 0
}

// listManagedInterfaces reports only interfaces whose names start with the
// Anvil-managed prefix. It never returns private keys or preshared keys.
func (d *Detector) listManagedInterfaces(ctx context.Context) ([]ManagedInterface, error) {
	dump, err := d.probe.Run("wg", "show", "all", "dump")
	if err != nil {
		// `wg show` failing when no WireGuard interface exists is a legitimate
		// empty result. The WireGuardAvailable flag already reflects binary
		// presence; an empty dump is an honest absence, not a hidden error.
		return []ManagedInterface{}, nil
	}
	interfaces := ParseWireGuardDump(d.prefix, dump)
	if len(interfaces) == 0 {
		return interfaces, nil
	}

	addrTable, err := d.readAddressTable()
	if err != nil {
		// Without `ip` JSON we cannot enrich addresses; return the parsed
		// WireGuard interfaces without addresses rather than guessing.
		return interfaces, nil
	}
	for i := range interfaces {
		if entry, ok := addrTable[interfaces[i].Name]; ok {
			interfaces[i].Addresses = entry.Addresses
			if interfaces[i].Status == "" {
				interfaces[i].Status = entry.Status
			}
		}
	}
	return interfaces, nil
}

func (d *Detector) readAddressTable() (map[string]addressEntry, error) {
	out, err := d.probe.Run("ip", "-json", "addr")
	if err != nil {
		return nil, err
	}
	return ParseIPAddrJSON(out, d.prefix)
}

type addressEntry struct {
	Status    string
	Addresses []string
}

// WireGuardAvailable reports whether the host has the `wg` binary available.
// It satisfies the state package's WireGuardDetector interface without
// importing state (structural interface satisfaction).
func (d *Detector) WireGuardAvailable(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return d.binaryAvailable("wg"), nil
}
