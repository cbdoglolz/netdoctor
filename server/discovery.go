package main

import (
	"strings"
	"sync"
	"time"
)

var esxiState = struct {
	sync.Mutex
	at     time.Time
	busy   bool
	status map[string]any
}{}

func esxiStatus(c integrationConfig) map[string]any {
	esxiState.Lock()
	defer esxiState.Unlock()
	if !c.Enabled || c.Username == "" || c.Password == "" {
		return map[string]any{"name": "esxi", "available": false, "reason": "未配置 ESXi 只读凭据"}
	}
	if !esxiState.busy && time.Since(esxiState.at) > 60*time.Second {
		esxiState.busy = true
		go func() {
			data, err := readESXi(c)
			s := map[string]any{"name": "esxi", "available": err == nil, "observedAt": time.Now().Format(time.RFC3339), "networkCards": data}
			if err != nil {
				s["reason"] = err.Error()
			}
			esxiState.Lock()
			esxiState.at = time.Now()
			esxiState.status = s
			esxiState.busy = false
			esxiState.Unlock()
		}()
	}
	if esxiState.status == nil {
		return map[string]any{"name": "esxi", "available": false, "reason": "读取中"}
	}
	return esxiState.status
}

type discoveredName struct{ Name, Kind, Source string }

func parseMDNS(text string) map[string]discoveredName {
	result := map[string]discoveredName{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Split(line, ";")
		if len(f) < 9 || f[0] != "=" || f[2] != "IPv4" {
			continue
		}
		kind := "iot"
		switch {
		case strings.Contains(f[4], "_ipp"), strings.Contains(f[4], "_printer"):
			kind = "printer"
		case strings.Contains(f[4], "_airplay"), strings.Contains(f[4], "_googlecast"):
			kind = "tv"
		case strings.Contains(f[4], "_smb"), strings.Contains(f[4], "_nfs"):
			kind = "server"
		case strings.Contains(f[4], "_workstation"):
			kind = "computer"
		}
		result[f[7]] = discoveredName{f[3], kind, "mDNS 缓存 · 服务推断，型号未核实"}
	}
	return result
}
