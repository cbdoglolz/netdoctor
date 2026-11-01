package main

// Public device metadata has one authority. Replicas persist it independently
// of the observer's ephemeral peer cache; no routing or node secrets are copied.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type catalogPeer struct {
	peerTarget
	SyncToken string `json:"syncToken"`
}
type catalogSettings struct {
	Enabled    bool          `json:"enabled"`
	ManagerID  string        `json:"managerID"`
	ManagerURL string        `json:"managerURL"`
	Token      string        `json:"token"`
	Peers      []catalogPeer `json:"peers"`
}
type catalogDevice struct {
	peerAnnotation
	MAC     string `json:"mac,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}
type catalogContent struct {
	Devices []catalogDevice                   `json:"devices"`
	Layouts map[string]map[string]layoutPoint `json:"layouts"`
}
type catalogDocument struct {
	Schema    int            `json:"schema"`
	ManagerID string         `json:"managerID"`
	Revision  uint64         `json:"revision"`
	Hash      string         `json:"hash"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Content   catalogContent `json:"content"`
}
type catalogAck struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Revision uint64    `json:"revision"`
	Hash     string    `json:"hash"`
	At       time.Time `json:"at"`
	Error    string    `json:"error,omitempty"`
}

var catalog = struct {
	sync.RWMutex
	settings  catalogSettings
	doc       catalogDocument
	dir       string
	savedAt   time.Time
	contactAt time.Time
	err       string
	acks      []catalogAck
}{acks: []catalogAck{}}
var catalogRound sync.Mutex

