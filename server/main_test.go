package main

import (
	"net/http/httptest"
	"testing"
)

func TestVersionOrder(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v0.2.1", "0.2.0", true}, {"v0.10.0", "0.2.9", true}, {"v0.2.0", "0.2.0", false}, {"v0.1.9", "0.2.0", false}, {"v0.2.1-beta", "0.2.0", false}, {"broken", "0.2.0", false}} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("%s > %s = %v", c.a, c.b, got)
		}
	}
}
func TestOrigin(t *testing.T) {
	r := httptest.NewRequest("POST", "http://router:8088/api/update", nil)
	if sameOrigin(r) {
		t.Fatal("missing origin accepted")
	}
	r.Header.Set("Origin", "http://evil.example")
	if sameOrigin(r) {
		t.Fatal("cross origin accepted")
	}
	r.Header.Set("Origin", "http://router:8088")
	if !sameOrigin(r) {
		t.Fatal("same origin rejected")
	}
}
func TestAnonymousDenied(t *testing.T) {
	r := httptest.NewRequest("GET", "http://router/api/snapshot", nil)
	if auth(r) {
		t.Fatal("anonymous accepted")
	}
	r.Header.Set("Cookie", "homenet_session=00000000000000000000000000000000")
	if auth(r) {
		t.Fatal("ubus anonymous session accepted")
	}
}
func TestAssets(t *testing.T) {
	if hasAssets(release{}) {
		t.Fatal("empty release installable")
	}
	if hasAssets(release{Assets: []asset{{Name: "homenet-doctor-linux-amd64"}}}) {
		t.Fatal("missing checksum accepted")
	}
}
