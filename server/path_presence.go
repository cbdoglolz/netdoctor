package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Receipt evidence is deliberately weaker than the manual forwarding experiment.
// No payload, destination list, nft rule or inferred default route is stored here.
type pathReceipt struct {
	IP  string    `json:"ip"`
	MAC string    `json:"mac"`
	At  time.Time `json:"at"`
}
type pathPresence struct {
	mu      sync.Mutex
	seen    map[string]pathReceipt
	detail  string
	running bool
}

var presence = &pathPresence{seen: map[string]pathReceipt{}, detail: "后台路径观察初始化中"}

func receiptPacket(a *pathAccumulator, p pathPacket) (pathReceipt, bool) {
	_, known := a.Clients[p.Tuple.Src]
	if !known && net.ParseIP(p.Tuple.Src).To4() == nil {
		_, known = a.KnownMAC[p.SrcMAC]
	}
	_, macErr := net.ParseMAC(p.SrcMAC)
	if !known || macErr != nil || !a.LocalMAC[p.DstMAC] || a.LocalMAC[p.SrcMAC] || a.LocalIP[p.Tuple.Src] || !publicDestination(p.Tuple.Dst) {
		return pathReceipt{}, false
	}
	return pathReceipt{p.Tuple.Src, p.SrcMAC, p.At}, true
}
func (m *pathPresence) remember(r pathReceipt) {
	if r.IP == "" || r.MAC == "" || r.At.IsZero() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen == nil {
		m.seen = map[string]pathReceipt{}
	}
	key := r.IP + "/" + strings.ToLower(r.MAC)
	for k, v := range m.seen {
		if time.Since(v.At) > 24*time.Hour {
			delete(m.seen, k)
		}
	}
	if old, ok := m.seen[key]; ok && !r.At.After(old.At) {
		return
	}
	if len(m.seen) >= 512 { // Evict the oldest evidence, never grow with network size.
		oldest := ""
		var at time.Time
		for k, v := range m.seen {
			if oldest == "" || v.At.Before(at) {
				oldest, at = k, v.At
			}
		}
		delete(m.seen, oldest)
	}
	m.seen[key] = r
}
func (m *pathPresence) setState(running bool, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running, m.detail = running, detail
}
func (m *pathPresence) restore() {
	var b string
	if engine.db.QueryRow("SELECT payload FROM path_presence WHERE id=1").Scan(&b) == nil {
		var receipts []pathReceipt
		if json.Unmarshal([]byte(b), &receipts) == nil {
			for _, r := range receipts {
				if time.Since(r.At) < 24*time.Hour {
					m.remember(r)
				}
			}
		}
	}
	// Preserve already collected manual experiment evidence, with its original timestamp.
	rows, err := engine.db.Query("SELECT payload FROM path_reports ORDER BY created DESC LIMIT 20")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		rows.Scan(&b)
		var r pathReport
		if json.Unmarshal([]byte(b), &r) != nil {
			continue
		}
		all := append([]pathEvidence{}, r.Devices...)
		for _, p := range r.Phases {
			all = append(all, p.Devices...)
		}
		for _, d := range all {
			if d.Received > 0 {
				at, err := time.Parse(time.RFC3339, d.LastSeen)
				if err == nil && time.Since(at) < 24*time.Hour {
					m.remember(pathReceipt{d.IP, d.MAC, at})
				}
			}
		}
	}
}
func (m *pathPresence) save() {
	if !diskHasReserve(engine.dir) {
		m.setState(false, "存储空间不足，路径记录只在内存更新")
		return
	}
	m.mu.Lock()
	all := make([]pathReceipt, 0, len(m.seen))
	for _, v := range m.seen {
		all = append(all, v)
	}
	m.mu.Unlock()
	b, _ := json.Marshal(all)
	if _, err := engine.db.Exec("INSERT OR REPLACE INTO path_presence VALUES(1,?)", string(b)); err != nil {
		m.setState(false, "路径记录保存失败，历史可能丢失")
	}
}
func (m *pathPresence) capture(ctx context.Context) {
	a, err := pathInventory()
	if err != nil {
		m.setState(false, err.Error())
		return
	}
	if _, err := net.InterfaceByName("eth0"); err != nil {
		m.setState(false, "找不到 eth0，后台路径观察暂不支持此环境")
		return
	}
	round, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	c := exec.CommandContext(round, "tcpdump", "-i", "eth0", "-p", "-U", "-n", "-s", "128", "-c", "10000", "-w", "-", "ip or ip6")
	stderr := &boundedStderr{}
	c.Stderr = stderr
	pipe, err := c.StdoutPipe()
	if err == nil {
		err = c.Start()
	}
	if err != nil {
		m.setState(false, "后台路径观察无法启动，请检查 tcpdump 与权限")
		return
	}
	m.setState(true, "后台被动观察中 · 55 秒 / 10,000 包上限，未观察到不等于未经过")
	err = readPathPCAP(pipe, func(p pathPacket) {
		if r, ok := receiptPacket(a, p); ok {
			m.remember(r)
		}
	}, func() { m.setState(true, "后台有界采样，达到包数或包头上限；覆盖不完整") })
	if err != nil {
		c.Process.Kill()
	}
	waitErr := c.Wait()
	if ctx.Err() != nil {
		return
	}
	if waitErr != nil && round.Err() == nil {
		m.setState(false, "后台抓包提前退出，覆盖不完整")
		return
	}
	if dropped, ok := pathDropStats(stderr.String()); !ok || dropped > 0 {
		m.setState(true, "后台有界采样 · 存在丢包或丢包统计未知，覆盖不完整")
	}
}
func (m *pathPresence) run(ctx context.Context) {
	m.restore()
	for ctx.Err() == nil {
		start := time.Now()
		// Use existing experiment evidence while it runs; avoid a second capture then.
		paths.mu.Lock()
		manual := paths.report != nil && paths.report.State == "running"
		if manual {
			for _, d := range paths.acc.Clients {
				if d.Received > 0 {
					at, _ := time.Parse(time.RFC3339, d.LastSeen)
					m.remember(pathReceipt{d.IP, d.MAC, at})
				}
			}
		}
		paths.mu.Unlock()
		wait := 60 * time.Second
		if manual {
			m.setState(true, "复用正在运行的路径验证任务")
			wait = 5 * time.Second
		} else {
			m.capture(ctx)
			m.save()
			m.mu.Lock()
			if !m.running {
				wait = 10 * time.Second
			}
			m.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(max(time.Millisecond, wait-time.Since(start))):
		}
	}
}

