package main

// Control is intentionally separate from read-only peering. No shell command is
// accepted from a client. Persistent desired policies are reconciled locally.
import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type controlSettings struct {
	Enabled    bool              `json:"enabled"`
	ManagerID  string            `json:"managerID"`
	Token      string            `json:"token"`
	PeerTokens map[string]string `json:"peerTokens"`
}
type controlRequest struct {
	ID       string `json:"id"`
	Node     string `json:"node"`
	Kind     string `json:"kind"`
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Minutes  int    `json:"minutes"`
	Start    string `json:"start"`
	End      string `json:"end"`
	PolicyID string `json:"policyID"`
	Enabled  bool   `json:"enabled"`
	Target   string `json:"target"`
	Service  string `json:"service"`
}
type controlTask struct {
	Request  controlRequest `json:"request"`
	State    string         `json:"state"`
	At       time.Time      `json:"at"`
	Finished *time.Time     `json:"finished,omitempty"`
	Output   string         `json:"output"`
}
type accessPolicy struct {
	ID      string    `json:"id"`
	IP      string    `json:"ip"`
	MAC     string    `json:"mac"`
	Enabled bool      `json:"enabled"`
	Until   time.Time `json:"until"`
	Start   string    `json:"start,omitempty"`
	End     string    `json:"end,omitempty"`
	Status  string    `json:"status"`
	Detail  string    `json:"detail"`
}
type controlState struct {
	Tasks    []controlTask  `json:"tasks"`
	Policies []accessPolicy `json:"policies"`
}

