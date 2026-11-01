package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const pathTable = "hnd_path_observe"
const pathOwner = "HomeNet Doctor path observer v1"

type pathPhase struct {
	Name      string         `json:"name"`
	StartedAt string         `json:"startedAt"`
	Devices   []pathEvidence `json:"devices"`
}
type pathReport struct {
	ID         string         `json:"id"`
	State      string         `json:"state"`
	StartedAt  string         `json:"startedAt"`
	EndsAt     string         `json:"endsAt"`
	Phase      string         `json:"phase"`
	Devices    []pathEvidence `json:"devices"`
	Phases     []pathPhase    `json:"phases"`
	Warnings   []string       `json:"warnings"`
	Conclusion string         `json:"conclusion"`
	FinishedAt string         `json:"finishedAt,omitempty"`
}
type pathObserver struct {
	mu       sync.Mutex
	report   *pathReport
	acc      *pathAccumulator
	cancel   context.CancelFunc
	done     chan struct{}
	baseline map[string]uint64
	targets  map[string]bool
	phaseAt  string
}

var paths = &pathObserver{}

func nftPath(script string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "nft", "-f", "-")
	c.Stdin = strings.NewReader(script)
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("观察计数规则失败: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
func cleanupPathRules() error {
	listed, err := runTimeout(3*time.Second, "nft", "-j", "list", "tables")
	if err != nil {
		return fmt.Errorf("无法核查观察计数表，清理状态未知")
	}
	var tables struct {
		NFTables []struct {
			Table struct{ Family, Name string } `json:"table"`
		} `json:"nftables"`
	}
	if json.Unmarshal(listed, &tables) != nil {
		return fmt.Errorf("无法解析计数表清单，清理状态未知")
	}
	found := false
	for _, v := range tables.NFTables {
		if v.Table.Family == "inet" && v.Table.Name == pathTable {
			found = true
		}
	}
	if !found {
		return nil
	}
	out, err := runTimeout(3*time.Second, "nft", "-j", "list", "table", "inet", pathTable)
	if err != nil {
		return fmt.Errorf("无法读取观察表归属，拒绝删除")
	}
	if !ownedPathTable(out) {
		return fmt.Errorf("同名 nftables 表不属于本观察模块，拒绝删除")
	}
	return nftPath("delete table inet " + pathTable + "\n")
}
func ownedPathTable(data []byte) bool {
	var v struct {
		NFTables []struct {
			Table struct{ Family, Name, Comment string } `json:"table"`
		} `json:"nftables"`
	}
	if json.Unmarshal(data, &v) != nil {
		return false
	}
	for _, e := range v.NFTables {
		if e.Table.Family == "inet" && e.Table.Name == pathTable && e.Table.Comment == pathOwner {
			return true
		}
	}
	return false
}
func (o *pathObserver) warning(s string) {
	for _, v := range o.report.Warnings {
		if v == s {
			return
		}
	}
	o.report.Warnings = append(o.report.Warnings, s)
}
func (o *pathObserver) addTargets() error {
	script := ""
	for ip := range o.acc.Clients {
		if o.targets[ip] {
			continue
		}
		if len(o.targets) >= 512 {
			o.warning("目标超过 512，覆盖不完整")
			break
		}
		family := "ip"
		if net.ParseIP(ip).To4() == nil {
			family = "ip6"
		}
		script += fmt.Sprintf("add rule inet %s forward %s saddr %s counter comment \"out:%s\"\nadd rule inet %s forward %s daddr %s counter comment \"in:%s\"\n", pathTable, family, ip, ip, pathTable, family, ip, ip)
	}
	if script == "" {
		return nil
	}
	if err := nftPath(script); err != nil {
		return err
	}
	for ip := range o.acc.Clients {
		o.targets[ip] = true
	}
	return nil
}
func parsePathCounters(data []byte) map[string]uint64 {
	var v struct {
		NFTables []struct {
			Rule struct {
				Comment string `json:"comment"`
				Expr    []struct {
					Counter struct {
						Packets uint64 `json:"packets"`
					} `json:"counter"`
				} `json:"expr"`
			} `json:"rule"`
		} `json:"nftables"`
	}
	json.Unmarshal(data, &v)
	m := map[string]uint64{}
	for _, e := range v.NFTables {
		for _, x := range e.Rule.Expr {
			if e.Rule.Comment != "" {
				m[e.Rule.Comment] += x.Counter.Packets
			}
		}
	}
	return m
}
func (o *pathObserver) refresh() {
	out, err := runTimeout(4*time.Second, "nft", "-j", "list", "table", "inet", pathTable)
	ct, ctErr := runTimeout(4*time.Second, "conntrack", "-L", "-o", "extended")
	o.mu.Lock()
	defer o.mu.Unlock()
	if err != nil {
		o.warning("无法读取转发计数，转发结论不完整")
	}
	counts := parsePathCounters(out)
	for ip, e := range o.acc.Clients {
		if n := counts["out:"+ip]; n >= o.baseline["out:"+ip] {
			e.ForwardPackets = n - o.baseline["out:"+ip]
		}
		if n := counts["in:"+ip]; n >= o.baseline["in:"+ip] {
			e.ReplyPackets = n - o.baseline["in:"+ip]
		}
		o.acc.Clients[ip] = e
	}
	if ctErr != nil {
		o.warning("无法读取连接映射，NAT / 加速关联未验证")
	} else {
		o.acc.mapConnections(parsePathMappings(string(ct)))
	}
	if err := o.addTargets(); err != nil {
		o.warning(err.Error())
	}
	o.snapshotLocked()
}
func (o *pathObserver) snapshotLocked() {
	o.report.Devices = o.acc.evidence()
	sort.Slice(o.report.Devices, func(i, j int) bool { return o.report.Devices[i].IP < o.report.Devices[j].IP })
	n, f, b := 0, 0, 0
	for _, e := range o.report.Devices {
		if e.Received > 0 {
			n++
		}
		if e.Forwarded > 0 {
			f++
		}
		if e.Bidirectional > 0 {
			b++
		}
	}
	o.report.Conclusion = fmt.Sprintf("本阶段 %d 台设备向本机交付公网请求，%d 台有指定连接转发证据，%d 台有双向路径证据。均不等于已读取默认网关或全部流量受控。", n, f, b)
}
func pathInventory() (*pathAccumulator, error) {
	a := &pathAccumulator{LocalMAC: map[string]bool{}, LocalIP: map[string]bool{}, Clients: map[string]pathEvidence{}, KnownMAC: map[string]string{}, Flows: map[pathTuple]*pathFlow{}, Aliases: map[pathTuple]pathTuple{}, Phase: "baseline"}
	iface, err := net.InterfaceByName("br-lan")
	if err != nil {
		return nil, fmt.Errorf("找不到 br-lan，暂不支持此环境")
	}
	addrs, _ := iface.Addrs()
	subnets := []*net.IPNet{}
	for _, v := range addrs {
		ip, n, e := net.ParseCIDR(v.String())
		if e == nil {
			a.LocalIP[ip.String()] = true
			subnets = append(subnets, n)
		}
	}
	// Include addresses of all local interfaces so router-originated traffic is excluded.
	interfaces, _ := net.Interfaces()
	for _, i := range interfaces {
		if len(i.HardwareAddr) > 0 {
			a.LocalMAC[strings.ToLower(i.HardwareAddr.String())] = true
		}
		v, _ := i.Addrs()
		for _, s := range v {
			ip, _, err := net.ParseCIDR(s.String())
			if err == nil {
				a.LocalIP[ip.String()] = true
			}
		}
	}
	if engine == nil {
		return nil, fmt.Errorf("采集引擎未初始化")
	}
	engine.mu.RLock()
	raw := engine.snapshot
	b, _ := json.Marshal(raw)
	engine.mu.RUnlock()
	var s struct {
		Devices []struct{ IP, Name, MAC, MACSource string }
	}
	json.Unmarshal(b, &s)
	for _, d := range s.Devices {
		ip := net.ParseIP(d.IP)
		if ip == nil || a.LocalIP[d.IP] {
			continue
		}
		lan := false
		for _, n := range subnets {
			if n.Contains(ip) {
				lan = true
			}
		}
		if !lan {
			continue
		}
		mac := strings.ToLower(d.MAC)
		if d.MACSource != "" {
			mac = ""
		}
		if _, err := net.ParseMAC(mac); err != nil {
			mac = ""
		}
		a.Clients[d.IP] = pathEvidence{IP: d.IP, Name: d.Name, MAC: mac, Family: func() int {
			if ip.To4() == nil {
				return 6
			}
			return 4
		}(), Status: "unknown", Phase: "baseline", Flows: []string{}}
		if mac != "" {
			a.KnownMAC[mac] = d.Name
		}
	}
	// Refresh L2 identities from the actual LAN neighbor table; never restore old MACs here.
	for _, f := range strings.Split(string(command("ip", "neigh", "show", "dev", "br-lan")), "\n") {
		v := strings.Fields(f)
		for i := 1; i+1 < len(v); i++ {
			if v[i] == "lladdr" {
				if d, ok := a.Clients[v[0]]; ok {
					delete(a.KnownMAC, d.MAC)
					d.MAC = strings.ToLower(v[i+1])
					a.Clients[d.IP] = d
					a.KnownMAC[d.MAC] = d.Name
				}
			}
		}
	}
	if len(a.Clients) == 0 {
		return nil, fmt.Errorf("尚无可观察的 LAN 设备，请等待设备发现")
	}
	return a, nil
}

func (o *pathObserver) start(seconds int) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.report != nil && o.report.State == "running" {
		return fmt.Errorf("已有观察任务正在运行")
	}
	a, err := pathInventory()
	if err != nil {
		return err
	}
	for _, name := range []string{"nft", "tcpdump", "conntrack"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("缺少工具 %s", name)
		}
	}
	if err := cleanupPathRules(); err != nil {
		return err
	}
	if err := nftPath("add table inet " + pathTable + " { comment \"" + pathOwner + "\"; }\nadd chain inet " + pathTable + " forward { type filter hook forward priority 300; policy accept; }\n"); err != nil {
		return err
	}
	now := time.Now()
	o.report = &pathReport{ID: strconv.FormatInt(now.UnixNano(), 10), State: "running", StartedAt: now.Format(time.RFC3339), EndsAt: now.Add(time.Duration(seconds) * time.Second).Format(time.RFC3339), Phase: "baseline", Devices: []pathEvidence{}, Phases: []pathPhase{}, Warnings: []string{"IPv4 / IPv6 与代理路径分别验证；静默设备不能判断为网关未指向本机。", "观察样本不证明全部流量受控；加速、丢包、回程绕行会限制结论。"}}
	o.acc = a
	o.phaseAt = o.report.StartedAt
	o.baseline = map[string]uint64{}
	o.targets = map[string]bool{}
	if err := o.addTargets(); err != nil {
		cleanupPathRules()
		o.report.State = "failed"
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	o.cancel = cancel
	o.done = make(chan struct{})
	go o.run(ctx)
	return nil
}