// Called with the runtime inventory and IDs; never match a reused IP with a different MAC.
func (m *pathPresence) decorate(byIP map[string]map[string]any, byID map[string]string, links []any, now time.Time, routerIP string) ([]any, map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ip, d := range byIP {
		if ip == routerIP || net.ParseIP(ip) == nil {
			continue
		}
		mac, _ := d["mac"].(string)
		p := map[string]any{"state": "unknown", "label": "尚未观察到经旁路由", "source": "后台包头观察", "detail": "未观察到不能判断未经过；空闲、采样上限、加速及其他协议会影响覆盖"}
		var latest pathReceipt
		for _, r := range m.seen {
			// IPv6 may be linked to the same known L2 device; expose the actual observed IP.
			if strings.EqualFold(r.MAC, mac) && (r.IP == ip || (net.ParseIP(r.IP) != nil && net.ParseIP(r.IP).To4() == nil)) && r.At.After(latest.At) && !r.At.After(now.Add(time.Second)) {
				latest = r
			}
		}
		if !latest.At.IsZero() && now.Sub(latest.At) < 24*time.Hour {
			state, label := "historical", "曾观察到经旁路由"
			if now.Sub(latest.At) < 2*time.Minute {
				state, label = "recent", "流量经旁路由"
			}
			p["state"], p["label"], p["observedAt"], p["observedIP"] = state, label, latest.At.Format(time.RFC3339), latest.IP
			p["detail"] = "公网目的 IP 的请求包以本机为下一跳 MAC；证明请求交付，不等于成功转发或全部流量受控"
			if state == "recent" && byID[ip] != "" && byID[routerIP] != "" {
				// Replace a duplicate manual edge to this same router, retaining other manual routes.
				out := links[:0]
				for _, l := range links {
					v, ok := l.(map[string]any)
					if ok && v["from"] == byID[ip] && v["to"] == byID[routerIP] && v["relation"] == "gateway" {
						continue
					}
					out = append(out, l)
				}
				links = out
				links = append(links, map[string]any{"id": fmt.Sprintf("observed-%s-doctor", byID[ip]), "from": byID[ip], "to": byID[routerIP], "relation": "gateway", "confidence": "confirmed", "status": "online", "availability": "available", "observedAt": latest.At.Format(time.RFC3339), "source": p["detail"], "pathObserved": true, "measured": false})
			}
		}
		d["bypassPath"] = p
	}
	return links, map[string]any{"running": m.running, "detail": m.detail, "source": "eth0 包头 · 下一跳 MAC + 公网目的 IP；不修改转发规则"}
}
