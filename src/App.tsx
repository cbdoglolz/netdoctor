import Peers from './Peers';
import Connections from './Connections';
import {compareDevices} from './device-sort';
import InventoryControls, {DeleteDevice, ScanChanges} from './InventoryControls';
import PathObservation from './PathObservation';
import DeviceEditor, { MonitoringButton } from './DeviceEditor';
import ServiceSettings from './ServiceSettings';
import { useLiveSnapshot } from './useLiveSnapshot';
import packageInfo from '../package.json';
import VersionSettings from './VersionSettings';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import {
  Activity,
  ArrowDown,
  ArrowRight,
  ArrowUp,
  Bell,
  Check,
  CheckCheck,
  ChevronDown,
  ChevronRight,
  Clock3,
  Database,
  Download,
  Globe2,
  Info,
  LayoutDashboard,
  ListFilter,
  Network,
  Radio,
  Search,
  Settings2,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  X,
} from 'lucide-react';
import {
  ChartLegend,
  BypassBadge,
  DeviceBadge,
  DeviceIcon,
  StatusBadge,
  Topology,
  TrafficChart,
} from './components';
import {
  availabilityLabels,
  defaults,
  demoService,
  displayMetric,
  kindLabels,
  loadSettings,
  scenarioLabels,
  SETTINGS_KEY,
  type Alert,
  type Device,
  type Mode,
  type Range,
  type Scenario,
  type Settings,
  type Snapshot,
} from './data';

const pages = [
  { id: 'overview', title: '网络总览', icon: LayoutDashboard },
  { id: 'topology', title: '设备拓扑', icon: Network },
  { id: 'devices', title: '设备列表', icon: Radio },
  { id: 'traffic', title: '流量与访问', icon: Activity },
  { id: 'alerts', title: '告警与诊断', icon: Bell },
  { id: 'path', title: '路径验证', icon: Network },
  { id: 'peers', title: '节点联动', icon: Network },
  { id: 'settings', title: '设置', icon: Settings2 },
] as const;
type Page = (typeof pages)[number]['id'];
const pageFromHash = (): Page => pages.find((p) => `#${p.id}` === location.hash)?.id ?? 'overview';
const dateText = (value: string) =>
  new Date(value).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    timeZone: 'Asia/Shanghai',
  });
function Panel({
  title,
  subtitle,
  action,
  children,
  className = '',
}: {
  title: string;
  subtitle?: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`panel ${className}`}>
      <div className="panel-header">
        <div>
          <h2>{title}</h2>
          {subtitle && <p>{subtitle}</p>}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}
function Empty({
  title = '没有符合条件的记录',
  text = '试试调整筛选条件。',
}: {
  title?: string;
  text?: string;
}) {
  return (
    <div className="empty">
      <Search size={28} />
      <strong>{title}</strong>
      <span>{text}</span>
    </div>
  );
}
function RangeControl({
  value,
  onChange,
  disabled = false,
}: {
  value: Range;
  onChange: (value: Range) => void;
  disabled?: boolean;
}) {
  return (
    <div className="segmented" aria-label="时间范围">
      {(['24h', '7d'] as Range[]).map((r) => (
        <button
          key={r}
          className={value === r ? 'active' : ''}
          disabled={disabled}
          onClick={() => onChange(r)}
          aria-pressed={value === r}
        >
          {r === '24h' ? '24 小时' : '7 天'}
        </button>
      ))}
    </div>
  );
}

