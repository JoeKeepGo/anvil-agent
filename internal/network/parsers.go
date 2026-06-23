package network

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseWireGuardDump parses the documented `wg show all dump` output and
// returns only Anvil-managed interfaces (names starting with prefix).
//
// The `wg show all dump` format is tab-separated, one line per interface and
// one line per peer (documented in wg(8)). Interface lines carry the private
// key and public key; peer lines carry the preshared key. Private keys and
// preshared keys are read only to advance parsing and are NEVER returned.
//
// Format (tab-delimited fields):
//
//	interface-line: <name> <private-key> <public-key> <listen-port> <fwmark>
//	peer-line:      <name> peer <public-key> <preshared-key> <endpoint> <allowed-ips> <latest-handshake> <rx> <tx> <keepalive>
//
// Real WireGuard output verification happens on a host that has WireGuard
// (Phase 5 Docker lab / VM). The parser is tested against the documented
// format fixtures here.
func ParseWireGuardDump(prefix string, data []byte) []ManagedInterface {
	interfaces := map[string]*ManagedInterface{}
	var order []string

	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		if len(fields) >= 5 && fields[1] != "peer" {
			// Interface line: name private-key public-key listen-port fwmark.
			iface := &ManagedInterface{
				Name:       name,
				PublicKey:  fields[2],
				ListenPort: parseIntOrZero(fields[3]),
				Addresses:  []string{},
				Peers:      []Peer{},
			}
			if _, exists := interfaces[name]; !exists {
				order = append(order, name)
			}
			interfaces[name] = iface
			continue
		}

		if len(fields) >= 10 && fields[1] == "peer" {
			iface, ok := interfaces[name]
			if !ok {
				// Peer for an unreported/managed interface observed before its
				// interface line: create a placeholder so the peer is not lost.
				iface = &ManagedInterface{Name: name, Addresses: []string{}, Peers: []Peer{}}
				interfaces[name] = iface
				order = append(order, name)
			}
			iface.Peers = append(iface.Peers, parsePeer(fields))
		}
	}

	result := make([]ManagedInterface, 0, len(order))
	for _, name := range order {
		iface := interfaces[name]
		if iface.Peers == nil {
			iface.Peers = []Peer{}
		}
		if iface.Addresses == nil {
			iface.Addresses = []string{}
		}
		result = append(result, *iface)
	}
	return result
}

func parsePeer(fields []string) Peer {
	peer := Peer{
		PublicKey: fields[2],
	}
	if handshake := parseInt64OrZero(fields[6]); handshake > 0 {
		t := time.Unix(handshake, 0).UTC()
		peer.LatestHandshakeAt = &t
	}
	peer.RxBytes = parseInt64OrZero(fields[7])
	peer.TxBytes = parseInt64OrZero(fields[8])
	return peer
}

// ParseIPAddrJSON parses the documented `ip -json addr` output and returns a
// map of interface name to addresses/status for Anvil-managed interfaces.
//
// The `ip -json addr` schema is stable JSON from iproute2. Each entry has an
// `ifname`, an `operstate`, and an `addr_info` array of `{local, prefixlen,
// family}` objects. Real output verification happens on Phase 5 hosts.
func ParseIPAddrJSON(data []byte, prefix string) (map[string]addressEntry, error) {
	var entries []struct {
		IfName    string `json:"ifname"`
		OperState string `json:"operstate"`
		AddrInfo  []struct {
			Local     string `json:"local"`
			PrefixLen int    `json:"prefixlen"`
			Family    string `json:"family"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse ip addr json: %w", err)
	}

	table := map[string]addressEntry{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.IfName, prefix) {
			continue
		}
		addresses := make([]string, 0, len(entry.AddrInfo))
		for _, addr := range entry.AddrInfo {
			addresses = append(addresses, fmt.Sprintf("%s/%d", addr.Local, addr.PrefixLen))
		}
		status := strings.ToUpper(entry.OperState)
		if status == "" {
			status = "UNKNOWN"
		}
		table[entry.IfName] = addressEntry{Status: status, Addresses: addresses}
	}
	return table, nil
}

func parseIntOrZero(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return n
}

func parseInt64OrZero(value string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
