import { useEffect, useState } from 'react';
import { kindLabels, type Device, type DeviceKind } from './data';
import { DeviceIcon, DeviceBadge } from './components';

async function deviceConfig(d: Device) {
  const r = await fetch('/api/config', { cache: 'no-store' });
  if (!r.ok) throw new Error('读取设备设置失败');
  const c = await r.json();
  return (
    c.devices.find((v: { ip: string }) => v.ip === d.ip) || {
      ip: d.ip,
      group: d.group || '',
      name: d.name,
      kind: d.kind,
      model: d.model,
      gateway: '',
      key: false,
      passive: false,
      monitorDisabled: false,
      icon: d.icon || '',
      customIcon: d.customIcon || '',
    }
  );
}
async function saveDevice(value: unknown) {
  const r = await fetch('/api/device', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(value),
  });
  const result = await r.json();
  if (!r.ok) throw new Error(result.error || '保存失败');
  return result.message;
}
export function MonitoringButton({
  devices,
  onMessage,
}: {
  devices: Device[];
  onMessage: (s: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [disabled, setDisabled] = useState<boolean | null>(null);
  if (!devices.length) return null;
  const off = disabled ?? devices.every((d) => d.monitorDisabled);
  return (
    <button
      className="button small"
      disabled={busy}
      onClick={async () => {
        setBusy(true);
        try {
          for (const d of devices) {
            const current = await deviceConfig(d);
            await saveDevice({ ...current, monitorDisabled: !off, key: off ? true : current.key });
          }
          setDisabled(!off);
          onMessage(
            off ? '已开启主动探测与设备报警' : '已关闭主动探测与设备报警；被动发现与流量统计继续',
          );
        } catch (e) {
          onMessage(String(e));
        } finally {
          setBusy(false);
        }
      }}
    >
      {off ? '开启监测' : '关闭监测'}
    </button>
  );
}
export default function DeviceEditor({ device: d, live }: { device: Device; live: boolean }) {
  const [open, setOpen] = useState(false);
 const [managerURL,setManagerURL]=useState('');
 useEffect(()=>{if(live)fetch('/api/catalog').then(r=>r.json()).then(v=>{if(v.enabled&&!v.isManager)setManagerURL(v.managerURL)}).catch(()=>{});},[live]);
  const [form, setForm] = useState<Awaited<ReturnType<typeof deviceConfig>> | null>(null);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  if (!live) return <DeviceBadge device={d} />;
  async function upload(file: File) {
    if (!['image/png', 'image/jpeg'].includes(file.type) || file.size > 2 * 1024 * 1024) {
      setMessage('请选择不超过 2 MiB 的 PNG / JPEG 图片');
      return;
    }
    try {
      const bitmap = await createImageBitmap(file);
      const canvas = document.createElement('canvas');
      const ratio = Math.min(1, 128 / Math.max(bitmap.width, bitmap.height));
      canvas.width = Math.max(1, Math.round(bitmap.width * ratio));
      canvas.height = Math.max(1, Math.round(bitmap.height * ratio));
      canvas.getContext('2d')!.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
      bitmap.close();
      const customIcon = canvas.toDataURL('image/png');
      if (customIcon.length > 65536) throw new Error('图标压缩后仍过大');
      setForm({ ...form, customIcon });
      setMessage('图标已载入，点击保存生效');
    } catch (e) {
      setMessage(String(e));
    }
  }
  return (
    <div className="device-editor">
      <button
        className="icon-picker-trigger"
        aria-label="编辑设备名称类型和图标"
        onClick={async () => {
          setOpen(!open);
          if (!open)
            try {
              setForm(await deviceConfig(d));
            } catch (e) {
              setMessage(String(e));
            }
        }}
      >
        <DeviceBadge
          device={
            form ? { ...d, kind: form.kind, icon: form.icon, customIcon: form.customIcon } : d
          }
        />
        <span>备注 / 类型 / 更换图标</span>
      </button>
      {open && form && (
        <div className="device-edit-form">
 {managerURL&&<p>公共资料由管理中心统一维护。<a href={managerURL+'/#devices'} target="_blank" rel="noreferrer">前往编辑备注、类型与图标</a>；下方监测开关仅作用于本机。</p>}
 <fieldset disabled={!!managerURL} className="catalog-public-fields">
          <label>
            自定义备注名称
            <input
              maxLength={100}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </label>
          <label>
            设备类型
            <select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
              {Object.entries(kindLabels).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </label>
          <label>拓扑分组<input maxLength={40} value={form.group||''} onChange={e=>setForm({...form,group:e.target.value})}/></label><p>分组仅用于显示，不代表接线关系。</p>
          <div className="icon-preset-grid">
            {Object.entries(kindLabels).map(([k, v]) => (
              <button
                key={k}
                className={form.icon === k && !form.customIcon ? 'selected' : ''}
                aria-label={`选择${v}图标`}
                onClick={() => setForm({ ...form, icon: k, customIcon: '' })}
              >
                <DeviceIcon kind={k as DeviceKind} />
                <span>{v}</span>
              </button>
            ))}
          </div>
          <label>
            上传自定义图标
            <input
              type="file"
              accept="image/png,image/jpeg"
              onChange={(e) => {
                if (e.target.files?.[0]) void upload(e.target.files[0]);
              }}
            />
          </label>
          </fieldset>
          <label>
            <input
              type="checkbox"
              checked={!form.monitorDisabled}
              onChange={(e) =>
                setForm({
                  ...form,
                  monitorDisabled: !e.target.checked,
                  key: e.target.checked ? true : form.key,
                })
              }
            />
            开启主动探测与报警
          </label>
          <button
            className="button primary"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                setMessage(await saveDevice(form));
              } catch (e) {
                setMessage(String(e));
              } finally {
                setBusy(false);
              }
            }}
          >
            保存设备标注
          </button>
        </div>
      )}
      <p role="status">{message}</p>
    </div>
  );
}
