import { useEffect, useState } from 'react';
import { kindLabels, type DeviceKind } from './data';
interface DeviceConfig {
  ip: string;
  name: string;
  kind: string;
  model: string;
  gateway: string;
  passive: boolean;
  key: boolean;
}
interface Config {
  interval: number;
  cache: number;
  storage: number;
  retention: number;
  devices: DeviceConfig[];
  links: { from: string; to: string; relation: string; source: string }[];
  integrations: Record<
    string,
    { url: string; username: string; password?: string; enabled: boolean; fingerprint?: string }
  >;
}
export default function ServiceSettings() {
  const [config, setConfig] = useState<Config | null>(null);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<{ name: string; available: boolean; reason?: string }[]>([]);
  useEffect(() => {
    fetch('/api/snapshot')
      .then((r) => r.json())
      .then((s) => setStatus(s.integrations || []))
      .catch(() => {});
    fetch('/api/config', { cache: 'no-store' })
      .then((r) => {
        if (!r.ok) throw new Error('配置读取失败');
        return r.json();
      })
      .then(setConfig)
      .catch((e) => setMessage(String(e)));
  }, []);
  async function save() {
    setBusy(true);
    try {
      const r = await fetch('/api/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config),
      });
      const result = await r.json();
      if (!r.ok) throw new Error(result.error);
      setMessage(result.message);
    } catch (e) {
      setMessage(String(e));
    } finally {
      setBusy(false);
    }
  }
  if (!config)
    return (
      <section className="panel">
        <div className="panel-header">
          <h2>服务端采集设置</h2>
          <p>{message || '读取中…'}</p>
        </div>
      </section>
    );
  return (
    <section className="panel version-settings">
      <div className="panel-header">
        <h2>后台采集与设备标注</h2>
        <button className="button primary" disabled={busy} onClick={save}>
          保存到软路由
        </button>
      </div>
      <div className="version-body">
        <p>采集独立于网页运行。以下参数保存在软路由，设备网关是逻辑关系，不代表直接接线。</p>
        <div className="settings-fields">
          {(
            [
              { key: 'interval', title: '关键设备采集间隔（秒）', min: 5, max: 300 },
              { key: 'cache', title: 'SQLite 缓存预算（MiB）', min: 16, max: 512 },
              { key: 'storage', title: '历史存储预算（MiB）', min: 128, max: 8192 },
              { key: 'retention', title: '保留时间（天）', min: 1, max: 365 },
            ] as const
          ).map((f) => (
            <label key={f.key}>
              <strong>{f.title}</strong>
              <input
                aria-label={f.title}
                type="number"
                min={f.min}
                max={f.max}
                value={config[f.key]}
                onChange={(e) => setConfig({ ...config, [f.key]: Number(e.target.value) })}
              />
            </label>
          ))}
        </div>
        <h3>设备名称、类型和网关</h3>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>IP / 节点</th>
                <th>名称</th>
                <th>类型</th>
                <th>型号</th>
                <th>逻辑网关</th>
                <th>关键设备</th>
                <th>被动监测</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {config.devices.map((d, i) => (
                <tr key={i}>
                  {(['ip', 'name', 'kind', 'model', 'gateway', 'key', 'passive'] as const).map(
                    (key) => (
                      <td key={key}>
                        {key === 'kind' ? (
                          <select
                            value={d.kind}
                            onChange={(e) =>
                              setConfig({
                                ...config,
                                devices: config.devices.map((v, n) =>
                                  n === i ? { ...v, kind: e.target.value } : v,
                                ),
                              })
                            }
                          >
                            {Object.keys(kindLabels).map((k) => (
                              <option key={k} value={k}>
                                {kindLabels[k as DeviceKind]}
                              </option>
                            ))}
                          </select>
                        ) : (
                          <input
                            aria-label={`${d.ip} ${key}`}
                            type={key === 'key' || key === 'passive' ? 'checkbox' : 'text'}
                            checked={key === 'key' || key === 'passive' ? d[key] : undefined}
                            value={key === 'key' || key === 'passive' ? undefined : String(d[key])}
                            onChange={(e) =>
                              setConfig({
                                ...config,
                                devices: config.devices.map((v, n) =>
                                  n === i
                                    ? {
                                        ...v,
                                        [key]:
                                          key === 'key' || key === 'passive'
                                            ? e.target.checked
                                            : e.target.value,
                                      }
                                    : v,
                                ),
                              })
                            }
                          />
                        )}
                      </td>
                    ),
                  )}
                  <td>
                    <button
                      className="button"
                      onClick={() =>
                        setConfig({ ...config, devices: config.devices.filter((_, n) => n !== i) })
                      }
                    >
                      删除标注
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <button
          className="button"
          onClick={() =>
            setConfig({
              ...config,
              devices: [
                ...config.devices,
                {
                  ip: '',
                  name: '',
                  kind: 'iot',
                  model: '',
                  gateway: '',
                  key: false,
                  passive: false,
                },
              ],
            })
          }
        >
          添加设备标注
        </button>
        <h3>连接关系标注</h3>
        <p>填写两端 IP 或节点编号。仅将亲自确认的关系标为已确认；未知端口不要填写为实际接线。</p>
        {config.links.map((l, i) => (
          <fieldset key={i}>
            <legend>连接 {i + 1}</legend>
            {(['from', 'to', 'source'] as const).map((k) => (
              <label key={k}>
                {k === 'from'
                  ? '起点（上级）'
                  : k === 'to'
                    ? '终点（下级）'
                    : '来源 / 依据（推测请含“待核实”）'}
                <input
                  value={l[k]}
                  onChange={(e) =>
                    setConfig({
                      ...config,
                      links: config.links.map((v, n) =>
                        n === i ? { ...v, [k]: e.target.value } : v,
                      ),
                    })
                  }
                />
              </label>
            ))}
            <label>
              关系
              <select
                value={l.relation}
                onChange={(e) =>
                  setConfig({
                    ...config,
                    links: config.links.map((v, n) =>
                      n === i ? { ...v, relation: e.target.value } : v,
                    ),
                  })
                }
              >
                {[
                  ['physical', '物理接线'],
                  ['virtual', '虚拟连接'],
                  ['wireless', '无线关联'],
                ].map(([value, title]) => (
                  <option key={value} value={value}>
                    {title}
                  </option>
                ))}
              </select>
            </label>
            <button
              className="button"
              onClick={() =>
                setConfig({ ...config, links: config.links.filter((_, n) => n !== i) })
              }
            >
              删除连接
            </button>
          </fieldset>
        ))}
        <button
          className="button"
          onClick={() =>
            setConfig({
              ...config,
              links: [
                ...config.links,
                { from: '', to: '', relation: 'physical', source: '用户标注 · 待核实' },
              ],
            })
          }
        >
          添加连接
        </button>
        <h3>管理接口（凭据仅保存在设备端）</h3>
        {['esxi', 'ikuai', 'xiaomi'].map((name) => {
          const v = config.integrations[name] || { url: '', username: '', enabled: false };
          return (
            <fieldset key={name}>
              <legend>{name}</legend>
              <p>
                {status.find((s) => s.name === name)?.reason ||
                  (status.find((s) => s.name === name)?.available
                    ? '只读接口已连接'
                    : '等待读取状态')}
              </p>
              {name !== 'esxi' && <p>此版本尚未实现该接口适配，不会读取或使用其管理数据。</p>}
              <label>
                <input
                  type="checkbox"
                  disabled={name !== 'esxi'}
                  checked={v.enabled}
                  onChange={(e) =>
                    setConfig({
                      ...config,
                      integrations: {
                        ...config.integrations,
                        [name]: { ...v, enabled: e.target.checked },
                      },
                    })
                  }
                />
                启用只读接入
              </label>
              {name === 'esxi' &&
                (['url', 'username', 'password', 'fingerprint'] as const).map((key) => (
                  <label key={key}>
                    {key}
                    <input
                      type={key === 'password' ? 'password' : 'text'}
                      autoComplete="off"
                      value={v[key] || ''}
                      onChange={(e) =>
                        setConfig({
                          ...config,
                          integrations: {
                            ...config.integrations,
                            [name]: { ...v, [key]: e.target.value },
                          },
                        })
                      }
                    />
                  </label>
                ))}
            </fieldset>
          );
        })}
        <p role="status">{message}</p>
      </div>
    </section>
  );
}
