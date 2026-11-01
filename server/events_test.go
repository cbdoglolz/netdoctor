package main

import (
	"testing"
	"time"
)

func TestPausedEventPersistsAndRequiresSuccessfulNewProbe(t *testing.T) {
	e, err := openEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.db.Close()
	c := defaultConfig()
	c.Devices = []deviceConfig{{IP: "192.0.2.4", Name: "test", Key: true}}
	ids := map[string]string{"192.0.2.4": "device"}
	p := map[string]probeResult{"192.0.2.4": {Failures: 3, At: time.Now()}}
	first := time.Now().Format(time.RFC3339)
	a := e.eventHistory(c, p, ids, "", first)
	if len(a) != 1 {
		t.Fatal(a)
	}
	c.Devices[0].MonitorDisabled = true
	a = e.eventHistory(c, p, ids, "", first)
	if a[0].(map[string]any)["paused"] != true {
		t.Fatal(a)
	}
	c.Devices[0].MonitorDisabled = false
	a = e.eventHistory(c, map[string]probeResult{}, ids, "", first)
	if a[0].(map[string]any)["recovered"] == true {
		t.Fatal("resume incorrectly marked recovered")
	}
	p["192.0.2.4"] = probeResult{Success: true, At: time.Now()}
	a = e.eventHistory(c, p, ids, "", first)
	if a[0].(map[string]any)["recovered"] != true {
		t.Fatal(a)
	}
}
