package main

import "fmt"

// These rules explain observed local evidence; they never claim an Internet outage
// or infer a physical cable fault from ICMP silence.
func diagnosticAlerts(c runtimeConfig, probes map[string]probeResult, ids map[string]string, storageError, stamp string) []any {
	alerts := []any{}
	for _, d := range c.Devices {
		p, ok := probes[d.IP]
		if !d.Key || d.Passive || !ok || p.Failures < 3 {
			continue
		}
		alerts = append(alerts, map[string]any{
			"id": "probe-" + ids[d.IP], "title": d.Name + "连续探测无响应", "severity": "info",
			"summary":     "仅表示本机 ICMP 探测没有收到回复，不能据此确认离线或全网故障。",
			"cause":       "可能为设备休眠、ICMP 限制、地址变化或路径不可达，需要进一步核实。",
			"evidence":    []string{fmt.Sprintf("连续 %d 轮、每轮 2 个 ICMP 请求无响应", p.Failures), "最近探测：" + p.At.Format("2006-01-02 15:04:05")},
			"suggestions": []string{"确认设备仍使用该地址并连接家庭网络", "唤醒设备后检查；对禁止探测的设备启用被动监测", "结合网关与 AP 管理记录排查，不自动改动网络配置"},
			"deviceIds":   []string{ids[d.IP]}, "observedAt": stamp, "source": "后台 ICMP 规则", "availability": "available", "recovered": false,
		})
	}
	if storageError != "" {
		alerts = append(alerts, map[string]any{"id": "storage", "title": "历史写入受限", "severity": "warning", "summary": storageError, "cause": "历史存储预算、磁盘余量或数据库写入异常。", "evidence": []string{storageError}, "suggestions": []string{"检查设备可用磁盘和存储预算；已有历史保留，不自动删除网络配置"}, "deviceIds": []string{}, "observedAt": stamp, "source": "本机存储检查", "availability": "available", "recovered": false})
	}
	return alerts
}
