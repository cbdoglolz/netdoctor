export type Mode = 'bypass' | 'gateway';
export type Scenario = 'normal' | 'dns' | 'loss' | 'device' | 'congestion' | 'stale';
export type Availability = 'available' | 'invisible' | 'unsupported' | 'denied' | 'stale';
export type DeviceKind =
  | 'gateway'
  | 'switch'
  | 'ap'
  | 'server'
  | 'computer'
  | 'phone'
  | 'iot'
  | 'tablet'
  | 'tv'
  | 'camera'
  | 'printer'
  | 'speaker'
  | 'nas'
  | 'hypervisor'
  | 'vm'
  | 'console'
  | 'settop'
  | 'monitor'
  | 'fan'
  | 'amplifier'
  | 'hub'
  | 'light';
export type Status = 'online' | 'unresponsive' | 'offline' | 'unknown';
export type Range = '24h' | '7d';
export interface Observation<T> {
  value: T | null;
  availability: Availability;
  source: string;
  observedAt: string;
}
export interface Device {
  id: string;
  name: string;
  ip: string;
  mac: string;
  kind: DeviceKind;
  model: string;
  connection: string;
  status: Status;
  latency: Observation<number>;
  traffic: Observation<number>;
  cpu: Observation<number>;
  memory: Observation<number>;
  observedAt: string;
  source: string;
  history: string[];
  loss?: number;
  downloadMbps?: number;
  uploadMbps?: number;
  randomizedMac?: boolean;
  macSource?: string;
  icon?: DeviceKind;
  customIcon?: string;
  monitorDisabled?: boolean;
  connectionEvidence?: string;
  bypassPath?: { state: 'unknown' | 'recent' | 'historical'; label: string; detail: string; observedAt?: string; observedIP?: string };
}
export interface TopologyLink {
  pathObserved?: boolean;
  id: string;
  from: string;
  to: string;
  confidence: 'confirmed' | 'inferred';
  relation?: 'physical' | 'virtual' | 'gateway' | 'wireless';
  status?: string;
  measured?: boolean;
  downloadMbps?: number;
  uploadMbps?: number;
  source: string;
  observedAt: string;
  availability: Availability;
}
export interface Alert {
  id: string;
  title: string;
  severity: 'warning' | 'critical' | 'info';
  summary: string;
  cause: string;
  evidence: string[];
  suggestions: string[];
  deviceIds: string[];
  observedAt: string;
  source: string;
  availability: Availability;
  recovered: boolean;
}
export interface TrafficRecord {
  deviceId: string;
  domain: string;
  requests: number;
  source: string;
  observedAt: string;
  availability: Availability;
}
export interface Capability {
  name: string;
  availability: Availability;
  detail: string;
  source: string;
  observedAt: string;
}
export interface Sample {
  time: string;
  download: number | null;
  upload: number | null;
  latency: number | null;
  loss: number | null;
}
export interface Snapshot {
  pathMonitor?: { running: boolean; detail: string; source: string };
  live?: boolean;
  historyStart?: string;
  storageError?: string;
  integrations?: { name: string; available: boolean; reason?: string }[];
  mode: Mode;
  scenario: Scenario;
  devices: Device[];
  links: TopologyLink[];
  alerts: Alert[];
  traffic: TrafficRecord[];
  capabilities: Capability[];
  samples: Sample[];
  observedAt: string;
  metrics: {
    download: Observation<number>;
    upload: Observation<number>;
    latency: Observation<number>;
    loss: Observation<number>;
  };
  coverage: string;
}
export interface Settings {
  mode: Mode;
  interval: number;
  cache: number;
  storage: number;
  retention: number;
}
export interface NetworkService {
  getSnapshot(mode: Mode, scenario: Scenario, range: Range): Snapshot;
}
export const defaults: Settings = {
  mode: 'bypass',
  interval: 10,
  cache: 64,
  storage: 512,
  retention: 30,
};
export const SETTINGS_KEY = 'homenet-doctor.settings.v1';
export const scenarioLabels: Record<Scenario, string> = {
  normal: '网络正常',
  dns: 'DNS 异常',
  loss: '上游丢包',
  device: '设备不可达',
  congestion: '出口拥塞',
  stale: '采集数据中断',
};
export const kindLabels: Record<DeviceKind, string> = {
  gateway: '路由器',
  switch: '交换机',
  ap: '无线 AP',
  server: '服务器',
  computer: '电脑',
  phone: '手机',
  iot: '智能设备',
  tablet: '平板',
  tv: '电视',
  camera: '摄像头',
  printer: '打印机',
  speaker: '音箱',
  nas: 'NAS',
  hypervisor: '虚拟化宿主机',
  vm: '虚拟机',
  console: '游戏机',
  settop: '机顶盒',
  monitor: '显示器',
  fan: '风扇',
  amplifier: '功放',
  hub: '中枢网关',
  light: '灯',
};
export const statusLabels: Record<Status, string> = {
  online: '在线',
  unresponsive: '探测无响应',
  offline: '已确认离线',
  unknown: '状态未知',
};
export const availabilityLabels: Record<Availability, string> = {
  available: '可采集',
  invisible: '不可见',
  unsupported: '不支持采集',
  denied: '无权限',
  stale: '数据已过期',
};
export const STAMP = '2026-10-31T14:32:00+08:00';
const OLD_STAMP = '2026-10-31T14:17:00+08:00';
const observation = <T>(
  value: T | null,
  availability: Availability = 'available',
  source = '演示 · 本机采集',
  observedAt = STAMP,
): Observation<T> => ({ value, availability, source, observedAt });
export function displayMetric(metric: Observation<number>, unit = '') {
  return metric.availability === 'available' && metric.value !== null
    ? `${metric.value}${unit}`
    : availabilityLabels[metric.availability];
}
export function normalizeSettings(input: unknown): Settings {
  const value = input && typeof input === 'object' ? (input as Partial<Settings>) : {};
  const valid = (n: unknown, min: number, max: number, fallback: number) =>
    typeof n === 'number' && Number.isInteger(n) && n >= min && n <= max ? n : fallback;
  return {
    mode: value.mode === 'gateway' ? 'gateway' : 'bypass',
    interval: valid(value.interval, 5, 300, defaults.interval),
    cache: valid(value.cache, 16, 512, defaults.cache),
    storage: valid(value.storage, 128, 8192, defaults.storage),
    retention: valid(value.retention, 1, 365, defaults.retention),
  };
}
export function loadSettings(): Settings {
  try {
    return normalizeSettings(JSON.parse(localStorage.getItem(SETTINGS_KEY) ?? '{}'));
  } catch {
    return { ...defaults };
  }
}

