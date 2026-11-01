package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type pathTuple struct {
	Src, Dst     string
	Sport, Dport uint16
	Protocol     uint8
}

func (t pathTuple) reverse() pathTuple { return pathTuple{t.Dst, t.Src, t.Dport, t.Sport, t.Protocol} }

type pathPacket struct {
	Tuple          pathTuple
	SrcMAC, DstMAC string
	At             time.Time
}

// Only Ethernet/IP/transport headers are decoded. Captured payload is never retained.
func decodePathPacket(b []byte, at time.Time) (pathPacket, error) {
	var p pathPacket
	p.At = at
	if len(b) < 14 {
		return p, io.ErrUnexpectedEOF
	}
	p.DstMAC = net.HardwareAddr(b[:6]).String()
	p.SrcMAC = net.HardwareAddr(b[6:12]).String()
	kind := binary.BigEndian.Uint16(b[12:14])
	off := 14
	for tags := 0; (kind == 0x8100 || kind == 0x88a8) && tags < 2; tags++ {
		if len(b) < off+4 {
			return p, io.ErrUnexpectedEOF
		}
		kind = binary.BigEndian.Uint16(b[off+2 : off+4])
		off += 4
	}
	if kind == 0x0800 {
		if len(b) < off+20 || b[off]>>4 != 4 {
			return p, io.ErrUnexpectedEOF
		}
		ihl := int(b[off]&15) * 4
		if ihl < 20 || len(b) < off+ihl {
			return p, io.ErrUnexpectedEOF
		}
		// Non-first fragments have no trustworthy transport tuple.
		if binary.BigEndian.Uint16(b[off+6:off+8])&0x1fff != 0 {
			return p, errors.New("fragment")
		}
		p.Tuple = pathTuple{Src: net.IP(b[off+12 : off+16]).String(), Dst: net.IP(b[off+16 : off+20]).String(), Protocol: b[off+9]}
		off += ihl
	} else if kind == 0x86dd {
		if len(b) < off+40 || b[off]>>4 != 6 {
			return p, io.ErrUnexpectedEOF
		}
		next := b[off+6]
		p.Tuple.Src = net.IP(b[off+8 : off+24]).String()
		p.Tuple.Dst = net.IP(b[off+24 : off+40]).String()
		off += 40
		for n := 0; n < 8 && (next == 0 || next == 43 || next == 60 || next == 44 || next == 51); n++ {
			if len(b) < off+8 {
				return p, io.ErrUnexpectedEOF
			}
			size := (int(b[off+1]) + 1) * 8
			if next == 44 {
				if binary.BigEndian.Uint16(b[off+2:off+4])&0xfff8 != 0 {
					return p, errors.New("fragment")
				}
				size = 8
			}
			if next == 51 {
				size = (int(b[off+1]) + 2) * 4
			}
			next = b[off]
			off += size
			if len(b) < off {
				return p, io.ErrUnexpectedEOF
			}
		}
		p.Tuple.Protocol = next
	} else {
		return p, errors.New("non-IP")
	}
	if p.Tuple.Protocol != 6 && p.Tuple.Protocol != 17 {
		return p, errors.New("non TCP/UDP")
	}
	if len(b) < off+4 {
		return p, io.ErrUnexpectedEOF
	}
	p.Tuple.Sport = binary.BigEndian.Uint16(b[off : off+2])
	p.Tuple.Dport = binary.BigEndian.Uint16(b[off+2 : off+4])
	return p, nil
}