type boundedStderr struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *boundedStderr) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := 8192 - b.b.Len()
	if left > 0 {
		b.b.Write(p[:min(left, n)])
	}
	return n, nil
}
func (b *boundedStderr) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
func (o *pathObserver) capture(ctx context.Context) {
	round, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// Physical port avoids duplicate bridge/port capture; -p never enables promiscuous mode.
	c := exec.CommandContext(round, "tcpdump", "-i", "eth0", "-p", "-U", "-n", "-s", "128", "-c", "10000", "-w", "-", "ip or ip6")
	stderr := &boundedStderr{}
	c.Stderr = stderr
	pipe, err := c.StdoutPipe()
	if err == nil {
		err = c.Start()
	}
	if err != nil {
		o.mu.Lock()
		o.warning("抓包启动失败，路径无法验证")
		o.mu.Unlock()
		return
	}
	done := make(chan error, 1)
	go func() {
		done <- readPathPCAP(pipe, func(p pathPacket) {
			o.mu.Lock()
			o.acc.packet(p)
			if len(o.acc.Flows) >= 2048 || len(o.acc.Clients) >= 512 {
				o.warning("达到连接或设备数量上限，覆盖不完整")
			}
			o.mu.Unlock()
		}, func() { o.mu.Lock(); o.warning("达到包数上限或包头截断，覆盖不完整"); o.mu.Unlock() })
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
loop:
	for {
		select {
		case err = <-done:
			break loop
		case <-ticker.C:
			o.refresh()
		case <-round.Done():
			c.Process.Kill()
			err = <-done
			break loop
		}
	}
	if err != nil {
		c.Process.Kill()
	}
	waitErr := c.Wait()
	o.mu.Lock()
	dropped, hasDropStats := pathDropStats(stderr.String())
	if !hasDropStats {
		o.warning("抓包丢包统计未取得，不能证明完整覆盖")
	}
	if hasDropStats && dropped > 0 {
		o.warning("抓包报告内核丢包，覆盖不完整")
	}
	if err != nil && err != io.EOF && round.Err() == nil {
		o.warning("抓包解析未完成，覆盖不完整")
	}
	if waitErr != nil && round.Err() == nil {
		o.warning("抓包进程提前退出，覆盖不完整")
	}
	o.mu.Unlock()
}

var pathDropPattern = regexp.MustCompile(`(?m)^([0-9]+) packets dropped by kernel\s*$`)

func pathDropStats(text string) (uint64, bool) {
	m := pathDropPattern.FindStringSubmatch(text)
	if len(m) != 2 {
		return 0, false
	}
	n, err := strconv.ParseUint(m[1], 10, 64)
	return n, err == nil
}
func (o *pathObserver) run(ctx context.Context) {
	defer close(o.done)
	defer func() {
		o.refresh()
		err := cleanupPathRules()
		o.mu.Lock()
		if err != nil {
			o.warning(err.Error())
		}
		o.report.Phases = append(o.report.Phases, pathPhase{o.report.Phase, o.phaseAt, o.report.Devices})
		o.report.State = "finished"
		o.report.FinishedAt = time.Now().Format(time.RFC3339)
		o.snapshotLocked()
		o.persistLocked()
		o.mu.Unlock()
	}()
	for ctx.Err() == nil {
		start := time.Now()
		o.refresh()
		o.capture(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(max(time.Millisecond, 60*time.Second-time.Since(start))):
		}
	}
}
func (o *pathObserver) persistLocked() {
	if !diskHasReserve(engine.dir) {
		o.warning("存储受限，报告未持久保存")
		return
	}
	b, _ := json.Marshal(o.report)
	if len(b) > 4<<20 {
		o.warning("报告超过 4 MiB，未持久保存")
		return
	}
	if _, err := engine.db.Exec("INSERT OR REPLACE INTO path_reports VALUES(?,?,?)", o.report.ID, time.Now().Unix(), string(b)); err != nil {
		o.warning("报告保存失败")
		return
	}
	engine.db.Exec("DELETE FROM path_reports WHERE id NOT IN (SELECT id FROM path_reports ORDER BY created DESC LIMIT 20)")
}
func (o *pathObserver) stop() {
	o.mu.Lock()
	cancel, done := o.cancel, o.done
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func pathObservationAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		var q struct {
			Seconds int    `json:"seconds"`
			Phase   string `json:"phase"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&q); err != nil && err != io.EOF {
			reply(w, 400, map[string]string{"error": "请求格式无效"})
			return
		}
		if q.Phase != "" {
			if q.Phase != "gateway6" && q.Phase != "gateway3" && q.Phase != "restored" {
				reply(w, 400, map[string]string{"error": "测试阶段无效"})
				return
			}
			paths.mu.Lock()
			defer paths.mu.Unlock()
			if paths.report == nil || paths.report.State != "running" {
				reply(w, 409, map[string]string{"error": "请先开始观察"})
				return
			}
			if len(paths.report.Phases) >= 10 {
				reply(w, 409, map[string]string{"error": "一个任务最多 10 个阶段"})
				return
			}
			paths.snapshotLocked()
			paths.report.Phases = append(paths.report.Phases, pathPhase{paths.report.Phase, paths.phaseAt, paths.report.Devices})
			paths.acc.Phase = q.Phase
			paths.report.Phase = q.Phase
			paths.phaseAt = time.Now().Format(time.RFC3339)
			paths.acc.Flows = map[pathTuple]*pathFlow{}
			for ip, e := range paths.acc.Clients {
				paths.baseline["out:"+ip] += e.ForwardPackets
				paths.baseline["in:"+ip] += e.ReplyPackets
				e.Received = 0
				e.ForwardPackets = 0
				e.ReplyPackets = 0
				e.LastSeen = ""
				e.Phase = q.Phase
				e.Offloaded = false
				paths.acc.Clients[ip] = e
			}
			paths.snapshotLocked()
			reply(w, 200, map[string]string{"message": "已标记用户声明的 IPv4 网关阶段；请建立新的公网连接，程序不会修改手机网关"})
			return
		}
		if q.Seconds == 0 {
			q.Seconds = 900
		}
		if q.Seconds < 60 || q.Seconds > 900 {
			reply(w, 400, map[string]string{"error": "观察时长为 60–900 秒"})
			return
		}
		if err := paths.start(q.Seconds); err != nil {
			reply(w, 409, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 202, map[string]string{"message": "路径观察已开始"})
	case "DELETE":
		paths.stop()
		reply(w, 200, map[string]string{"message": "观察已结束，已尝试清理独立计数规则；请检查报告清理状态"})
	case "GET":
		if id := r.URL.Query().Get("report"); id != "" {
			var data string
			if engine.db.QueryRow("SELECT payload FROM path_reports WHERE id=?", id).Scan(&data) != nil {
				reply(w, 404, map[string]string{"error": "报告不存在"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			io.WriteString(w, data)
			return
		}
		paths.mu.Lock()
		var current any
		if paths.report != nil {
			paths.snapshotLocked()
			b, _ := json.Marshal(paths.report)
			json.Unmarshal(b, &current)
		}
		paths.mu.Unlock()
		history := []any{}
		rows, err := engine.db.Query("SELECT id,created FROM path_reports ORDER BY created DESC LIMIT 20")
		if err == nil {
			for rows.Next() {
				var id string
				var at int64
				rows.Scan(&id, &at)
				history = append(history, map[string]any{"id": id, "createdAt": time.Unix(at, 0).Format(time.RFC3339)})
			}
			rows.Close()
		}
		reply(w, 200, map[string]any{"current": current, "history": history})
	default:
		reply(w, 405, map[string]string{"error": "GET, POST or DELETE required"})
	}
}
