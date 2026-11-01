package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"strings"
	"time"
)

func validateIcon(value string) error {
	if value == "" {
		return nil
	}
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 || (parts[0] != "data:image/png;base64" && parts[0] != "data:image/jpeg;base64") {
		return errors.New("仅支持 PNG / JPEG 图标")
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || len(data) > 48*1024 {
		return errors.New("图标不能超过 48 KiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width > 512 || cfg.Height > 512 || (format != "png" && format != "jpeg") {
		return errors.New("图标内容无效或尺寸超过 512 像素")
	}
	return nil
}

func deviceAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		reply(w, 405, map[string]string{"error": "POST required"})
		return
	}
	var d deviceConfig
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 96<<10)).Decode(&d) != nil {
		reply(w, 400, map[string]string{"error": "设备标注无效"})
		return
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	c := engine.config
	c.Devices = append([]deviceConfig{}, c.Devices...)
	found := false
	for i, v := range c.Devices {
		if v.IP == d.IP {
			c.Devices[i] = d
			found = true
			break
		}
	}
	if !found {
		c.Devices = append(c.Devices, d)
	}
	if err := validateConfig(c); err != nil {
		reply(w, 400, map[string]string{"error": err.Error()})
		return
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	if len(data) > 1<<20 {
		reply(w, 400, map[string]string{"error": "配置图标总容量超过 1 MiB"})
		return
	}
	path := base + "/config/config.json"
	if err := os.WriteFile(path+".new", data, 0600); err != nil {
		reply(w, 500, map[string]string{"error": "保存失败"})
		return
	}
	if err := os.Rename(path+".new", path); err != nil {
		reply(w, 500, map[string]string{"error": "保存失败"})
		return
	}
	for _, old := range engine.config.Devices {
		if old.IP == d.IP && old.MonitorDisabled && !d.MonitorDisabled {
			engine.probeMu.Lock()
			delete(engine.probes, d.IP)
			if engine.monitorEpoch == nil {
				engine.monitorEpoch = map[string]uint64{}
			}
			engine.monitorEpoch[d.IP]++
			engine.probeMu.Unlock()
		}
	}
	engine.config = c
	stamp := time.Now().Format(time.RFC3339)
	if _, err := engine.db.Exec("INSERT INTO device_meta(ip,first_seen,last_scan,marked_at,hidden) VALUES(?,?,'',?,0) ON CONFLICT(ip) DO UPDATE SET marked_at=excluded.marked_at", d.IP, stamp, stamp); err != nil {
		reply(w, 500, map[string]string{"error": "备注已保存，但更新时间写入失败"})
		return
	}
	reply(w, 200, map[string]string{"message": "设备标注已保存，下一轮采集生效"})
}
