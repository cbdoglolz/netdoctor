package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testPathAccumulator() *pathAccumulator {
	return &pathAccumulator{LocalMAC: map[string]bool{"00:11:22:33:44:66": true}, LocalIP: map[string]bool{"192.168.1.6": true}, Clients: map[string]pathEvidence{"192.168.1.10": {IP: "192.168.1.10", MAC: "00:11:22:33:44:10", Family: 4}}, KnownMAC: map[string]string{}, Flows: map[pathTuple]*pathFlow{}, Aliases: map[pathTuple]pathTuple{}}
}
func TestPathRequiresReceiptEgressAndBothReturnStages(t *testing.T) {
	a := testPathAccumulator()
	orig := pathTuple{"192.168.1.10", "1.1.1.1", 50100, 443, 6}
	reply := pathTuple{"1.1.1.1", "192.168.1.6", 443, 60100, 6}
	a.mapConnections([]pathMapping{{orig, reply, true}})
	if a.evidence()[0].Status != "unknown" {
		t.Fatal("conntrack alone asserted a path")
	}
	p := pathPacket{orig, "00:11:22:33:44:10", "00:11:22:33:44:66", time.Now()}
	a.packet(p)
	if a.evidence()[0].Status != "received" {
		t.Fatal("receipt not detected")
	}
	a.packet(pathPacket{reply.reverse(), "00:11:22:33:44:66", "00:11:22:33:44:03", time.Now()})
	if a.evidence()[0].Status != "received" {
		t.Fatal("claimed accepted forwarding without counter evidence")
	}
	e := a.Clients[orig.Src]
	e.ForwardPackets = 1
	e.ReplyPackets = 1
	a.Clients[orig.Src] = e
	if a.evidence()[0].Status != "forwarded" {
		t.Fatal("NAT egress not associated")
	}
	a.packet(pathPacket{reply, "00:11:22:33:44:03", "00:11:22:33:44:66", time.Now()})
	if a.evidence()[0].Status != "forwarded" {
		t.Fatal("return arrival is not delivery")
	}
	a.packet(pathPacket{orig.reverse(), "00:11:22:33:44:66", "00:11:22:33:44:10", time.Now()})
	if a.evidence()[0].Status != "bidirectional" {
		t.Fatalf("missing bidirectional evidence %+v", a.evidence())
	}
}
func TestPathExcludesOtherGatewayAndLocalServices(t *testing.T) {
	a := testPathAccumulator()
	a.packet(pathPacket{pathTuple{"192.168.1.10", "1.1.1.1", 1, 443, 6}, "00:11:22:33:44:10", "00:11:22:33:44:03", time.Now()})
	a.packet(pathPacket{pathTuple{"192.168.1.10", "192.168.1.6", 2, 8088, 6}, "00:11:22:33:44:10", "00:11:22:33:44:66", time.Now()})
	a.packet(pathPacket{pathTuple{"192.168.1.6", "1.1.1.1", 3, 443, 6}, "00:11:22:33:44:66", "00:11:22:33:44:03", time.Now()})
	if a.evidence()[0].Status != "unknown" {
		t.Fatal("other gateway/local service/router probe misclassified")
	}
}
func TestPathOldEvidenceIsNotCurrent(t *testing.T) {
	a := testPathAccumulator()
	a.packet(pathPacket{pathTuple{"192.168.1.10", "1.1.1.1", 1, 443, 6}, "00:11:22:33:44:10", "00:11:22:33:44:66", time.Now().Add(-3 * time.Minute)})
	e := a.evidence()[0]
	if e.Current || e.Status != "received" {
		t.Fatal("old evidence lost or reported current")
	}
}
func TestPCAPBoundedHeaderDecodeAndVLAN(t *testing.T) {
	frame := make([]byte, 14+4+20+20)
	copy(frame[:6], []byte{0, 17, 34, 51, 68, 102})
	copy(frame[6:12], []byte{0, 17, 34, 51, 68, 16})
	binary.BigEndian.PutUint16(frame[12:14], 0x8100)
	binary.BigEndian.PutUint16(frame[16:18], 0x0800)
	off := 18
	frame[off] = 0x45
	frame[off+9] = 6
	copy(frame[off+12:], net.ParseIP("192.168.1.10").To4())
	copy(frame[off+16:], net.ParseIP("1.1.1.1").To4())
	binary.BigEndian.PutUint16(frame[off+20:], 5000)
	binary.BigEndian.PutUint16(frame[off+22:], 443)
	var pcap bytes.Buffer
	h := make([]byte, 24)
	copy(h, []byte{0xd4, 0xc3, 0xb2, 0xa1})
	binary.LittleEndian.PutUint32(h[20:], 1)
	pcap.Write(h)
	r := make([]byte, 16)
	binary.LittleEndian.PutUint32(r[0:], uint32(time.Now().Unix()))
	binary.LittleEndian.PutUint32(r[8:], uint32(len(frame)))
	pcap.Write(r)
	pcap.Write(frame)
	count := 0
	if err := readPathPCAP(&pcap, func(p pathPacket) {
		count++
		if p.Tuple.Src != "192.168.1.10" || p.Tuple.Dport != 443 {
			t.Fatal(p)
		}
	}, func() { t.Fatal("unexpected truncation") }); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal(count)
	}
	if _, err := decodePathPacket(frame[:19], time.Now()); err == nil {
		t.Fatal("truncated header accepted")
	}
}
func TestPathMappingsExtendedFormatAndCounterJSON(t *testing.T) {
	mappings := parsePathMappings("ipv4 2 tcp 6 60 ESTABLISHED src=192.168.1.10 dst=1.1.1.1 sport=5000 dport=443 src=1.1.1.1 dst=192.168.1.6 sport=443 dport=6000 [OFFLOAD]")
	if len(mappings) != 1 || !mappings[0].Offloaded || mappings[0].Reply.Dport != 6000 {
		t.Fatal(mappings)
	}
	c := parsePathCounters([]byte(`{"nftables":[{"rule":{"comment":"out:192.168.1.10","expr":[{"counter":{"packets":23,"bytes":1000}}]}}]}`))
	if c["out:192.168.1.10"] != 23 {
		t.Fatal(c)
	}
}

