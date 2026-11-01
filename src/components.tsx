import { useMemo, useState, useEffect, useRef } from 'react';
import {
  Gamepad2,
  RectangleEllipsis,
  Fan,
  AudioLines,
  Lightbulb,
  RadioTower,
  Camera,
  Printer,
  Speaker,
  Tablet,
  Tv,
  Server,
  Boxes,
  Activity,
  ArrowDown,
  ArrowUp,
  CircuitBoard,
  Cloud,
  Computer,
  HardDrive,
  Monitor,
  Network,
  Router,
  Smartphone,
  Wifi,
} from 'lucide-react';
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import {
  Background,
  Controls,
  Handle,
  Position,
  MarkerType,
  ReactFlow,
  useReactFlow,
  useNodesInitialized,
  BaseEdge,
  getSmoothStepPath,
  type EdgeProps,
  type Node,
  type NodeProps,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { statusLabels, type DeviceKind, type Device, type Snapshot, type Sample } from './data';
export const icons = {
  gateway: Router,
  switch: Network,
  ap: Wifi,
  server: HardDrive,
  computer: Monitor,
  phone: Smartphone,
  iot: CircuitBoard,
  tablet: Tablet,
  tv: Tv,
  camera: Camera,
  printer: Printer,
  speaker: Speaker,
  nas: HardDrive,
  hypervisor: Server,
  vm: Boxes,
  console: Gamepad2,
  settop: RectangleEllipsis,
  monitor: Monitor,
  fan: Fan,
  amplifier: AudioLines,
  hub: RadioTower,
  light: Lightbulb,
};
export function DeviceIcon({ kind, size = 20 }: { kind: DeviceKind; size?: number }) {
  const Icon = icons[kind];
  return <Icon size={size} strokeWidth={1.65} />;
}
export function DeviceBadge({ device }: { device: Device }) {
  return (
    <span className={`device-icon ${device.kind}`}>
      {device.customIcon ? (
        <img className="custom-device-icon" src={device.customIcon} alt="自定义设备图标" />
      ) : (
        <DeviceIcon kind={device.icon || device.kind} />
      )}
    </span>
  );
}
export function StatusBadge({ status }: { status: Device['status'] }) {
  return (
    <span className={`status-badge ${status}`}>
      <i />
      {statusLabels[status]}
    </span>
  );
}
export function TrafficChart({
  samples,
  compact = false,
  quality = false,
}: {
  samples: Sample[];
  compact?: boolean;
  quality?: boolean;
}) {
  if (samples.every((s) => s.download === null))
    return (
      <div className="chart-empty">
        <Activity size={30} />
        <strong>等待采集恢复</strong>
        <span>数据已过期，当前数值不可用</span>
      </div>
    );
  return (
    <div className={`chart ${compact ? 'compact' : ''}`}>
      <ResponsiveContainer width="100%" height="100%" minWidth={0} minHeight={0}>
        {quality ? (
          <LineChart data={samples} margin={{ top: 10, right: 12, bottom: 0, left: -16 }}>
            <CartesianGrid vertical={false} stroke="#edf0f5" />
            <XAxis
              dataKey="time"
              tickLine={false}
              axisLine={false}
              minTickGap={32}
              tick={{ fontSize: 11, fill: '#9aa4b4' }}
            />
            <YAxis tickLine={false} axisLine={false} tick={{ fontSize: 11, fill: '#9aa4b4' }} />
            <YAxis
              yAxisId="loss"
              orientation="right"
              unit="%"
              tickLine={false}
              axisLine={false}
              tick={{ fontSize: 11, fill: '#9aa4b4' }}
            />
            <Tooltip />
            <Line
              type="monotone"
              dataKey="latency"
              name="延迟 (ms)"
              stroke="#3182f6"
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
            <Line
              yAxisId="loss"
              type="monotone"
              dataKey="loss"
              name="丢包率 (%)"
              stroke="#edaa4a"
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
          </LineChart>
        ) : (
          <AreaChart data={samples} margin={{ top: 12, right: 10, bottom: 0, left: -16 }}>
            <defs>
              <linearGradient id="download-fill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#3a8aff" stopOpacity={0.19} />
                <stop offset="100%" stopColor="#3a8aff" stopOpacity={0.01} />
              </linearGradient>
            </defs>
            <CartesianGrid vertical={false} stroke="#edf0f5" />
            <XAxis
              dataKey="time"
              tickLine={false}
              axisLine={false}
              minTickGap={34}
              tick={{ fontSize: 11, fill: '#9aa4b4' }}
            />
            <YAxis tickLine={false} axisLine={false} tick={{ fontSize: 11, fill: '#9aa4b4' }} />
            <Tooltip
              contentStyle={{ borderRadius: 10, border: '1px solid #e8edf3', fontSize: 12 }}
            />
            <Area
              type="monotone"
              dataKey="download"
              name="下载 (Mbps)"
              stroke="#3182f6"
              strokeWidth={2.2}
              fill="url(#download-fill)"
              isAnimationActive={false}
            />
            <Area
              type="monotone"
              dataKey="upload"
              name="上传 (Mbps)"
              stroke="#4ec3b3"
              strokeWidth={1.8}
              fill="transparent"
              isAnimationActive={false}
            />
          </AreaChart>
        )}
      </ResponsiveContainer>
    </div>
  );
}
export function ChartLegend() {
  return (
    <span className="chart-legend">
      <span>
        <ArrowDown size={12} />
        下载
      </span>
      <span>
        <ArrowUp size={12} />
        上传
      </span>
      <small>Mbps</small>
    </span>
  );
}
type NetworkNode = Node<{
  bypassPath?: Device['bypassPath'];
  label: string;
  subtitle: string;
  kind: DeviceKind | 'internet' | 'unknown';
  status: string;
  compact: boolean;
  customIcon?: string;
  onOpen?: () => void;
}>;
function TopologyNode({ data }: NodeProps<NetworkNode>) {
  const Icon =
    data.kind === 'internet' ? Cloud : data.kind === 'unknown' ? Computer : icons[data.kind];
  return (
    <div
      className={`topology-node ${data.status} ${data.compact ? 'small' : ''}`}
      role={data.onOpen ? 'button' : undefined}
      tabIndex={data.onOpen ? 0 : undefined}
      aria-label={data.onOpen ? `查看${data.label}详情` : undefined}
      onKeyDown={(event) => {
        if (data.onOpen && (event.key === 'Enter' || event.key === ' ')) {
          event.preventDefault();
          data.onOpen();
        }
      }}
    >
      <Handle type="target" position={Position.Top} />
      <Handle type="source" id="upstream" position={Position.Top} />
      <div className="node-symbol">
        {data.customIcon ? (
          <img className="custom-device-icon" src={data.customIcon} alt="自定义设备图标" />
        ) : (
          <Icon size={data.compact ? 19 : 27} strokeWidth={1.5} />
        )}
        <i />
      </div>
      <strong>{data.label}</strong>
      <small>{data.subtitle}</small>
      {data.bypassPath && data.bypassPath.state !== 'unknown' && <BypassBadge path={data.bypassPath} />}
      <Handle type="source" position={Position.Bottom} />
      <Handle type="target" id="downstream" position={Position.Bottom} />
    </div>
  );
}
export function BypassBadge({path}: {path: Device['bypassPath']}) {
  if (!path) return null;
  const note = `${path.detail}${path.observedAt ? `；最近观测 ${new Date(path.observedAt).toLocaleString('zh-CN')}` : ''}${path.observedIP ? `；观测地址 ${path.observedIP}` : ''}`;
  return <span className={`bypass-badge ${path.state}`} title={note} aria-label={`${path.label}。${note}`}>{path.label}</span>;
}
const nodeTypes = { network: TopologyNode };
function FlowEdge(props: EdgeProps) {
  const [path, x, y] = getSmoothStepPath(props);
  const data = props.data as
    { measured?: boolean; downloadMbps?: number; uploadMbps?: number } | undefined;
  return (
    <>
      <BaseEdge
        id={props.id}
        path={path}
        style={props.style}
        markerEnd={props.markerEnd}
        label={props.label}
        labelX={x}
        labelY={y}
        labelStyle={props.labelStyle}
      />
      {data?.measured &&
        [
          ['uploadMbps', '#2ab58a', '1;0'],
          ['downloadMbps', '#3182f6', '0;1'],
        ].map(([key, color, points]) => {
          const rate = data[key as 'uploadMbps' | 'downloadMbps'] || 0;
          return rate > 0 ? (
            <circle key={key} r="3" fill={color} className="traffic-particle">
              <animateMotion
                path={path}
                dur={`${Math.max(0.45, 4 - Math.log10(1 + rate))}s`}
                repeatCount="indefinite"
                keyPoints={points}
                keyTimes="0;1"
                calcMode="linear"
              />
            </circle>
          ) : null;
        })}
    </>
  );
}
const edgeTypes = { flow: FlowEdge };
export function Topology({
  snapshot,
  compact = false,
  filter = 'all',
  onDevice,
}: {
  snapshot: Snapshot;
  compact?: boolean;
  filter?: string;
  onDevice: (id: string) => void;
}) {
  const [layer, setLayer] = useState<'gateway' | 'physical'>('gateway');
  const [showUnlinked, setShowUnlinked] = useState(false);
  const container = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 900, height: 570 });
  useEffect(() => {
    const el = container.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) =>
      setSize({ width: entry.contentRect.width, height: entry.contentRect.height }),
    );
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const { nodes, edges } = useMemo(() => {
    const gateway = snapshot.mode === 'gateway';
    const coords: Record<string, [number, number]> = {
      internet: [400, 0],
      router: [400, 125],
      switch: [400, 250],
      doctor: gateway ? [400, 125] : [20, 380],
      ap: [300, 380],
      nas: [560, 380],
      desktop: [780, 380],
      laptop: [80, 540],
      phone: [260, 540],
      tablet: [440, 540],
      tv: [620, 540],
      speaker: [800, 540],
      printer: [1020, 380],
    };
    if (compact) {
      Object.assign(coords, {
        internet: [160, 0],
        router: [160, 65],
        switch: [160, 130],
        doctor: gateway ? [160, 65] : [0, 195],
        ap: [160, 195],
        nas: [320, 195],
      });
    }
    const active = snapshot.devices.filter(
      (d) =>
        (!compact || snapshot.live || ['router', 'switch', 'doctor', 'ap', 'nas'].includes(d.id)) &&
        (filter !== 'issue' ||
          d.status !== 'online' ||
          ['router', 'switch', 'doctor', 'ap'].includes(d.id)),
    );
    const nodes: NetworkNode[] = [
      {
        id: 'internet',
        type: 'network',
        position: { x: coords.internet[0], y: coords.internet[1] },
        data: {
          label: '互联网',
          subtitle: snapshot.scenario === 'loss' ? '上游丢包' : '外部探测目标',
          kind: 'internet',
          status: ['loss', 'congestion'].includes(snapshot.scenario)
            ? 'unresponsive'
            : snapshot.scenario === 'stale'
              ? 'unknown'
              : 'online',
          compact,
        },
      },
      ...active.map((d) => ({
        id: d.id,
        type: 'network',
        position: {
          x: (coords[d.id] ?? [
            100 + (active.indexOf(d) % 5) * 200,
            380 + Math.floor(active.indexOf(d) / 5) * 140,
          ])[0],
          y: (coords[d.id] ?? [
            100 + (active.indexOf(d) % 5) * 200,
            380 + Math.floor(active.indexOf(d) / 5) * 140,
          ])[1],
        },
        data: {
          label: d.name,
          subtitle:
            d.ip === 'node-internet'
              ? '外部连通性未验证'
              : d.ip.startsWith('node-')
                ? d.model
                : d.id === 'printer'
                  ? '连接位置待确认'
                  : d.ip,
          kind: d.ip === 'node-internet' ? ('internet' as const) : d.icon || d.kind,
          status: d.status,
          customIcon: d.customIcon,
          bypassPath: d.bypassPath,
          compact,
          onOpen: () => onDevice(d.id),
        },
      })),
    ];
    if (snapshot.live) nodes.splice(0, 1);
    const visibleLinks = snapshot.links.filter(
      (l) =>
        !snapshot.live ||
        (layer === 'gateway' ? l.relation === 'gateway' : l.relation !== 'gateway'),
    );
    if (snapshot.live) {
      const included = new Set(visibleLinks.flatMap((l) => [l.from, l.to]));
      const logicalParent = new Map(
        visibleLinks.filter((l) => l.relation === 'gateway').map((l) => [l.from, l.to]),
      );
      const physicalParent = new Map(
        visibleLinks.filter((l) => l.relation !== 'gateway').map((l) => [l.to, l.from]),
      );
      const parent = layer === 'gateway' ? logicalParent : physicalParent;
      const rows = new Map<number, number>();
      let unlinked = 0;
      const count = nodes.filter((n) => !included.has(n.id)).length;
      const columns = Math.max(
        1,
        Math.ceil(Math.sqrt((((count * size.width) / size.height) * 145) / 190)),
      );
      for (const n of nodes) {
        let depth = 0,
          id = n.id;
        const visited = new Set<string>();
        while (parent.has(id) && !visited.has(id) && depth < 8) {
          visited.add(id);
          id = parent.get(id)!;
          depth++;
        }
        if (!included.has(n.id)) {
          depth = 5;
        }
        const col = rows.get(depth) || 0;
        rows.set(depth, col + 1);
        n.position = included.has(n.id)
          ? { x: col * 190, y: depth * 125 }
          : { x: (unlinked % columns) * 190, y: (5 + Math.floor(unlinked++ / columns)) * 145 };
      }
      if (compact || !showUnlinked) {
        for (let i = nodes.length - 1; i >= 0; i--)
          if (!included.has(nodes[i].id)) nodes.splice(i, 1);
      }
    }
    const ids = new Set(nodes.map((n) => n.id));
    const edges = visibleLinks
      .filter((l) => ids.has(l.from) && ids.has(l.to))
      .map((l) => ({
        id: l.id,
        source: l.pathObserved ? l.from : l.relation === 'gateway' ? l.to : l.from,
        target: l.pathObserved ? l.to : l.relation === 'gateway' ? l.from : l.to,
        sourceHandle: l.pathObserved ? 'upstream' : undefined,
        targetHandle: l.pathObserved ? 'downstream' : undefined,
        markerEnd: l.pathObserved ? { type: MarkerType.ArrowClosed, color: '#3182f6' } : undefined,
        type: 'flow',
        data: { measured: l.measured, downloadMbps: l.downloadMbps, uploadMbps: l.uploadMbps },
        style: {
          stroke:
            l.pathObserved ? '#3182f6' : l.status === 'online'
              ? '#32ad88'
              : l.status === 'unresponsive'
                ? '#dea14a'
                : l.availability === 'stale'
                  ? '#c3cad5'
                  : l.to === 'nas' && snapshot.scenario === 'device'
                    ? '#edab4b'
                    : '#9fb9d8',
          strokeWidth: 1.4,
          animationDuration: `${Math.max(0.3, 3 - Math.log10(1 + (l.downloadMbps || 0) + (l.uploadMbps || 0)))}s`,
          strokeDasharray: l.confidence === 'inferred' ? '6 5' : undefined,
        },
        label: l.pathObserved ? '流量 → 旁路由' : snapshot.live
          ? `${l.relation === 'gateway' ? '手动网关' : l.relation === 'virtual' ? '虚拟连接' : l.relation === 'wireless' ? '无线关联' : '物理接线'}${l.confidence === 'inferred' ? ' · 待核实' : ''}${l.measured ? ` ↓${(l.downloadMbps || 0).toFixed(2)} ↑${(l.uploadMbps || 0).toFixed(2)} Mbps` : ''}`
          : !compact && l.confidence === 'inferred'
            ? '推测'
            : undefined,
        labelStyle: { fill: '#8793a4', fontSize: 10 },
      }));
    return { nodes, edges };
  }, [snapshot, compact, filter, onDevice, layer, showUnlinked, size]);
  return (
    <div
      ref={container}
      className={`topology-canvas ${compact ? 'mini' : ''}`}
      data-testid="topology"
    >
      {snapshot.live && (
        <div className="topology-layer segmented">
          <button
            className={layer === 'gateway' ? 'active' : ''}
            onClick={() => setLayer('gateway')}
          >
            上网路径
          </button>
          <button
            className={layer === 'physical' ? 'active' : ''}
            onClick={() => setLayer('physical')}
          >
            物理 / 虚拟连接
          </button>
          {!compact && (
            <button onClick={() => setShowUnlinked(!showUnlinked)}>
              {showUnlinked ? '隐藏未关联设备' : '显示未关联设备'}
            </button>
          )}
        </div>
      )}
      <ReactFlow
        key={`${snapshot.mode}-${filter}-${compact}-${layer}-${showUnlinked}-${Math.round(size.width)}-${Math.round(size.height)}-${nodes.map(n=>n.id).sort().join(",")}`}
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView
        fitViewOptions={{ padding: compact ? 0.12 : 0.18 }}
        minZoom={0.015}
        maxZoom={1.6}
        nodesDraggable={false}
        nodesConnectable={false}
        onNodeClick={(_, node) => node.id !== 'internet' && onDevice(node.id)}
      >
        <AutoFit
          signature={nodes.map((n) => `${n.id}:${n.position.x}:${n.position.y}`).join('|')}
          width={size.width}
          height={size.height}
        />
        <Background gap={22} color="#e3e9f0" />
        {!compact && <Controls showInteractive={false} />}
      </ReactFlow>
    </div>
  );
}

function AutoFit({
  signature,
  width,
  height,
}: {
  signature: string;
  width: number;
  height: number;
}) {
  const ready = useNodesInitialized();
  const { fitView } = useReactFlow();
  useEffect(() => {
    if (!ready) return;
    const id = requestAnimationFrame(() => {
      void fitView({
        padding: { top: '55px', bottom: '25px', left: '18px', right: '18px' },
        minZoom: 0.015,
        maxZoom: 1.3,
        duration: 250,
      });
    });
    return () => cancelAnimationFrame(id);
  }, [ready, signature, width, height, fitView]);
  return null;
}
