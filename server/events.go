package main

import (
	"encoding/json"
	"net/http"
	"time"
)

func (e *runtimeEngine) eventHistory(c runtimeConfig, probes map[string]probeResult, ids map[string]string, storage, stamp string) []any {
	current := diagnosticAlerts(c, probes, ids, storage, stamp)
	saved := map[string]map[string]any{}
	rows, err := e.db.Query("SELECT id,payload FROM alert_records")
	if err == nil {
		for rows.Next() {
			var id, b string
			rows.Scan(&id, &b)
			var a map[string]any
			if json.Unmarshal([]byte(b), &a) == nil {
				saved[id] = a
			}
		}
		rows.Close()
	}
	for _, v := range current {
		a := v.(map[string]any)
		id := a["id"].(string)
		if old := saved[id]; old != nil && old["recovered"] == true {
			archive := id + "@" + old["observedAt"].(string)
			old["id"] = archive
			saved[archive] = old
		}
		if old := saved[id]; old != nil && old["recovered"] != true {
			a["observedAt"] = old["observedAt"]
		}
		a["lastSeenAt"] = stamp
		saved[id] = a
	}
	for _, d := range c.Devices {
		if ids[d.IP] == "" {
			continue
		}
		id := "probe-" + ids[d.IP]
		a := saved[id]
		if a == nil {
			continue
		}
		a["paused"] = d.MonitorDisabled || d.Passive
		if !d.MonitorDisabled && !d.Passive {
			if p, ok := probes[d.IP]; ok && p.Success && a["recovered"] != true {
				a["recovered"] = true
				a["resolvedAt"] = p.At.Format(time.RFC3339)
			}
		}
	}
	out := []any{}
	for id, a := range saved {
		out = append(out, a)
		b, _ := json.Marshal(a)
		e.db.Exec("INSERT OR REPLACE INTO alert_records VALUES(?,?)", id, string(b))
	}
	e.db.Exec("DELETE FROM alert_records WHERE id NOT IN (SELECT id FROM alert_records ORDER BY json_extract(payload,'$.observedAt') DESC LIMIT 200)")
	return out
}
func (e *runtimeEngine) recordScan(previous, current map[string]string, stamp string) {
	changes := []map[string]string{}
	if previous == nil {
		var b string
		if e.db.QueryRow("SELECT payload FROM scan_baseline WHERE id=1").Scan(&b) == nil {
			json.Unmarshal([]byte(b), &previous)
		}
	}
	if v := scanLastError.Load(); v != nil && v.(string) != "" {
		b, _ := json.Marshal([]map[string]string{{"kind": "扫描异常：" + v.(string)}})
		e.db.Exec("INSERT OR REPLACE INTO scan_batches VALUES(?,?)", stamp, string(b))
		return
	}
	baseline, _ := json.Marshal(current)
	e.db.Exec("INSERT OR REPLACE INTO scan_baseline VALUES(1,?)", string(baseline))
	for ip, mac := range current {
		kind := ""
		if previous == nil {
			kind = "基线记录"
		} else if old, ok := previous[ip]; !ok {
			kind = "新发现"
			for oldIP, oldMAC := range previous {
				if oldIP != ip && oldMAC == mac {
					kind = "同 MAC 地址变化（待确认）"
				}
			}
		} else if old != mac {
			kind = "MAC 变化"
		}
		if kind != "" {
			changes = append(changes, map[string]string{"kind": kind, "ip": ip, "mac": mac})
		}
	}
	for ip, mac := range previous {
		if _, ok := current[ip]; !ok {
			changes = append(changes, map[string]string{"kind": "本轮未响应（不代表离线）", "ip": ip, "mac": mac})
		}
	}
	b, _ := json.Marshal(changes)
	e.db.Exec("INSERT OR REPLACE INTO scan_batches VALUES(?,?)", stamp, string(b))
	e.db.Exec("DELETE FROM scan_batches WHERE ts NOT IN(SELECT ts FROM scan_batches ORDER BY ts DESC LIMIT 20)")
}
func changesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		reply(w, 405, map[string]string{"error": "GET required"})
		return
	}
	rows, err := engine.db.Query("SELECT ts,payload FROM scan_batches ORDER BY ts DESC LIMIT 20")
	if err != nil {
		reply(w, 500, map[string]string{"error": "读取失败"})
		return
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var ts, b string
		rows.Scan(&ts, &b)
		var changes any
		json.Unmarshal([]byte(b), &changes)
		out = append(out, map[string]any{"time": ts, "changes": changes})
	}
	reply(w, 200, out)
}
