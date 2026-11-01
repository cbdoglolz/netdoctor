package main

import (
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestForwardingEvidenceExcludesLocalAndReplyOnly(t *testing.T) {
	text := "tcp 6 10 ESTABLISHED src=192.0.2.5 dst=1.1.1.1 src=1.1.1.1 dst=192.0.2.5\n" +
		"ipv4 2 tcp 6 10 ESTABLISHED src=192.168.1.20 dst=8.8.8.8 src=8.8.8.8 dst=192.168.1.6\n" +
		"udp 17 10 src=192.168.1.21 dst=192.168.1.6 src=192.168.1.6 dst=192.168.1.21\n" +
		"tcp 6 10 src=192.168.1.6 dst=8.8.8.8 src=8.8.8.8 dst=192.168.1.6"
	got := forwardedClients(text, "192.168.1.6")
	if len(got) != 1 || !got["192.168.1.20"] {
		t.Fatal(got)
	}
}

func TestCustomIconRejectsActiveContent(t *testing.T) {
	for _, v := range []string{"data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>")), "https://example.com/icon.png"} {
		if validateIcon(v) == nil {
			t.Fatal("unsafe/invalid icon accepted")
		}
	}
	if validateIcon("") != nil {
		t.Fatal("empty preset reset rejected")
	}
	c := defaultConfig()
	c.Devices = []deviceConfig{{IP: "192.0.2.1", Kind: "console", Icon: "fan", MonitorDisabled: true}}
	if err := validateConfig(c); err != nil {
		t.Fatal(err)
	}
	p := map[string]probeResult{"192.0.2.1": {Failures: 5}}
	c.Devices[0].Key = true
	if len(diagnosticAlerts(c, p, map[string]string{"192.0.2.1": "id"}, "", "now")) != 0 {
		t.Fatal("disabled monitoring still alerts")
	}
}

