package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type visibleConnection struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	State    string `json:"state"`
	Count    int    `json:"count"`
}
type connectionSnapshot struct {
	Items        []visibleConnection `json:"items"`
	Availability string              `json:"availability"`
	Message      string              `json:"message"`
	ObservedAt   string              `json:"observedAt"`
	Truncated    bool                `json:"truncated"`
	ReadEntries  int                 `json:"readEntries"`
}

var connectionCache = struct {
	sync.RWMutex
	data connectionSnapshot
}{data: connectionSnapshot{Items: []visibleConnection{}, Availability: "unsupported", Message: "连接采集初始化中"}}

func parseVisibleConnections(raw []byte, known, local map[string]bool) ([]visibleConnection, int, bool) {
	rows := map[string]visibleConnection{}
	scan := bufio.NewScanner(bytes.NewReader(raw))
	scan.Buffer(make([]byte, 4096), 64<<10)
	read := 0
	limited := false
	for scan.Scan() {
		read++
		if read > 5000 {
			limited = true
			break
		}
		f := strings.Fields(scan.Text())
		if len(f) < 4 {
			continue
		}
		pi := 0
		if f[0] == "ipv4" || f[0] == "ipv6" {
			pi = 2
		}
		if len(f) <= pi || f[pi] != "tcp" && f[pi] != "udp" {
			continue
		}
		src, dst, sp, dp := "", "", "", ""
		state := "TRACKED"
		valid := true
		for _, v := range f[pi+1:] {
			if strings.HasPrefix(v, "src=") {
				if src != "" {
					break
				}
				src = strings.TrimPrefix(v, "src=")
				continue
			}
			kv := strings.SplitN(v, "=", 2)
			if len(kv) == 2 {
				switch kv[0] {
				case "dst":
					if dst != "" {
						valid = false
					}
					dst = kv[1]
				case "sport":
					if sp != "" {
						valid = false
					}
					sp = kv[1]
				case "dport":
					if dp != "" {
						valid = false
					}
					dp = kv[1]
				}
				continue
			}
			switch v {
			case "ESTABLISHED", "SYN_SENT", "SYN_RECV", "FIN_WAIT", "TIME_WAIT", "CLOSE", "CLOSE_WAIT", "LAST_ACK", "LISTEN", "[UNREPLIED]", "[ASSURED]":
				state = strings.Trim(v, "[]")
			}
		}
		sport, e1 := strconv.ParseUint(sp, 10, 16)
		port, e2 := strconv.ParseUint(dp, 10, 16)
		_ = sport
		if !valid || e1 != nil || e2 != nil || net.ParseIP(src) == nil || !known[src] || local[src] || local[dst] || !publicDestination(dst) {
			continue
		}
		key := fmt.Sprintf("%s/%s/%d/%s/%s", src, dst, port, f[pi], state)
		r := rows[key]
		r.Source, r.Target, r.Port, r.Protocol, r.State = src, dst, int(port), strings.ToUpper(f[pi]), state
		r.Count++
		rows[key] = r
	}
	if scan.Err() != nil {
		limited = true
	}
	items := make([]visibleConnection, 0, len(rows))
	for _, r := range rows {
		items = append(items, r)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		return fmt.Sprintf("%s/%s/%05d/%s/%s", a.Source, a.Target, a.Port, a.Protocol, a.State) < fmt.Sprintf("%s/%s/%05d/%s/%s", b.Source, b.Target, b.Port, b.Protocol, b.State)
	})
	if len(items) > 1000 {
		items = items[:1000]
		limited = true
	}
	return items, min(read, 5000), limited
}
func connectionInventory() (map[string]bool, map[string]bool) {
	known, local := map[string]bool{}, map[string]bool{}
	if engine != nil {
		engine.mu.RLock()
		if engine.snapshot != nil {
			if ds, ok := engine.snapshot["devices"].([]any); ok {
				for _, v := range ds {
					d := v.(map[string]any)
					ip, _ := d["ip"].(string)
					if net.ParseIP(ip) != nil {
						known[ip] = true
					}
				}
			}
		}
		engine.mu.RUnlock()
	}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		as, _ := i.Addrs()
		for _, a := range as {
			ip, _, e := net.ParseCIDR(a.String())
			if e == nil {
				local[ip.String()] = true
			}
		}
	}
	return known, local
}
func refreshConnections(ctx context.Context) {
	round, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	c := exec.CommandContext(round, "conntrack", "-L", "-o", "extended")
	stderr := &boundedStderr{}
	c.Stderr = stderr
	pipe, err := c.StdoutPipe()
	if err == nil {
		err = c.Start()
	}
	var raw []byte
	truncated := false
	if err == nil {
		raw, err = io.ReadAll(io.LimitReader(pipe, (4<<20)+1))
		if len(raw) > 4<<20 {
			truncated = true
			c.Process.Kill()
			raw = raw[:4<<20]
			if n := bytes.LastIndexByte(raw, '\n'); n >= 0 {
				raw = raw[:n+1]
			} else {
				raw = nil
			}
		}
		waitErr := c.Wait()
		if err == nil && !truncated {
			err = waitErr
		}
	}
	connectionCache.Lock()
	defer connectionCache.Unlock()
	if err != nil {
		connectionCache.data.Availability = "unsupported"
		if connectionCache.data.ObservedAt != "" {
			connectionCache.data.Availability = "stale"
		}
		connectionCache.data.Message = "连接采集失败或超时，请检查 conntrack、权限与采集负载；旧记录不能当作当前连接"
		return
	}
	known, local := connectionInventory()
	items, n, limited := parseVisibleConnections(raw, known, local)
	connectionCache.data = connectionSnapshot{items, "available", "仅为本机可见连接跟踪记录，不代表正在传输、成功访问或完整浏览历史", time.Now().Format(time.RFC3339), truncated || limited, n}
}
func runConnections(ctx context.Context) {
	for ctx.Err() == nil {
		refreshConnections(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
}
func connectionsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		reply(w, 405, map[string]string{"error": "GET required"})
		return
	}
	connectionCache.RLock()
	data := connectionCache.data
	data.Items = append([]visibleConnection{}, data.Items...)
	connectionCache.RUnlock()
	if data.ObservedAt != "" {
		at, _ := time.Parse(time.RFC3339, data.ObservedAt)
		if time.Since(at) > 45*time.Second {
			data.Availability = "stale"
		}
	}
	reply(w, 200, data)
}
