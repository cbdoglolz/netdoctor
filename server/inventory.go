package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os/exec"
	"sync/atomic"
	"time"
)

var scanQueue atomic.Bool
var scanBusy atomic.Bool
var scanStatus atomic.Value
var scanLastError atomic.Value

func scanText(v *atomic.Value) string {
	if s := v.Load(); s != nil {
		return s.(string)
	}
	return "尚未发起手动扫描"
}

func (e *runtimeEngine) inventoryMeta(devices []any, byIP map[string]map[string]any, stamp string) []any {
	out := []any{}
	writable := diskHasReserve(e.dir)
	for _, v := range devices {
		d := v.(map[string]any)
		ip := d["ip"].(string)
		if writable {
			e.db.Exec("INSERT OR IGNORE INTO device_meta(ip,first_seen,last_scan,marked_at,hidden) VALUES(?,?, '', '',0)", ip, stamp)
		}
		if _, found := e.discovered[ip]; found && writable {
			e.db.Exec("UPDATE device_meta SET last_scan=? WHERE ip=? AND COALESCE(last_scan,'')<>?", e.discoveredAt.Format(time.RFC3339), ip, e.discoveredAt.Format(time.RFC3339))
		}
		var first, scan, marked string
		var hidden int
		e.db.QueryRow("SELECT COALESCE(first_seen,''),COALESCE(last_scan,''),COALESCE(marked_at,''),hidden FROM device_meta WHERE ip=?", ip).Scan(&first, &scan, &marked, &hidden)
		if hidden == 1 && ip != localIP() {
			delete(byIP, ip)
			continue
		}
		d["firstRecordedAt"], d["lastScannedAt"], d["annotatedAt"] = first, scan, marked
		out = append(out, d)
	}
	return out
}
func inventoryAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		rows, err := engine.db.Query("SELECT ip FROM device_meta WHERE hidden=1 ORDER BY ip")
		if err != nil {
			reply(w, 500, map[string]string{"error": "读取失败"})
			return
		}
		defer rows.Close()
		hidden := []string{}
		for rows.Next() {
			var ip string
			rows.Scan(&ip)
			hidden = append(hidden, ip)
		}
		reply(w, 200, map[string]any{"hidden": hidden, "scanning": scanBusy.Load(), "status": scanText(&scanStatus), "error": scanText(&scanLastError)})
		return
	}
	var q struct {
		IP      string `json:"ip"`
		Restore bool   `json:"restore"`
		Scan    bool   `json:"scan"`
	}
	if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&q) != nil {
		reply(w, 400, map[string]string{"error": "请求格式无效"})
		return
	}
	if q.Scan {
		if _, err := exec.LookPath("arp-scan"); err != nil {
			reply(w, 503, map[string]string{"error": "未安装 arp-scan，无法重新扫描"})
			return
		}
		if !scanBusy.CompareAndSwap(false, true) {
			reply(w, 409, map[string]string{"error": "扫描已在队列中或正在进行"})
			return
		}
		scanStatus.Store("等待后台扫描")
		scanQueue.Store(true)
		reply(w, 202, map[string]string{"message": "扫描已排队，最多等待一个采集周期；保留手动备注，已删除设备保持隐藏"})
		return
	}
	if q.IP == "" || q.IP == localIP() || q.IP == "node-internet" {
		reply(w, 400, map[string]string{"error": "不能删除本机或互联网根节点"})
		return
	}
	var exists int
	if engine.db.QueryRow("SELECT COUNT(*) FROM device_meta WHERE ip=?", q.IP).Scan(&exists) != nil || exists == 0 {
		reply(w, 404, map[string]string{"error": "设备记录不存在"})
		return
	}
	hidden := 1
	if q.Restore {
		hidden = 0
	}
	if _, err := engine.db.Exec("UPDATE device_meta SET hidden=? WHERE ip=?", hidden, q.IP); err != nil {
		reply(w, 500, map[string]string{"error": "保存失败"})
		return
	}
	reply(w, 200, map[string]string{"message": "已保存，下一轮采集生效；删除仅隐藏记录，不改设备网络，备注和历史可恢复"})
}

type layoutPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func validLayout(p map[string]layoutPoint) bool {
	if p == nil || len(p) > 512 {
		return false
	}
	for k, v := range p {
		if len(k) > 160 || math.IsNaN(v.X) || math.IsNaN(v.Y) || math.IsInf(v.X, 0) || math.IsInf(v.Y, 0) || math.Abs(v.X) > 100000 || math.Abs(v.Y) > 100000 {
			return false
		}
	}
	return true
}
func layoutAPI(w http.ResponseWriter, r *http.Request) {
	layer := r.URL.Query().Get("layer")
	if layer != "gateway" && layer != "physical" {
		reply(w, 400, map[string]string{"error": "布局层无效"})
		return
	}
	if r.Method == "GET" {
		if p, ok := replicaLayout(layer); ok {
			reply(w, 200, p)
			return
		}
		var b string
		if engine.db.QueryRow("SELECT payload FROM ui_layout WHERE id=?", layer).Scan(&b) != nil {
			reply(w, 200, map[string]any{})
			return
		}
		var p map[string]layoutPoint
		json.Unmarshal([]byte(b), &p)
		reply(w, 200, p)
		return
	}
	if r.Method != "POST" {
		reply(w, 405, map[string]string{"error": "POST required"})
		return
	}
	if catalogReplica() {
		reply(w, 409, map[string]string{"error": "布局由管理中心统一维护，请到管理中心修改"})
		return
	}
	var p map[string]layoutPoint
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 96<<10)).Decode(&p) != nil || !validLayout(p) {
		reply(w, 400, map[string]string{"error": "布局坐标无效或超限"})
		return
	}
	b, _ := json.Marshal(p)
	if _, err := engine.db.Exec("INSERT OR REPLACE INTO ui_layout VALUES(?,?)", layer, string(b)); err != nil {
		reply(w, 500, map[string]string{"error": "保存失败"})
		return
	}
	msg := syncCatalogOnce()
	if msg == "" {
		msg = fmt.Sprintf("已保存 %d 个节点的位置，电脑与手机共用", len(p))
	}
	reply(w, 200, map[string]string{"message": msg})
}