export default function App() {
  const [page, setPage] = useState<Page>(pageFromHash);
  const [settings, setSettings] = useState<Settings>(loadSettings);
  const [scenario, setScenario] = useState<Scenario>('normal');
  const [range, setRange] = useState<Range>('24h');
  const [quality, setQuality] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [notice, setNotice] = useState('');
  const [search, setSearch] = useState('');
  const [kind, setKind] = useState('all');
  const [sortKey,setSortKey]=useState(()=>{try{return localStorage.getItem('hnd-sort-key')||'ip'}catch{return 'ip'}});
  const [sortDir,setSortDir]=useState(()=>{try{return localStorage.getItem('hnd-sort-dir')==='-1'?-1:1}catch{return 1}});
 useEffect(()=>{try{localStorage.setItem('hnd-sort-key',sortKey);localStorage.setItem('hnd-sort-dir',String(sortDir));}catch{}},[sortKey,sortDir]);
  const [status, setStatus] = useState('all');
  const [topologyFilter, setTopologyFilter] = useState('all');
  const [trafficDevice, setTrafficDevice] = useState('all');
  const [alertFilter, setAlertFilter] = useState('all');
  const [expandedAlert, setExpandedAlert] = useState<string | null>(null);
  const [alertActions, setAlertActions] = useState<Record<string, { mutedUntil?: number }>>({});
  const [clock, setClock] = useState(Date.now());
  const liveState = useLiveSnapshot(range);
  const live = liveState.live;
  const snapshot = liveState.snapshot ?? demoService.getSnapshot(settings.mode, scenario, range);
  const activeAlerts = snapshot.alerts.filter((a) => !a.recovered && !a.paused);
  const actionableCount = activeAlerts.filter(
    (a) => !(alertActions[a.id]?.mutedUntil && alertActions[a.id].mutedUntil! > clock),
  ).length;
  const currentDevice = snapshot.devices.find((d) => d.id === selected);
  const online = snapshot.devices.filter((d) => d.status === 'online').length;
  const stale = live ? Boolean(liveState.error) : scenario === 'stale';
  useEffect(() => {
    const change = () => setPage(pageFromHash());
    window.addEventListener('hashchange', change);
    return () => window.removeEventListener('hashchange', change);
  }, []);
  useEffect(() => {
    document.title = `HomeNet Doctor · ${pages.find((p) => p.id === page)?.title}`;
  }, [page]);
  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => setNotice(''), 4000);
    return () => clearTimeout(timer);
  }, [notice]);
  useEffect(() => {
    const timer = setInterval(() => setClock(Date.now()), 30000);
    return () => clearInterval(timer);
  }, []);
  const navigate = (target: Page) => {
    setPage(target);
    location.hash = target;
    window.scrollTo({ top: 0, behavior: 'instant' });
  };
  const saveSettings = (next: Settings) => {
    setSettings(next);
    try {
      localStorage.setItem(SETTINGS_KEY, JSON.stringify(next));
      setNotice('设置已保存到当前浏览器');
    } catch {
      setNotice('浏览器存储不可用，设置仅在本次会话生效');
    }
  };
  const viewAlert = (id: string) => {
    setExpandedAlert(id);
    setAlertFilter('all');
    navigate('alerts');
  };
  const filteredDevices = snapshot.devices.filter(
    (d) =>
      `${d.name} ${d.ip} ${d.model}`.toLowerCase().includes(search.toLowerCase()) &&
      (kind === 'all' || d.kind === kind) &&
      (status === 'all' || (status === 'bypass' ? d.bypassPath?.state === 'recent' : status === 'bypass-history' ? d.bypassPath?.state === 'historical' : status === 'issue' ? d.status !== 'online' : d.status === status)),
  );
  filteredDevices.sort((a,b)=>compareDevices(a,b,sortKey,sortDir));
  const ranked = snapshot.devices
    .filter(
      (d) =>
        d.traffic.availability === 'available' &&
        (trafficDevice === 'all' || d.id === trafficDevice),
    )
    .sort((a, b) => (b.traffic.value ?? 0) - (a.traffic.value ?? 0));
  const domains = snapshot.traffic.filter(
    (t) => trafficDevice === 'all' || t.deviceId === trafficDevice,
  );
  if (!liveState.checked || (live && !liveState.snapshot))
    return (
      <main>
        <h1>HomeNet Doctor</h1>
        <p>{liveState.error || '正在连接采集服务…'}</p>
        <a href="/login">重新登录</a>
      </main>
    );
  return (
    <div className="app-shell" data-page={page}>
      <aside className="sidebar">
        <a className="brand" href="#overview" onClick={() => navigate('overview')}>
          <span className="brand-mark">
            <Activity size={25} strokeWidth={2.2} />
          </span>
          <span>
            HomeNet<span className="brand-sub">DOCTOR</span>
          </span>
        </a>
        <div className="site-selector">
          <span className="site-avatar">
            <Globe2 size={18} />
          </span>
          <span>
            <strong>我的家庭网络</strong>
            <small>本地网络空间</small>
          </span>
          <ChevronDown size={14} />
        </div>
        <div className="nav-caption">网络管理</div>
        <nav>
          {pages.map((p) => (
            <button
              key={p.id}
              className={`nav-item ${page === p.id ? 'active' : ''}`}
              onClick={() => navigate(p.id)}
              aria-current={page === p.id ? 'page' : undefined}
            >
              <p.icon size={19} />
              <span>{p.title}</span>
              {p.id === 'alerts' && actionableCount > 0 && (
                <b className="nav-count">{actionableCount}</b>
              )}
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="local-card">
            <span className="green-dot" />
            <strong>单机 · 本地优先</strong>
            <p>看清网络，理解每一次异常。</p>
          </div>
          <div className="version">
            <ShieldCheck size={15} />
            HomeNet Doctor <span>v{packageInfo.version}</span>
          </div>
        </div>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <div className="breadcrumb">
            我的家庭网络 <ChevronRight size={13} />
            <strong>{pages.find((p) => p.id === page)?.title}</strong>
          </div>
          <div className="topbar-right">
            <span className="demo-badge">
              <span />
              {live ? '真实采集' : '演示模式'}
            </span>
            <button
              className="icon-button notification-button"
              aria-label="查看告警"
              onClick={() => navigate('alerts')}
            >
              <Bell size={18} />
              {actionableCount > 0 && <i />}
            </button>
            <span className="avatar">H</span>
          </div>
        </header>
        <main>
          {liveState.error && (
            <p role="alert" className="coverage-note">
              {liveState.error}，最后样本已失效，请重新登录。
            </p>
          )}
          <div className="page-heading">
            <div>
              <div className="eyebrow">HOME NETWORK INTELLIGENCE</div>
              <h1>{pages.find((p) => p.id === page)?.title}</h1>
              <p>
                {page === 'overview'
                  ? '网络的每一个变化，都清晰可见。'
                  : page === 'topology'
                    ? '看清设备之间的连接，也看清观测的边界。'
                    : page === 'devices'
                      ? '了解每台设备的连接、状态和活动。'
                      : page === 'traffic'
                        ? '从流量趋势到域名活动，了解可见范围内的网络使用。'
                        : page === 'alerts'
                          ? '从异常出发，用证据缩小问题范围。'
                          : '让监测方式适合你的网络。'}
              </p>
            </div>
            <div className="heading-status">
              <span className={stale ? 'amber-dot' : 'green-dot'} />
              <span>
                {stale ? '采集异常' : live ? '实时样本' : '演示样本'}
                <small>
                  {new Date(snapshot.observedAt).toLocaleDateString('zh-CN', {
                    timeZone: 'Asia/Shanghai',
                  })}{' '}
                  {dateText(snapshot.observedAt)}
                </small>
              </span>
            </div>
          </div>
          <div className="demo-toolbar">
            <div className="demo-toolbar-label">
              <SlidersHorizontal size={16} />
              <strong>{live ? '真实只读采集' : '体验演示'}</strong>
              <span>{live ? '每 10 秒刷新 · 不修改网络配置' : '切换场景，查看诊断如何变化'}</span>
            </div>
            {!live && (
              <div className="demo-selects">
                <label>
                  <span className="sr-only">部署模式</span>
                  <select
                    disabled={live}
                    aria-label="部署模式"
                    value={settings.mode}
                    onChange={(e) => {
                      saveSettings({ ...settings, mode: e.target.value as Mode });
                      setSelected(null);
                      setTrafficDevice('all');
                    }}
                  >
                    <option value="bypass">爱快 + iStoreOS 旁路由</option>
                    <option value="gateway">iStoreOS / OpenWrt 主路由</option>
                  </select>
                </label>
                <label>
                  <span className="sr-only">演示场景</span>
                  <select
                    disabled={live}
                    aria-label="演示场景"
                    value={scenario}
                    onChange={(e) => setScenario(e.target.value as Scenario)}
                  >
                    {Object.entries(scenarioLabels).map(([id, name]) => (
                      <option key={id} value={id}>
                        {name}
                      </option>
                    ))}
                  </select>
                </label>
              </div>
            )}
          </div>

          {page === 'overview' && (
            <>
              <section
                className={`health-banner ${activeAlerts.length ? 'warning' : ''} ${stale ? 'stale' : ''}`}
              >
                <div className="health-icon">
                  {activeAlerts.length ? <Activity size={27} /> : <ShieldCheck size={28} />}
                </div>
                <div className="health-copy">
                  <div className="health-kicker">网络健康概况</div>
                  <h2>
                    {stale
                      ? '采集中断，网络状态待确认'
                      : activeAlerts.length
                        ? activeAlerts[0].title
                        : live
                          ? '实时采集已连接'
                          : '网络运行良好'}
                  </h2>
                  <p>
                    {activeAlerts.length
                      ? activeAlerts[0].summary
                      : live
                        ? '已读取本机接口和邻居记录。当前采集范围不足以判断全网健康。'
                        : `已观测到 ${online} 台设备在线，当前观测范围内没有发现异常。`}
                  </p>
                </div>
                <div className="health-side">
                  {activeAlerts.length ? (
                    <button className="button" onClick={() => viewAlert(activeAlerts[0].id)}>
                      查看诊断 <ArrowRight size={15} />
                    </button>
                  ) : (
                    <>
                      <span className="health-bars">
                        {Array.from({ length: 25 }, (_, i) => (
                          <i key={i} style={{ opacity: 0.35 + (i / 25) * 0.65 }} />
                        ))}
                      </span>
                      <small>{live ? '只读采集 · 健康待评估' : '当前样本 · 状态正常'}</small>
                    </>
                  )}
                </div>
              </section>
              <div className="metrics-grid">
                <MetricCard
                  label={
                    live
                      ? 'br-lan 接收速率'
                      : settings.mode === 'bypass'
                        ? '观测下载速率'
                        : '出口下载速率'
                  }
                  value={snapshot.metrics.download}
                  unit="Mbps"
                  icon={<ArrowDown size={18} />}
                  note="当前观测路径"
                  bars
                  color="blue"
                />
                <MetricCard
                  label={live ? 'br-lan 发送速率' : '观测上传速率'}
                  value={snapshot.metrics.upload}
                  unit="Mbps"
                  icon={<ArrowUp size={18} />}
                  note="当前观测路径"
                  bars
                  color="teal"
                />
                <MetricCard
                  label={live ? '软路由外网往返延迟' : '外部网络延迟'}
                  value={snapshot.metrics.latency}
                  unit="ms"
                  icon={<Activity size={18} />}
                  note={
                    live
                      ? '公网 ICMP 探测 · 非全部业务 RTT'
                      : `丢包率 ${displayMetric(snapshot.metrics.loss, '%')}`
                  }
                  color="violet"
                />
                <div className="metric-card">
                  <div className="metric-label">
                    <span>在线设备</span>
                    <Radio size={18} />
                  </div>
                  <div className="metric-value">
                    {stale ? '—' : online}
                    <span>/ {snapshot.devices.length}</span>
                  </div>
                  <div className="metric-footer">
                    <span>
                      {stale ? '等待新鲜观测' : `${snapshot.devices.length - online} 台状态待确认`}
                    </span>
                    <button className="text-button" onClick={() => navigate('devices')}>
                      全部设备 <ArrowRight size={12} />
                    </button>
                  </div>
                </div>
              </div>
              <div className="coverage-note">
                <Info size={14} />
                <span>{snapshot.coverage}；未观察到的数据不会记为零。</span>
              </div>
              <div className="overview-grid">
                <Panel
                  title="网络趋势"
                  subtitle="可见流量与连接质量"
                  action={<RangeControl value={range} onChange={setRange} />}
                >
                  <div className="chart-top">
                    <strong>
                      {stale ||
                      (quality
                        ? snapshot.metrics.latency.value
                        : snapshot.metrics.download.value) === null
                        ? '—'
                        : `${quality ? snapshot.metrics.latency.value : snapshot.metrics.download.value} `}
                      <small>
                        {stale ? '等待采集' : quality ? 'ms 当前延迟' : 'Mbps 当前下载'}
                      </small>
                    </strong>
                    <div className="segmented">
                      <button
                        className={!quality ? 'active' : ''}
                        onClick={() => setQuality(false)}
                        aria-pressed={!quality}
                      >
                        流量
                      </button>
                      <button
                        className={quality ? 'active' : ''}
                        onClick={() => setQuality(true)}
                        aria-pressed={quality}
                      >
                        延迟 / 丢包
                      </button>
                    </div>
                  </div>
                  <div className="quality-legend">
                    {quality
                      ? '蓝色：延迟（ms） · 橙色：丢包（%）'
                      : '蓝色：下载 · 绿色：上传 · Mbps'}
                  </div>
                  <TrafficChart samples={snapshot.samples} quality={quality} />
                </Panel>
                <Panel
                  title="连接概览"
                  subtitle={
                    settings.mode === 'bypass'
                      ? '爱快主路由 · iStoreOS 旁路观测'
                      : 'iStoreOS 主路由 · 本机观测'
                  }
                  action={
                    <button className="text-button" onClick={() => navigate('topology')}>
                      完整拓扑 <ArrowRight size={13} />
                    </button>
                  }
                >
                  <Topology snapshot={snapshot} compact onDevice={setSelected} />
                  <div className="topology-caption">
                    <span>
                      <i className="line" />
                      已确认
                    </span>
                    <span>
                      <i className="line dashed" />
                      推测关系
                    </span>
                    <span>点击设备查看详情</span>
                  </div>
                </Panel>
              </div>
              <div className="overview-grid lower">
                <Panel
                  title="近期事件"
                  action={
                    <button className="text-button" onClick={() => navigate('alerts')}>
                      查看全部 <ArrowRight size={13} />
                    </button>
                  }
                >
                  <div className="event-list">
                    {snapshot.alerts.map((a) => (
                      <button className="event-row" key={a.id} onClick={() => viewAlert(a.id)}>
                        <span className={`event-symbol ${a.recovered ? 'resolved' : 'warn'}`}>
                          {a.recovered ? <Check size={17} /> : <Activity size={17} />}
                        </span>
                        <span>
                          <strong>{a.title}</strong>
                          <small>
                            {a.recovered ? '已恢复' : '需要关注'} · {dateText(a.observedAt)}
                          </small>
                        </span>
                        <ChevronRight size={16} />
                      </button>
                    ))}
                    <div className="event-row quiet">
                      <span className="event-symbol">
                        <Network size={17} />
                      </span>
                      <span>
                        <strong>网络设备清单已更新</strong>
                        <small>
                          发现 {snapshot.devices.length} 台设备 ·{' '}
                          {live ? dateText(snapshot.observedAt) : '09:10'}
                        </small>
                      </span>
                    </div>
                  </div>
                </Panel>
                <Panel title="值得关注" subtitle="设备状态与采集覆盖">
                  <div className="attention-list">
                    {snapshot.devices
                      .filter((d) => d.status !== 'online' || d.id === 'doctor')
                      .slice(0, 3)
                      .map((d) => (
                        <button
                          key={d.id}
                          className="attention-row"
                          onClick={() => setSelected(d.id)}
                        >
                          <DeviceBadge device={d} />
                          <span>
                            <strong>{d.name}</strong>
                            <small>
                              {d.id === 'doctor' && !stale
                                ? `CPU ${displayMetric(d.cpu, '%')} · 内存 ${displayMetric(d.memory, '%')}`
                                : d.status === 'unknown'
                                  ? '需要更新观测信息'
                                  : '探测无响应，原因待确认'}
                            </small>
                          </span>
                          <ChevronRight size={15} />
                        </button>
                      ))}
                  </div>
                </Panel>
              </div>
            </>
          )}

          {page === 'topology' && (
            <Panel
              title="网络连接地图"
              subtitle={
                live
                  ? '实线：用户确认或接口证据；虚线：待核实。网关路径与实际接线分层展示。'
                  : '实线表示演示数据中已确认的连接，虚线表示推测；并非真实扫描结果。'
              }
              action={
                <select
                  aria-label="拓扑筛选"
                  value={topologyFilter}
                  onChange={(e) => setTopologyFilter(e.target.value)}
                >
                  <option value="all">全部设备</option>
                  <option value="issue">关注异常与未知设备</option>
                </select>
              }
            >
              <div className="coverage-note inset">
                <Info size={15} />
                <span>
                  {live && snapshot.pathMonitor ? `${snapshot.pathMonitor.detail}。蓝色箭头：近期公网请求交给本机；手动网关标注单独保留。` : settings.mode === 'bypass'
                    ? 'iStoreOS 连接在核心交换机旁路位置；其他设备不一定经过它上网。'
                    : 'iStoreOS 承担主路由角色，未连接爱快管理数据。'}
                </span>
              </div>
              <Topology snapshot={snapshot} onDevice={setSelected} filter={topologyFilter} />
              <div className="topology-caption">
                <span>
                  <i className="line" />
                  已确认连接
                </span>
                <span>
                  <i className="line dashed" />
                  推测连接
                </span>
                <span>
                  {live
                    ? '蓝色箭头：已观察到流量交付；绿色：两端可达；灰蓝：状态待确认。'
                    : '独立节点：连接位置待确认'}
                </span>
              </div>
            </Panel>
          )}

          {page === 'devices' && (
            <Panel
              title="所有设备"
              subtitle={`${snapshot.devices.length} 台设备 · ${stale ? '当前在线数未知' : `${online} 台在线`}`}
            >
              {live && <><InventoryControls/><ScanChanges/></>}
              <div className="table-toolbar"><label>排序 <select aria-label="排序字段" value={sortKey} onChange={e=>setSortKey(e.target.value)}>{[['ip','IP 地址'],['name','设备名称'],['status','状态'],['traffic','流量'],['latency','延迟']].map(([k,v])=><option key={k} value={k}>{v}</option>)}</select></label><button className="button small" onClick={()=>setSortDir(-sortDir)}>{sortDir===1?'升序 ↑':'降序 ↓'}</button>
                <label className="search-box">
                  <Search size={17} />
                  <input
                    aria-label="搜索设备"
                    placeholder="搜索设备名称、IP 或型号"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                  />
                </label>
                <div className="filters">
                  <ListFilter size={16} />
                  <select
                    aria-label="设备类型"
                    value={kind}
                    onChange={(e) => setKind(e.target.value)}
                  >
                    <option value="all">全部类型</option>
                    {Object.entries(kindLabels).map(([id, label]) => (
                      <option key={id} value={id}>
                        {label}
                      </option>
                    ))}
                  </select>
                  <select
                    aria-label="设备状态"
                    value={status}
                    onChange={(e) => setStatus(e.target.value)}
                  >
                    <option value="all">全部状态</option>
                    <option value="online">在线</option>
                    <option value="issue">异常与未知</option>
                    <option value="unresponsive">探测无响应</option>
                    <option value="offline">已确认离线</option>
                    <option value="unknown">状态未知</option>
                    {live && <option value="bypass">近期流量经旁路由</option>}
                    {live && <option value="bypass-history">曾观察到经旁路由</option>}
                  </select>
                </div>
              </div>
              {live && snapshot.pathMonitor && <div className="coverage-note inset"><Info size={15}/><span>{snapshot.pathMonitor.detail}；近期 = 最近 2 分钟。历史标记保留最多 24 小时；未观察到不等于未经过。</span></div>}
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th aria-sort={sortKey==='name'?(sortDir===1?'ascending':'descending'):'none'}><button className="text-button" onClick={()=>{setSortKey('name');setSortDir(sortKey==='name'?-sortDir:1);}}>设备 ↕</button></th>
                      <th aria-sort={sortKey==='status'?(sortDir===1?'ascending':'descending'):'none'}><button className="text-button" onClick={()=>{setSortKey('status');setSortDir(sortKey==='status'?-sortDir:1);}}>状态 ↕</button></th>
                      <th aria-sort={sortKey==='ip'?(sortDir===1?'ascending':'descending'):'none'}><button className="text-button" onClick={()=>{setSortKey('ip');setSortDir(sortKey==='ip'?-sortDir:1);}}>IP 地址 ↕</button></th>
                      <th>连接</th>
                      <th aria-sort={sortKey==='latency'?(sortDir===1?'ascending':'descending'):'none'}><button className="text-button" onClick={()=>{setSortKey('latency');setSortDir(sortKey==='latency'?-sortDir:1);}}>延迟 ↕</button></th>
                      <th><button className="text-button" onClick={()=>{setSortKey('traffic');setSortDir(sortKey==='traffic'?-sortDir:-1);}}>{range === '24h' ? '24 小时' : '7 天'}流量 ↕</button></th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {filteredDevices.map((d) => (
                      <tr key={d.id}>
                        <td>
                          <button className="device-name" onClick={() => setSelected(d.id)}>
                            <DeviceBadge device={d} />
                            <span>
                              <strong>{d.name}</strong>
                              <small>{d.model}</small>
                              <small>{d.annotatedAt ? `标注 ${new Date(d.annotatedAt).toLocaleDateString('zh-CN')}` : d.firstRecordedAt ? `首次记录 ${new Date(d.firstRecordedAt).toLocaleDateString('zh-CN')}` : ''}</small>
                              <BypassBadge path={d.bypassPath} />{d.peerPaths?.map(p=><span className="bypass-badge recent" key={p.nodeIP} title={`${p.node} · ${p.observedAt}`}>流量经 {p.nodeIP}（远端）</span>)}
                            </span>
                          </button>
                        </td>
                        <td>
                          <StatusBadge status={d.status} />
                        </td>
                        <td className="mono">{d.ip}</td>
                        <td>{d.connection}<small className="path-last-seen">{d.bypassPath?.observedAt ? `流量路径观测：${dateText(d.bypassPath.observedAt)}` : ''}</small></td>
                        <td>
                          {d.monitorDisabled
                            ? '监测已关闭'
                            : d.status === 'unresponsive'
                              ? '无响应'
                              : displayMetric(d.latency, ' ms')}
                        </td>
                        <td>{displayMetric(d.traffic, ' GB')}</td>
                        <td>
                          <button
                            className="icon-button"
                            aria-label={`查看${d.name}详情`}
                            onClick={() => setSelected(d.id)}
                          >
                            <ChevronRight size={16} />
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {!filteredDevices.length && <Empty />}
              <div className="table-footer">
                <span>
                  显示 {filteredDevices.length} / {snapshot.devices.length} 台设备
                </span>
                <span>{live ? '来自本机邻居表的观测' : '所有地址均为虚构演示数据'}</span>
              </div>
            </Panel>
          )}

          {page === 'traffic' && (
            <>
              <div className="section-controls">
                <span className="coverage-note">
                  <Info size={15} />
                  {snapshot.coverage}
                </span>
                <RangeControl value={range} onChange={setRange} />
              </div>
              <Panel
                title="观测路径流量趋势"
                subtitle="单位 Mbps · 趋势图展示当前观测路径汇总"
                action={<ChartLegend />}
              >
                <TrafficChart samples={snapshot.samples} />
              </Panel>
              <div className="section-controls">
                <h2>设备活动</h2>
                <select
                  aria-label="流量设备筛选"
                  value={trafficDevice}
                  onChange={(e) => setTrafficDevice(e.target.value)}
                >
                  <option value="all">全部设备</option>
                  {snapshot.devices.map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.name}
                    </option>
                  ))}
                </select>
              </div>
              <Connections devices={snapshot.devices} selected={trafficDevice} live={live} onDevice={setSelected}/>
              <div className="overview-grid">
                <Panel
                  title="设备流量排行"
                  subtitle={
                    range === '24h' ? '最近 24 小时 · 上下行合计' : '最近 7 天 · 上下行合计'
                  }
                >
                  <div className="ranking">
                    {ranked.slice(0, 7).map((d, i) => (
                      <button className="rank-row" key={d.id} onClick={() => setSelected(d.id)}>
                        <span className="rank-number">{String(i + 1).padStart(2, '0')}</span>
                        <DeviceBadge device={d} />
                        <span className="rank-main">
                          <span>
                            <strong>{d.name}</strong>
                            <b>{displayMetric(d.traffic, ' GB')}</b>
                          </span>
                          <i>
                            <i
                              style={{
                                width: `${((d.traffic.value ?? 0) / (ranked[0].traffic.value || 1)) * 100}%`,
                              }}
                            />
                          </i>
                        </span>
                      </button>
                    ))}
                  </div>
                  {!ranked.length && (
                    <Empty
                      title={stale ? '采集数据已过期' : '所选设备流量不可见'}
                      text="当前没有可用于统计的流量数据。"
                    />
                  )}
                </Panel>
                <Panel title="可见域名活动" subtitle="DNS 查询记录，不代表完整浏览历史">
                  <div className="domain-list">
                    {domains.map((t) => (
                      <div className="domain-row" key={`${t.deviceId}-${t.domain}`}>
                        <span className="domain-symbol">
                          <Globe2 size={17} />
                        </span>
                        <span>
                          <strong>{t.domain}</strong>
                          <small>
                            {snapshot.devices.find((d) => d.id === t.deviceId)?.name} ·{' '}
                            {dateText(t.observedAt)}
                          </small>
                        </span>
                        <b>
                          {t.requests}
                          <small>次查询</small>
                        </b>
                      </div>
                    ))}
                  </div>
                  {!domains.length && (
                    <Empty
                      title="没有可见域名活动"
                      text="可能没有观测记录、使用加密 DNS，或采集已中断。"
                    />
                  )}
                  <div className="panel-footnote">
                    <Info size={13} />
                    不解析 HTTPS 页面内容；查询次数不代表访问次数。
                  </div>
                </Panel>
              </div>
            </>
          )}

          {page === 'alerts' && (
            <>
              {live && <Panel title="已暂停监测"><p>关闭监测不会清除事件；可在此重新开启。</p>{snapshot.devices.filter(d=>d.monitorDisabled).map(d=><div key={d.id}><span>{d.name} · {d.ip} </span><MonitoringButton devices={[d]} onMessage={setNotice}/></div>)}</Panel>}
              <div className="alert-summary">
                <div>
                  <span className="summary-icon">
                    <Bell size={20} />
                  </span>
                  <span>
                    <strong>{activeAlerts.length}</strong> 当前异常
                  </span>
                </div>
                <div>
                  <span className="summary-icon green">
                    <CheckCheck size={20} />
                  </span>
                  <span>
                    <strong>{snapshot.alerts.filter((a) => a.recovered).length}</strong> 已恢复事件
                  </span>
                </div>
                <p>关闭监测会停止该设备主动探测与报警；忽略一小时仅临时静音。</p>
              </div>
              <Panel
                title="事件与诊断"
                action={
                  <select
                    aria-label="事件筛选"
                    value={alertFilter}
                    onChange={(e) => setAlertFilter(e.target.value)}
                  >
                    <option value="all">全部事件</option>
                    <option value="active">当前异常</option>
                    <option value="recovered">已恢复</option><option value="paused">已暂停监测</option>
                  </select>
                }
              >
                <div className="alerts-list">
                  {snapshot.alerts
                    .filter(
                      (a) =>
                        alertFilter === 'all' ||
                        (alertFilter === 'paused' ? a.paused : alertFilter === 'active' ? !a.recovered && !a.paused : a.recovered),
                    )
                    .map((a) => {
                      const action = alertActions[a.id];
                      const muted = Boolean(action?.mutedUntil && action.mutedUntil > clock);
                      return (
                        <article
                          className={`alert-card ${a.recovered ? 'recovered' : a.severity}`}
                          key={a.id}
                        >
                          <button
                            className="alert-card-heading"
                            onClick={() => setExpandedAlert(expandedAlert === a.id ? null : a.id)}
                            aria-expanded={expandedAlert === a.id}
                          >
                            <span className={`event-symbol ${a.recovered ? 'resolved' : 'warn'}`}>
                              {a.recovered ? <Check size={20} /> : <Activity size={20} />}
                            </span>
                            <span>
                              <strong>{a.title}</strong>
                              <small>
                                {dateText(a.observedAt)} ·{' '}
                                {a.paused ? '监测已暂停' : a.recovered
                                  ? '已恢复'
                                  : a.severity === 'critical'
                                    ? '严重'
                                    : '提醒'}
                                {muted ? ' · 已暂时忽略 1 小时' : ''}
                              </small>
                            </span>
                            <ChevronDown size={18} />
                          </button>
                          <p className="alert-summary-text">{a.summary}</p><small>首次发生：{new Date(a.observedAt).toLocaleString('zh-CN')}{a.resolvedAt ? ` · 恢复：${new Date(a.resolvedAt).toLocaleString('zh-CN')}` : ''}</small>
                          {expandedAlert === a.id && (
                            <div className="diagnosis">
                              <div className="diagnosis-cause">
                                <Sparkles size={18} />
                                <div>
                                  <h3>可能原因</h3>
                                  <p>{a.cause}</p>
                                </div>
                              </div>
                              <div className="diagnosis-columns">
                                <div>
                                  <h3>观察证据</h3>
                                  <ul>
                                    {a.evidence.map((e) => (
                                      <li key={e}>{e}</li>
                                    ))}
                                  </ul>
                                </div>
                                <div>
                                  <h3>建议排查</h3>
                                  <ol>
                                    {a.suggestions.map((s) => (
                                      <li key={s}>{s}</li>
                                    ))}
                                  </ol>
                                </div>
                              </div>
                              <div className="related-devices">
                                <span>关联设备</span>
                                {a.deviceIds.map((id) => {
                                  const d = snapshot.devices.find((d) => d.id === id);
                                  return (
                                    d && (
                                      <button
                                        className="chip"
                                        key={id}
                                        onClick={() => setSelected(id)}
                                      >
                                        <DeviceIcon kind={d.kind} size={14} />
                                        {d.name}
                                      </button>
                                    )
                                  );
                                })}
                              </div>
                              <small className="muted">
                                来源：{a.source} · 诊断为可能原因，未经实机验证。
                              </small>
                            </div>
                          )}
                          {!a.recovered && (
                            <div className="alert-actions">
                              {live && (
                                <MonitoringButton
                                  devices={snapshot.devices.filter((d) =>
                                    a.deviceIds.includes(d.id),
                                  )}
                                  onMessage={setNotice}
                                />
                              )}
                              <button
                                className="button small secondary"
                                onClick={() => {
                                  setAlertActions((s) => ({
                                    ...s,
                                    [a.id]: {
                                      ...s[a.id],
                                      mutedUntil: muted ? undefined : Date.now() + 3600000,
                                    },
                                  }));
                                  setNotice(muted ? '已恢复提醒' : '本次会话内暂时忽略提醒 1 小时');
                                }}
                              >
                                <Clock3 size={14} />
                                {muted ? '恢复提醒' : '忽略 1 小时'}
                              </button>
                              <button
                                className="text-button"
                                onClick={() =>
                                  setExpandedAlert(expandedAlert === a.id ? null : a.id)
                                }
                              >
                                {expandedAlert === a.id ? '收起诊断' : '查看诊断'}
                                <ArrowRight size={13} />
                              </button>
                            </div>
                          )}
                        </article>
                      );
                    })}
                  {!snapshot.alerts.filter(
                    (a) =>
                      alertFilter === 'all' ||
                      (alertFilter === 'paused' ? a.paused : alertFilter === 'active' ? !a.recovered && !a.paused : a.recovered),
                  ).length && (
                    <Empty title="当前没有异常事件" text="没有当前事件；已暂停监测可在上方恢复。" />
                  )}
                </div>
              </Panel>
            </>
          )}

          {page === 'peers' && (live ? <Peers/> : <p>节点联动仅在真实采集服务中提供。</p>)}
          {page === 'path' && <PathObservation live={live} />}
          {page === 'settings' && (
            <SettingsPage
              key={`${settings.mode}-${settings.interval}-${settings.cache}-${settings.storage}-${settings.retention}`}
              settings={settings}
              snapshot={snapshot}
              onSave={saveSettings}
            />
          )}
          {live && snapshot.historyStart && (
            <p className="coverage-note">
              历史自 {new Date(snapshot.historyStart).toLocaleString('zh-CN')}{' '}
              开始，尚不足完整保留时长的部分不补造。
            </p>
          )}
          <footer className="page-footer">
            <span>
              <ShieldCheck size={13} />
              数据留在本地，洞察尽在眼前
            </span>
            <span>
              HomeNet Doctor · v{packageInfo.version} ·{' '}
              {live ? '真实只读采集测试版' : '所有数据均为演示'}
            </span>
          </footer>
        </main>
      </div>
      <nav className="mobile-nav" aria-label="移动端导航">
        {pages.map((p) => (
          <button
            key={p.id}
            className={page === p.id ? 'active' : ''}
            onClick={() => navigate(p.id)}
            aria-current={page === p.id ? 'page' : undefined}
          >
            <p.icon size={19} />
            <span>
              {p.title
                .replace('网络', '')
                .replace('设备', '')
                .replace('与访问', '')
                .replace('与诊断', '')}
            </span>
          </button>
        ))}
      </nav>
      {notice && (
        <div role="status" className="toast">
          <Check size={16} />
          {notice}
        </div>
      )}
      {currentDevice && (
        <DeviceDetails
          onConnections={()=>{setTrafficDevice(currentDevice.id);setSelected(null);navigate('traffic');}}
          live={live}
          device={currentDevice}
          alerts={snapshot.alerts.filter((a) => a.deviceIds.includes(currentDevice.id))}
          onClose={() => setSelected(null)}
          onAlert={(id) => {
            setSelected(null);
            viewAlert(id);
          }}
        />
      )}
    </div>
  );
}

function MetricCard({
  label,
  value,
  unit,
  icon,
  note,
  bars = false,
  color,
}: {
  label: string;
  value: Snapshot['metrics']['download'];
  unit: string;
  icon: ReactNode;
  note: string;
  bars?: boolean;
  color: string;
}) {
  const available = value.availability === 'available';
  return (
    <div className={`metric-card ${color}`}>
      <div className="metric-label">
        <span>{label}</span>
        {icon}
      </div>
      <div className="metric-value">
        {available ? value.value : '—'}
        <span>{available ? unit : availabilityLabels[value.availability]}</span>
      </div>
      <div className="metric-footer">
        <span>{note}</span>
        {bars && available && (
          <span className="spark-bars">
            {[8, 14, 11, 20, 16, 23, 18, 27, 22, 31, 25, 35, 29, 25, 33, 30].map((h, i) => (
              <i style={{ height: h }} key={i} />
            ))}
          </span>
        )}
      </div>
    </div>
  );
}

function DeviceDetails({
 onConnections,
  live,
  device: d,
  alerts,
  onClose,
  onAlert,
}: {
  onConnections: () => void;
  live: boolean;
  device: Device;
  alerts: Alert[];
  onClose: () => void;
  onAlert: (id: string) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  return (
    <dialog
      className="device-dialog"
      ref={dialog}
      onCancel={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      aria-labelledby="device-title"
    >
      <div className="drawer">
        <header>
          <span>设备详情</span>
          <button className="icon-button" aria-label="关闭设备详情" onClick={onClose}>
            <X size={20} />
          </button>
        </header>
        <div className="drawer-body">
          <div className="device-hero">
            {live && <button className="button small" onClick={onConnections}>查看当前可见连接</button>}
            <DeviceEditor device={d} live={live} />
            {live && d.id!=='doctor' && d.ip!=='node-internet' && <DeleteDevice ip={d.ip}/>}
            <h2 id="device-title">{d.name}</h2>
            <p>{d.model}</p>
            <StatusBadge status={d.status} />
          </div>
          <div className="detail-section">
            <h3>连接信息</h3>
            <dl>
              <dt>IP 地址</dt>
              <dd className="mono">{d.ip}</dd>
              <dt>MAC 地址</dt>
              <dd className="mono">
                {d.mac || '尚未取得'}
                {d.macSource && <small> · {d.macSource}</small>}
              </dd>
              <dt>首次记录</dt><dd>{d.firstRecordedAt ? new Date(d.firstRecordedAt).toLocaleString('zh-CN'):'尚无记录（不补造历史）'}</dd>
              <dt>最近 ARP 扫描响应</dt><dd>{d.lastScannedAt ? new Date(d.lastScannedAt).toLocaleString('zh-CN'):'尚未取得扫描响应'}</dd>
              <dt>最近标注</dt><dd>{d.annotatedAt ? new Date(d.annotatedAt).toLocaleString('zh-CN'):'尚未记录标注时间'}</dd>
              <dt>设备类型</dt>
              <dd>{kindLabels[d.kind]}</dd>
              <dt>连接方式</dt>
              <dd>{d.connection}</dd>
              {d.bypassPath && <><dt>旁路由流量路径</dt><dd><BypassBadge path={d.bypassPath}/><p>{d.bypassPath.detail}</p>{d.bypassPath.observedAt && <small>最近流量观测：{dateText(d.bypassPath.observedAt)} · {d.bypassPath.observedIP}</small>}</dd></>}
              {d.connectionEvidence && (
                <>
                  <dt>连接证据</dt>
                  <dd>{d.connectionEvidence}</dd>
                </>
              )}
              <dt>最近观测</dt>
              <dd>
                {new Date(d.observedAt).toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai' })}{' '}
                {dateText(d.observedAt)}
              </dd>
            </dl>
          </div>
          <div className="detail-section">
            <h3>运行指标</h3>
            <dl>
              <dt>探测延迟</dt>
              <dd>
                {d.monitorDisabled
                  ? '主动监测已关闭'
                  : d.status === 'unresponsive'
                    ? '无响应，尚不能确认离线'
                    : displayMetric(d.latency, ' ms')}
              </dd>
              {d.loss !== undefined && (
                <>
                  <dt>本轮 ICMP 丢包</dt>
                  <dd>{d.loss}%（不代表互联网丢包）</dd>
                </>
              )}
              {d.downloadMbps !== undefined && (
                <>
                  <dt>可见路径速率</dt>
                  <dd>
                    ↓{d.downloadMbps.toFixed(2)} / ↑{(d.uploadMbps || 0).toFixed(2)} Mbps
                  </dd>
                </>
              )}
              <dt>所选时段流量</dt>
              <dd>{displayMetric(d.traffic, ' GB')}</dd>
              <dt>CPU</dt>
              <dd>{displayMetric(d.cpu, '%')}</dd>
              <dt>内存</dt>
              <dd>{displayMetric(d.memory, '%')}</dd>
            </dl>
          </div>
          <div className="detail-section">
            <h3>设备事件</h3>
            <ul className="device-history">
              {d.history.map((h) => (
                <li key={h}>
                  {h.replace(
                    /unresponsive|unknown|online|offline/g,
                    (s) =>
                      ({
                        unresponsive: '探测无响应',
                        unknown: '状态未知',
                        online: '在线',
                        offline: '离线',
                      })[s] || s,
                  )}
                </li>
              ))}
            </ul>
            {alerts.map((a) => (
              <button key={a.id} className="device-alert" onClick={() => onAlert(a.id)}>
                <Bell size={15} />
                {a.title}
                <ChevronRight size={14} />
              </button>
            ))}
          </div>
          <div className="source-box">
            <Info size={16} />
            <span>
              <strong>数据来源</strong>
              <br />
              {d.source}
              <br />
              {d.randomizedMac && (
                <>
                  本地管理 / 随机 MAC，不能据此可靠识别品牌
                  <br />
                </>
              )}
              {d.latency.source}
              <br />
              {d.traffic.source}
              <br />
              {d.cpu.source}
              <br />
              <small>
                {d.source.includes('演示')
                  ? '原型演示，不代表你的真实设备。'
                  : '真实观测，未确认的数据保持未知。'}
              </small>
            </span>
          </div>
        </div>
      </div>
    </dialog>
  );
}

function SettingsPage({
  settings,
  snapshot,
  onSave,
}: {
  settings: Settings;
  snapshot: Snapshot;
  onSave: (settings: Settings) => void;
}) {
  const [draft, setDraft] = useState(settings);
  return (
    <div className="settings-grid">
      <VersionSettings />
      {snapshot.live ? (
        <ServiceSettings />
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            onSave(draft);
          }}
        >
          <Panel title="监测与数据存储" subtitle="当前仅保存原型偏好，不会更改路由器配置。">
            <div className="settings-fields">
              <label>
                <span>
                  <strong>部署模式</strong>
                  <small>决定拓扑结构与观测覆盖说明</small>
                </span>
                <select
                  aria-label="设置部署模式"
                  value={draft.mode}
                  onChange={(e) => setDraft({ ...draft, mode: e.target.value as Mode })}
                >
                  <option value="bypass">爱快 + iStoreOS 旁路由</option>
                  <option value="gateway">iStoreOS / OpenWrt 主路由</option>
                </select>
              </label>
              {(
                [
                  {
                    key: 'interval',
                    title: '监测间隔',
                    help: '原型演示参数，真实采集阶段生效',
                    unit: '秒',
                    min: 5,
                    max: 300,
                  },
                  {
                    key: 'cache',
                    title: '内存缓存上限',
                    help: '拟定采集缓冲区预算',
                    unit: 'MiB',
                    min: 16,
                    max: 512,
                  },
                  {
                    key: 'storage',
                    title: '数据存储上限',
                    help: '达到预算后按保留策略清理历史数据',
                    unit: 'MiB',
                    min: 128,
                    max: 8192,
                  },
                  {
                    key: 'retention',
                    title: '历史保留时间',
                    help: '原型默认值，实际开销需实机验证',
                    unit: '天',
                    min: 1,
                    max: 365,
                  },
                ] as const
              ).map((f) => (
                <label key={f.key}>
                  <span>
                    <strong>{f.title}</strong>
                    <small>{f.help}</small>
                  </span>
                  <span className="number-input">
                    <input
                      aria-label={f.title}
                      type="number"
                      min={f.min}
                      max={f.max}
                      step={1}
                      required
                      value={Number.isNaN(draft[f.key]) ? '' : draft[f.key]}
                      onChange={(e) =>
                        setDraft({
                          ...draft,
                          [f.key]: e.target.value === '' ? NaN : Number(e.target.value),
                        })
                      }
                    />
                    <span>{f.unit}</span>
                  </span>
                </label>
              ))}
            </div>
            <div className="settings-actions">
              <button type="submit" className="button primary">
                <Check size={15} />
                保存设置
              </button>
              <button
                type="button"
                className="button secondary"
                onClick={() => setDraft({ ...defaults })}
              >
                恢复默认值
              </button>
            </div>
          </Panel>
        </form>
      )}
      <Panel title="采集能力" subtitle="能力会随部署模式与数据状态变化">
        <div className="capability-list">
          {snapshot.capabilities.map((c) => (
            <div key={c.name}>
              <div>
                <strong>{c.name}</strong>
                <span className={`capability-state ${c.availability}`}>
                  {availabilityLabels[c.availability]}
                </span>
              </div>
              <p>{c.detail}</p>
            </div>
          ))}
        </div>
      </Panel>
      <div className="settings-note">
        <Database size={21} />
        <div>
          <h3>为单台软路由设计</h3>
          <p>
            {snapshot.live
              ? '后台独立采集本机状态、设备连通性和 nlbwmon 可见路径流量，SQLite 保存历史，设置保存在软路由。网页每 10 秒读取快照；管理接口、域名活动和全网流量覆盖仍待接入与核验。'
              : '演示版使用固定数据，监测参数仅保存在当前浏览器。连接真实采集服务后，设备端负责持续采集与历史保存。'}
          </p>
        </div>
        <Download size={20} />
      </div>
    </div>
  );
}
