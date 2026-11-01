package main

import (
	"net"
	"strings"
	"time"
)

func parseARP(text string) map[string]string {
	found := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || net.ParseIP(f[0]) == nil {
			continue
		}
		if _, err := net.ParseMAC(f[1]); err == nil {
			found[f[0]] = f[1]
		}
	}
	return found
}
func discoverLAN(config []deviceConfig) map[string]string {
	iface, err := net.InterfaceByName("br-lan")
	if err != nil {
		return nil
	}
	addrs, _ := iface.Addrs()
	for _, addr := range addrs {
		ip, n, err := net.ParseCIDR(addr.String())
		if err != nil || ip.To4() == nil {
			continue
		}
		bits, _ := n.Mask.Size()
		if bits < 24 {
			return nil
		}
		passive := map[string]bool{}
		for _, d := range config {
			if d.Passive || d.MonitorDisabled {
				passive[d.IP] = true
			}
		}
		args := []string{"arp-scan", "--interface=br-lan", "--retry=1", "--timeout=500"}
		for candidate := append(net.IP(nil), n.IP.To4()...); n.Contains(candidate); incrementIP(candidate) {
			if !passive[candidate.String()] && !candidate.Equal(ip) {
				args = append(args, candidate.String())
			}
		}
		out, _ := runTimeout(8*time.Second, args...)
		return parseARP(string(out))
	}
	return nil
}
func incrementIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}
