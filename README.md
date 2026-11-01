# HomeNet Doctor

面向 iStoreOS / OpenWrt 的中文家庭网络观察界面，采用 UniFi 风格浅色设计，支持电脑与手机。

## v0.2.0 真实采集测试版

原生 Go 服务读取本机 CPU、内存、br-lan 接口速率和 IPv4 邻居表，界面每 10 秒刷新。使用路由器 root 账号通过本机 ubus 登录，密码不持久保存。只绑定 LAN 地址的 8088 端口，不修改转发、DNS 或防火墙。

**当前不能判断全网健康。** 按设备流量、DNS 活动、延迟丢包和物理拓扑尚未接入；邻居缓存不等于在线，只有 REACHABLE 记录标为在线。真实模式不会填充演示拓扑或告警。当前历史为最多 360 个内存样本，重启后清空，设置中的存储参数尚未驱动服务。

设置页显示本地版本，可从公开 GitHub Releases 检查在线版本与更新说明。安装前要求确认，服务端重新核验最新版本、下载大小与 SHA-256。切换后验证服务健康，失败回退旧版本；保留旧发布目录。升级不会改动路由器网络配置。

## 本地原型开发

```sh
npm ci
npm run dev
npm run typecheck
npm test
npm run build
```

本地静态运行使用固定演示数据，包含六个页面、两种部署模式、六种诊断场景。演示时刻固定为 2026-10-31 14:32。原生服务通过 runtime.json 开启真实模式。

## 原生服务构建

需要 Go 1.24+，构建前将 dist 内容复制到 server/web。然后在 server 目录使用 GOOS=linux GOARCH=amd64 CGO_ENABLED=0 编译。服务内嵌网页，无需在路由器安装 Node.js、Python 或 Go。

安装与回退见 [部署说明](docs/deployment-status.md)，每个版本的变更见 [CHANGELOG](CHANGELOG.md)。

![演示界面](docs/images/overview-desktop.png)

## 发布约定

仓库公开。每次实际变更更新版本、锁文件与 CHANGELOG，创建对应 Git tag 和 GitHub Release，并附 x86_64 二进制与 SHA-256。检查失败不能显示为“已是最新”。真实网络清单、密码、会话、配置与采集日志不提交。

Recharts、React Flow、Lucide 等组件遵循各自许可证；未复制 UniFi 的代码、标识或素材。
