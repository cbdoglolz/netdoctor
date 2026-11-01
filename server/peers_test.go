package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPeerTargetRestrictions(t *testing.T) {
	p := peerTarget{ID: strings.Repeat("a", 32), Name: "test", URL: "https://192.168.1.2:8099", Fingerprint: strings.Repeat("a", 64), Token: strings.Repeat("a", 64)}
	if validatePeer(p) != nil {
		t.Fatal("valid LAN target rejected")
	}
	for _, url := range []string{"http://192.168.1.2:8099", "https://example.com", "https://127.0.0.1", "https://8.8.8.8", "https://192.168.1.2/secret", "https://user:password@192.168.1.2"} {
		q := p
		q.URL = url
		if validatePeer(q) == nil {
			t.Fatal("unsafe target accepted", url)
		}
	}
}
func TestPeerReadOnlyAuthorization(t *testing.T) {
	peers.Lock()
	old := peers.config
	peers.config.Token = strings.Repeat("a", 64)
	peers.Unlock()
	defer func() { peers.Lock(); peers.config = old; peers.Unlock() }()
	for _, q := range []struct {
		method, path, token string
		code                int
	}{{"GET", "/snapshot", "", 401}, {"POST", "/snapshot", strings.Repeat("a", 64), 405}, {"GET", "/api/config", strings.Repeat("a", 64), 405}, {"GET", "/snapshot", "wrong", 401}} {
		r := httptest.NewRequest(q.method, q.path, nil)
		r.Header.Set("Authorization", "Bearer "+q.token)
		w := httptest.NewRecorder()
		peerReadHandler(w, r)
		if w.Code != q.code {
			t.Fatal(w.Code, q.code)
		}
	}
}
func TestPeerCertificatePinAndIdentity(t *testing.T) {
	id := strings.Repeat("a", 32)
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"schema":1,"id":"%s","name":"test","snapshot":{"devices":[]}}`, id)
	}))
	defer s.Close()
	sum := sha256.Sum256(s.Certificate().Raw)
	p := peerTarget{URL: s.URL, ID: id, Fingerprint: hex.EncodeToString(sum[:]), Token: strings.Repeat("a", 64)}
	if _, err := fetchPeer(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	p.Fingerprint = strings.Repeat("0", 64)
	if _, err := fetchPeer(context.Background(), p); err == nil {
		t.Fatal("bad fingerprint accepted")
	}
	p.Fingerprint = hex.EncodeToString(sum[:])
	p.ID = strings.Repeat("b", 32)
	if _, err := fetchPeer(context.Background(), p); err == nil {
		t.Fatal("wrong node identity accepted")
	}
}
