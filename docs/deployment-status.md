# 原生部署与回退

目标环境：已实测 iStoreOS 24.10.7、Linux x86_64、约 2GB 内存。其他固件需分别验证。

服务目录 /opt/homenet-doctor；二进制按 releases/版本号 保存；current 为当前版本软链接。配置与历史版本位于发布目录外，不随版本切换覆盖。启动脚本 /etc/init.d/homenet-doctor 使用 procd，监听由 UCI network.lan.ipaddr 读取的 LAN 地址 8088 端口。

页面使用与路由器相同的 root 账号密码登录。HTTP 入口只适用于可信局域网；生产 HTTPS 集成尚未完成。不对 WAN 新增开放规则。

v0.2.1 起已集成 LuCI「服务 → HomeNet Doctor 网络诊断」菜单，点击后衔接已登录的 root 会话。服务启动会自动安装其专用控制器并失效菜单索引缓存，不重启 uhttpd。HTTPS 集成仍待完成。

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
