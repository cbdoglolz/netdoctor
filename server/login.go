package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type loginFailure struct {
	Message string
	Status  int
}

func (e *loginFailure) Error() string { return e.Message }

type loginRunner func(string) ([]byte, error)

func loginUBus(input string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "ubus", "call", "session", "login", input).CombinedOutput()
}
func createSession(run loginRunner, username, password string) (string, *loginFailure) {
	username = strings.TrimSpace(username)
	if username != "root" || password == "" {
		return "", &loginFailure{"请输入 root 用户名和软路由管理员密码。", 401}
	}
	args, _ := json.Marshal(map[string]any{"username": username, "password": password, "timeout": 1800})
	out, err := run(string(args))
	if err != nil {
		if strings.Contains(string(out), "Permission denied") {
			return "", &loginFailure{"用户名或密码不正确。请核对大小写、首尾空格，或从 iStoreOS「服务」菜单进入以复用登录。", 401}
		}
		return "", &loginFailure{"本机认证服务暂时不可用，请稍后重试或从 iStoreOS 服务菜单进入。", 503}
	}
	var result struct {
		Session string `json:"ubus_rpc_session"`
	}
	if json.Unmarshal(out, &result) != nil || !sessionPattern.MatchString(result.Session) || result.Session == strings.Repeat("0", 32) {
		return "", &loginFailure{"认证服务返回异常，未建立会话。请稍后重试。", 503}
	}
	return result.Session, nil
}

var loginTemplate = template.Must(template.New("login").Parse(strings.Replace(loginPage, "<input name=\"username\"", `{{if .Error}}<p role="alert" style="color:#b34d4d;background:#fff3f3;padding:12px;border-radius:7px">{{.Error}}</p>{{end}}<input name="username"`, 1)))

func renderLogin(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := loginTemplate.Execute(w, struct{ Error string }{message}); err != nil {
		fmt.Print("login render failed")
	}
}
func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderLogin(w, 200, "")
		return
	}
	if r.Method != http.MethodPost || !sameOrigin(r) {
		renderLogin(w, 403, "登录请求来源验证失败。请直接打开本机地址后重试。")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		renderLogin(w, 400, "无法读取登录表单，请重新输入。")
		return
	}
	session, failure := createSession(loginUBus, r.FormValue("username"), r.FormValue("password"))
	if failure != nil {
		renderLogin(w, failure.Status, failure.Message)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "homenet_session", Value: session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 1800})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