func TestIPv6ExtensionHeadersStaySeparate(t *testing.T) {
	frame := make([]byte, 14+40+8+8)
	binary.BigEndian.PutUint16(frame[12:14], 0x86dd)
	frame[14] = 0x60
	frame[20] = 0
	copy(frame[22:38], net.ParseIP("2001:db8::10").To16())
	copy(frame[38:54], net.ParseIP("2606:4700:4700::1111").To16())
	frame[54] = 17
	frame[55] = 0
	binary.BigEndian.PutUint16(frame[62:64], 50000)
	binary.BigEndian.PutUint16(frame[64:66], 443)
	p, err := decodePathPacket(frame, time.Now())
	if err != nil || p.Tuple.Protocol != 17 || p.Tuple.Src != "2001:db8::10" || p.Tuple.Dport != 443 {
		t.Fatalf("bad IPv6 decode %+v %v", p, err)
	}
	frame[20] = 44
	binary.BigEndian.PutUint16(frame[56:58], 8)
	if _, err := decodePathPacket(frame, time.Now()); err == nil {
		t.Fatal("non-first IPv6 fragment supplied a transport tuple")
	}
}

func TestGatewayPhaseCannotCarryOldPathProof(t *testing.T) {
	old := paths
	defer func() { paths = old }()
	a := testPathAccumulator()
	a.Clients["192.168.1.10"] = pathEvidence{IP: "192.168.1.10", Family: 4, Received: 42, ForwardPackets: 10, ReplyPackets: 9, LastSeen: time.Now().Format(time.RFC3339), Phase: "gateway6"}
	a.Flows[pathTuple{"192.168.1.10", "1.1.1.1", 5000, 443, 6}] = &pathFlow{Tuple: pathTuple{"192.168.1.10", "1.1.1.1", 5000, 443, 6}, Received: true, Outgoing: true, ReturnWire: true, Delivered: true}
	paths = &pathObserver{report: &pathReport{State: "running", Phase: "gateway6", Warnings: []string{}, Phases: []pathPhase{}}, acc: a, baseline: map[string]uint64{}, phaseAt: "test"}
	w := httptest.NewRecorder()
	pathObservationAPI(w, httptest.NewRequest("POST", "/api/path-observation", strings.NewReader(`{"phase":"gateway3"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(paths.report.Phases) != 1 || paths.report.Phases[0].Devices[0].Bidirectional != 1 {
		t.Fatal("previous phase evidence missing")
	}
	if paths.report.Devices[0].Received != 0 || paths.report.Devices[0].Status != "unknown" || len(paths.acc.Flows) != 0 {
		t.Fatal("old connection proof leaked into .3 phase")
	}
	if paths.baseline["out:192.168.1.10"] != 10 || paths.baseline["in:192.168.1.10"] != 9 {
		t.Fatal("kernel counter baseline carried over")
	}
}

func TestPCAPRejectsOversizedRecordBeforeAllocation(t *testing.T) {
	var b bytes.Buffer
	h := make([]byte, 24)
	copy(h, []byte{0xd4, 0xc3, 0xb2, 0xa1})
	binary.LittleEndian.PutUint32(h[20:], 1)
	b.Write(h)
	r := make([]byte, 16)
	binary.LittleEndian.PutUint32(r[8:], 1<<30)
	b.Write(r)
	if err := readPathPCAP(&b, func(pathPacket) { t.Fatal("unbounded record consumed") }, func() {}); err == nil {
		t.Fatal("oversized record accepted")
	}
}

func TestCleanupRequiresOwnedTableNotJustMatchingText(t *testing.T) {
	if !ownedPathTable([]byte(`{"nftables":[{"table":{"family":"inet","name":"hnd_path_observe","comment":"HomeNet Doctor path observer v1"}}]}`)) {
		t.Fatal("owned table rejected")
	}
	for _, data := range []string{`{"nftables":[{"table":{"family":"inet","name":"hnd_path_observe"}},{"rule":{"comment":"HomeNet Doctor path observer v1"}}]}`, `{"nftables":[{"table":{"family":"inet","name":"other","comment":"HomeNet Doctor path observer v1"}}]}`, `invalid`} {
		if ownedPathTable([]byte(data)) {
			t.Fatal("foreign/unknown table would be deleted")
		}
	}
}