func readPathPCAP(r io.Reader, consume func(pathPacket), incomplete func()) error {
	header := make([]byte, 24)
	if _, err := io.ReadFull(r, header); err != nil {
		return err
	}
	var order binary.ByteOrder
	nano := false
	switch string(header[:4]) {
	case "\xd4\xc3\xb2\xa1":
		order = binary.LittleEndian
	case "\xa1\xb2\xc3\xd4":
		order = binary.BigEndian
	case "\x4d\x3c\xb2\xa1":
		order = binary.LittleEndian
		nano = true
	case "\xa1\xb2\x3c\x4d":
		order = binary.BigEndian
		nano = true
	default:
		return errors.New("不支持的抓包格式")
	}
	if order.Uint32(header[20:24]) != 1 {
		return errors.New("需要 Ethernet 链路抓包")
	}
	for count := 0; count < 10000; count++ {
		h := make([]byte, 16)
		_, err := io.ReadFull(r, h)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		size := order.Uint32(h[8:12])
		if size > 128 {
			return errors.New("包头长度超过限制")
		}
		b := make([]byte, size)
		if _, err = io.ReadFull(r, b); err != nil {
			return err
		}
		ns := int64(order.Uint32(h[4:8]))
		if !nano {
			ns *= 1000
		}
		p, err := decodePathPacket(b, time.Unix(int64(order.Uint32(h[:4])), ns))
		if err == nil {
			consume(p)
		} else if err == io.ErrUnexpectedEOF {
			incomplete()
		}
	}
	incomplete()
	return nil
}

type pathMapping struct {
	Original, Reply pathTuple
	Offloaded       bool
}

func parsePathMappings(text string) []pathMapping {
	result := []pathMapping{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		protocol := f[0]
		if (protocol == "ipv4" || protocol == "ipv6") && len(f) > 2 {
			protocol = f[2]
		}
		var n uint8
		if protocol == "tcp" {
			n = 6
		} else if protocol == "udp" {
			n = 17
		} else {
			continue
		}
		src, dst := []string{}, []string{}
		sp, dp := []uint16{}, []uint16{}
		for _, v := range f {
			kv := strings.SplitN(v, "=", 2)
			if len(kv) != 2 {
				continue
			}
			num, _ := strconv.ParseUint(kv[1], 10, 16)
			switch kv[0] {
			case "src":
				src = append(src, kv[1])
			case "dst":
				dst = append(dst, kv[1])
			case "sport":
				sp = append(sp, uint16(num))
			case "dport":
				dp = append(dp, uint16(num))
			}
		}
		if len(src) != 2 || len(dst) != 2 || len(sp) != 2 || len(dp) != 2 || net.ParseIP(src[0]) == nil || net.ParseIP(dst[0]) == nil {
			continue
		}
		result = append(result, pathMapping{pathTuple{src[0], dst[0], sp[0], dp[0], n}, pathTuple{src[1], dst[1], sp[1], dp[1], n}, strings.Contains(line, "OFFLOAD")})
	}
	return result
}
func (t pathTuple) text() string {
	return fmt.Sprintf("%s:%d → %s:%d (%d)", t.Src, t.Sport, t.Dst, t.Dport, t.Protocol)
}
func publicDestination(ip string) bool {
	v := net.ParseIP(ip)
	return v != nil && v.IsGlobalUnicast() && !v.IsPrivate()
}

type pathFlow struct {
	Tuple                                     pathTuple
	Received, Outgoing, ReturnWire, Delivered bool
	Last                                      time.Time
}
type pathEvidence struct {
	IP             string   `json:"ip"`
	Name           string   `json:"name"`
	MAC            string   `json:"mac"`
	Family         int      `json:"family"`
	Current        bool     `json:"current"`
	Status         string   `json:"status"`
	LastSeen       string   `json:"lastSeen,omitempty"`
	Received       int      `json:"received"`
	ForwardPackets uint64   `json:"forwardPackets"`
	ReplyPackets   uint64   `json:"replyPackets"`
	Forwarded      int      `json:"forwarded"`
	Bidirectional  int      `json:"bidirectional"`
	Offloaded      bool     `json:"offloaded"`
	Flows          []string `json:"flows"`
	Phase          string   `json:"phase"`
}
type pathAccumulator struct {
	LocalMAC, LocalIP map[string]bool
	Clients           map[string]pathEvidence
	KnownMAC          map[string]string
	Flows             map[pathTuple]*pathFlow
	Aliases           map[pathTuple]pathTuple
	Phase             string
}

