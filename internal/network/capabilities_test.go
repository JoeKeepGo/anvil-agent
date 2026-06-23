package network

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeProbe struct {
	lookPath map[string]bool
	files    map[string][]byte
	stat     map[string]bool
	runs     map[string][]byte
	runErr   map[string]error
}

func (f fakeProbe) LookPath(file string) (string, error) {
	if f.lookPath[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (f fakeProbe) Stat(name string) (os.FileInfo, error) {
	if f.stat[name] {
		return fakeFileInfo{}, nil
	}
	return nil, os.ErrNotExist
}

func (f fakeProbe) ReadFile(name string) ([]byte, error) {
	if data, ok := f.files[name]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (f fakeProbe) Run(name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	if err, ok := f.runErr[key]; ok {
		return nil, err
	}
	if out, ok := f.runs[key]; ok {
		return out, nil
	}
	return nil, os.ErrNotExist
}

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }

func TestDetectReportsAllCapabilityFlagsAndForwarding(t *testing.T) {
	probe := fakeProbe{
		lookPath: map[string]bool{
			"wg": true, "ip": true, "iptables": true, "ip6tables": true, "sysctl": true,
		},
		stat: map[string]bool{"/dev/net/tun": true},
		files: map[string][]byte{
			"/proc/sys/net/ipv4/ip_forward":          []byte("1\n"),
			"/proc/sys/net/ipv6/conf/all/forwarding": []byte("1\n"),
		},
	}
	detector := NewDetector("anvilwg", probe)

	caps, err := detector.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !caps.WireGuardAvailable || !caps.IPCommandAvailable || !caps.IPTablesAvailable || !caps.IP6TablesAvailable {
		t.Fatalf("expected all binaries available, got %+v", caps)
	}
	if !caps.TunAvailable {
		t.Fatal("tun available = false, want true")
	}
	if !caps.Forwarding.IPv4 || !caps.Forwarding.IPv6 {
		t.Fatalf("forwarding = %+v, want both true", caps.Forwarding)
	}
}

func TestDetectHandlesMissingBinariesAndDisabledForwarding(t *testing.T) {
	probe := fakeProbe{
		lookPath: map[string]bool{"wg": true},
		files: map[string][]byte{
			"/proc/sys/net/ipv4/ip_forward":          []byte("0\n"),
			"/proc/sys/net/ipv6/conf/all/forwarding": []byte("0\n"),
		},
	}
	detector := NewDetector("anvilwg", probe)

	caps, err := detector.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !caps.WireGuardAvailable {
		t.Fatal("wireguard available = false, want true")
	}
	if caps.IPCommandAvailable || caps.IPTablesAvailable || caps.IP6TablesAvailable || caps.SysctlAvailable {
		t.Fatalf("expected missing ip/iptables/ip6tables/sysctl, got %+v", caps)
	}
	if caps.TunAvailable {
		t.Fatal("tun available = true, want false")
	}
	if caps.Forwarding.IPv4 || caps.Forwarding.IPv6 {
		t.Fatalf("forwarding = %+v, want both false", caps.Forwarding)
	}
}

func TestDetectBlankPrefixDefaultsToAnvilwg(t *testing.T) {
	detector := NewDetector("", nil)
	if detector.ManagedPrefix() != "anvilwg" {
		t.Fatalf("prefix = %q, want anvilwg", detector.ManagedPrefix())
	}
}

func TestNetworkStateExposesDocumentedShapeAndNoSecrets(t *testing.T) {
	probe := fakeProbe{
		lookPath: map[string]bool{"wg": true, "ip": true, "iptables": true, "ip6tables": true},
		files: map[string][]byte{
			"/proc/sys/net/ipv4/ip_forward":          []byte("1\n"),
			"/proc/sys/net/ipv6/conf/all/forwarding": []byte("1\n"),
		},
		runs: map[string][]byte{
			"wg show all dump": []byte(
				"anvilwg0\tPRIVATE-KEY-MUST-NOT-LEAK\tanvilwg-public-key\t51820\t0\n" +
					"anvilwg0\tpeer\tpeer-public-key\tPSK-MUST-NOT-LEAK\t10.0.0.2:51820\t10.42.0.2/32,fd42:42:42::2/128\t1719427200\t1234\t5678\t0\n" +
					"wg0\tPRIVATE-KEY\twg0-public-key\t51821\t0\n" +
					"wg0\tpeer\twg0-peer-public-key\tPSK\t10.0.0.3:51820\t10.0.0.3/32\t0\t0\t0\t0\n",
			),
			"ip -json addr": []byte(`[
				{"ifname":"anvilwg0","operstate":"UP","addr_info":[{"local":"10.42.0.1","prefixlen":24,"family":"inet"},{"local":"fd42:42:42::1","prefixlen":64,"family":"inet6"}]},
				{"ifname":"wg0","operstate":"UP","addr_info":[{"local":"10.0.0.1","prefixlen":24,"family":"inet"}]},
				{"ifname":"eth0","operstate":"UP","addr_info":[{"local":"192.168.1.5","prefixlen":24,"family":"inet"}]}
			]`),
		},
	}
	detector := NewDetector("anvilwg", probe)

	state, err := detector.NetworkState(context.Background(), AgentSummary{
		ID:                 "11111111-1111-4111-8111-111111111111",
		StateSchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("NetworkState error: %v", err)
	}
	if state.Agent.ID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("agent id = %q", state.Agent.ID)
	}
	if state.Agent.StateSchemaVersion != 1 {
		t.Fatalf("schema version = %d", state.Agent.StateSchemaVersion)
	}
	if !state.Network.WireGuardAvailable || !state.Network.IPCommandAvailable {
		t.Fatalf("network capabilities = %+v", state.Network)
	}
	if len(state.Network.ManagedInterfaces) != 1 {
		t.Fatalf("managed interfaces = %d, want 1 (anvilwg0 only; wg0/eth0 excluded)", len(state.Network.ManagedInterfaces))
	}
	iface := state.Network.ManagedInterfaces[0]
	if iface.Name != "anvilwg0" {
		t.Fatalf("interface name = %q, want anvilwg0", iface.Name)
	}
	if iface.PublicKey != "anvilwg-public-key" {
		t.Fatalf("public key = %q", iface.PublicKey)
	}
	if iface.ListenPort != 51820 {
		t.Fatalf("listen port = %d, want 51820", iface.ListenPort)
	}
	if iface.Status != "UP" {
		t.Fatalf("status = %q, want UP", iface.Status)
	}
	if len(iface.Addresses) != 2 {
		t.Fatalf("addresses = %v, want 2", iface.Addresses)
	}
	if len(iface.Peers) != 1 {
		t.Fatalf("peers = %d, want 1", len(iface.Peers))
	}
	if iface.Peers[0].PublicKey != "peer-public-key" {
		t.Fatalf("peer public key = %q", iface.Peers[0].PublicKey)
	}
	if iface.Peers[0].RxBytes != 1234 || iface.Peers[0].TxBytes != 5678 {
		t.Fatalf("peer counters = rx %d tx %d", iface.Peers[0].RxBytes, iface.Peers[0].TxBytes)
	}
	if iface.Peers[0].LatestHandshakeAt == nil {
		t.Fatal("latest handshake = nil, want timestamp")
	}

	serialized, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	lower := strings.ToLower(string(serialized))
	for _, forbidden := range []string{
		"private-key-must-not-leak",
		"private-key",
		"psk-must-not-leak",
		"preshared",
		"psk",
		"token",
		"authorization",
		"cookie",
		"password",
		"unix.socket",
		"/var/lib/incus",
		"tenant",
		"project",
		"user",
		"rbac",
		"audit",
		"wg0-public-key",
		"eth0",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("network state contains forbidden %q: %s", forbidden, serialized)
		}
	}
}

func TestNetworkStateWithoutWireGuardReturnsEmptyManagedInterfaces(t *testing.T) {
	probe := fakeProbe{
		lookPath: map[string]bool{"ip": true},
		files: map[string][]byte{
			"/proc/sys/net/ipv4/ip_forward": []byte("0\n"),
		},
	}
	detector := NewDetector("anvilwg", probe)

	state, err := detector.NetworkState(context.Background(), AgentSummary{ID: "id", StateSchemaVersion: 1})
	if err != nil {
		t.Fatalf("NetworkState error: %v", err)
	}
	if state.Network.WireGuardAvailable {
		t.Fatal("wireguard available = true, want false")
	}
	if len(state.Network.ManagedInterfaces) != 0 {
		t.Fatalf("managed interfaces = %d, want 0", len(state.Network.ManagedInterfaces))
	}
}

func TestParseWireGuardDumpFiltersByPrefixAndOmitsSecrets(t *testing.T) {
	dump := []byte(
		"anvilwg0\tPRIV\tanvilwg-pub\t51820\t0\n" +
			"anvilwg0\tpeer\tpeer-pub\tPSK\t1.2.3.4:51820\t10.42.0.2/32\t1719427200\t10\t20\t0\n" +
			"wg0\tPRIV\twg0-pub\t51821\t0\n" +
			"wg0\tpeer\twg0-peer\tPSK\t1.2.3.5:51820\t10.0.0.2/32\t0\t0\t0\t0\n",
	)
	interfaces := ParseWireGuardDump("anvilwg", dump)
	if len(interfaces) != 1 {
		t.Fatalf("interfaces = %d, want 1", len(interfaces))
	}
	if interfaces[0].Name != "anvilwg0" || interfaces[0].PublicKey != "anvilwg-pub" {
		t.Fatalf("interface = %+v", interfaces[0])
	}
	if interfaces[0].ListenPort != 51820 {
		t.Fatalf("listen port = %d", interfaces[0].ListenPort)
	}
	if len(interfaces[0].Peers) != 1 || interfaces[0].Peers[0].PublicKey != "peer-pub" {
		t.Fatalf("peers = %+v", interfaces[0].Peers)
	}
	serialized, _ := json.Marshal(interfaces)
	if strings.Contains(strings.ToLower(string(serialized)), "priv") {
		t.Fatalf("dump parse leaked private key: %s", serialized)
	}
	if strings.Contains(strings.ToLower(string(serialized)), "psk") {
		t.Fatalf("dump parse leaked psk: %s", serialized)
	}
}

func TestParseIPAddrJSONFiltersByPrefix(t *testing.T) {
	data := []byte(`[
		{"ifname":"anvilwg0","operstate":"up","addr_info":[{"local":"10.42.0.1","prefixlen":24,"family":"inet"}]},
		{"ifname":"eth0","operstate":"up","addr_info":[{"local":"192.168.1.5","prefixlen":24,"family":"inet"}]}
	]`)
	table, err := ParseIPAddrJSON(data, "anvilwg")
	if err != nil {
		t.Fatalf("ParseIPAddrJSON error: %v", err)
	}
	if _, ok := table["anvilwg0"]; !ok {
		t.Fatal("missing anvilwg0 entry")
	}
	if _, ok := table["eth0"]; ok {
		t.Fatal("eth0 should be filtered out")
	}
	if table["anvilwg0"].Status != "UP" {
		t.Fatalf("status = %q, want UP", table["anvilwg0"].Status)
	}
	if len(table["anvilwg0"].Addresses) != 1 || table["anvilwg0"].Addresses[0] != "10.42.0.1/24" {
		t.Fatalf("addresses = %v", table["anvilwg0"].Addresses)
	}
}

func TestParseIPAddrJSONRejectsMalformed(t *testing.T) {
	if _, err := ParseIPAddrJSON([]byte("not json"), "anvilwg"); err == nil {
		t.Fatal("expected error for malformed ip json")
	}
}
