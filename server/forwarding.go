package main

import (
	"net"
	"strings"
	"time"
)

// Only original-direction client -> public destinations count. Local probes,
// DNS requests to this router and reply-direction matches do not establish forwarding.
func forwardedClients(text string, local string) map[string]bool {
	result := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		protocol := f[0]
		if (protocol == "ipv4" || protocol == "ipv6") && len(f) > 2 {
			protocol = f[2]
		}
		if protocol != "tcp" && protocol != "udp" {
			continue
		}
		src, dst := "", ""
		for _, v := range f {
			if strings.HasPrefix(v, "src=") && src == "" {
				src = strings.TrimPrefix(v, "src=")
			}
			if strings.HasPrefix(v, "dst=") && dst == "" {
				dst = strings.TrimPrefix(v, "dst=")
			}
		}
		a, b := net.ParseIP(src), net.ParseIP(dst)
		if a != nil && b != nil && a.IsPrivate() && src != local && b.IsGlobalUnicast() && !b.IsPrivate() && dst != local {
			result[src] = true
		}
	}
	return result
}
func observeForwarding() map[string]bool {
	out, _ := runTimeout(3*time.Second, "conntrack", "-L", "-o", "extended")
	return forwardedClients(string(out), localIP())
}