func (a *pathAccumulator) mapConnections(mappings []pathMapping) {
	a.Aliases = map[pathTuple]pathTuple{}
	for _, m := range mappings {
		if _, ok := a.Clients[m.Original.Src]; !ok {
			continue
		}
		a.Aliases[m.Reply.reverse()] = m.Original
		a.Aliases[m.Reply] = m.Original
		e := a.Clients[m.Original.Src]
		e.Offloaded = e.Offloaded || m.Offloaded
		a.Clients[e.IP] = e
	}
}
func (a *pathAccumulator) packet(p pathPacket) {
	t := p.Tuple
	// Receipt must originate at a known endpoint, addressed to this router at L2.
	e, known := a.Clients[t.Src]
	// Learn the current address/MAC pairing from the actual observed Ethernet frame.
	if known && a.LocalMAC[p.DstMAC] && !a.LocalMAC[p.SrcMAC] && publicDestination(t.Dst) && !a.LocalIP[t.Src] && !a.LocalIP[t.Dst] {
		e.MAC = p.SrcMAC
		a.KnownMAC[p.SrcMAC] = e.Name
	}
	if !known && len(a.Clients) < 512 && net.ParseIP(t.Src).To4() == nil && a.KnownMAC[p.SrcMAC] != "" && net.ParseIP(t.Src).IsGlobalUnicast() {
		e = pathEvidence{IP: t.Src, Name: a.KnownMAC[p.SrcMAC], MAC: p.SrcMAC, Family: 6, Phase: a.Phase, Status: "unknown", Flows: []string{}}
		known = true
	}
	if known && e.MAC != "" && p.SrcMAC == e.MAC && a.LocalMAC[p.DstMAC] && publicDestination(t.Dst) && !a.LocalIP[t.Src] && !a.LocalIP[t.Dst] {
		if a.Flows[t] == nil && len(a.Flows) < 2048 {
			a.Flows[t] = &pathFlow{Tuple: t}
		}
		if f := a.Flows[t]; f != nil {
			f.Received = true
			f.Last = p.At
			e.Received++
			e.LastSeen = p.At.Format(time.RFC3339)
			a.Clients[t.Src] = e
		}
	}
	original, alias := a.Aliases[t]
	if !alias {
		original = t
		if a.Flows[t] == nil {
			original = t.reverse()
		}
	}
	f := a.Flows[original]
	if f == nil || !f.Received {
		return
	}
	if a.LocalMAC[p.SrcMAC] && !a.LocalMAC[p.DstMAC] && publicDestination(t.Dst) {
		f.Outgoing = true
	}
	if a.LocalMAC[p.DstMAC] && net.ParseIP(t.Src).IsGlobalUnicast() {
		f.ReturnWire = true
	}
	if a.LocalMAC[p.SrcMAC] && t == f.Tuple.reverse() && p.DstMAC == a.Clients[f.Tuple.Src].MAC {
		f.Delivered = true
	}
	f.Last = p.At
}
func (a *pathAccumulator) evidence() []pathEvidence {
	out := []pathEvidence{}
	for ip, e := range a.Clients {
		e.Status = "unknown"
		e.Forwarded = 0
		e.Bidirectional = 0
		e.Flows = []string{}
		if e.Received > 0 {
			e.Status = "received"
		}
		for _, f := range a.Flows {
			if f.Tuple.Src != ip || !f.Received {
				continue
			}
			if f.Outgoing && e.ForwardPackets > 0 {
				e.Forwarded++
				e.Status = "forwarded"
				if f.ReturnWire && f.Delivered && e.ReplyPackets > 0 {
					e.Bidirectional++
					e.Status = "bidirectional"
				}
			}
			if len(e.Flows) < 4 {
				e.Flows = append(e.Flows, f.Tuple.text())
			}
		}
		if e.Bidirectional > 0 {
			e.Status = "bidirectional"
		}
		if at, err := time.Parse(time.RFC3339, e.LastSeen); err == nil {
			e.Current = time.Since(at) < 2*time.Minute
		}
		out = append(out, e)
	}
	return out
}