func durableCatalogFile(path string, b []byte) error {
	f, e := os.OpenFile(path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = os.Rename(path+".tmp", path); e != nil {
		return e
	}
	if runtime.GOOS != "windows" {
		dir, e := os.Open(filepath.Dir(path))
		if e != nil {
			return e
		}
		defer dir.Close()
		return dir.Sync()
	}
	return nil
}
func catalogHash(c catalogContent) string {
	b, _ := json.Marshal(c)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func validateCatalog(d catalogDocument, manager string) error {
	if d.Schema != 1 || d.ManagerID != manager || d.Revision == 0 || d.Hash != catalogHash(d.Content) || d.UpdatedAt.IsZero() {
		return errors.New("公共资料来源、版本或摘要无效")
	}
	if len(d.Content.Devices) > 512 || len(d.Content.Layouts) > 2 {
		return errors.New("公共资料超过上限")
	}
	ips := map[string]bool{}
	for _, v := range d.Content.Devices {
		if ips[v.IP] {
			return errors.New("公共设备地址重复")
		}
		ips[v.IP] = true
		c := defaultConfig()
		c.Devices = []deviceConfig{{IP: v.IP, Name: v.Name, Kind: v.Kind, Model: v.Model, Group: v.Group, Icon: v.Icon, CustomIcon: v.CustomIcon}}
		if e := validateConfig(c); e != nil {
			return e
		}
		if v.MAC != "" {
			m, e := net.ParseMAC(v.MAC)
			if e != nil || len(m) != 6 {
				return errors.New("公共设备 MAC 无效")
			}
		}
	}
	for k, p := range d.Content.Layouts {
		if (k != "gateway" && k != "physical") || !validLayout(p) {
			return errors.New("公共布局无效")
		}
	}
	b, _ := json.Marshal(d)
	if len(b) > 2<<20 {
		return errors.New("公共资料超过 2 MiB")
	}
	return nil
}
func initCatalog(dir string) error {
	var cfg catalogSettings
	b, e := os.ReadFile(filepath.Join(dir, "catalog-settings.json"))
	if e == nil {
		if e = json.Unmarshal(b, &cfg); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if cfg.Enabled {
		if len(cfg.ManagerID) != 32 || len(cfg.Token) != 64 || len(cfg.Peers) > 8 {
			return errors.New("公共资料同步授权无效")
		}
		u := strings.TrimSuffix(cfg.ManagerURL, "/")
		if !strings.HasPrefix(u, "http://") || net.ParseIP(strings.Split(strings.TrimPrefix(u, "http://"), ":")[0]) == nil {
			return errors.New("管理中心地址无效")
		}
		for _, p := range cfg.Peers {
			if validatePeer(p.peerTarget) != nil || len(p.SyncToken) != 64 {
				return errors.New("备份节点授权无效")
			}
		}
	}
	var doc catalogDocument
	b, e = os.ReadFile(filepath.Join(dir, "catalog.json"))
	if e == nil {
		if e = json.Unmarshal(b, &doc); e != nil {
			return e
		}
		if e = validateCatalog(doc, cfg.ManagerID); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	catalog.Lock()
	defer catalog.Unlock()
	catalog.settings = cfg
	catalog.dir = dir
	catalog.doc = doc
	catalog.err = ""
	catalog.acks = []catalogAck{}
	if info, e := os.Stat(filepath.Join(dir, "catalog.json")); e == nil {
		catalog.savedAt = info.ModTime()
	}
	return nil
}
func catalogReplica() bool {
	catalog.RLock()
	defer catalog.RUnlock()
	return catalog.settings.Enabled && catalog.settings.ManagerID != localNodeID()
}
func publicAnnotation(d deviceConfig) peerAnnotation {
	return peerAnnotation{d.IP, d.Name, d.Kind, d.Model, d.Group, d.Icon, d.CustomIcon}
}
func annotationPresent(d deviceConfig) bool {
	return d.Name != "" || d.Icon != "" || d.CustomIcon != "" || d.Group != "" || d.Model != ""
}
func applyPublicConfig(d *deviceConfig, n peerAnnotation) {
	d.Name = n.Name
	d.Kind = n.Kind
	d.Model = n.Model
	d.Group = n.Group
	d.Icon = n.Icon
	d.CustomIcon = n.CustomIcon
}
func overlayCatalogConfig(c *runtimeConfig) {
	if !catalogReplica() {
		return
	}
	catalog.RLock()
	notes := append([]catalogDevice{}, catalog.doc.Content.Devices...)
	catalog.RUnlock()
	for _, n := range notes {
		found := false
		for i := range c.Devices {
			if c.Devices[i].IP == n.IP {
				applyPublicConfig(&c.Devices[i], n.peerAnnotation)
				found = true
				break
			}
		}
		if !found {
			d := deviceConfig{IP: n.IP}
			applyPublicConfig(&d, n.peerAnnotation)
			c.Devices = append(c.Devices, d)
		}
	}
}
func catalogDeviceWriteAllowed(d deviceConfig) error {
	if !catalogReplica() {
		return nil
	}
	c := runtimeConfig{Devices: []deviceConfig{d}}
	overlayCatalogConfig(&c)
	if publicAnnotation(c.Devices[0]) != publicAnnotation(d) {
		return errors.New("公共资料由管理中心统一维护，请到管理中心修改；本机监测设置仍可单独保存")
	}
	// A replica cannot invent a new public record outside the catalog either.
	catalog.RLock()
	defer catalog.RUnlock()
	for _, n := range catalog.doc.Content.Devices {
		if n.IP == d.IP {
			return nil
		}
	}
	if annotationPresent(d) {
		return errors.New("请到管理中心新增公共设备资料")
	}
	return nil
}
func overlayCatalogSnapshot(snapshot map[string]any) {
	if !catalogReplica() {
		return
	}
	catalog.RLock()
	notes := append([]catalogDevice{}, catalog.doc.Content.Devices...)
	revision := catalog.doc.Revision
	catalog.RUnlock()
	ds, _ := snapshot["devices"].([]any)
	for _, raw := range ds {
		d, _ := raw.(map[string]any)
		if d["id"] == "doctor" && (d["mac"] == nil || d["mac"] == "") {
			if iface, e := net.InterfaceByName("br-lan"); e == nil {
				d["mac"] = strings.ToLower(iface.HardwareAddr.String())
			}
		}
		for _, n := range notes {
			if d["ip"] != n.IP {
				continue
			}
			mac := strings.ToLower(fmt.Sprint(d["mac"]))
			if n.MAC != "" && mac != strings.ToLower(n.MAC) {
				d["catalogWarning"] = "公共资料 MAC 与本机记录不符，未套用"
				continue
			}
			d["name"] = n.Name
			if n.Name == "" {
				d["name"] = "设备 " + n.IP
			}
			d["kind"], d["model"], d["group"], d["icon"], d["customIcon"] = n.Kind, n.Model, n.Group, n.Icon, n.CustomIcon
			d["identitySource"] = "管理中心公共资料 · 本机持久副本"
			d["catalogRevision"] = revision
			break
		}
	}
}

// Canonical IP layout keys avoid confusing the local 'doctor' node on .6 with
// the local 'doctor' node on .144. Unknown helper node IDs are namespaced.
func catalogLayoutKey(id string, snapshot map[string]any) string {
	ds, _ := snapshot["devices"].([]any)
	for _, raw := range ds {
		d, _ := raw.(map[string]any)
		if d["id"] == id {
			if ip, ok := d["ip"].(string); ok && net.ParseIP(ip) != nil {
				return "device:" + ip
			}
		}
	}
	return "node:" + id
}
func replicaLayout(layer string) (map[string]layoutPoint, bool) {
	if !catalogReplica() {
		return nil, false
	}
	catalog.RLock()
	p := catalog.doc.Content.Layouts[layer]
	copyP := map[string]layoutPoint{}
	for k, v := range p {
		copyP[k] = v
	}
	catalog.RUnlock()
	snapshot := localPeerPayload().Snapshot
	result := map[string]layoutPoint{}
	ds, _ := snapshot["devices"].([]any)
	for _, raw := range ds {
		d, _ := raw.(map[string]any)
		id, _ := d["id"].(string)
		if v, ok := copyP[catalogLayoutKey(id, snapshot)]; ok {
			result[id] = v
		}
	}
	for k, v := range copyP {
		if strings.HasPrefix(k, "node:") {
			result[strings.TrimPrefix(k, "node:")] = v
		}
	}
	return result, true
}
func buildCatalogContent() catalogContent {
	engine.mu.RLock()
	devices := append([]deviceConfig{}, engine.config.Devices...)
	engine.mu.RUnlock()
	snapshot := localPeerPayload().Snapshot
	ds, _ := snapshot["devices"].([]any)
	macs := map[string]string{}
	for _, raw := range ds {
		d, _ := raw.(map[string]any)
		ip, _ := d["ip"].(string)
		m, _ := d["mac"].(string)
		macs[ip] = strings.ToLower(m)
	}
	c := catalogContent{Devices: []catalogDevice{}, Layouts: map[string]map[string]layoutPoint{}}
	catalog.RLock()
	oldNotes := append([]catalogDevice{}, catalog.doc.Content.Devices...)
	catalog.RUnlock()
	oldByIP := map[string]catalogDevice{}
	for _, n := range oldNotes {
		oldByIP[n.IP] = n
	}
	seen := map[string]bool{}
	for _, d := range devices {
		seen[d.IP] = true
		mac := macs[d.IP]
		if mac == "" {
			mac = oldByIP[d.IP].MAC
		}
		c.Devices = append(c.Devices, catalogDevice{publicAnnotation(d), mac, false})
	}
	for _, n := range oldNotes {
		if !seen[n.IP] {
			n.Deleted = true
			n.Name = ""
			n.Model = ""
			n.Group = ""
			n.Icon = ""
			n.CustomIcon = ""
			c.Devices = append(c.Devices, n)
		}
	}
	sort.Slice(c.Devices, func(i, j int) bool { return c.Devices[i].IP < c.Devices[j].IP })
	for _, layer := range []string{"gateway", "physical"} {
		var raw string
		if engine.db.QueryRow("SELECT payload FROM ui_layout WHERE id=?", layer).Scan(&raw) != nil {
			continue
		}
		var p map[string]layoutPoint
		if json.Unmarshal([]byte(raw), &p) != nil {
			continue
		}
		result := map[string]layoutPoint{}
		for k, v := range p {
			result[catalogLayoutKey(k, snapshot)] = v
		}
		c.Layouts[layer] = result
	}
	return c
}
func saveCatalogLocked(d catalogDocument) error {
	b, _ := json.MarshalIndent(d, "", "  ")
	path := filepath.Join(catalog.dir, "catalog.json")
	if catalog.doc.Revision > 0 && catalog.doc.Hash != d.Hash {
		old, _ := json.MarshalIndent(catalog.doc, "", "  ")
		if e := durableCatalogFile(filepath.Join(catalog.dir, fmt.Sprintf("catalog-backup-%02d.json", catalog.doc.Revision%8)), old); e != nil {
			return e
		}
	}
	if e := durableCatalogFile(path, b); e != nil {
		return e
	}
	catalog.doc = d
	catalog.savedAt = time.Now()
	return nil
}

type catalogWire struct {
	Document *catalogDocument `json:"document,omitempty"`
}

func catalogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		reply(w, 405, map[string]string{"error": "POST required"})
		return
	}
	catalog.RLock()
	cfg := catalog.settings
	catalog.RUnlock()
	actual := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !cfg.Enabled || cfg.ManagerID == localNodeID() || len(actual) != 64 || subtle.ConstantTimeCompare([]byte(actual), []byte(cfg.Token)) != 1 || r.Header.Get("X-HND-Manager") != cfg.ManagerID {
		reply(w, 401, map[string]string{"error": "公共资料复制授权无效"})
		return
	}
	var q catalogWire
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&q) != nil {
		reply(w, 400, map[string]string{"error": "复制请求无效"})
		return
	}
	catalog.Lock()
	defer catalog.Unlock()
	if q.Document != nil {
		d := *q.Document
		if e := validateCatalog(d, cfg.ManagerID); e != nil {
			reply(w, 400, map[string]string{"error": e.Error()})
			return
		}
		if d.Revision < catalog.doc.Revision || (d.Revision == catalog.doc.Revision && d.Hash != catalog.doc.Hash) {
			reply(w, 409, map[string]string{"error": "拒绝旧版本或同版本冲突，保留已有副本"})
			return
		}
		if d.Revision != catalog.doc.Revision || d.Hash != catalog.doc.Hash {
			if e := saveCatalogLocked(d); e != nil {
				catalog.err = e.Error()
				reply(w, 500, map[string]string{"error": "备用副本保存失败，未确认备份"})
				return
			}
		}
	}
	catalog.contactAt = time.Now()
	catalog.err = ""
	reply(w, 200, map[string]any{"id": localNodeID(), "revision": catalog.doc.Revision, "hash": catalog.doc.Hash, "savedAt": catalog.savedAt})
}
func catalogExchange(ctx context.Context, p catalogPeer, d *catalogDocument) (catalogAck, error) {
	a := catalogAck{ID: p.ID, Name: p.Name}
	client, closeClient := pinnedPeerClient(p.peerTarget)
	defer closeClient()
	b, _ := json.Marshal(catalogWire{Document: d})
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(p.URL, "/")+"/catalog", bytes.NewReader(b))
	if e != nil {
		return a, e
	}
	req.Header.Set("Authorization", "Bearer "+p.SyncToken)
	req.Header.Set("X-HND-Manager", localNodeID())
	req.Header.Set("Content-Type", "application/json")
	res, e := client.Do(req)
	if e != nil {
		return a, errors.New("备份节点连接失败，尚未确认持久保存")
	}
	defer res.Body.Close()
	b, e = io.ReadAll(io.LimitReader(res.Body, 8192))
	if e != nil {
		return a, e
	}
	if res.StatusCode != 200 {
		return a, fmt.Errorf("备份节点拒绝：%s", b)
	}
	var ack struct {
		ID       string `json:"id"`
		Revision uint64 `json:"revision"`
		Hash     string `json:"hash"`
	}
	if json.Unmarshal(b, &ack) != nil || ack.ID != p.ID {
		return a, errors.New("备份节点身份响应无效")
	}
	a.Revision = ack.Revision
	a.Hash = ack.Hash
	a.At = time.Now()
	return a, nil
}
func syncCatalogOnce() string {
	catalogRound.Lock()
	defer catalogRound.Unlock()
	catalog.RLock()
	cfg := catalog.settings
	catalog.RUnlock()
	if !cfg.Enabled {
		return ""
	}
	if cfg.ManagerID != localNodeID() {
		return "本机监测设置已保存；公共资料由管理中心维护"
	}
	snapshot := localPeerPayload().Snapshot
	at, _ := time.Parse(time.RFC3339, fmt.Sprint(snapshot["observedAt"]))
	if at.IsZero() || time.Since(at) > 2*time.Minute {
		return "本机已保存，等待采集就绪后更新公共副本"
	}
	c := buildCatalogContent()
	hash := catalogHash(c)
	catalog.Lock()
	if hash != catalog.doc.Hash {
		d := catalogDocument{Schema: 1, ManagerID: cfg.ManagerID, Revision: catalog.doc.Revision + 1, Hash: hash, UpdatedAt: time.Now(), Content: c}
		if e := validateCatalog(d, cfg.ManagerID); e != nil {
			catalog.err = e.Error()
			catalog.Unlock()
			return "本机配置已保存，但公共资料构建失败：" + e.Error()
		}
		if e := saveCatalogLocked(d); e != nil {
			catalog.err = e.Error()
			catalog.Unlock()
			return "本机配置已保存，但公共资料持久保存失败：" + e.Error()
		}
	}
	d := catalog.doc
	catalog.Unlock()
	acks := []catalogAck{}
	pending := 0
	for _, p := range cfg.Peers {
		a, e := catalogExchange(context.Background(), p, nil)
		if e == nil && (a.Revision != d.Revision || a.Hash != d.Hash) {
			a, e = catalogExchange(context.Background(), p, &d)
		}
		if e == nil && (a.Revision != d.Revision || a.Hash != d.Hash) {
			e = errors.New("节点返回的备份版本与当前版本不符")
		}
		if e != nil {
			a.Error = e.Error()
			pending++
		}
		acks = append(acks, a)
	}
	catalog.Lock()
	catalog.acks = acks
	catalog.err = ""
	catalog.Unlock()
	if len(cfg.Peers) == 0 {
		return "仅保存于管理中心，尚未配置备用节点"
	}
	if pending > 0 {
		return fmt.Sprintf("仅管理中心已保存；%d 个节点待同步，请查看公共资料同步状态", pending)
	}
	return fmt.Sprintf("已保存并备份至 %d 个节点（公共资料 v%d）", len(cfg.Peers), d.Revision)
}
func runCatalog(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		syncCatalogOnce()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func catalogAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		reply(w, 405, map[string]string{"error": "GET required"})
		return
	}
	catalog.RLock()
	defer catalog.RUnlock()
	cfg := catalog.settings
	manager := cfg.Enabled && cfg.ManagerID == localNodeID()
	acks := append([]catalogAck{}, catalog.acks...)
	all := len(cfg.Peers) > 0 && len(acks) == len(cfg.Peers)
	for _, a := range acks {
		if a.Error != "" || a.Revision != catalog.doc.Revision || a.Hash != catalog.doc.Hash || time.Since(a.At) > 20*time.Second {
			all = false
		}
	}
	contact := !catalog.contactAt.IsZero() && time.Since(catalog.contactAt) < 20*time.Second
	reply(w, 200, map[string]any{"enabled": cfg.Enabled, "isManager": manager, "managerURL": cfg.ManagerURL, "revision": catalog.doc.Revision, "hash": catalog.doc.Hash, "updatedAt": catalog.doc.UpdatedAt, "savedAt": catalog.savedAt, "managerOnline": manager || contact, "lastContact": catalog.contactAt, "replicas": acks, "allBackedUp": all, "error": catalog.err, "devices": len(catalog.doc.Content.Devices), "layouts": len(catalog.doc.Content.Layouts), "scope": "设备备注、类型、型号、分组、图标和已保存布局；不包含路由、DNS、代理、凭据、流量历史和管理权接管。"})
}
