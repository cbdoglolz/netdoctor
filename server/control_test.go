package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlRequestAllowlist(t *testing.T) {
	valid := []controlRequest{{ID: "task-123456", Kind: "pause", IP: "192.168.2.136", MAC: "02:11:22:33:44:55", Minutes: 1}, {ID: "task-123456", Kind: "schedule", IP: "192.168.2.136", MAC: "02:11:22:33:44:55", Start: "22:00", End: "07:00"}, {ID: "task-123456", Kind: "diagnose", Target: "1.1.1.1"}, {ID: "task-123456", Kind: "dns", Target: "example.com"}, {ID: "task-123456", Kind: "service-restart", Service: "nlbwmon"}}
	for _, r := range valid {
		if e := validateControlRequest(r); e != nil {
			t.Fatal(r, e)
		}
	}
	invalid := []controlRequest{{ID: "task-123456", Kind: "shell", Target: "reboot"}, {ID: "task-123456", Kind: "service-restart", Service: "network"}, {ID: "task-123456", Kind: "service-status", Service: "../../bin/sh"}, {ID: "task-123456", Kind: "diagnose", Target: "1.1.1.1;reboot"}, {ID: "task-123456", Kind: "diagnose", Target: "::1"}, {ID: "task-123456", Kind: "dns", Target: "-q;reboot"}, {ID: "task-123456", Kind: "pause", IP: "192.168.2.136", MAC: "ff:ff:ff:ff:ff:ff", Minutes: 1}, {ID: "task-123456", Kind: "pause", IP: "192.168.2.136", MAC: "02:11:22:33:44:55", Minutes: 0}, {ID: "task-123456", Kind: "schedule", IP: "192.168.2.136", MAC: "02:11:22:33:44:55", Start: "22:00", End: "22:00"}}
	for _, r := range invalid {
		if e := validateControlRequest(r); e == nil {
			t.Fatal("unexpected acceptance", r)
		}
	}
}
func TestDailyAndTemporaryPolicyBoundaries(t *testing.T) {
	p := accessPolicy{Enabled: true, Start: "22:00", End: "07:00"}
	for _, c := range []struct {
		at     string
		active bool
	}{{"2026-11-01T21:59:59+08:00", false}, {"2026-11-01T22:00:00+08:00", true}, {"2026-11-02T06:59:59+08:00", true}, {"2026-11-02T07:00:00+08:00", false}} {
		at, _ := time.Parse(time.RFC3339, c.at)
		if policyActive(p, at) != c.active {
			t.Fatal(c)
		}
	}
	now := time.Now()
	p.Start = ""
	p.Until = now.Add(time.Second)
	if !policyActive(p, now) || policyActive(p, p.Until) {
		t.Fatal("expiry boundary")
	}
	p.Enabled = false
	if policyActive(p, now) {
		t.Fatal("disabled policy active")
	}
}
func TestControlRequiresFreshLocalEvidenceAndProtectsInfrastructure(t *testing.T) {
	now := time.Now()
	r := controlRequest{IP: "192.168.2.136", MAC: "02:11:22:33:44:55"}
	d := map[string]any{"ip": r.IP, "mac": r.MAC, "kind": "phone", "bypassPath": map[string]any{"state": "recent", "observedAt": now.Format(time.RFC3339)}}
	s := map[string]any{"observedAt": now.Format(time.RFC3339), "devices": []any{d}}
	if e := validateControlDevice(r, s, now); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"gateway", "router", "switch", "ap"} {
		d["kind"] = kind
		if validateControlDevice(r, s, now) == nil {
			t.Fatal("infrastructure accepted", kind)
		}
	}
	d["kind"] = "phone"
	d["mac"] = "02:11:22:33:44:99"
	if validateControlDevice(r, s, now) == nil {
		t.Fatal("reused ip accepted")
	}
	d["mac"] = r.MAC
	d["bypassPath"] = nil
	if validateControlDevice(r, s, now) == nil {
		t.Fatal("remote/missing proof accepted")
	}
	d["bypassPath"] = map[string]any{"state": "recent", "observedAt": now.Add(-3 * time.Minute).Format(time.RFC3339)}
	if validateControlDevice(r, s, now) == nil {
		t.Fatal("old receipt accepted")
	}
}
func setupControlTest(t *testing.T) {
	t.Helper()
	controls.Lock()
	oldDir, oldCfg, oldState, oldErr := controls.dir, controls.settings, controls.state, controls.err
	controls.Unlock()
	t.Cleanup(func() {
		controls.Lock()
		controls.dir, controls.settings, controls.state, controls.err = oldDir, oldCfg, oldState, oldErr
		controls.Unlock()
	})
	if e := initControl(t.TempDir()); e != nil {
		t.Fatal(e)
	}
}
func TestControlDurabilityIdempotencyAndRestartNoReplay(t *testing.T) {
	setupControlTest(t)
	controls.Lock()
	controls.settings.Enabled = true
	controls.Unlock()
	r := controlRequest{ID: "testtask1234", Kind: "service-restart", Service: "nlbwmon"}
	a, e := enqueueControl(r)
	if e != nil {
		t.Fatal(e)
	}
	b, e := enqueueControl(r)
	if e != nil || a.Request.ID != b.Request.ID || len(controls.state.Tasks) != 1 {
		t.Fatal("duplicate task", e)
	}
	r.Service = "avahi-daemon"
	if _, e = enqueueControl(r); e == nil {
		t.Fatal("conflicting id accepted")
	}
	dir := controls.dir
	if e = initControl(dir); e != nil {
		t.Fatal(e)
	}
	if controls.state.Tasks[0].State != "interrupted" {
		t.Fatal("unsafe replay after restart")
	}
	controls.Lock()
	controls.settings.Enabled = true
	controls.dir = filepath.Join(dir, "missing")
	controls.Unlock()
	n := len(controls.state.Tasks)
	r.ID = "secondtask123"
	if _, e = enqueueControl(r); e == nil || len(controls.state.Tasks) != n {
		t.Fatal("unpersisted task accepted")
	}
}
func TestReadonlyTokenCannotControl(t *testing.T) {
	setupControlTest(t)
	controls.Lock()
	controls.settings = controlSettings{Enabled: true, Token: strings.Repeat("a", 64), ManagerID: strings.Repeat("b", 32)}
	controls.Unlock()
	for _, c := range []struct {
		token, manager string
		code           int
	}{{strings.Repeat("c", 64), strings.Repeat("b", 32), 401}, {strings.Repeat("a", 64), strings.Repeat("c", 32), 401}, {strings.Repeat("a", 64), strings.Repeat("b", 32), 200}} {
		r := httptest.NewRequest("POST", "https://local/control", strings.NewReader(`{"kind":"status"}`))
		r.Header.Set("Authorization", "Bearer "+c.token)
		r.Header.Set("X-HND-Manager", c.manager)
		w := httptest.NewRecorder()
		peerControlHandler(w, r)
		if w.Code != c.code {
			t.Fatal(w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), strings.Repeat("a", 64)) {
			t.Fatal("secret exposed")
		}
	}
}
func TestNftControlRulesAreScopedAndExpiring(t *testing.T) {
	ps := []accessPolicy{{ID: "testtask1234", IP: "192.168.2.136", MAC: "02:11:22:33:44:55"}}
	s := controlTableScript(ps, "00:11:22:33:44:66", true)
	for _, want := range []string{"delete table netdev hnd_control", "priority -500", "flags timeout", "ether daddr", "ip saddr . ether saddr", "192.168.0.0/16", "counter name"} {
		if !strings.Contains(s, want) {
			t.Fatal(want)
		}
	}
	if strings.Contains(s, "flush ruleset") || strings.Contains(s, "table inet fw4") {
		t.Fatal("touches global firewall")
	}
}
func TestControlSettingsDisabledByDefault(t *testing.T) {
	setupControlTest(t)
	if controls.settings.Enabled {
		t.Fatal("control enabled by default")
	}
	b, e := os.ReadFile(filepath.Join(controls.dir, "control-state.json"))
	if e != nil {
		t.Fatal(e)
	}
	var v controlState
	if json.Unmarshal(b, &v) != nil {
		t.Fatal("invalid persistent state")
	}
}
