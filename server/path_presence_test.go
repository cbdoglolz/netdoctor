package main

import (
	"fmt"
	"testing"
	"time"
)

func TestReceiptEvidenceExcludesAdminOtherGatewayAndRouterTraffic(t *testing.T) {
	a := testPathAccumulator()
	now := time.Now()
	p := pathPacket{pathTuple{"192.168.1.10", "1.1.1.1", 5000, 443, 6}, "00:11:22:33:44:10", "00:11:22:33:44:66", now}
	if _, ok := receiptPacket(a, p); !ok {
		t.Fatal("actual public request missing")
	}
	for _, change := range []func(*pathPacket){func(p *pathPacket) { p.Tuple.Dst = "192.168.1.6" }, func(p *pathPacket) { p.DstMAC = "00:11:22:33:44:03" }, func(p *pathPacket) { p.Tuple.Src = "192.168.1.6" }, func(p *pathPacket) { p.SrcMAC = "00:11:22:33:44:66" }, func(p *pathPacket) { p.Tuple.Src = "192.168.1.99" }} {
		q := p
		change(&q)
		if _, ok := receiptPacket(a, q); ok {
			t.Fatal("unrelated request misclassified", q)
		}
	}
}

func TestPresenceFreshHistoricalExpiryAndIPReuse(t *testing.T) {
	now := time.Now()
	m := &pathPresence{seen: map[string]pathReceipt{}}
	mac := "00:11:22:33:44:10"
	ip := "192.168.1.10"
	for _, c := range []struct {
		age        time.Duration
		mac, state string
		edges      int
	}{{time.Minute, mac, "recent", 1}, {3 * time.Minute, mac, "historical", 0}, {25 * time.Hour, mac, "unknown", 0}, {time.Minute, "00:11:22:33:44:99", "unknown", 0}} {
		m.seen = map[string]pathReceipt{}
		m.remember(pathReceipt{ip, mac, now.Add(-c.age)})
		devices := map[string]map[string]any{ip: {"mac": c.mac}, "192.168.1.6": {}}
		links, _ := m.decorate(devices, map[string]string{ip: "client", "192.168.1.6": "doctor"}, nil, now, "192.168.1.6")
		p := devices[ip]["bypassPath"].(map[string]any)
		if p["state"] != c.state || len(links) != c.edges {
			t.Fatal(p, links, c)
		}
	}
}

func TestPresenceReplacesOnlyDuplicateManualGateway(t *testing.T) {
	now := time.Now()
	m := &pathPresence{seen: map[string]pathReceipt{}}
	m.remember(pathReceipt{"192.168.1.10", "00:11:22:33:44:10", now})
	devices := map[string]map[string]any{"192.168.1.10": {"mac": "00:11:22:33:44:10"}, "192.168.1.6": {}}
	manual := []any{map[string]any{"from": "client", "to": "doctor", "relation": "gateway"}, map[string]any{"from": "switch", "to": "doctor", "relation": "physical"}}
	links, _ := m.decorate(devices, map[string]string{"192.168.1.10": "client", "192.168.1.6": "doctor"}, manual, now, "192.168.1.6")
	if len(links) != 2 || links[0].(map[string]any)["relation"] != "physical" || links[1].(map[string]any)["pathObserved"] != true {
		t.Fatal(links)
	}
}

func TestPresenceBoundedAndTimestampNotRefreshedByRestore(t *testing.T) {
	m := &pathPresence{seen: map[string]pathReceipt{}}
	at := time.Now().Add(-time.Hour)
	for i := 0; i < 700; i++ {
		m.remember(pathReceipt{fmt.Sprint(i), "00:11:22:33:44:10", at.Add(time.Duration(i) * time.Second)})
	}
	if len(m.seen) != 512 {
		t.Fatal(len(m.seen))
	}
	r := pathReceipt{"test", "00:11:22:33:44:10", at}
	m.remember(r)
	m.remember(pathReceipt{r.IP, r.MAC, at.Add(-time.Minute)})
	if !m.seen[r.IP+"/"+r.MAC].At.Equal(at) {
		t.Fatal("historical timestamp refreshed")
	}
}

func TestPresenceSQLiteRestartKeepsOriginalAge(t *testing.T) {
	previous := engine
	r, err := openEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine = r
	defer func() { engine = previous; r.db.Close() }()
	at := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	m := &pathPresence{seen: map[string]pathReceipt{}}
	m.remember(pathReceipt{"192.168.1.10", "00:11:22:33:44:10", at})
	m.save()
	restored := &pathPresence{seen: map[string]pathReceipt{}}
	restored.restore()
	if !restored.seen["192.168.1.10/00:11:22:33:44:10"].At.Equal(at) {
		t.Fatal("evidence lost or made current on restart", restored.seen)
	}
}
