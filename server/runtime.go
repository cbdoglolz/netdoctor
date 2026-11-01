package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type deviceConfig struct {
	Icon            string `json:"icon,omitempty"`
	CustomIcon      string `json:"customIcon,omitempty"`
	MonitorDisabled bool   `json:"monitorDisabled"`
	IP              string `json:"ip"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Model           string `json:"model"`
	Gateway         string `json:"gateway"`
	Passive         bool   `json:"passive"`
	Key             bool   `json:"key"`
}
type manualLink struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
	Source   string `json:"source"`
}
type integrationConfig struct {
	URL         string `json:"url"`
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Enabled     bool   `json:"enabled"`
}
type runtimeConfig struct {
	Interval     int                          `json:"interval"`
	Cache        int                          `json:"cache"`
	Storage      int                          `json:"storage"`
	Retention    int                          `json:"retention"`
	Devices      []deviceConfig               `json:"devices"`
	Links        []manualLink                 `json:"links"`
	Integrations map[string]integrationConfig `json:"integrations"`
}
type probeResult struct {
	Latency  *float64
	Loss     float64
	At       time.Time
	Success  bool
	Failures int
}
type runtimeEngine struct {
	sampleMu     sync.Mutex
	mu           sync.RWMutex
	config       runtimeConfig
	db           *sql.DB
	snapshot     map[string]any
	probes       map[string]probeResult
	lastFlow     map[string]flowCounter
	lastFlowAt   time.Time
	writeError   string
	identity     map[string]string
	discovered   map[string]string
	discoveredAt time.Time
	names        map[string]discoveredName
	nameAt       time.Time
	dir          string
	probeMu      sync.Mutex
	pending      map[string]bool
	slots        chan struct{}
}
type flowCounter struct {
	RX float64
	TX float64
}

func hasCounter(counters map[string]flowCounter, ip string) bool { _, ok := counters[ip]; return ok }

var engine *runtimeEngine

func defaultConfig() runtimeConfig {
	return runtimeConfig{Interval: 10, Cache: 64, Storage: 512, Retention: 30, Devices: []deviceConfig{}, Links: []manualLink{}, Integrations: map[string]integrationConfig{}}
}
func validateConfig(c runtimeConfig) error {
	if c.Interval < 5 || c.Interval > 300 || c.Cache < 16 || c.Cache > 512 || c.Storage < 128 || c.Storage > 8192 || c.Retention < 1 || c.Retention > 365 || len(c.Devices) > 512 || len(c.Links) > 1024 {
		return errors.New("监测参数超出允许范围")
	}
	kinds := map[string]bool{"gateway": true, "switch": true, "ap": true, "server": true, "computer": true, "phone": true, "iot": true, "tablet": true, "tv": true, "camera": true, "printer": true, "speaker": true, "nas": true, "hypervisor": true, "vm": true, "console": true, "settop": true, "monitor": true, "fan": true, "amplifier": true, "hub": true, "light": true}
	seen := map[string]bool{}
	parents := map[string]string{}
	for _, d := range c.Devices {
		if seen[d.IP] {
			return errors.New("设备 IP / 节点不能重复")
		}
		seen[d.IP] = true
		if d.Gateway != "" {
			parents[d.IP] = d.Gateway
		}
		if net.ParseIP(d.IP) == nil && !strings.HasPrefix(d.IP, "node-") {
			return errors.New("设备 IP 无效")
		}
		if err := validateIcon(d.CustomIcon); err != nil {
			return err
		}
		if d.Icon != "" && !kinds[d.Icon] {
			return errors.New("图标类型无效")
		}
		if !kinds[d.Kind] || len(d.Name) > 100 || len(d.Model) > 100 || len(d.Gateway) > 100 {
			return errors.New("设备信息无效")
		}
	}
	for ip := range parents {
		visited := map[string]bool{}
		for next := ip; next != ""; next = parents[next] {
			if visited[next] {
				return errors.New("逻辑网关不能形成循环")
			}
			visited[next] = true
		}
	}
	for _, l := range c.Links {
		if l.Relation != "physical" && l.Relation != "virtual" && l.Relation != "gateway" && l.Relation != "wireless" {
			return errors.New("连接类型无效")
		}
	}
	return nil
}
func openEngine(dir string) (*runtimeEngine, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	c := defaultConfig()
	if b, e := os.ReadFile(filepath.Join(dir, "config.json")); e == nil {
		if e = json.Unmarshal(b, &c); e != nil {
			return nil, e
		}
		if e = validateConfig(c); e != nil {
			return nil, e
		}
	}
	db, e := sql.Open("sqlite", filepath.Join(dir, "history.db"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=3000; CREATE TABLE IF NOT EXISTS device_meta(ip TEXT PRIMARY KEY,first_seen TEXT,last_scan TEXT,marked_at TEXT,hidden INTEGER DEFAULT 0); CREATE TABLE IF NOT EXISTS ui_layout(id TEXT PRIMARY KEY,payload TEXT); CREATE TABLE IF NOT EXISTS path_presence(id INTEGER PRIMARY KEY,payload TEXT); CREATE TABLE IF NOT EXISTS path_reports(id TEXT PRIMARY KEY,created INTEGER,payload TEXT); CREATE TABLE IF NOT EXISTS samples(ts INTEGER PRIMARY KEY,payload TEXT NOT NULL); CREATE TABLE IF NOT EXISTS usage(ts INTEGER,identity TEXT,rx REAL,tx REAL,PRIMARY KEY(ts,identity)); CREATE TABLE IF NOT EXISTS identities(mac TEXT PRIMARY KEY,ip TEXT,name TEXT,kind TEXT,model TEXT); CREATE INDEX IF NOT EXISTS usage_time ON usage(ts); CREATE TABLE IF NOT EXISTS accounting(ip TEXT PRIMARY KEY,rx REAL,tx REAL,ts INTEGER); CREATE TABLE IF NOT EXISTS events(ts INTEGER,identity TEXT,status TEXT); CREATE INDEX IF NOT EXISTS event_identity ON events(identity,ts);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	r := &runtimeEngine{dir: dir, pending: map[string]bool{}, slots: make(chan struct{}, 4), config: c, db: db, probes: map[string]probeResult{}, lastFlow: map[string]flowCounter{}, identity: map[string]string{}, names: map[string]discoveredName{}}
	rows, err := db.Query("SELECT ip,rx,tx,ts FROM accounting")
	if err == nil {
		for rows.Next() {
			var ip string
			var c flowCounter
			var stamp int64
			rows.Scan(&ip, &c.RX, &c.TX, &stamp)
			r.lastFlow[ip] = c
			if time.Unix(stamp, 0).After(r.lastFlowAt) {
				r.lastFlowAt = time.Unix(stamp, 0)
			}
		}
		rows.Close()
	}
	return r, nil
}
func (e *runtimeEngine) run(ctx context.Context) {
	for {
		started := time.Now()
		e.sample()
		e.mu.RLock()
		seconds := e.config.Interval
		e.mu.RUnlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(max(time.Millisecond, time.Duration(seconds)*time.Second-time.Since(started))):
		}
	}
}
func collect() map[string]any {
	if engine == nil {
		return collectRaw()
	}
	engine.mu.RLock()
	defer engine.mu.RUnlock()
	if engine.snapshot == nil {
		return map[string]any{"error": "后台采集正在初始化"}
	}
	b, _ := json.Marshal(engine.snapshot)
	var value map[string]any
	json.Unmarshal(b, &value)
	return value
}
func runTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, args[0], args[1:]...).Output()
}

var timePattern = regexp.MustCompile(`time[=<]([0-9.]+)`)

func probe(ip string) probeResult {
	r := probeResult{At: time.Now(), Loss: 100}
	out, _ := runTimeout(4*time.Second, "ping", "-c", "2", "-W", "1", ip)
	matches := timePattern.FindAllStringSubmatch(string(out), -1)
	if len(matches) > 0 {
		sum := 0.0
		for _, m := range matches {
			v, _ := strconv.ParseFloat(m[1], 64)
			sum += v
		}
		v := float64(int(sum/float64(len(matches))*10)) / 10
		r.Latency = &v
		r.Success = true
		r.Loss = 100 - float64(len(matches))*50
	}
	return r
}
func (e *runtimeEngine) sample() {
	e.sampleMu.Lock()
	defer e.sampleMu.Unlock()
	force := scanQueue.Swap(false)
	if force {
		e.discoveredAt = time.Time{}
		e.nameAt = time.Time{}
		scanStatus.Store("扫描中")
	}
	defer func() {
		if force {
			scanBusy.Store(false)
			scanStatus.Store("扫描完成，返回 " + strconv.Itoa(len(e.discovered)) + " 台 ARP 响应；手动备注保留")
		}
	}()
	raw := collectRaw()
	e.mu.RLock()
	c := e.config
	e.mu.RUnlock()
	e.db.Exec("PRAGMA cache_size=" + strconv.Itoa(-c.Cache*1024))
	e.db.Exec("PRAGMA journal_size_limit=16777216")
	// Reserve headroom for WAL, shared memory and the accounting daemon's files.
	e.db.Exec("PRAGMA max_page_count=" + strconv.Itoa(c.Storage*1024*1024/4096*3/4))
	now := time.Now()
	stamp := now.Format(time.RFC3339)
	devices := raw["devices"].([]any)
	byIP := map[string]map[string]any{}
	for _, v := range devices {
		d := v.(map[string]any)
		byIP[d["ip"].(string)] = d
	}
	if time.Since(e.discoveredAt) > 5*time.Minute {
		e.discovered = discoverLAN(c.Devices)
		e.discoveredAt = now
	}
	for ip, mac := range e.discovered {
		if d := byIP[ip]; d != nil {
			d["mac"] = mac
			delete(d, "macSource")
			if now.Sub(e.discoveredAt) < 70*time.Second {
				d["status"] = "online"
			}
			continue
		}
		d := map[string]any{"id": ip, "ip": ip, "mac": mac, "kind": "iot", "name": "设备 " + ip, "model": "类型待识别", "connection": "连接位置待确认", "status": "unknown", "observedAt": e.discoveredAt.Format(time.RFC3339), "source": "ARP 发现", "history": []string{}, "latency": observation(nil, "unsupported", "尚无 ICMP 样本", stamp), "traffic": observation(nil, "invisible", "尚无可见流量", stamp), "cpu": observation(nil, "unsupported", "无管理接口", stamp), "memory": observation(nil, "unsupported", "无管理接口", stamp)}
		byIP[ip] = d
		devices = append(devices, d)
	}

	for _, cfg := range c.Devices {
		if byIP[cfg.IP] == nil {
			d := map[string]any{"id": cfg.IP, "ip": cfg.IP, "mac": "", "kind": cfg.Kind, "name": cfg.Name, "model": cfg.Model, "status": "unknown", "connection": "连接位置待确认", "observedAt": stamp, "source": "用户确认", "history": []string{}, "latency": observation(nil, "unsupported", "尚无探测", stamp), "traffic": observation(nil, "invisible", "尚无流量统计", stamp), "cpu": observation(nil, "unsupported", "无管理接口", stamp), "memory": observation(nil, "unsupported", "无管理接口", stamp)}
			byIP[cfg.IP] = d
			devices = append(devices, d)
		}
	}
	devices = e.inventoryMeta(devices, byIP, stamp)
	settings := map[string]deviceConfig{}
	for _, d := range c.Devices {
		settings[d.IP] = d
	}
	var hints map[string]struct {
		Name string   `json:"name"`
		IP   []string `json:"ipaddrs"`
	}
	json.Unmarshal(command("ubus", "call", "luci-rpc", "getHostHints"), &hints)
	if time.Since(e.nameAt) > 5*time.Minute {
		out, _ := runTimeout(3*time.Second, "avahi-browse", "-a", "-r", "-t", "-p", "-c")
		e.names = parseMDNS(string(out))
		e.nameAt = now
	}
	e.probeMu.Lock()
	probes := make(map[string]probeResult, len(e.probes))
	for ip, p := range e.probes {
		probes[ip] = p
	}
	ips := []string{}
	for ip := range byIP {
		ips = append(ips, ip)
	}
	sort.Slice(ips, func(i, j int) bool {
		a, b := settings[ips[i]].Key, settings[ips[j]].Key
		if a != b {
			return a
		}
		return probes[ips[i]].At.Before(probes[ips[j]].At)
	})
	scheduled := 0
	for _, ip := range ips {
		d := byIP[ip]
		cfg := settings[ip]
		if ip == localIP() || net.ParseIP(ip) == nil || cfg.Passive || cfg.MonitorDisabled {
			continue
		}
		last := probes[ip]
		interval := 60 * time.Second
		if cfg.Key {
			interval = time.Duration(c.Interval) * time.Second
		}
		if e.pending[ip] || now.Sub(last.At) < interval {
			continue
		}
		_ = d
		if scheduled >= 32 {
			break
		}
		scheduled++
		e.pending[ip] = true
		go func(ip string) {
			e.slots <- struct{}{}
			p := probe(ip)
			<-e.slots
			e.probeMu.Lock()
			if !p.Success {
				p.Failures = e.probes[ip].Failures + 1
			}
			e.probes[ip] = p
			delete(e.pending, ip)
			e.probeMu.Unlock()
		}(ip)
	}
	e.probeMu.Unlock()
	restoreIdentities(e.db, byIP)
	counters, flowErr := readAccounting()
	elapsed := now.Sub(e.lastFlowAt).Seconds()
	first := e.lastFlowAt.IsZero()
	rateByIP := map[string]flowCounter{}
	writable := e.storageReady(c)
	if flowErr == nil && writable {
		tx, txErr := e.db.Begin()
		if txErr != nil {
			flowErr = txErr
		} else {
			for ip, counter := range counters {
				before, exists := e.lastFlow[ip]
				if !first && exists && elapsed > 0 && counter.RX >= before.RX && counter.TX >= before.TX {
					delta := flowCounter{counter.RX - before.RX, counter.TX - before.TX}
					rateByIP[ip] = flowCounter{delta.RX * 8 / elapsed / 1e6, delta.TX * 8 / elapsed / 1e6}
					if _, err := tx.Exec("INSERT INTO usage(ts,identity,rx,tx) VALUES(?,?,?,?) ON CONFLICT(ts,identity) DO UPDATE SET rx=rx+excluded.rx,tx=tx+excluded.tx", now.Unix()/60*60, trafficIdentity(ip, byIP), delta.RX, delta.TX); err != nil {
						e.writeError = err.Error()
						txErr = err
					}
				}
			}
			for ip, counter := range counters {
				if _, err := tx.Exec("INSERT OR REPLACE INTO accounting VALUES(?,?,?,?)", ip, counter.RX, counter.TX, now.Unix()); err != nil {
					txErr = err
				}
			}
			if txErr != nil {
				tx.Rollback()
				flowErr = txErr
			} else if err := tx.Commit(); err != nil {
				flowErr = err
			}
			if flowErr == nil {
				e.lastFlow = counters
				e.lastFlowAt = now
			}
		}
	}
	forwarding := observeForwarding()
	macCounts := map[string]int{}
	for _, d := range byIP {
		if mac, _ := d["mac"].(string); mac != "" {
			macCounts[strings.ToLower(mac)]++
		}
	}
	byID := map[string]string{}
	for ip, d := range byIP {
		cfg := settings[ip]
		mac, _ := d["mac"].(string)

		id := ip
		if mac != "" {
			id = addressIdentity(ip, mac, macCounts[strings.ToLower(mac)])
		}
		if ip == localIP() {
			id = "doctor"
		}
		d["id"] = id
		byID[ip] = id
		if cfg.Name != "" {
			d["name"] = cfg.Name
			d["kind"] = cfg.Kind
			d["model"] = cfg.Model
			d["source"] = "用户确认"
		} else {
			if h, ok := e.names[ip]; ok {
				d["name"] = h.Name
				d["kind"] = h.Kind
				d["source"] = h.Source
				d["model"] = "服务类型识别，型号未核实"
			}
			var savedName, savedKind, savedModel string
			if mac != "" && e.db.QueryRow("SELECT name,kind,model FROM identities WHERE mac=?", mac).Scan(&savedName, &savedKind, &savedModel) == nil {
				d["name"] = savedName
				if macCounts[strings.ToLower(mac)] > 1 {
					d["name"] = savedName + " · " + ip
				}
				d["kind"] = savedKind
				d["model"] = savedModel
				d["source"] = "按 MAC 恢复的用户标注"
			}
			for m, h := range hints {
				if strings.EqualFold(m, mac) && h.Name != "" {
					d["name"] = h.Name
					d["source"] = "luci-rpc HostHints"
				}
			}
		}
		if ip != localIP() {
			p, ok := probes[ip]
			if ok && now.Sub(p.At) < 2*time.Minute {
				if p.Success {
					d["status"] = "online"
					d["latency"] = observation(p.Latency, "available", "ICMP 探测（非物理链路状态）", p.At.Format(time.RFC3339))
				} else {
					if d["status"] != "online" {
						d["status"] = "unresponsive"
					}
					d["latency"] = observation(nil, "unsupported", "ICMP 无响应，不能确认关机", p.At.Format(time.RFC3339))
				}
				d["loss"] = p.Loss
				d["lastProbe"] = p.At.Format(time.RFC3339)
			}
		}
		var rx, tx sql.NullFloat64
		e.db.QueryRow("SELECT SUM(rx),SUM(tx) FROM usage WHERE identity=? AND ts>=?", trafficIdentity(ip, byIP), now.Add(-24*time.Hour).Unix()).Scan(&rx, &tx)
		if rx.Valid || tx.Valid {
			d["traffic"] = observation(float64(int((rx.Float64+tx.Float64)/1e3))/1e6, "available", "nlbwmon · 可见路径 · 自采集开始累计 GB", stamp)
		}
		rate, hasRate := rateByIP[ip]
		if hasRate {
			d["downloadMbps"] = rate.RX
			d["uploadMbps"] = rate.TX
		}
		d["identitySource"] = d["source"]
		d["icon"] = cfg.Icon
		d["customIcon"] = cfg.CustomIcon
		d["monitorDisabled"] = cfg.MonitorDisabled
		if cfg.MonitorDisabled {
			d["latency"] = observation(nil, "unsupported", "主动监测已关闭", stamp)
			delete(d, "loss")
		}
		if cfg.Passive {
			d["history"] = []string{"已设为被动监测，不执行定时 ICMP"}
		}
		if mac != "" {
			firstByte, _ := strconv.ParseUint(strings.Split(mac, ":")[0], 16, 8)
			d["randomizedMac"] = firstByte&2 != 0
		}
		d["connectionEvidence"] = "只有邻居 / 探测证据，无法确定物理接线或客户端网关"
		if forwarding[ip] {
			d["connection"] = "连接跟踪表中观察到相关连接"
			d["connectionEvidence"] = "conntrack 原始方向：该设备到公网的 TCP/UDP 连接；不代表所有流量都经过本机"
		}
		if cfg.Gateway != "" {
			d["connection"] = "逻辑网关 " + cfg.Gateway
			if forwarding[ip] {
				d["connection"] = "逻辑网关 " + cfg.Gateway + " · 观察到连接记录"
			}
			d["connectionEvidence"] = "网关为用户标注；" + d["connectionEvidence"].(string)
		}
		if cfg.Name != "" && mac != "" {
			e.db.Exec("INSERT OR REPLACE INTO identities(mac,ip,name,kind,model) VALUES(?,?,?,?,?)", mac, ip, cfg.Name, cfg.Kind, cfg.Model)
		}
		if writable {
			var previous string
			e.db.QueryRow("SELECT status FROM events WHERE identity=? ORDER BY ts DESC LIMIT 1", id).Scan(&previous)
			if previous != d["status"] {
				e.db.Exec("INSERT INTO events VALUES(?,?,?)", now.Unix(), id, d["status"])
			}
		}
		if rows, err := e.db.Query("SELECT ts,status FROM events WHERE identity=? ORDER BY ts DESC LIMIT 5", id); err == nil {
			history := []string{}
			for rows.Next() {
				var ts int64
				var state string
				rows.Scan(&ts, &state)
				history = append(history, time.Unix(ts, 0).Format("01-02 15:04")+" · "+state)
			}
			rows.Close()
			if len(history) > 0 {
				d["history"] = history
			}
		}
	}
	links := []any{}
	allLinks := append([]manualLink{}, c.Links...)
	for _, d := range c.Devices {
		if d.Gateway != "" {
			allLinks = append(allLinks, manualLink{d.IP, d.Gateway, "gateway", "用户确认的网关"})
		}
	}
	linkIDs := map[string]bool{}
	for _, link := range allLinks {
		from, to := byID[link.From], byID[link.To]
		if from == "" || to == "" || linkIDs[from+to+link.Relation] {
			continue
		}
		linkIDs[from+to+link.Relation] = true
		status := "unknown"
		if byIP[link.From]["status"] == "online" && byIP[link.To]["status"] == "online" {
			status = "online"
		}
		rate := rateByIP[link.From]
		if link.Relation != "gateway" {
			rate = flowCounter{}
		}
		_, measured := rateByIP[link.From]
		links = append(links, map[string]any{"id": from + "-" + to + "-" + link.Relation, "from": from, "to": to, "confidence": func() string {
			if strings.Contains(link.Source, "待核实") {
				return "inferred"
			}
			return "confirmed"
		}(), "source": link.Source, "observedAt": stamp, "availability": "available", "relation": link.Relation, "status": status, "downloadMbps": rate.RX, "uploadMbps": rate.TX, "measured": measured && link.Relation == "gateway"})
	}
	links, raw["pathMonitor"] = presence.decorate(byIP, byID, links, now, localIP())
	raw["devices"] = devices
	raw["alerts"] = diagnosticAlerts(c, probes, byID, e.writeError, stamp)
	raw["links"] = links
	raw["coverage"] = "后台持续采集 · 仅统计本机可见流量，历史自采集开始累计；物理接线按用户确认展示"
	raw["integrations"] = integrationStatus(c)
	caps := []any{}
	for _, v := range []struct{ name, state, detail string }{{"设备连通性与延迟", "available", "后台 ICMP 与邻居观察；无响应不等于关机"}, {"可见路径流量", func() string {
		if flowErr == nil {
			return "available"
		}
		return "unsupported"
	}(), "nlbwmon 字节计数，历史自采集开始累计；覆盖与误差尚待验收"}, {"物理拓扑", "unsupported", "H3C 无管理交换机；接线来源为用户确认"}, {"管理接口", "unsupported", "ESXi 等待读取凭据；爱快和小米适配尚未实现"}} {
		caps = append(caps, map[string]any{"name": v.name, "availability": v.state, "detail": v.detail, "source": "后台采集能力", "observedAt": stamp})
	}
	raw["capabilities"] = caps
	// Probe the router's external path, independently of any client device.
	e.probeMu.Lock()
	var externalLatency *float64
	var externalLoss *float64
	for _, target := range []string{"1.1.1.1", "223.5.5.5"} {
		key := "external-" + target
		p, ok := e.probes[key]
		if ok && now.Sub(p.At) < 60*time.Second {
			if p.Success && externalLatency == nil {
				externalLatency = p.Latency
				loss := p.Loss
				externalLoss = &loss
			}
		}
		if !e.pending[key] && now.Sub(p.At) > time.Duration(c.Interval)*time.Second {
			e.pending[key] = true
			go func(key, target string) {
				e.slots <- struct{}{}
				p := probe(target)
				<-e.slots
				e.probeMu.Lock()
				e.probes[key] = p
				delete(e.pending, key)
				e.probeMu.Unlock()
			}(key, target)
		}
	}
	e.probeMu.Unlock()
	metrics := raw["metrics"].(map[string]any)
	if externalLatency != nil {
		metrics["latency"] = observation(externalLatency, "available", "软路由到公网目标 ICMP RTT；不是全部业务流量延迟", stamp)
		metrics["loss"] = observation(externalLoss, "available", "公网 ICMP 探测样本", stamp)
	}

	e.prune(c)
	points := raw["samples"].([]any)
	if len(points) > 0 {
		points = points[len(points)-1:]
		point := points[0].(map[string]any)
		metrics := raw["metrics"].(map[string]any)
		point["latency"] = metrics["latency"].(map[string]any)["value"]
		point["loss"] = metrics["loss"].(map[string]any)["value"]
	}
	payload, _ := json.Marshal(points)
	if writable {
		if _, err := e.db.Exec("INSERT OR REPLACE INTO samples(ts,payload) VALUES(?,?)", now.Unix(), string(payload)); err != nil {
			e.writeError = err.Error()
		}
	}
	raw["storageError"] = e.writeError
	raw["historyStart"] = e.historyStart()
	e.mu.Lock()
	e.snapshot = raw
	e.mu.Unlock()
}
func trafficIdentity(ip string, devices map[string]map[string]any) string {
	if d := devices[ip]; d != nil {
		if mac, ok := d["mac"].(string); ok && mac != "" {
			return strings.ToLower(mac)
		}
	}
	return ip
}
func readAccounting() (map[string]flowCounter, error) {
	out, err := runTimeout(5*time.Second, "nlbw", "-c", "json", "-n")
	if err != nil {
		return nil, errors.New("nlbwmon 未接通")
	}
	return parseAccounting(out)
}
func parseAccounting(out []byte) (map[string]flowCounter, error) {
	var v struct {
		Columns []string `json:"columns"`
		Data    [][]any  `json:"data"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, err
	}
	cols := map[string]int{}
	for i, s := range v.Columns {
		cols[s] = i
	}
	ipIndex, ok := cols["ip"]
	if !ok {
		return nil, errors.New("nlbwmon 格式缺少 ip")
	}
	rxIndex, rxOK := cols["rx_bytes"]
	txIndex, txOK := cols["tx_bytes"]
	if !rxOK || !txOK {
		return nil, errors.New("nlbwmon 格式缺少字节列")
	}
	result := map[string]flowCounter{}
	for _, row := range v.Data {
		if len(row) <= ipIndex || len(row) <= rxIndex || len(row) <= txIndex {
			continue
		}
		ip, _ := row[ipIndex].(string)
		rx, _ := row[rxIndex].(float64)
		tx, _ := row[txIndex].(float64)
		if net.ParseIP(ip) == nil {
			continue
		}
		c := result[ip]
		c.RX += rx
		c.TX += tx
		result[ip] = c
	}
	return result, nil
}
func (e *runtimeEngine) historyStart() any {
	var start sql.NullInt64
	e.db.QueryRow("SELECT MIN(ts) FROM samples").Scan(&start)
	if !start.Valid {
		return nil
	}
	return time.Unix(start.Int64, 0).Format(time.RFC3339)
}
func (e *runtimeEngine) prune(c runtimeConfig) {
	cutoff := time.Now().Add(-time.Duration(c.Retention) * 24 * time.Hour).Unix()
	e.db.Exec("DELETE FROM usage WHERE ts < ?", cutoff)
	e.db.Exec("DELETE FROM samples WHERE ts < ?", cutoff)
	e.db.Exec("DELETE FROM events WHERE ts < ?", cutoff)
	var pages, free, size int64
	e.db.QueryRow("PRAGMA page_count").Scan(&pages)
	e.db.QueryRow("PRAGMA freelist_count").Scan(&free)
	e.db.QueryRow("PRAGMA page_size").Scan(&size)
	if (pages-free)*size > int64(c.Storage)*1024*1024/2 {
		e.db.Exec("DELETE FROM samples WHERE ts IN (SELECT ts FROM samples ORDER BY ts LIMIT 500)")
		e.db.Exec("DELETE FROM usage WHERE ts IN (SELECT ts FROM usage ORDER BY ts LIMIT 500)")
	}
	e.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
}
func (e *runtimeEngine) storageReady(c runtimeConfig) bool {
	var total int64
	filepath.WalkDir(e.dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	if total >= int64(c.Storage)*1024*1024*9/10 || !diskHasReserve(e.dir) {
		e.writeError = "存储预算或磁盘余量不足，已暂停历史写入；实时探测继续"
		return false
	}
	e.writeError = ""
	return true
}
func configAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		engine.mu.RLock()
		b, _ := json.Marshal(engine.config)
		engine.mu.RUnlock()
		var c runtimeConfig
		json.Unmarshal(b, &c)
		for key, v := range c.Integrations {
			v.Password = ""
			c.Integrations[key] = v
		}
		reply(w, 200, c)
		return
	}
	if r.Method != "POST" {
		reply(w, 405, map[string]string{"error": "POST required"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var c runtimeConfig
	if json.NewDecoder(r.Body).Decode(&c) != nil {
		reply(w, 400, map[string]string{"error": "配置格式错误"})
		return
	}
	if err := validateConfig(c); err != nil {
		reply(w, 400, map[string]string{"error": err.Error()})
		return
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	for key, v := range c.Integrations {
		if v.Password == "" {
			v.Password = engine.config.Integrations[key].Password
			c.Integrations[key] = v
		}
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(base+"/config/config.json.new", data, 0600); err != nil {
		reply(w, 500, map[string]string{"error": "保存配置失败"})
		return
	}
	if err := os.Rename(base+"/config/config.json.new", base+"/config/config.json"); err != nil {
		reply(w, 500, map[string]string{"error": "保存配置失败"})
		return
	}
	engine.config = c
	reply(w, 200, map[string]string{"message": "服务端设置已保存，下一轮采集生效"})
}
func historyAPI(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if r.URL.Query().Get("range") == "7d" {
		hours = 168
	}
	rows, err := engine.db.Query("SELECT MIN(ts),payload FROM samples WHERE ts>=? GROUP BY ts / ? ORDER BY ts", time.Now().Add(-time.Duration(hours)*time.Hour).Unix(), func() int {
		if hours == 168 {
			return 300
		}
		return 60
	}())
	if err != nil {
		reply(w, 500, map[string]string{"error": "历史读取失败"})
		return
	}
	defer rows.Close()
	result := []any{}
	var previousTS int64
	for rows.Next() {
		var ts int64
		var payload string
		rows.Scan(&ts, &payload)
		var points []map[string]any
		json.Unmarshal([]byte(payload), &points)
		if len(points) > 0 {
			gap := int64(180)
			if hours == 168 {
				gap = 600
			}
			if previousTS != 0 && ts-previousTS > gap {
				result = append(result, map[string]any{"time": time.Unix(previousTS+1, 0).Format("01-02 15:04"), "download": nil, "upload": nil, "latency": nil, "loss": nil})
			}
			previousTS = ts
			point := points[len(points)-1]
			point["time"] = time.Unix(ts, 0).Format("01-02 15:04")
			result = append(result, point)
		}
	}
	rows.Close()
	usage := map[string]float64{}
	ur, err := engine.db.Query("SELECT identity,SUM(rx+tx) FROM usage WHERE ts>=? GROUP BY identity", time.Now().Add(-time.Duration(hours)*time.Hour).Unix())
	if err == nil {
		for ur.Next() {
			var id string
			var total float64
			ur.Scan(&id, &total)
			usage[id] = total / 1e9
		}
		ur.Close()
	}
	reply(w, 200, map[string]any{"samples": result, "historyStart": engine.historyStart(), "usage": usage})
}
func integrationStatus(c runtimeConfig) []any {
	result := []any{}
	for _, name := range []string{"esxi", "ikuai", "xiaomi"} {
		v := c.Integrations[name]
		if name == "esxi" {
			result = append(result, esxiStatus(v))
			continue
		}
		reason := "未配置读取凭据"
		if v.Enabled {
			reason = "接口尚未通过实机验证，未使用其数据"
		}
		result = append(result, map[string]any{"name": name, "available": false, "reason": reason})
	}
	return result
}

// An absent neighbor can retain a unique historical identity, explicitly marked.
// Multiple MACs previously using an IP are ambiguous and must not be guessed.
func restoreIdentities(db *sql.DB, devices map[string]map[string]any) {
	for ip, d := range devices {
		if mac, _ := d["mac"].(string); mac != "" || net.ParseIP(ip) == nil {
			continue
		}
		var mac string
		if db.QueryRow("SELECT MIN(mac) FROM identities WHERE ip=? HAVING COUNT(*)=1", ip).Scan(&mac) == nil {
			d["mac"] = mac
			d["macSource"] = "历史标注中的 MAC，当前邻居记录未确认"
		}
	}
}

func addressIdentity(ip, mac string, count int) string {
	id := "mac-" + strings.ReplaceAll(strings.ToLower(mac), ":", "")
	if count > 1 {
		id += "-" + strings.NewReplacer(".", "-", ":", "-").Replace(ip)
	}
	return id
}
