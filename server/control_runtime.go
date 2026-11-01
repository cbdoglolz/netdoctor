package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

func executeControl(r controlRequest) (string, error) {
	switch r.Kind {
	case "pause", "schedule":
		if e := validateControlDevice(r, controlSnapshot(), time.Now()); e != nil {
			return "", e
		}
		if e := controlCapability(); e != nil {
			return "", e
		}
		controls.Lock()
		if len(controls.state.Policies) >= 64 {
			controls.Unlock()
			return "", errors.New("最多保存 64 条策略，请先删除停用策略")
		}
		old := append([]accessPolicy{}, controls.state.Policies...)
		p := accessPolicy{ID: r.ID, IP: r.IP, MAC: strings.ToLower(r.MAC), Enabled: true, Until: time.Now().Add(time.Duration(r.Minutes) * time.Minute), Status: "pending", Detail: "待本机规则安装"}
		if r.Kind == "schedule" {
			p.Start = r.Start
			p.End = r.End
			p.Until = time.Time{}
		}
		controls.state.Policies = append(controls.state.Policies, p)
		e := saveControlLocked()
		if e != nil {
			controls.state.Policies = old
		}
		controls.Unlock()
		if e != nil {
			return "", e
		}
		if e = reconcileControl(); e != nil {
			return "策略已保存，但安装未确认；请查看策略状态，可随时停用", e
		}
		return "策略已保存；规则安装状态和匹配计数见策略列表。未验证终端最终体验。", nil
	case "delete-policy":
		controls.Lock()
		old := append([]accessPolicy{}, controls.state.Policies...)
		found := false
		for i, p := range controls.state.Policies {
			if p.ID == r.PolicyID {
				if p.Enabled && policyActive(p, time.Now()) {
					controls.Unlock()
					return "", errors.New("请先停用生效中的策略")
				}
				controls.state.Policies = append(controls.state.Policies[:i], controls.state.Policies[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			controls.Unlock()
			return "", errors.New("策略不存在")
		}
		e := saveControlLocked()
		if e != nil {
			controls.state.Policies = old
		}
		controls.Unlock()
		if e != nil {
			return "", e
		}
		if len(old) == 1 {
			controlRun("nft", "delete", "table", "netdev", "hnd_control")
			controlFirewall.signature = ""
			return "已删除策略；保留执行历史", nil
		}
		return "已删除策略；保留执行历史", reconcileControl()
	case "policy":
		controls.Lock()
		found := false
		old := append([]accessPolicy{}, controls.state.Policies...)
		for i := range controls.state.Policies {
			if controls.state.Policies[i].ID == r.PolicyID {
				controls.state.Policies[i].Enabled = r.Enabled
				found = true
			}
		}
		if !found {
			controls.Unlock()
			return "", errors.New("策略不存在")
		}
		e := saveControlLocked()
		if e != nil {
			controls.state.Policies = old
		}
		controls.Unlock()
		if e != nil {
			return "", e
		}
		e = reconcileControl()
		return "策略开关已保存；其他仍启用的重叠策略继续生效。若清理失败，旧阻断元素将在最多约 45 秒内失效。", e
	case "diagnose":
		ctx, cancel := context.WithTimeout(context.Background(), 22*time.Second)
		defer cancel()
		ping, e := runControlCommand(ctx, "ping", "-4", "-c", "3", "-W", "2", r.Target)
		trace, traceErr := runControlCommand(ctx, "traceroute", "-n", "-m", "6", "-w", "1", r.Target)
		result := "结论：本节点到目标的 ICMP 探测有响应；不代表终端实际业务体验。"
		if e != nil {
			result = "结论：本节点未确认目标 ICMP 可达；目标禁 ICMP 或链路问题均有可能，不能直接判断断网。"
		}
		if traceErr != nil {
			trace += "\n路由追踪未完成或目标不应答"
		}
		return result + "\n\nICMP：\n" + ping + "\n路由追踪：\n" + trace, nil
	case "dns":
		out, e := controlRun("nslookup", r.Target)
		if e != nil {
			return "结论：本节点系统解析器查询失败或超时。\n" + out, e
		}
		return "结论：本节点系统解析器查询成功；不代表终端采用相同 DNS。\n" + out, nil
	case "service-status", "service-restart":
		path := "/etc/init.d/" + r.Service
		if _, e := os.Stat(path); e != nil {
			return "", errors.New("服务未安装")
		}
		if r.Kind == "service-restart" {
			out, e := controlRun(path, "restart")
			if e != nil {
				return out, e
			}
			time.Sleep(time.Second)
		}
		out, e := controlRun(path, "status")
		if e != nil {
			return "服务状态未确认运行：\n" + out, e
		}
		return "服务状态检查通过：\n" + out, nil
	}
	return "", errors.New("未知操作")
}

var controlFirewall = struct{ signature string }{}

func controlCapability() error {
	if _, e := net.InterfaceByName("eth0"); e != nil {
		return errors.New("首版只支持 eth0 入站的 x86 旁路由，当前接口不支持")
	}
	hw, _ := controlRun("uci", "-q", "get", "firewall.@defaults[0].flow_offloading_hw")
	if strings.TrimSpace(hw) == "1" {
		return errors.New("启用了硬件卸载，不能保证入口规则覆盖；本软件不会自动更改卸载配置")
	}
	if _, e := controlRun("nft", "--version"); e != nil {
		return errors.New("nft 不可用")
	}
	return nil
}
func controlMACs() (string, error) {
	macs := []string{}
	for _, name := range []string{"eth0", "br-lan"} {
		if i, e := net.InterfaceByName(name); e == nil && len(i.HardwareAddr) == 6 {
			m := i.HardwareAddr.String()
			if len(macs) == 0 || macs[0] != m {
				macs = append(macs, m)
			}
		}
	}
	if len(macs) == 0 {
		return "", errors.New("无法确认本节点下一跳 MAC")
	}
	return strings.Join(macs, ", "), nil
}
func nftControl(script string, check bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	args := []string{"-f", "-"}
	if check {
		args = []string{"-c", "-f", "-"}
	}
	c := exec.CommandContext(ctx, "nft", args...)
	c.Stdin = strings.NewReader(script)
	out, e := c.CombinedOutput()
	if e != nil {
		return fmt.Errorf("nft 操作失败：%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// All identifiers and addresses in this script have strict validators. Only our
// own table is modified; firewall4, proxy and route configuration remain intact.
func controlTableScript(ps []accessPolicy, macs string, exists bool) string {
	s := ""
	if exists {
		s = "delete table netdev hnd_control\n"
	}
	s += "add table netdev hnd_control\nadd chain netdev hnd_control ingress { type filter hook ingress device \"eth0\" priority -500; policy accept; }\n"
	for _, p := range ps {
		if !controlID.MatchString(p.ID) || net.ParseIP(p.IP) == nil {
			continue
		}
		id := strings.ReplaceAll(p.ID, "-", "")
		s += fmt.Sprintf("add set netdev hnd_control p%s { type ipv4_addr . ether_addr; flags timeout; }\nadd counter netdev hnd_control c%s\nadd rule netdev hnd_control ingress ether daddr { %s } ip saddr . ether saddr @p%s ip daddr != { 0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4, 240.0.0.0/4 } counter name c%s drop\n", id, id, macs, id, id)
	}
	return s
}

var packetsPattern = regexp.MustCompile(`packets ([0-9]+)`)

func reconcileControl() error {
	controls.Lock()
	defer controls.Unlock()
	if !controls.settings.Enabled {
		return nil
	}
	if len(controls.state.Policies) == 0 {
		return nil
	}
	macs, e := controlMACs()
	if e != nil {
		controls.err = e.Error()
		return e
	}
	if e = controlCapability(); e != nil {
		controls.err = e.Error()
		return e
	}
	snapshot := controlSnapshot()
	ds, _ := snapshot["devices"].([]any)
	identities := map[string]string{}
	stamp, _ := time.Parse(time.RFC3339, fmt.Sprint(snapshot["observedAt"]))
	fresh := !stamp.IsZero() && time.Since(stamp) < 2*time.Minute && time.Until(stamp) < time.Minute
	for _, raw := range ds {
		d, _ := raw.(map[string]any)
		identities[fmt.Sprint(d["ip"])] = strings.ToLower(fmt.Sprint(d["mac"]))
	}
	_, listErr := controlRun("nft", "list", "table", "netdev", "hnd_control")
	sig := controlTableScript(controls.state.Policies, macs, false)
	if controlFirewall.signature != sig || listErr != nil {
		script := controlTableScript(controls.state.Policies, macs, listErr == nil)
		if e = nftControl(script, true); e == nil {
			e = nftControl(script, false)
		}
		if e != nil {
			controls.err = e.Error()
			return e
		}
		controlFirewall.signature = sig
	}
	script := ""
	now := time.Now()
	for i := range controls.state.Policies {
		p := &controls.state.Policies[i]
		id := strings.ReplaceAll(p.ID, "-", "")
		script += fmt.Sprintf("flush set netdev hnd_control p%s\n", id)
		p.Status = "inactive"
		p.Detail = "策略未到生效时间或已暂停"
		if p.Start == "" && !now.Before(p.Until) {
			p.Status = "expired"
			p.Detail = "暂停已到期"
		}
		if policyActive(*p, now) {
			if !fresh || identities[p.IP] != p.MAC {
				p.Status = "unavailable"
				p.Detail = "采集过期或 IP/MAC 不匹配，未继续阻断"
				continue
			}
			seconds := 45
			if p.Start == "" {
				seconds = min(seconds, max(1, int(time.Until(p.Until).Seconds())))
			}
			script += fmt.Sprintf("add element netdev hnd_control p%s { %s . %s timeout %ds }\n", id, p.IP, p.MAC, seconds)
			p.Status = "installed"
			p.Detail = "已安装入口规则；尚无终端结果验证"
		}
	}
	if e = nftControl(script, true); e == nil {
		e = nftControl(script, false)
	}
	if e != nil {
		for i := range controls.state.Policies {
			controls.state.Policies[i].Status = "error"
			controls.state.Policies[i].Detail = "规则更新失败，旧元素最多约 45 秒过期"
		}
		controls.err = e.Error()
		saveControlLocked()
		return e
	}
	// Counter lifetime is since this node's control table was created/rebuilt.
	for i := range controls.state.Policies {
		p := &controls.state.Policies[i]
		if p.Status != "installed" {
			continue
		}
		out, _ := controlRun("nft", "list", "counter", "netdev", "hnd_control", "c"+strings.ReplaceAll(p.ID, "-", ""))
		if m := packetsPattern.FindString(out); m != "" {
			p.Detail = "已安装；本次规则表累计 " + m + "（匹配不等于终端完整断网验证）"
		}
	}
	controls.err = ""
	if e = saveControlLocked(); e != nil {
		controls.err = "状态持久保存失败：" + e.Error()
	}
	return e
}
func runControls(ctx context.Context) {
	// A single worker serializes side effects and nft transactions.
	timer := time.NewTicker(10 * time.Second)
	defer timer.Stop()
	reconcileControl()
	lastReconcile := time.Now()
	for {
		if ctx.Err() != nil {
			controlRun("nft", "delete", "table", "netdev", "hnd_control")
			return
		}
		if time.Since(lastReconcile) > 10*time.Second {
			reconcileControl()
			lastReconcile = time.Now()
		}
		controls.Lock()
		index := -1
		var req controlRequest
		for i, t := range controls.state.Tasks {
			if t.State == "queued" {
				index = i
				req = t.Request
				break
			}
		}
		if index >= 0 {
			controls.state.Tasks[index].State = "running"
			if e := saveControlLocked(); e != nil {
				controls.state.Tasks[index].State = "queued"
				controls.err = e.Error()
				index = -1
			}
		}
		controls.Unlock()
		if index >= 0 {
			out, e := executeControl(req)
			if len(out) > 8000 {
				out = out[:8000]
			}
			if e != nil {
				out += "\n" + e.Error()
			}
			controls.Lock()
			for i := range controls.state.Tasks {
				t := &controls.state.Tasks[i]
				if t.Request.ID == req.ID {
					t.State = "completed"
					if e != nil {
						t.State = "failed"
					}
					t.Output = out
					now := time.Now()
					t.Finished = &now
				}
			}
			if e := saveControlLocked(); e != nil {
				controls.err = e.Error()
			}
			controls.Unlock()
			continue
		}
		select {
		case <-ctx.Done():
			controlRun("nft", "delete", "table", "netdev", "hnd_control")
			return
		case <-controls.wake:
		case <-timer.C:
			reconcileControl()
		}
	}
}

// Read-only status is encoded separately from private authorization material.
func controlStateJSON() []byte {
	controls.Lock()
	defer controls.Unlock()
	b, _ := json.Marshal(controls.state)
	return b
}
