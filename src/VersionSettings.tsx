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
          v0.2.1：新增 iStoreOS
          服务菜单入口与管理员会话衔接；修复登录失败后显示空白错误页，增加手机友好的错误提示、重试及密码显示。已核对
          8088 端口独占。
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
