package main

import (
	"context"
	"errors"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	"net/url"
	"time"
)

func readESXi(cfg integrationConfig) ([]any, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, errors.New("ESXi 需要有效 HTTPS 地址")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	transport := soap.NewClient(u, false)
	if cfg.Fingerprint != "" {
		transport.SetThumbprint(u.Host, cfg.Fingerprint)
	}
	v, err := vim25.NewClient(ctx, transport)
	if err != nil {
		return nil, errors.New("ESXi 连接或证书校验失败")
	}
	client := &govmomi.Client{Client: v, SessionManager: session.NewManager(v)}
	if err = client.SessionManager.Login(ctx, url.UserPassword(cfg.Username, cfg.Password)); err != nil {
		return nil, errors.New("ESXi 认证失败")
	}
	defer client.Logout(ctx)
	manager := view.NewManager(v)
	container, err := manager.CreateContainerView(ctx, v.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, errors.New("ESXi 无虚拟机读取权限")
	}
	defer container.Destroy(ctx)
	var machines []mo.VirtualMachine
	if err = container.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "config.hardware.device", "runtime.powerState"}, &machines); err != nil {
		return nil, errors.New("ESXi 网络读取失败")
	}
	result := []any{}
	for _, vm := range machines {
		if vm.Config == nil {
			continue
		}
		for _, device := range vm.Config.Hardware.Device {
			nic, ok := device.(types.BaseVirtualEthernetCard)
			if !ok {
				continue
			}
			card := nic.GetVirtualEthernetCard()
			entry := map[string]any{"vm": vm.Name, "mac": card.MacAddress, "powerState": vm.Runtime.PowerState, "source": "ESXi vSphere API"}
			if card.Connectable != nil {
				entry["connected"] = card.Connectable.Connected
			}
			switch b := card.Backing.(type) {
			case *types.VirtualEthernetCardNetworkBackingInfo:
				entry["portgroup"] = b.DeviceName
			case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
				entry["portgroup"] = b.Port.PortgroupKey
				entry["switch"] = b.Port.SwitchUuid
			}
			result = append(result, entry)
		}
	}
	return result, nil
}
