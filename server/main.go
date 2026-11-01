package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/*
var web embed.FS
var version = "0.3.6"
var sessionPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

const base = "/opt/homenet-doctor"
const repo = "https://api.github.com/repos/cbdoglolz/netdoctor"

var client = &http.Client{Timeout: 90 * time.Second}
var updateMu sync.Mutex

func command(args ...string) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, args[0], args[1:]...).Output()
	return out
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func auth(r *http.Request) bool {
	cookie, e := r.Cookie("homenet_session")
	if e != nil || cookie.Value == strings.Repeat("0", 32) || !sessionPattern.MatchString(cookie.Value) {
		return false
	}
	args, _ := json.Marshal(map[string]any{"ubus_rpc_session": cookie.Value, "scope": "ubus", "object": "system", "function": "board"})
	var session struct {
		Data struct {
			Username string `json:"username"`
		} `json:"data"`
	}
	json.Unmarshal(command("ubus", "call", "session", "list", string(args)), &session)
	if session.Data.Username != "root" {
		return false
	}
	var result struct{ Access bool }
	json.Unmarshal(command("ubus", "call", "session", "access", string(args)), &result)
	return result.Access
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	u, e := url.Parse(origin)
	return e == nil && origin != "" && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
}

const loginPage = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>HomeNet Doctor 登录</title><style>body{font:14px system-ui;background:#f6f8fb;color:#354866;display:grid;place-items:center;min-height:95vh}form{background:white;padding:35px;border:1px solid #e4eaf2;border-radius:14px;width:290px}input,button{box-sizing:border-box;width:100%;padding:12px;margin-top:15px;border:1px solid #dce5f1;border-radius:7px}button{background:#2976eb;color:white}p{font-size:12px;line-height:1.8;color:#8191a7}</style><form method="post" action="/login"><h2>HomeNet Doctor</h2><p>使用软路由的管理员账号登录。认证由本机 ubus 完成，密码不保存。</p><input name="username" value="root" autocapitalize="none" spellcheck="false" autocomplete="username" aria-label="用户名" required><input name="password" id="password" type="password" autocapitalize="none" spellcheck="false" autocomplete="current-password" aria-label="密码" required><label style="display:block;margin-top:12px"><input type="checkbox" style="width:auto;margin:0 6px 0 0" onchange="document.getElementById('password').type=this.checked?'text':'password'">显示密码</label><button>登录</button><p>当前为真实采集测试版。请在可信局域网中使用。</p></form></html>`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Print(version)
		return
	}
	if err := installLuCI(); err != nil {
		log.Printf("LuCI entry installation failed: %v", err)
	}
	var err error
	engine, err = openEngine(base + "/config")
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := cleanupPathRules(); err != nil {
		log.Printf("Path observer cleanup: %v", err)
	}
	go engine.run(ctx)
	connectionsDone := make(chan struct{})
	go func() { defer close(connectionsDone); runConnections(ctx) }()
	presenceDone := make(chan struct{})
	go func() { defer close(presenceDone); presence.run(ctx) }()
	static, _ := fs.Sub(web, "web")
	files := http.FileServer(http.FS(static))
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"version": version, "status": "ok"})
	})
	mux.HandleFunc("/login", handleLogin)
	mux.HandleFunc("/api/snapshot", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, collect()) })
	mux.HandleFunc("/runtime.json", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]bool{"live": true}) })
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		rel, e := latest()
		if e != nil {
			reply(w, 503, map[string]string{"error": e.Error()})
			return
		}
		reply(w, 200, map[string]any{"localVersion": version, "onlineVersion": strings.TrimPrefix(rel.Tag, "v"), "notes": rel.Body, "updateAvailable": newer(rel.Tag, version), "canUpdate": hasAssets(rel), "reason": "安装前验证版本及 SHA-256；失败时保留当前版本。", "releaseUrl": rel.URL})
	})
	mux.HandleFunc("/api/update", update)
	mux.HandleFunc("/api/config", configAPI)
	mux.HandleFunc("/api/device", deviceAPI)
	mux.HandleFunc("/api/inventory", inventoryAPI)
	mux.HandleFunc("/api/layout", layoutAPI)
	mux.HandleFunc("/api/changes", changesAPI)
	mux.HandleFunc("/api/connections", connectionsAPI)
	mux.HandleFunc("/api/path-observation", pathObservationAPI)
	mux.HandleFunc("/api/history", historyAPI)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: os.Getenv("HOMENET_LISTEN"), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 30 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.URL.Path != "/login" && r.URL.Path != "/health" && !auth(r) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				reply(w, 401, map[string]string{"error": "登录已过期，请重新登录"})
			} else {
				http.Redirect(w, r, "/login", 303)
			}
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.URL.Path != "/login" && !sameOrigin(r) {
			reply(w, 403, map[string]string{"error": "Origin validation failed"})
			return
		}
		mux.ServeHTTP(w, r)
	})}
	if server.Addr == "" {
		server.Addr = "127.0.0.1:8088"
	}
	go func() {
		<-ctx.Done()
		paths.stop()
		<-presenceDone
		<-connectionsDone
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
type release struct {
	Tag    string  `json:"tag_name"`
	Body   string  `json:"body"`
	URL    string  `json:"html_url"`
	Assets []asset `json:"assets"`
}

func github(endpoint string) (*http.Response, error) {
	if !strings.HasPrefix(endpoint, repo) {
		return nil, errors.New("Invalid GitHub endpoint")
	}
	req, _ := http.NewRequest("GET", endpoint, nil)

	req.Header.Set("Accept", "application/vnd.github+json")
	if strings.Contains(endpoint, "/releases/assets/") {
		req.Header.Set("Accept", "application/octet-stream")
	}
	resp, e := client.Do(req)
	if e != nil {
		return nil, errors.New("连接 GitHub 失败")
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub 返回 %d，请检查凭据与 Release", resp.StatusCode)
	}
	return resp, nil
}
func latest() (release, error) {
	var rel release
	resp, e := github(repo + "/releases/latest")
	if e != nil {
		return rel, e
	}
	defer resp.Body.Close()
	e = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&rel)
	return rel, e
}
func newer(tag, local string) bool {
	a, b := strings.Split(strings.TrimPrefix(tag, "v"), "."), strings.Split(local, ".")
	if len(a) != 3 || len(b) != 3 {
		return false
	}
	for i := range a {
		x, e := strconv.Atoi(a[i])
		y, f := strconv.Atoi(b[i])
		if e != nil || f != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}
func hasAssets(rel release) bool {
	names := map[string]bool{}
	for _, a := range rel.Assets {
		names[a.Name] = true
	}
	return names["homenet-doctor-linux-amd64"] && names["homenet-doctor-linux-amd64.sha256"]
}
func downloadAsset(rel release, name string, limit int64) ([]byte, error) {
	for _, a := range rel.Assets {
		if a.Name == name {
			resp, e := github(a.URL)
			if e != nil {
				return nil, e
			}
			defer resp.Body.Close()
			data, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
			if int64(len(data)) > limit {
				return nil, errors.New("更新文件超过大小限制")
			}
			return data, e
		}
	}
	return nil, errors.New("发布缺少安装文件")
}
func update(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		reply(w, 405, map[string]string{"error": "POST required"})
		return
	}
	if !updateMu.TryLock() {
		reply(w, 409, map[string]string{"error": "已有更新正在进行"})
		return
	}
	defer updateMu.Unlock()
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		Version string `json:"confirmedVersion"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		reply(w, 400, map[string]string{"error": "无效请求"})
		return
	}
	rel, e := latest()
	if e != nil {
		reply(w, 503, map[string]string{"error": e.Error()})
		return
	}
	v := strings.TrimPrefix(rel.Tag, "v")
	if input.Version != v || !newer(v, version) || !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(v) {
		reply(w, 409, map[string]string{"error": "在线版本已变化或不是更新版本，请重新检查并确认"})
		return
	}
	data, e := downloadAsset(rel, "homenet-doctor-linux-amd64", 64<<20)
	if e != nil {
		reply(w, 502, map[string]string{"error": e.Error()})
		return
	}
	checksum, e := downloadAsset(rel, "homenet-doctor-linux-amd64.sha256", 4096)
	hash := sha256.Sum256(data)
	parts := strings.Fields(string(checksum))
	if e != nil || len(parts) == 0 || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(hex.EncodeToString(hash[:]))) != 1 {
		reply(w, 400, map[string]string{"error": "更新文件校验失败，未安装"})
		return
	}
	dir := filepath.Join(base, "releases", v)
	os.MkdirAll(dir, 0755)
	target := dir + "/homenet-doctor"
	if e = os.WriteFile(target, data, 0755); e != nil {
		reply(w, 500, map[string]string{"error": "写入失败，当前版本保留"})
		return
	}
	reply(w, 202, map[string]string{"message": "安装文件校验通过，正在切换并验证服务。失败会回退，请稍后重新检查本地版本。"})
	go func() {
		time.Sleep(time.Second)
		cmd := exec.Command("setsid", "/bin/sh", base+"/apply-update.sh", v)
		if e := cmd.Start(); e != nil {
			log.Print(e)
		}
	}()
}

var sampleMu sync.Mutex
var previousRX, previousTX uint64
var previousAt time.Time
var samples = []any{}
var previousCPU, previousIdle uint64

func observation(value any, availability, source, stamp string) map[string]any {
	return map[string]any{"value": value, "availability": availability, "source": source, "observedAt": stamp}
}
func collectRaw() map[string]any {
	sampleMu.Lock()
	defer sampleMu.Unlock()
	now := time.Now()
	stamp := now.Format(time.RFC3339)
	obs := func(v any, a, s string) map[string]any { return observation(v, a, s, stamp) }
	unavailable := func() map[string]any { return obs(nil, "unsupported", "未配置真实探测") }
	source := "本机 /proc 与 ip neigh（被动观察）"
	var cpu any
	cpuState := "unsupported"
	stat, _ := os.ReadFile("/proc/stat")
	lines := strings.Split(string(stat), "\n")
	if len(lines) > 0 {
		f := strings.Fields(lines[0])
		if len(f) > 5 {
			var total, idle uint64
			for i := 1; i < len(f) && i <= 8; i++ {
				n, _ := strconv.ParseUint(f[i], 10, 64)
				total += n
				if i == 4 || i == 5 {
					idle += n
				}
			}
			if total > previousCPU && previousCPU > 0 && idle >= previousIdle {
				cpu = float64(int((1-float64(idle-previousIdle)/float64(total-previousCPU))*1000)) / 10
				cpuState = "available"
			}
			previousCPU = total
			previousIdle = idle
		}
	}
	memory := any(nil)
	memData, _ := os.ReadFile("/proc/meminfo")
	mem := map[string]float64{}
	for _, line := range strings.Split(string(memData), "\n") {
		f := strings.Fields(line)
		if len(f) > 1 {
			mem[f[0]], _ = strconv.ParseFloat(f[1], 64)
		}
	}
	if mem["MemTotal:"] > 0 {
		memory = float64(int((1-mem["MemAvailable:"]/mem["MemTotal:"])*1000)) / 10
	}
	local := map[string]any{"id": "doctor", "name": "iStoreOS 旁路由", "ip": localIP(), "mac": "", "kind": "gateway", "model": strings.TrimSpace(string(command("uname", "-m"))), "connection": "本机 · br-lan", "status": "online", "latency": unavailable(), "traffic": obs(nil, "invisible", "尚未接入按设备流量统计"), "cpu": obs(cpu, cpuState, "/proc/stat 两次采样差值"), "memory": obs(memory, "available", "/proc/meminfo"), "observedAt": stamp, "source": source, "history": []string{"当前采集成功；未推断历史在线状态"}}
	devices := []any{local}
	hostnames := map[string]string{}
	leases, _ := os.ReadFile("/tmp/dhcp.leases")
	for _, line := range strings.Split(string(leases), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[3] != "*" {
			hostnames[f[2]] = f[3]
		}
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(command("ip", "-4", "neigh", "show", "dev", "br-lan")), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || net.ParseIP(f[0]) == nil || seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		mac := ""
		for i, s := range f {
			if s == "lladdr" && i+1 < len(f) {
				mac = f[i+1]
			}
		}
		if mac == "" {
			continue
		}
		name := hostnames[f[0]]
		if name == "" {
			name = "设备 " + f[0]
		}
		status := "unknown"
		if f[len(f)-1] == "REACHABLE" {
			status = "online"
		}
		devices = append(devices, map[string]any{"id": "neighbor-" + strings.ReplaceAll(f[0], ".", "-"), "name": name, "ip": f[0], "mac": mac, "kind": "iot", "model": "设备类型待确认", "connection": "连接位置待确认", "status": status, "latency": unavailable(), "traffic": obs(nil, "invisible", "旁路由未接入按设备流量统计"), "cpu": unavailable(), "memory": unavailable(), "observedAt": stamp, "source": "ip neigh · " + f[len(f)-1], "history": []string{"邻居表存在记录，不代表已确认当前在线或实际接线"}})
	}
	rxBytes, _ := os.ReadFile("/sys/class/net/br-lan/statistics/rx_bytes")
	txBytes, _ := os.ReadFile("/sys/class/net/br-lan/statistics/tx_bytes")
	rx, e1 := strconv.ParseUint(strings.TrimSpace(string(rxBytes)), 10, 64)
	tx, e2 := strconv.ParseUint(strings.TrimSpace(string(txBytes)), 10, 64)
	var down, up any
	availability := "unsupported"
	elapsed := now.Sub(previousAt).Seconds()
	if e1 == nil && e2 == nil && !previousAt.IsZero() && elapsed >= 1 && rx >= previousRX && tx >= previousTX {
		down = float64(int(float64(rx-previousRX)*8/elapsed/1e5)) / 10
		up = float64(int(float64(tx-previousTX)*8/elapsed/1e5)) / 10
		availability = "available"
	}
	if elapsed >= 1 {
		previousRX = rx
		previousTX = tx
		previousAt = now
		samples = append(samples, map[string]any{"time": now.Format("15:04:05"), "download": down, "upload": up, "latency": nil, "loss": nil})
		if len(samples) > 360 {
			samples = samples[len(samples)-360:]
		}
	}
	capabilities := []any{}
	for _, c := range []struct{ Name, State, Detail string }{{"设备发现与连通性", "available", "只读邻居表；REACHABLE 外均显示状态未知"}, {"设备流量统计", "invisible", "仅有本机 br-lan 接口吞吐，不是全网出口或按设备统计"}, {"物理拓扑", "unsupported", "没有交换机/AP 证据，不推断接线"}, {"终端 CPU / 内存", "unsupported", "目前仅采集本机内存"}, {"域名活动", "unsupported", "未接入 DNS 日志"}} {
		capabilities = append(capabilities, map[string]any{"name": c.Name, "availability": c.State, "detail": c.Detail, "source": source, "observedAt": stamp})
	}
	return map[string]any{"live": true, "mode": "bypass", "scenario": "normal", "devices": devices, "links": []any{}, "alerts": []any{}, "traffic": []any{}, "capabilities": capabilities, "samples": samples, "observedAt": stamp, "metrics": map[string]any{"download": obs(down, availability, "br-lan 接收速率"), "upload": obs(up, availability, "br-lan 发送速率"), "latency": unavailable(), "loss": unavailable()}, "coverage": "真实采集 · 本机 br-lan 接口收发与邻居表；尚不能判断全网健康、物理拓扑或出口流量"}
}

func localIP() string {
	iface, e := net.InterfaceByName("br-lan")
	if e != nil {
		return "未知"
	}
	addrs, _ := iface.Addrs()
	for _, addr := range addrs {
		ip, _, e := net.ParseCIDR(addr.String())
		if e == nil && ip.To4() != nil {
			return ip.String()
		}
	}
	return "未知"
}
