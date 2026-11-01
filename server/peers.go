package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type peerConfig struct {
	Enabled bool         `json:"enabled"`
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Token   string       `json:"token"`
	Peers   []peerTarget `json:"peers"`
}
type peerTarget struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Fingerprint string `json:"fingerprint"`
	Token       string `json:"token"`
}
type peerAnnotation struct {
	IP         string `json:"ip"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Model      string `json:"model"`
	Group      string `json:"group"`
	Icon       string `json:"icon"`
	CustomIcon string `json:"customIcon"`
}
type peerPayload struct {
	Schema      int              `json:"schema"`
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	Snapshot    map[string]any   `json:"snapshot"`
	Annotations []peerAnnotation `json:"annotations"`
}
type peerView struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	URL        string       `json:"url"`
	Status     string       `json:"status"`
	Error      string       `json:"error,omitempty"`
	ReceivedAt string       `json:"receivedAt,omitempty"`
	Payload    *peerPayload `json:"payload,omitempty"`
}

var peers = struct {
	sync.RWMutex
	config      peerConfig
	views       []peerView
	fingerprint string
	status      string
}{views: []peerView{}, status: "联动未启用"}

func initPeers(dir string) error {
	path := filepath.Join(dir, "peers.json")
	var c peerConfig
	if b, err := os.ReadFile(path); err == nil {
		if err = json.Unmarshal(b, &c); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		secret := make([]byte, 32)
		rand.Read(secret)
		id := make([]byte, 16)
		rand.Read(id)
		c = peerConfig{ID: hex.EncodeToString(id), Name: "iStoreOS " + localIP(), Token: hex.EncodeToString(secret), Peers: []peerTarget{}}
		b, _ := json.MarshalIndent(c, "", "  ")
		if err = os.WriteFile(path, b, 0600); err != nil {
			return err
		}
	}
	if len(c.Token) != 64 || len(c.ID) != 32 || len(c.Peers) > 8 {
		return fmt.Errorf("节点配置格式无效")
	}
	for _, p := range c.Peers {
		if err := validatePeer(p); err != nil {
			return err
		}
	}
	certPath, keyPath := filepath.Join(dir, "peer.crt"), filepath.Join(dir, "peer.key")
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return e
		}
		serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		tmpl := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: c.ID}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(5, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP(localIP())}}
		der, e := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
		if e != nil {
			return e
		}
		pk, _ := x509.MarshalECPrivateKey(key)
		if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: pk}), 0600); e != nil {
			return e
		}
		if e = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
			return e
		}
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return err
	}
	fp := sha256.Sum256(cert.Certificate[0])
	peers.Lock()
	peers.config = c
	peers.fingerprint = hex.EncodeToString(fp[:])
	peers.Unlock()
	return nil
}
func validatePeer(p peerTarget) error {
	u, e := url.Parse(p.URL)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return fmt.Errorf("节点必须使用 HTTPS LAN 地址")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsPrivate() || ip.IsLoopback() {
		return fmt.Errorf("只允许明确的私有 LAN IP")
	}
	if len(p.ID) != 32 || len(p.Name) > 100 || len(p.Token) != 64 {
		return fmt.Errorf("节点标识或令牌无效")
	}
	if b, e := hex.DecodeString(p.Fingerprint); e != nil || len(b) != 32 {
		return fmt.Errorf("证书指纹无效")
	}
	return nil
}
func localPeerPayload() peerPayload {
	peers.RLock()
	c := peers.config
	peers.RUnlock()
	engine.mu.RLock()
	defer engine.mu.RUnlock()
	b, _ := json.Marshal(engine.snapshot)
	var snapshot map[string]any
	json.Unmarshal(b, &snapshot)
	if iface, err := net.InterfaceByName("br-lan"); err == nil && snapshot != nil {
		if ds, ok := snapshot["devices"].([]any); ok {
			for _, v := range ds {
				if d, ok := v.(map[string]any); ok && d["id"] == "doctor" {
					d["mac"] = strings.ToLower(iface.HardwareAddr.String())
				}
			}
		}
	}
	notes := []peerAnnotation{}
	for _, d := range engine.config.Devices {
		if d.Name != "" {
			notes = append(notes, peerAnnotation{d.IP, d.Name, d.Kind, d.Model, d.Group, d.Icon, d.CustomIcon})
		}
	}
	return peerPayload{1, c.ID, c.Name, version, snapshot, notes}
}
func peerReadHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/control" {
		peerControlHandler(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" || r.URL.Path != "/snapshot" {
		reply(w, 405, map[string]string{"error": "只允许读取节点快照"})
		return
	}
	peers.RLock()
	token := peers.config.Token
	peers.RUnlock()
	actual := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(actual) != len(token) || subtle.ConstantTimeCompare([]byte(actual), []byte(token)) != 1 {
		reply(w, 401, map[string]string{"error": "节点授权无效"})
		return
	}
	reply(w, 200, localPeerPayload())
}
func pinnedPeerClient(p peerTarget) (*http.Client, func()) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return fmt.Errorf("节点未提供证书")
		}
		cert := cs.PeerCertificates[0]
		sum := sha256.Sum256(cert.Raw)
		if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(strings.ToLower(p.Fingerprint))) != 1 {
			return fmt.Errorf("节点证书指纹不匹配")
		}
		if time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
			return fmt.Errorf("节点证书时间无效")
		}
		return nil
	}}
	tr := &http.Transport{TLSClientConfig: tlsConfig, DialContext: (&net.Dialer{Timeout: 4 * time.Second}).DialContext}
	client := &http.Client{Transport: tr, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("拒绝节点重定向") }}
	return client, tr.CloseIdleConnections
}
func fetchPeer(ctx context.Context, p peerTarget) (peerPayload, error) {
	var data peerPayload
	client, closeClient := pinnedPeerClient(p)
	defer closeClient()
	req, _ := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(p.URL, "/")+"/snapshot", nil)
	req.Header.Set("Authorization", "Bearer "+p.Token)
	resp, e := client.Do(req)
	if e != nil {
		return data, fmt.Errorf("节点连接/TLS 校验失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return data, fmt.Errorf("节点返回 %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if e != nil || len(b) > 8<<20 {
		return data, fmt.Errorf("节点响应超限或中断")
	}
	if json.Unmarshal(b, &data) != nil || data.Schema != 1 || data.ID != p.ID || data.Snapshot == nil {
		return data, fmt.Errorf("节点标识、协议或快照无效")
	}
	return data, nil
}
func runPeers(ctx context.Context) {
	peers.RLock()
	c := peers.config
	peers.RUnlock()
	if !c.Enabled {
		return
	}
	server := &http.Server{Addr: net.JoinHostPort(localIP(), "8099"), Handler: http.HandlerFunc(peerReadHandler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	peers.Lock()
	peers.status = "HTTPS 只读联动已启用"
	peers.Unlock()
	go func() {
		err := server.ListenAndServeTLS(filepath.Join(engine.dir, "peer.crt"), filepath.Join(engine.dir, "peer.key"))
		if err != http.ErrServerClosed {
			peers.Lock()
			peers.status = "联动监听失败，请检查 8099 端口"
			peers.Unlock()
		}
	}()
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		server.Shutdown(stop)
	}()
	for ctx.Err() == nil {
		views := []peerView{}
		for _, p := range c.Peers {
			data, e := fetchPeer(ctx, p)
			v := peerView{ID: p.ID, Name: p.Name, URL: p.URL, Status: "unavailable"}
			if e == nil {
				v.Status = "online"
				v.Payload = &data
				v.ReceivedAt = time.Now().Format(time.RFC3339)
				at, _ := time.Parse(time.RFC3339, fmt.Sprint(data.Snapshot["observedAt"]))
				if at.IsZero() || time.Since(at) > 2*time.Minute || time.Until(at) > time.Minute {
					v.Status = "stale"
					v.Error = "远端采集过期或节点时钟偏差"
				}
			} else {
				v.Error = e.Error()
			}
			views = append(views, v)
		}
		peers.Lock()
		peers.views = views
		peers.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
}
func peersAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		reply(w, 405, map[string]string{"error": "当前只允许读取；节点配对由本机受限配置管理"})
		return
	}
	peers.RLock()
	views := append([]peerView{}, peers.views...)
	status, enabled, fp := peers.status, peers.config.Enabled, peers.fingerprint
	peers.RUnlock()
	reply(w, 200, map[string]any{"local": localPeerPayload(), "peers": views, "enabled": enabled, "status": status, "fingerprint": fp})
}
func sharedPeerNotes() map[string]peerAnnotation {
	notes := map[string]peerAnnotation{}
	peers.RLock()
	defer peers.RUnlock()
	for _, v := range peers.views {
		if v.Status == "online" && v.Payload != nil {
			for _, n := range v.Payload.Annotations {
				candidate := defaultConfig()
				candidate.Devices = []deviceConfig{{IP: n.IP, Name: n.Name, Kind: n.Kind, Model: n.Model, Group: n.Group, Icon: n.Icon, CustomIcon: n.CustomIcon}}
				if validateConfig(candidate) != nil {
					continue
				}
				if _, exists := notes[n.IP]; !exists {
					notes[n.IP] = n
				}
			}
		}
	}
	return notes
}
func decoratePeerPaths(byIP map[string]map[string]any, byID map[string]string, links []any) []any {
	peers.RLock()
	views := append([]peerView{}, peers.views...)
	peers.RUnlock()
	for _, v := range views {
		if v.Status != "online" || v.Payload == nil {
			continue
		}
		collector := ""
		devices, _ := v.Payload.Snapshot["devices"].([]any)
		for _, raw := range devices {
			d, _ := raw.(map[string]any)
			if d["id"] == "doctor" {
				collector, _ = d["ip"].(string)
			}
		}
		if byID[collector] == "" {
			continue
		}
		for _, raw := range devices {
			d, _ := raw.(map[string]any)
			ip, _ := d["ip"].(string)
			target := byIP[ip]
			if target == nil || byID[ip] == "" || ip == collector {
				continue
			}
			mac, _ := d["mac"].(string)
			localMAC, _ := target["mac"].(string)
			if mac == "" || !strings.EqualFold(mac, localMAC) {
				continue
			}
			path, _ := d["bypassPath"].(map[string]any)
			if path["state"] != "recent" {
				continue
			}
			stamp, _ := path["observedAt"].(string)
			at, err := time.Parse(time.RFC3339, stamp)
			if err != nil || time.Since(at) > 2*time.Minute || time.Until(at) > time.Second {
				continue
			}
			evidence := map[string]any{"node": v.Name, "nodeIP": collector, "observedAt": stamp}
			existing, _ := target["peerPaths"].([]any)
			target["peerPaths"] = append(existing, evidence)
			out := links[:0]
			for _, l := range links {
				m, _ := l.(map[string]any)
				if m["from"] == byID[ip] && m["to"] == byID[collector] && m["relation"] == "gateway" {
					continue
				}
				out = append(out, l)
			}
			links = out
			links = append(links, map[string]any{"id": "peer-" + v.ID + "-" + byID[ip], "from": byID[ip], "to": byID[collector], "relation": "gateway", "confidence": "confirmed", "status": "online", "availability": "available", "observedAt": stamp, "source": "远端节点 " + v.Name + " 的公网请求交付证据；不证明全部流量受控", "pathObserved": true, "pathNode": collector, "measured": false})
		}
	}
	return links
}