const seeds: [string, string, DeviceKind, string, string, number, number][] = [
  ['router', '爱快主路由', 'gateway', 'iKuai · x86 Gateway', '有线 · 核心网关', 1, 0.6],
  ['switch', '核心交换机', 'switch', 'Managed Switch · 8 Port', '有线 · 2.5 GbE', 2, 0.4],
  ['doctor', 'iStoreOS 旁路由', 'gateway', 'iStoreOS · x86_64', '有线 · 2.5 GbE', 3, 0.3],
  ['ap', '客厅无线 AP', 'ap', 'Wi-Fi 6 Access Point', '有线 · 1 GbE', 4, 1.2],
  ['nas', '家庭 NAS', 'server', 'Home Storage', '有线 · 2.5 GbE', 10, 0.8],
  ['desktop', '书房工作站', 'computer', 'Windows Desktop', '有线 · 2.5 GbE', 11, 1.1],
  ['laptop', '轻薄笔记本', 'computer', 'Laptop', '无线 · 5 GHz', 12, 3.2],
  ['phone', '我的手机', 'phone', 'Smartphone', '无线 · 5 GHz', 20, 4.6],
  ['tablet', '客厅平板', 'phone', 'Tablet', '无线 · 5 GHz', 21, 5.1],
  ['tv', '客厅电视', 'iot', 'Smart TV', '无线 · 5 GHz', 30, 3.4],
  ['speaker', '智能音箱', 'iot', 'Home Speaker', '无线 · 2.4 GHz', 31, 8.2],
  ['printer', '书房打印机', 'iot', 'Network Printer', '连接位置待确认', 40, 0],
];
const visible = new Set(['doctor', 'nas', 'desktop', 'laptop', 'phone', 'tablet', 'tv']);
const volumes: Record<string, number> = {
  doctor: 0.21,
  nas: 18.6,
  desktop: 8.4,
  laptop: 3.2,
  phone: 2.1,
  tablet: 1.4,
  tv: 6.8,
  speaker: 0.12,
  printer: 0.02,
  router: 0.4,
  switch: 0.03,
  ap: 0.06,
};
export const demoService: NetworkService = {
  getSnapshot(mode, scenario, range) {
    const stale = scenario === 'stale';
    const stamp = stale ? OLD_STAMP : STAMP;
    const devices: Device[] = seeds
      .filter(([id]) => mode === 'bypass' || id !== 'router')
      .map(([id, title, kind, model, connection, host, latency]) => {
        const status: Status =
          stale || id === 'printer'
            ? 'unknown'
            : scenario === 'device' && id === 'nas'
              ? 'unresponsive'
              : 'online';
        const canSee = mode === 'gateway' || visible.has(id);
        const available: Availability = stale ? 'stale' : 'available';
        return {
          id,
          name: id === 'doctor' && mode === 'gateway' ? 'iStoreOS 主路由' : title,
          ip: `192.168.50.${host}`,
          mac: `02:00:00:50:00:${host.toString(16).padStart(2, '0')}`,
          kind,
          model,
          connection,
          status,
          latency: observation(
            status !== 'online' ? null : latency,
            status === 'unknown' ? 'stale' : available,
            '演示 · ICMP / 邻居观察',
            stamp,
          ),
          traffic: observation(
            canSee && !stale ? Number((volumes[id] * (range === '7d' ? 5.7 : 1)).toFixed(2)) : null,
            !canSee ? 'invisible' : available,
            '演示 · conntrack 流量统计',
            stamp,
          ),
          cpu: observation(
            id === 'doctor' && !stale ? (scenario === 'congestion' ? 74 : 12) : null,
            id === 'doctor' ? available : id === 'router' ? 'denied' : 'unsupported',
            id === 'doctor' ? '演示 · 本机系统指标' : '演示 · 未连接设备管理接口',
            stamp,
          ),
          memory: observation(
            id === 'doctor' && !stale ? 28 : null,
            id === 'doctor' ? available : id === 'router' ? 'denied' : 'unsupported',
            '演示 · 本机系统指标',
            stamp,
          ),
          observedAt: stamp,
          source: '演示 · 邻居表 / 手工标注',
          history:
            status === 'unresponsive'
              ? [
                  '14:30 连续 3 次探测无响应；尚不能确认设备关机',
                  '14:27 最近一次收到响应，延迟 0.8 ms',
                ]
              : status === 'unknown'
                ? ['没有足够的新鲜观测确认当前状态']
                : ['14:32 收到最新设备观测', '09:10 设备被发现，连接信息已更新'],
        };
      });
    const pairs: [string, string, boolean][] =
      mode === 'bypass'
        ? [
            ['internet', 'router', true],
            ['router', 'switch', true],
            ['switch', 'doctor', true],
            ['switch', 'ap', true],
            ['switch', 'nas', true],
            ['switch', 'desktop', true],
          ]
        : [
            ['internet', 'doctor', true],
            ['doctor', 'switch', true],
            ['switch', 'ap', true],
            ['switch', 'nas', true],
            ['switch', 'desktop', true],
          ];
    pairs.push(
      ['ap', 'laptop', true],
      ['ap', 'phone', true],
      ['ap', 'tablet', true],
      ['ap', 'tv', false],
      ['ap', 'speaker', false],
    );
    const links: TopologyLink[] = pairs.map(([from, to, confirmed]) => ({
      id: `${from}-${to}`,
      from,
      to,
      confidence: confirmed ? 'confirmed' : 'inferred',
      source: confirmed ? '演示 · LLDP / AP 关联记录' : '演示 · 连接关系推测',
      observedAt: stamp,
      availability: stale ? 'stale' : 'available',
    }));
    const alertDetails: Record<
      Exclude<Scenario, 'normal'>,
      Pick<
        Alert,
        'title' | 'severity' | 'summary' | 'cause' | 'evidence' | 'suggestions' | 'deviceIds'
      >
    > = {
      dns: {
        title: 'DNS 响应异常，部分网站可能打开缓慢',
        severity: 'warning',
        summary: '设备连接正常，但域名解析出现超时。',
        cause: '可能是当前 DNS 上游响应不稳定。',
        evidence: [
          'DNS 探测 10 次中 4 次超时',
          '同一目标 IP 连通，平均延迟 18 ms',
          '本机网关可达，未观察到链路断开',
        ],
        suggestions: [
          '检查当前 DNS 服务运行状态',
          '对比另一个 DNS 上游的解析结果',
          '结合主路由记录确认影响范围',
        ],
        deviceIds: ['desktop', 'phone', 'laptop'],
      },
      loss: {
        title: '上游链路出现丢包',
        severity: 'warning',
        summary: '视频会议或在线游戏可能出现间歇卡顿。',
        cause: '异常可能位于网关之后的上游链路，尚不能定位到运营商。',
        evidence: [
          '本地网关探测丢包率 0%',
          '两个外部探测目标丢包率分别为 8% 和 10%',
          '外部目标平均延迟升至 86 ms',
        ],
        suggestions: [
          '检查主路由 WAN 口错误计数',
          '对照不同时段与多个目标的探测记录',
          '保留记录，再联系上游网络维护方',
        ],
        deviceIds: ['desktop', 'phone', 'tv'],
      },
      device: {
        title: '家庭 NAS 连续探测无响应',
        severity: 'warning',
        summary: '文件访问可能受影响，暂不能判断设备已关机。',
        cause: '可能是设备休眠、探测被拦截或连接异常。',
        evidence: [
          '14:30 起连续 3 次 ICMP 探测未响应',
          '同一交换机下其他设备仍正常响应',
          '最后一次成功探测时间为 14:27',
        ],
        suggestions: [
          '确认设备是否休眠',
          '检查交换机端口和设备链路指示状态',
          '通过设备管理页面核对运行状态',
        ],
        deviceIds: ['nas'],
      },
      congestion: {
        title: '观测路径负载升高，疑似出口拥塞',
        severity: 'warning',
        summary: '大流量传输与延迟上升同时出现。',
        cause: '可能存在带宽竞争；旁路模式下还需主路由数据确认出口总量。',
        evidence: [
          '观测下载速率达到 926 Mbps',
          '外部探测延迟从 18 ms 升至 124 ms',
          '家庭 NAS 在观测范围内占用流量最多',
        ],
        suggestions: [
          '检查大文件下载或同步任务',
          '结合主路由 WAN 统计核对实际出口利用率',
          '确认瓶颈后再评估带宽分配策略',
        ],
        deviceIds: ['nas', 'desktop'],
      },
      stale: {
        title: '采集数据中断，当前网络状态未知',
        severity: 'critical',
        summary: '最新可用样本停留在 14:17，不能据此判定网络故障。',
        cause: '采集进程或数据通道可能异常。',
        evidence: [
          '演示时刻为 14:32，最后样本距今 15 分钟',
          '设备、指标和拓扑观察时间均已过期',
          '缺少新的连通性证据',
        ],
        suggestions: [
          '检查采集服务状态与日志',
          '检查本机资源和数据目录可用性',
          '恢复采集后重新观察网络状态',
        ],
        deviceIds: ['doctor'],
      },
    };
    const alerts: Alert[] =
      scenario === 'normal'
        ? []
        : [
            {
              id: `event-${scenario}`,
              ...alertDetails[scenario],
              observedAt: STAMP,
              source: '演示 · 本地诊断规则',
              availability: 'available',
              recovered: false,
            },
          ];
    alerts.push({
      id: 'recovered-ap',
      title: '客厅无线 AP 连接已恢复',
      severity: 'info',
      summary: '09:14 至 09:16 短暂中断，后续连续探测恢复正常。',
      cause: '已有恢复证据；中断原因未确认。',
      evidence: ['09:14 未收到 AP 响应', '09:16 起连续 3 次探测成功'],
      suggestions: ['若再次出现，检查 AP 电源和上联端口'],
      deviceIds: ['ap'],
      observedAt: '2026-10-31T09:16:00+08:00',
      source: '演示 · 探测恢复记录',
      availability: 'available',
      recovered: true,
    });
    const download = scenario === 'congestion' ? 926 : 128.4;
    const upload = scenario === 'congestion' ? 82.6 : 24.8;
    const latency = scenario === 'loss' ? 86 : scenario === 'congestion' ? 124 : 18;
    const loss = scenario === 'loss' ? 8 : scenario === 'congestion' ? 1.2 : 0;
    const metric = (n: number) =>
      observation(stale ? null : n, stale ? 'stale' : 'available', '演示 · 观测路径', stamp);
    const samples: Sample[] = Array.from({ length: range === '24h' ? 24 : 7 }, (_, i) => ({
      time: range === '24h' ? `${String((15 + i) % 24).padStart(2, '0')}:00` : `10/${25 + i}`,
      download: stale
        ? null
        : Math.round(download * (0.22 + (Math.sin(i * 1.7) + 1) * 0.19 + (i > 15 ? 0.22 : 0))),
      upload: stale ? null : Math.round(upload * (0.3 + (Math.cos(i * 1.3) + 1) * 0.2)),
      latency: stale ? null : Math.round(latency * (0.8 + Math.sin(i) * 0.15)),
      loss: stale ? null : Number((loss * (0.8 + Math.cos(i) * 0.2)).toFixed(1)),
    }));
    const traffic: TrafficRecord[] = stale
      ? []
      : [
          ['nas', 'updates.example.net', 124],
          ['desktop', 'work.example.com', 86],
          ['phone', 'media.example.org', 213],
          ['tv', 'video.example.net', 65],
          ['laptop', 'docs.example.com', 42],
        ].map(([deviceId, domain, requests]) => ({
          deviceId: String(deviceId),
          domain: String(domain),
          requests: Number(requests) * (range === '7d' ? 7 : 1),
          source: '演示 · 可见 DNS 请求',
          observedAt: STAMP,
          availability: 'available',
        }));
    const capabilities: Capability[] = [
      {
        name: '设备发现与连通性',
        availability: stale ? 'stale' : 'available',
        detail: '结合邻居观察与探测；跨网段设备可能不完整。',
      },
      {
        name: '设备流量统计',
        availability: stale ? 'stale' : 'available',
        detail:
          mode === 'bypass'
            ? '仅统计经过旁路由的流量，不代表全网出口总量。'
            : '统计本机可见转发流量；硬件加速与绕行可能影响完整性。',
      },
      {
        name: '爱快管理数据',
        availability: 'denied',
        detail: '未连接爱快管理接口，原型只展示角色与演示探测。',
      },
      {
        name: '终端 CPU / 内存',
        availability: 'unsupported',
        detail: '普通终端需额外管理接口；目前仅本机资源可见。',
      },
      {
        name: '域名活动',
        availability: stale ? 'stale' : 'available',
        detail: '仅展示可见 DNS 活动；加密 DNS 与缓存命中可能不可见。',
      },
    ]
      .filter((c) => mode === 'bypass' || c.name !== '爱快管理数据')
      .map((c) => ({
        ...c,
        availability: c.availability as Availability,
        source: '演示 · 采集能力清单',
        observedAt: stamp,
      }));
    return {
      mode,
      scenario,
      devices,
      links,
      alerts,
      traffic,
      capabilities,
      samples,
      observedAt: stamp,
      metrics: {
        download: metric(download),
        upload: metric(upload),
        latency: metric(latency),
        loss: metric(loss),
      },
      coverage:
        mode === 'bypass'
          ? '旁路观测 · 仅包含经过 iStoreOS 的流量'
          : '主路由观测 · 加速、绕行或采集缺失可能影响完整性',
    };
  },
};
