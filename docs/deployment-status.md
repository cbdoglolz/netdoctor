# 原生部署与回退

目标环境：已实测 iStoreOS 24.10.7、Linux x86_64、约 2GB 内存。其他固件需分别验证。

服务目录 /opt/homenet-doctor；二进制按 releases/版本号 保存；current 为当前版本软链接。配置与历史版本位于发布目录外，不随版本切换覆盖。启动脚本 /etc/init.d/homenet-doctor 使用 procd，监听由 UCI network.lan.ipaddr 读取的 LAN 地址 8088 端口。

页面使用与路由器相同的 root 账号密码登录。HTTP 入口只适用于可信局域网；生产 HTTPS 集成尚未完成。不对 WAN 新增开放规则。

v0.2.1 起已集成 LuCI「服务 → HomeNet Doctor 网络诊断」菜单，点击后衔接已登录的 root 会话。服务启动会自动安装其专用控制器并失效菜单索引缓存，不重启 uhttpd。HTTPS 集成仍待完成。

## v0.3.0 运行依赖与存储

先在固件支持的官方源安装 `nlbwmon`、`arp-scan`；`avahi-utils` 用于可选的 mDNS 名称发现。缺包时对应能力不可用，不会自动更改转发设置。升级二进制不会替用户安装固件包。

```sh
opkg update
opkg install nlbwmon arp-scan
mkdir -p /opt/homenet-doctor/config/accounting
uci set nlbwmon.@nlbwmon[0].database_directory='/opt/homenet-doctor/config/accounting'
uci set nlbwmon.@nlbwmon[0].refresh_interval='10s'
uci set nlbwmon.@nlbwmon[0].commit_interval='60s'
uci set nlbwmon.@nlbwmon[0].database_limit='10000'
uci set nlbwmon.@nlbwmon[0].database_generations='2'
uci commit nlbwmon
/etc/init.d/nlbwmon enable
/etc/init.d/nlbwmon restart
```

已有 nlbwmon 时先备份并检查其用途，不能无条件覆盖既有统计位置。实际测试设备已将修改前配置备份到设备配置目录。上述为 x86 iStoreOS 当前包管理器示例，其他 OpenWrt 版本需按其包管理器适配。

设置与 SQLite 位于 `config/`，nlbwmon 持久文件位于 `config/accounting/`。备份应停止本监测服务并对 SQLite 完整备份（含 WAL 或完成检查点），同时保留 nlbwmon 数据；不能仅在服务运行时复制主库。回退旧二进制保留这些文件，但旧版不会读取新增历史。

## 安装

1. 从 GitHub Release 下载 homenet-doctor-linux-amd64 和对应 .sha256，核验散列。
2. 创建 /opt/homenet-doctor/releases/版本号，将二进制命名为 homenet-doctor，赋可执行权限。
3. 安装 server/apply-update.sh 到 /opt/homenet-doctor/apply-update.sh，安装 server/homenet-doctor.init 到 /etc/init.d/homenet-doctor，赋可执行权限。
4. 创建 current 软链接指向该发布目录，然后 enable/start 服务。
5. 访问 LAN地址:8088，验证 /health 版本、未登录 API 的 401，以及登录后的真实快照。

首次安装不覆盖已有目录，已有部署必须先确认当前版本并保留回退路径。

## 停止与回退

停止：/etc/init.d/homenet-doctor stop。取消开机启动：/etc/init.d/homenet-doctor disable。

回退：停止服务，将 current 链接指回此前 releases 中的已验证版本，再启动服务。原始路由器管理入口与转发配置不受影响。

更新脚本在安装后检查 /health 的版本；失败时还原旧链接并重启。当前安装包仅适配 Linux x86_64。不要将凭据、生产快照或路由器配置提交到仓库。