var controls = struct {
	sync.Mutex
	settings controlSettings
	state    controlState
	dir      string
	err      string
	wake     chan struct{}
}{wake: make(chan struct{}, 1)}
var controlID = regexp.MustCompile(`^[a-zA-Z0-9-]{8,80}$`)
var dnsName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}$`)

func initControl(dir string) error {
	controls.Lock()
	defer controls.Unlock()
	controls.dir = dir
	controls.settings = controlSettings{}
	controls.state = controlState{Tasks: []controlTask{}, Policies: []accessPolicy{}}
	b, e := os.ReadFile(filepath.Join(dir, "control-settings.json"))
	if e == nil {
		if e = json.Unmarshal(b, &controls.settings); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if controls.settings.Enabled && (len(controls.settings.Token) != 64 || len(controls.settings.ManagerID) != 32) {
		return errors.New("控制授权配置无效")
	}
	b, e = os.ReadFile(filepath.Join(dir, "control-state.json"))
	if e == nil {
		if e = json.Unmarshal(b, &controls.state); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if len(controls.state.Policies) > 64 || len(controls.state.Tasks) > 100 {
		return errors.New("控制记录超过上限")
	}
	for i := range controls.state.Tasks {
		t := &controls.state.Tasks[i]
		if t.State == "queued" || t.State == "running" {
			t.State = "interrupted"
			t.Output = "服务重启，未自动重放操作；请检查结果后重新提交"
		}
	}
	return saveControlLocked()
}
func saveControlLocked() error {
	b, e := json.MarshalIndent(controls.state, "", "  ")
	if e != nil {
		return e
	}
	path := filepath.Join(controls.dir, "control-state.json")
	if e = os.WriteFile(path+".tmp", b, 0600); e != nil {
		return e
	}
	// fsync the replacement before acknowledging persistence.
	f, e := os.OpenFile(path+".tmp", os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	e = f.Sync()
	f.Close()
	if e != nil {
		return e
	}
	return os.Rename(path+".tmp", path)
}
func localNodeID() string { peers.RLock(); defer peers.RUnlock(); return peers.config.ID }
func runControlCommand(ctx context.Context, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	var out limitedControlOutput
	c.Stdout = &out
	c.Stderr = &out
	e := c.Run()
	return out.String(), e
}

type limitedControlOutput struct{ bytes.Buffer }

func (b *limitedControlOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 8192 {
		b.Buffer.Write(p[:min(len(p), 8192-b.Len())])
	}
	return n, nil
}
func controlRun(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return runControlCommand(ctx, name, args...)
}

func policyActive(p accessPolicy, now time.Time) bool {
	if !p.Enabled {
		return false
	}
	if p.Start == "" {
		return now.Before(p.Until)
	}
	hm := now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("15:04")
	if p.Start < p.End {
		return hm >= p.Start && hm < p.End
	}
	return hm >= p.Start || hm < p.End
}
func validClock(s string) bool { _, e := time.Parse("15:04", s); return e == nil && len(s) == 5 }
func validateControlRequest(r controlRequest) error {
	if !controlID.MatchString(r.ID) {
		return errors.New("任务 ID 无效")
	}
	switch r.Kind {
	case "pause", "schedule":
		ip := net.ParseIP(r.IP)
		mac, e := net.ParseMAC(r.MAC)
		if ip == nil || ip.To4() == nil || !ip.IsPrivate() || e != nil || len(mac) != 6 || mac[0]&1 != 0 || r.MAC == "00:00:00:00:00:00" {
			return errors.New("需要有效的内网 IPv4 和终端 MAC")
		}
		if r.Kind == "pause" && (r.Minutes < 1 || r.Minutes > 1440) {
			return errors.New("暂停时间须为 1–1440 分钟")
		}
		if r.Kind == "schedule" && (!validClock(r.Start) || !validClock(r.End) || r.Start == r.End) {
			return errors.New("请输入不同的每日开始和结束时间（北京时间）")
		}
	case "policy", "delete-policy":
		if !controlID.MatchString(r.PolicyID) {
			return errors.New("策略 ID 无效")
		}
	case "diagnose":
		if net.ParseIP(r.Target) == nil {
			return errors.New("探测目标须为明确 IPv4 地址")
		}
		ip := net.ParseIP(r.Target)
		if ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLoopback() {
			return errors.New("探测地址无效")
		}
	case "dns":
		if !dnsName.MatchString(r.Target) || strings.Contains(r.Target, "..") {
			return errors.New("域名无效")
		}
	case "service-status", "service-restart":
		if r.Service != "nlbwmon" && r.Service != "avahi-daemon" {
			return errors.New("服务不在允许名单内")
		}
	default:
		return errors.New("操作不在允许名单内")
	}
	return nil
}
func validateControlDevice(r controlRequest, snapshot map[string]any, now time.Time) error {
	at, _ := time.Parse(time.RFC3339, fmt.Sprint(snapshot["observedAt"]))
	if at.IsZero() || now.Sub(at) > 2*time.Minute || at.After(now.Add(time.Minute)) {
		return errors.New("本机采集已过期，拒绝新建控制策略")
	}
	ds, _ := snapshot["devices"].([]any)
	for _, raw := range ds {
		d, _ := raw.(map[string]any)
		if d["ip"] != r.IP {
			continue
		}
		kind := fmt.Sprint(d["kind"])
		if d["id"] == "doctor" || kind == "gateway" || kind == "router" || kind == "switch" || kind == "ap" {
			return errors.New("禁止对网络基础设施暂停上网")
		}
		if !strings.EqualFold(fmt.Sprint(d["mac"]), r.MAC) {
			return errors.New("设备 MAC 已变化，请刷新后重试")
		}
		p, _ := d["bypassPath"].(map[string]any)
		seen, _ := time.Parse(time.RFC3339, fmt.Sprint(p["observedAt"]))
		if p["state"] != "recent" || seen.IsZero() || now.Sub(seen) > 2*time.Minute || seen.After(now.Add(time.Minute)) {
			return errors.New("没有本节点的新鲜流量交付证据，无法确认控制位置")
		}
		return nil
	}
	return errors.New("设备不在本节点清单内")
}
func controlSnapshot() map[string]any { p := localPeerPayload(); return p.Snapshot }
func enqueueControl(r controlRequest) (controlTask, error) {
	if e := validateControlRequest(r); e != nil {
		return controlTask{}, e
	}
	controls.Lock()
	defer controls.Unlock()
	if !controls.settings.Enabled {
		return controlTask{}, errors.New("本节点控制未启用")
	}
	for _, t := range controls.state.Tasks {
		if t.Request.ID == r.ID {
			if t.Request != r {
				return controlTask{}, errors.New("任务 ID 与已有请求冲突")
			}
			return t, nil
		}
	}
	pending := 0
	for _, t := range controls.state.Tasks {
		if t.State == "queued" || t.State == "running" {
			pending++
		}
	}
	if pending >= 8 {
		return controlTask{}, errors.New("任务队列已满")
	}
	t := controlTask{Request: r, State: "queued", At: time.Now(), Output: "已接收，等待本节点执行"}
	before := append([]controlTask{}, controls.state.Tasks...)
	if len(controls.state.Tasks) >= 100 {
		if controls.state.Tasks[0].State == "queued" || controls.state.Tasks[0].State == "running" {
			return t, errors.New("历史记录忙，请稍后重试")
		}
		controls.state.Tasks = controls.state.Tasks[1:]
	}
	controls.state.Tasks = append(controls.state.Tasks, t)
	if e := saveControlLocked(); e != nil {
		controls.state.Tasks = before
		return controlTask{}, e
	}
	select {
	case controls.wake <- struct{}{}:
	default:
	}
	return t, nil
}
func controlSummary() map[string]any {
	controls.Lock()
	defer controls.Unlock()
	// Serialize under lock: callers must not race reconciliation of policies.
	b, _ := json.Marshal(controls.state)
	var state controlState
	json.Unmarshal(b, &state)
	manager := controls.settings.ManagerID
	return map[string]any{"nodeID": localNodeID(), "managerID": manager, "enabled": controls.settings.Enabled, "canManage": manager == localNodeID(), "state": state, "error": controls.err, "scope": "仅本节点收到的 IPv4 公网请求；保留内网访问；不保证绕行、IPv6、硬件卸载路径。策略由本节点定时执行，软件停止后临时阻断最多约 45 秒失效。", "services": []string{"nlbwmon", "avahi-daemon"}}
}
func peerControlHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		reply(w, 405, map[string]string{"error": "POST required"})
		return
	}
	controls.Lock()
	cfg := controls.settings
	controls.Unlock()
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !cfg.Enabled || len(cfg.Token) != 64 || len(token) != len(cfg.Token) || subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1 || r.Header.Get("X-HND-Manager") != cfg.ManagerID {
		reply(w, 401, map[string]string{"error": "独立控制授权无效"})
		return
	}
	var req controlRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		reply(w, 400, map[string]string{"error": "无效控制请求"})
		return
	}
	if req.Kind == "status" {
		reply(w, 200, controlSummary())
		return
	}
	task, e := enqueueControl(req)
	if e != nil {
		reply(w, 400, map[string]string{"error": e.Error()})
		return
	}
	reply(w, 202, task)
}
func remoteControl(ctx context.Context, id string, req controlRequest) (any, error) {
	peers.RLock()
	var target peerTarget
	for _, p := range peers.config.Peers {
		if p.ID == id {
			target = p
		}
	}
	peers.RUnlock()
	controls.Lock()
	token := controls.settings.PeerTokens[id]
	manager := controls.settings.ManagerID
	controls.Unlock()
	if target.ID == "" || validatePeer(target) != nil || len(token) != 64 {
		return nil, errors.New("节点未配置独立控制授权")
	}
	client, closeClient := pinnedPeerClient(target)
	defer closeClient()
	client.Timeout = 12 * time.Second
	b, _ := json.Marshal(req)
	r, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(target.URL, "/")+"/control", bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-HND-Manager", manager)
	r.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(r)
	if e != nil {
		return nil, errors.New("节点未确认收到；结果未知，请先查询任务记录，勿盲目重试")
	}
	defer resp.Body.Close()
	b, e = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if e != nil {
		return nil, e
	}
	var result any
	if e = json.Unmarshal(b, &result); e != nil {
		return nil, e
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("节点拒绝：%s", b)
	}
	return result, nil
}
func controlAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req controlRequest
	switch r.Method {
	case "GET":
		req.Kind = "status"
		req.Node = r.URL.Query().Get("node")
	case "POST":
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
			reply(w, 400, map[string]string{"error": "请求无效"})
			return
		}
		if e := validateControlRequest(req); e != nil {
			reply(w, 400, map[string]string{"error": e.Error()})
			return
		}
	default:
		reply(w, 405, map[string]string{"error": "GET / POST required"})
		return
	}
	controls.Lock()
	cfg := controls.settings
	controls.Unlock()
	if req.Kind != "status" && (!cfg.Enabled || cfg.ManagerID != localNodeID()) {
		reply(w, 403, map[string]string{"error": "请从指定管理中心执行操作；本节点无管理权"})
		return
	}
	if req.Node != "" && req.Node != localNodeID() {
		if !cfg.Enabled || cfg.ManagerID != localNodeID() {
			reply(w, 403, map[string]string{"error": "请从管理中心查看远端控制状态"})
			return
		}
		v, e := remoteControl(r.Context(), req.Node, req)
		if e != nil {
			reply(w, 502, map[string]string{"error": e.Error()})
			return
		}
		reply(w, 200, v)
		return
	}
	if req.Kind == "status" {
		reply(w, 200, controlSummary())
		return
	}
	t, e := enqueueControl(req)
	if e != nil {
		reply(w, 400, map[string]string{"error": e.Error()})
		return
	}
	reply(w, 202, t)
}
func newControlID() string { b := make([]byte, 16); rand.Read(b); return hex.EncodeToString(b) }
