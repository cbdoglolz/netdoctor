package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginInputAndPasswordPreservation(t *testing.T) {
	sid, f := createSession(func(input string) ([]byte, error) {
		var v map[string]any
		json.Unmarshal([]byte(input), &v)
		if v["username"] != "root" || v["password"] != " Test+&密碼 " {
			t.Fatal("credentials changed during encoding")
		}
		return []byte(`{"ubus_rpc_session":"abcdef0123456789abcdef0123456789"}`), nil
	}, " root ", " Test+&密碼 ")
	if f != nil || sid == "" {
		t.Fatalf("valid login failed: %v", f)
	}
}
func TestAuthenticationFailureClasses(t *testing.T) {
	for _, tc := range []struct {
		out    string
		err    error
		status int
	}{{"Command failed: Permission denied", errors.New("exit 6"), 401}, {"Connection failed", errors.New("exit 1"), 503}, {"not json", nil, 503}, {`{"ubus_rpc_session":"00000000000000000000000000000000"}`, nil, 503}} {
		sid, f := createSession(func(string) ([]byte, error) { return []byte(tc.out), tc.err }, "root", "wrong")
		if sid != "" || f == nil || f.Status != tc.status {
			t.Fatalf("incorrect classification: %v", f)
		}
	}
}
func TestLoginFailureRemainsMobileForm(t *testing.T) {
	w := httptest.NewRecorder()
	renderLogin(w, 401, "密码不正确 <unsafe>")
	if w.Code != 401 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatal("wrong response type")
	}
	body := w.Body.String()
	for _, want := range []string{`name="viewport"`, `role="alert"`, `type="password"`, `method="post"`, `autocomplete="current-password"`, `&lt;unsafe&gt;`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing mobile retry behavior %s", want)
		}
	}
	if strings.Contains(body, "<unsafe>") {
		t.Fatal("unescaped error")
	}
}
