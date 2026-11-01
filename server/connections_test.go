package main

import (
	"context"
	"strings"
	"testing"
)

func TestVisibleConnectionsOriginalDirectionAndAggregation(t *testing.T) {
	raw := `ipv4 2 tcp 6 80 ESTABLISHED src=192.168.1.10 dst=1.1.1.1 sport=123 dport=443 src=1.1.1.1 dst=192.168.1.6 sport=443 dport=456
ipv4 2 tcp 6 80 ESTABLISHED src=192.168.1.10 dst=1.1.1.1 sport=124 dport=443 src=1.1.1.1 dst=192.168.1.6 sport=443 dport=457
udp 17 10 src=192.168.1.10 dst=8.8.8.8 sport=123 dport=53 [UNREPLIED] src=8.8.8.8 dst=192.168.1.10 sport=53 dport=123
ipv6 2 tcp 6 80 SYN_SENT src=2001:db8::10 dst=2606:4700:4700::1111 sport=123 dport=443 src=2606:4700:4700::1111 dst=2001:db8::10 sport=443 dport=123`
	rows, n, limited := parseVisibleConnections([]byte(raw), map[string]bool{"192.168.1.10": true, "2001:db8::10": true}, map[string]bool{"192.168.1.6": true})
	if n != 4 || limited || len(rows) != 3 {
		t.Fatal(rows, n, limited)
	}
	total := 0
	for _, r := range rows {
		total += r.Count
		if r.Target == "1.1.1.1" && r.Count != 2 {
			t.Fatal(r)
		}
	}
	if total != 4 {
		t.Fatal(total)
	}
}
func TestVisibleConnectionsExcludeLocalUnknownAndInvalid(t *testing.T) {
	raw := `tcp 6 10 ESTABLISHED src=192.168.1.6 dst=1.1.1.1 sport=2 dport=443 src=1.1.1.1 dst=192.168.1.6 sport=443 dport=2
udp 17 10 src=192.168.1.10 dst=192.168.1.6 sport=3 dport=8088 src=192.168.1.6 dst=192.168.1.10 sport=8088 dport=3
tcp 6 10 src=192.168.1.99 dst=1.1.1.1 sport=3 dport=443
tcp 6 10 src=192.168.1.10 dst=1.1.1.1 sport=NaN dport=443
tcp 6 10 src=192.168.1.10 dst=1.1.1.1 sport=123 dport=65536`
	r, _, _ := parseVisibleConnections([]byte(raw), map[string]bool{"192.168.1.10": true, "192.168.1.6": true}, map[string]bool{"192.168.1.6": true})
	if len(r) != 0 {
		t.Fatal(r)
	}
}
func TestVisibleConnectionsBoundedInput(t *testing.T) {
	_, n, limited := parseVisibleConnections([]byte(strings.Repeat("invalid\n", 6000)), nil, nil)
	if n != 5000 || !limited {
		t.Fatal(n, limited)
	}
}
func TestConnectionFailureRetainsStaleEvidence(t *testing.T) {
	connectionCache.Lock()
	old := connectionCache.data
	connectionCache.data = connectionSnapshot{Items: []visibleConnection{{Source: "192.0.2.4", Target: "1.1.1.1", Count: 1}}, Availability: "available", ObservedAt: "2026-01-01T00:00:00Z"}
	connectionCache.Unlock()
	defer func() { connectionCache.Lock(); connectionCache.data = old; connectionCache.Unlock() }()
	t.Setenv("PATH", t.TempDir())
	refreshConnections(context.Background())
	connectionCache.RLock()
	defer connectionCache.RUnlock()
	if connectionCache.data.Availability != "stale" || len(connectionCache.data.Items) != 1 || connectionCache.data.ObservedAt != "2026-01-01T00:00:00Z" {
		t.Fatal(connectionCache.data)
	}
}