func TestAccountingAggregation(t *testing.T) {
	v, err := parseAccounting([]byte(`{"columns":["ip","rx_bytes","tx_bytes"],"data":[["192.0.2.1",100,20],["192.0.2.1",50,5],["invalid",99,99]]}`))
	if err != nil || v["192.0.2.1"].RX != 150 || v["192.0.2.1"].TX != 25 || len(v) != 1 {
		t.Fatalf("bad aggregation %v %v", v, err)
	}
	if _, err = parseAccounting([]byte(`{"columns":["ip"],"data":[]}`)); err == nil {
		t.Fatal("accepted missing counters")
	}
}
func TestPersistentHistoryAndConfiguration(t *testing.T) {
	dir := t.TempDir()
	e, err := openEngine(dir)
	if err != nil {
		t.Fatal(err)
	}
	e.db.Exec("INSERT INTO usage VALUES(?,?,?,?)", time.Now().Unix(), "phone", 100, 50)
	e.db.Close()
	e, err = openEngine(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.db.Close()
	var bytes int
	e.db.QueryRow("SELECT SUM(rx+tx) FROM usage").Scan(&bytes)
	if bytes != 150 {
		t.Fatal("history lost on restart")
	}
	if validateConfig(runtimeConfig{}) == nil {
		t.Fatal("invalid limits accepted")
	}
}
func TestHistoryResponseDoesNotDeadlock(t *testing.T) {
	e, err := openEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.db.Close()
	previous := engine
	engine = e
	defer func() { engine = previous }()
	e.db.Exec("INSERT INTO samples VALUES(?,?)", time.Now().Unix(), `[{"time":"test","download":1}]`)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := httptest.NewRecorder()
		historyAPI(w, httptest.NewRequest("GET", "/api/history", nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "historyStart") {
			t.Error("invalid history response")
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("history connection deadlock")
	}
}

func TestConfigRejectsGatewayCyclesAndDuplicates(t *testing.T) {
	c := defaultConfig()
	c.Devices = []deviceConfig{{IP: "192.0.2.1", Kind: "gateway", Gateway: "192.0.2.2"}, {IP: "192.0.2.2", Kind: "gateway", Gateway: "192.0.2.1"}}
	if validateConfig(c) == nil {
		t.Fatal("gateway cycle accepted")
	}
	c.Devices[1].IP = c.Devices[0].IP
	if validateConfig(c) == nil {
		t.Fatal("duplicate identity accepted")
	}
}

func TestAccountingBaselineSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	e, err := openEngine(dir)
	if err != nil {
		t.Fatal(err)
	}
	e.db.Exec("INSERT INTO accounting VALUES(?,?,?,?)", "192.0.2.1", 1024, 2048, time.Now().Unix())
	e.db.Close()
	e, err = openEngine(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.db.Close()
	if e.lastFlow["192.0.2.1"].RX != 1024 || e.lastFlowAt.IsZero() {
		t.Fatal("restart would duplicate cumulative traffic")
	}
}

func TestDiscoverySources(t *testing.T) {
	if decodeAvahi(`\229\176\143\231\136\177`) != "小爱" {
		t.Fatal("mDNS UTF-8 byte escapes were not decoded")
	}
	arp := parseARP("192.0.2.1\t00:11:22:33:44:55\tVendor\nnot-an-ip 00:11:22:33:44:55\n192.0.2.2 invalid")
	if len(arp) != 1 {
		t.Fatal(arp)
	}
	names := parseMDNS("=;br-lan;IPv4;Office Printer;_ipp._tcp;local;printer.local;192.0.2.1;631;")
	if names["192.0.2.1"].Kind != "printer" {
		t.Fatal(names)
	}
}

func TestProbeDiagnosticEvidenceBoundary(t *testing.T) {
	c := defaultConfig()
	c.Devices = []deviceConfig{{IP: "192.0.2.1", Kind: "phone", Name: "Test phone", Key: true}}
	p := map[string]probeResult{"192.0.2.1": {At: time.Now(), Failures: 2}}
	ids := map[string]string{"192.0.2.1": "phone"}
	if len(diagnosticAlerts(c, p, ids, "", "now")) != 0 {
		t.Fatal("alert before three failures")
	}
	p["192.0.2.1"] = probeResult{At: time.Now(), Failures: 3}
	a := diagnosticAlerts(c, p, ids, "", "now")
	if len(a) != 1 || a[0].(map[string]any)["severity"] != "info" {
		t.Fatal("ICMP silence must not assert network outage")
	}
	c.Devices[0].Passive = true
	if len(diagnosticAlerts(c, p, ids, "", "now")) != 0 {
		t.Fatal("passive device probed")
	}
}

func TestAbsentNeighborRetainsOnlyUnambiguousIdentity(t *testing.T) {
	e, err := openEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.db.Close()
	e.db.Exec("INSERT INTO identities VALUES(?,?,?,?,?)", "00:11:22:33:44:55", "192.0.2.1", "Phone", "phone", "")
	d := map[string]map[string]any{"192.0.2.1": {"mac": ""}}
	restoreIdentities(e.db, d)
	if trafficIdentity("192.0.2.1", d) != "00:11:22:33:44:55" || d["192.0.2.1"]["macSource"] == nil {
		t.Fatal("offline identity or evidence lost")
	}
	e.db.Exec("INSERT INTO identities VALUES(?,?,?,?,?)", "00:11:22:33:44:66", "192.0.2.1", "Other", "phone", "")
	d["192.0.2.1"]["mac"] = ""
	restoreIdentities(e.db, d)
	if d["192.0.2.1"]["mac"] != "" {
		t.Fatal("ambiguous reused address guessed")
	}
}

func TestMultipleAddressesDoNotCollapseTopologyNodes(t *testing.T) {
	a := addressIdentity("192.0.2.1", "00:11:22:33:44:55", 2)
	b := addressIdentity("192.0.2.2", "00:11:22:33:44:55", 2)
	if a == b {
		t.Fatal("same MAC addresses collapse to one React Flow node")
	}
}
