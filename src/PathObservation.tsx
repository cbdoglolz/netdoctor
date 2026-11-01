import { useEffect, useState } from 'react';
type Evidence = {
  ip: string;
  name: string;
  mac: string;
  family: number;
  status: string;
  current: boolean;
  lastSeen?: string;
  received: number;
  forwardPackets: number;
  replyPackets: number;
  forwarded: number;
  bidirectional: number;
  offloaded: boolean;
  flows: string[];
  phase: string;
};
type Report = {
  id: string;
  state: string;
  startedAt: string;
  endsAt: string;
  phase: string;
  devices: Evidence[];
  phases: { name: string; startedAt: string; devices: Evidence[] }[];
  warnings: string[];
  conclusion: string;
};
type Response = { current: Report | null; history: { id: string; createdAt: string }[] };
const states: Record<string, string> = {
  unknown: '尚无路径证据',
  received: '公网请求交给本机，后续未验证',
  forwarded: '指定连接转发已验证',
  bidirectional: '指定连接双向路径已验证',
};
const phases: Record<string, string> = {
  baseline: '初始观察',
  gateway6: '用户声明 IPv4 网关 .6',
  gateway3: '用户声明 IPv4 网关 .3',
  restored: '用户声明已恢复原设置',
};
function EvidenceTable({ devices }: { devices: Evidence[] }) {
  const [showAll, setShowAll] = useState(false);
  const visible = showAll ? devices : devices.filter((e) => e.received > 0);
  return (
    <>
      <button className="text-button" onClick={() => setShowAll(!showAll)}>
        {showAll ? '只看有证据的设备' : '展开全部观察对象'}（{devices.length}）
      </button>
      {!visible.length && (
        <p>本阶段尚无设备交付公网请求的有效样本；不代表这些设备没有设置旁路由网关。</p>
      )}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>设备 / 地址</th>
              <th>路径结论</th>
              <th>本阶段证据</th>
              <th>最近交付公网请求</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((e) => (
              <tr key={e.ip}>
                <td>
                  <strong>{e.name}</strong>
                  <br />
                  {e.ip} · IPv{e.family}
                  <br />
                  <small>{e.mac || '未取得当前 MAC'}</small>
                </td>
                <td>
                  {states[e.status] || e.status}
                  {e.status !== 'unknown' && !e.current && (
                    <>
                      <br />
                      <strong>历史证据，非当前状态</strong>
                    </>
                  )}
                  {e.offloaded && (
                    <>
                      <br />
                      <small>连接存在加速标记，覆盖可能不完整</small>
                    </>
                  )}
                </td>
                <td>
                  交付 {e.received} 包 · 转发 {e.forwarded} 条 · 双向 {e.bidirectional} 条<br />
                  <small>
                    转发链出 / 回程计数：{e.forwardPackets} / {e.replyPackets}
                  </small>
                  <details>
                    <summary>连接头证据</summary>
                    {e.flows.length
                      ? e.flows.map((f) => (
                          <div key={f} className="mono">
                            {f}
                          </div>
                        ))
                      : '未取得有效样本'}
                  </details>
                </td>
                <td>{e.lastSeen ? new Date(e.lastSeen).toLocaleString('zh-CN') : '尚无样本'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
export default function PathObservation({ live }: { live: boolean }) {
  const [data, setData] = useState<Response>({ current: null, history: [] });
  const [saved, setSaved] = useState<Report | null>(null);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  async function refresh() {
    try {
      const r = await fetch('/api/path-observation', { cache: 'no-store' });
      if (!r.ok)
        throw new Error(r.status === 401 ? '登录已过期，请重新登录' : '路径观察状态读取失败');
      setData(await r.json());
    } catch (e) {
      setMessage(String(e));
    }
  }
  useEffect(() => {
    if (!live) return;
    void refresh();
    const id = setInterval(() => void refresh(), 5000);
    return () => clearInterval(id);
  }, [live]);
  async function action(method: string, body?: unknown) {
    setBusy(true);
    try {
      const r = await fetch('/api/path-observation', {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      const v = await r.json();
      if (!r.ok) throw new Error(v.error);
      setMessage(v.message);
      setSaved(null);
      await refresh();
    } catch (e) {
      setMessage(String(e));
    } finally {
      setBusy(false);
    }
  }
  const report = saved || data.current;
  const running = data.current?.state === 'running';
  function download() {
    if (!report) return;
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' }),
    );
    const a = document.createElement('a');
    a.href = url;
    a.download = `homenet-path-${report.id}.json`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  if (!live)
    return (
      <section className="panel version-body">
        <h2>路径验证</h2>
        <p>此功能需要连接软路由真实服务，不使用演示证据。</p>
      </section>
    );
  return (
    <section className="panel">
      <div className="panel-header">
        <div>
          <h2>设备路径验证</h2>
          <p>证明实际观察到的连接路径，不冒充终端默认网关配置。</p>
        </div>
      </div>
      <div className="version-body path-observation">
        <p>
          观察只添加独立计数规则，不修改放行、路由、DNS、加速或终端网关。原始抓包仅在内存处理，默认运行
          15 分钟，关闭网页仍继续。单轮最多 60 秒、10,000 包、128 字节包头；达到限制会提示覆盖不足。
        </p>
        <div className="alert-actions">
          <button
            className="button primary"
            disabled={busy || running}
            onClick={() => void action('POST', { seconds: 900 })}
          >
            开始观察 15 分钟
          </button>
          <button
            className="button"
            disabled={busy || !running}
            onClick={() => void action('DELETE')}
          >
            停止并保存报告
          </button>
          <button className="button" disabled={!report} onClick={download}>
            导出当前报告
          </button>
        </div>
        <p role="status">{message}</p>
        <details className="path-instructions">
          <summary>手机网关对照测试步骤</summary>
          <ol>
            <li>连接家庭 Wi-Fi，暂时关闭蜂窝数据和终端 VPN。记录手机当前 IP / MAC。</li>
            <li>将 IPv4 网关设为 .6，点击下面“.6 阶段”，建立新的公网连接并持续下载约一分钟。</li>
            <li>
              将 IPv4 网关改为 .3，点击“.3 阶段”，关闭旧连接后对相同公网目标重新测试约一分钟。
            </li>
            <li>恢复原设置，点击“恢复阶段”，重复新连接测试。</li>
            <li>
              阶段按钮只记录你的声明，不会修改手机网关。IPv6需独立验证；没有样本不能推断已关闭。
            </li>
          </ol>
        </details>
        <div className="alert-actions">
          {(['gateway6', 'gateway3', 'restored'] as const).map((phase) => (
            <button
              className="button"
              key={phase}
              disabled={!running || busy}
              onClick={() => void action('POST', { phase })}
            >
              {phase === 'gateway6' ? '.6 阶段' : phase === 'gateway3' ? '.3 阶段' : '恢复阶段'}
            </button>
          ))}
        </div>
        {saved && (
          <button className="text-button" onClick={() => setSaved(null)}>
            返回当前任务
          </button>
        )}
        {report ? (
          <>
            <h3>
              {report.state === 'running' ? '观察进行中' : '观察已结束'} · {phases[report.phase]}
            </h3>
            <p>
              开始：{new Date(report.startedAt).toLocaleString('zh-CN')} · 计划结束：
              {new Date(report.endsAt).toLocaleString('zh-CN')}
            </p>
            <div className="coverage-note">{report.conclusion || '等待有效路径样本'}</div>
            <ul>
              {report.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
            <p>
              仅表示这些具体连接在观察窗口内的证据。空闲设备、其他协议、绕行连接和终端的全部默认路由不在该结论之内。只收到请求、未证实转发的样本也可能来自代理／重定向或被丢弃的连接。
            </p>
            <EvidenceTable devices={report.devices} />
            <h3>对照阶段记录</h3>
            {report.phases.map((p, i) => (
              <details key={i}>
                <summary>
                  {phases[p.name] || p.name} · {new Date(p.startedAt).toLocaleTimeString('zh-CN')}
                  （用户声明，非自动读取配置）
                </summary>
                <EvidenceTable devices={p.devices} />
              </details>
            ))}
          </>
        ) : (
          <p>尚未开始观察。开始后才能获得路径证据。</p>
        )}
        <h3>最近报告（最多 20 份）</h3>
        <div className="path-history">
          {data.history.map((h) => (
            <button
              key={h.id}
              className="button"
              onClick={async () => {
                try {
                  const r = await fetch(
                    `/api/path-observation?report=${encodeURIComponent(h.id)}`,
                    { cache: 'no-store' },
                  );
                  if (!r.ok) throw new Error('报告读取失败');
                  setSaved(await r.json());
                } catch (e) {
                  setMessage(String(e));
                }
              }}
            >
              {new Date(h.createdAt).toLocaleString('zh-CN')}
            </button>
          ))}
        </div>
      </div>
    </section>
  );
}
