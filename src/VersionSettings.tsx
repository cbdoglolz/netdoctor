import { useRef, useState } from 'react';
import { Download, RefreshCw, X } from 'lucide-react';
import packageInfo from '../package.json';

interface ReleaseInfo {
  localVersion: string;
  onlineVersion: string;
  notes: string;
  updateAvailable: boolean;
  canUpdate: boolean;
  reason?: string;
  releaseUrl?: string;
}
export default function VersionSettings() {
  const [release, setRelease] = useState<ReleaseInfo | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const confirmDialog = useRef<HTMLDialogElement>(null);
  async function check() {
    setBusy(true);
    setMessage('');
    setRelease(null);
    try {
      const response = await fetch('/api/version', {
        cache: 'no-store',
        headers: { Accept: 'application/json' },
      });
      if (!response.headers.get('content-type')?.includes('application/json'))
        throw new Error('尚未连接软路由更新服务，无法检查公开 GitHub Releases。');
      const data = (await response.json()) as ReleaseInfo & { error?: string };
      if (!response.ok || data.error) throw new Error(data.error || '检查更新失败');
      if (
        typeof data.localVersion !== 'string' ||
        typeof data.onlineVersion !== 'string' ||
        typeof data.updateAvailable !== 'boolean'
      )
        throw new Error('更新服务返回的数据格式无效。');
      setRelease(data);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '检查更新失败，请稍后重试。');
    } finally {
      setBusy(false);
    }
  }
  async function update() {
    confirmDialog.current?.close();
    if (!release?.canUpdate || !release.updateAvailable) return;
    setBusy(true);
    setMessage('正在提交更新，请保持页面打开。');
    try {
      const response = await fetch('/api/update', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirmedVersion: release.onlineVersion }),
      });
      const data = (await response.json()) as { message?: string; error?: string };
      if (!response.ok || data.error) throw new Error(data.error || '更新请求失败');
      setRelease(null);
      setMessage(data.message || '更新已提交，请等待服务恢复后重新检查本地版本。');
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '更新失败，尚未确认安装成功。');
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel version-settings">
      <div className="panel-header">
        <div>
          <h2>版本与更新</h2>
          <p>从公开 GitHub 仓库检查发布版本，安装前需要确认。</p>
        </div>
        <button className="button" disabled={busy} onClick={check}>
          <RefreshCw size={14} />
          {busy ? '处理中…' : '检查更新'}
        </button>
      </div>
      <div className="version-body">
        <dl>
          <dt>本地版本</dt>
          <dd>v{release?.localVersion || packageInfo.version}</dd>
          <dt>在线版本</dt>
          <dd>{release ? `v${release.onlineVersion}` : '尚未检查'}</dd>
          <dt>发布来源</dt>
          <dd>
            <a
              href="https://github.com/cbdoglolz/netdoctor/releases"
              target="_blank"
              rel="noreferrer"
            >
              cbdoglolz/netdoctor
            </a>
          </dd>
        </dl>
        <h3>本地版本更新内容</h3>

        <p>
          v0.4.0
          测试构建：新增管理中心受限任务通道、按节点暂停与到期恢复、每日定时策略、策略停用恢复入口、双节点主动探测、DNS 查询和允许名单内服务管理。每项操作保留接收与执行记录，暂停仅覆盖本节点可见 IPv4 公网请求。流量路径徽标、详情和拓扑连线明确显示具体旁路由 IP，历史与过期路径保留节点地址。新增只读 HTTPS 双节点联动、统一设备与路径视图、共享标注补充，各节点计量独立保留。保留当前可见连接，按设备查看外部目标 IP、端口、协议、状态及数量；支持搜索、排序和分页，仅内存保存。保留双向排序、扫描变更、暂停监测恢复入口和事件记录、拓扑分组折叠、网格吸附与撤销重做。流量自动选择 B/KB/MB/GB/TB；新增设备删除与恢复、重新扫描与记录日期；拓扑支持拖动、跨浏览器保存和自动分层恢复。后台有界观察自动标注流量经旁路由的设备；拓扑显示蓝色有向连线，列表可筛选近期与历史路径，标注保留观测时间。只证明公网请求交付，不读取终端网关配置。保留
          SQLite
          历史、可见路径流量、服务端设置与物理/虚拟/网关分层拓扑。接口接入状态按实际能力显示。
        </p>
        {release && (
          <>
            <h3>{release.updateAvailable ? '发现新版本' : '当前已是最新版本'}</h3>
            <pre>{release.notes || '该版本未提供更新说明。'}</pre>
            {release.reason && <p>{release.reason}</p>}
            {release.updateAvailable && (
              <button
                className="button primary"
                disabled={!release.canUpdate || busy}
                onClick={() => confirmDialog.current?.showModal()}
              >
                <Download size={14} />
                更新到 v{release.onlineVersion}
              </button>
            )}
          </>
        )}
        {message && (
          <p role="status" className="update-message">
            {message}
          </p>
        )}
        <p className="muted">公开 Releases 无需访问凭据。检查失败不会显示为“已是最新”。</p>
      </div>
      <dialog ref={confirmDialog} className="update-confirm" aria-labelledby="update-title">
        <h2 id="update-title">确认安装更新？</h2>
        <p>
          本地 v{release?.localVersion} → 在线 v{release?.onlineVersion}
        </p>
        <p>更新会短暂中断 HomeNet Doctor 服务。请确认已经阅读上方更新内容。</p>
        <div className="settings-actions">
          <button className="button secondary" onClick={() => confirmDialog.current?.close()}>
            <X size={14} />
            取消
          </button>
          <button className="button primary" onClick={update}>
            确认更新
          </button>
        </div>
      </dialog>
    </section>
  );
}
