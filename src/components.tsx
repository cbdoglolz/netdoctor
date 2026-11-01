import { useMemo } from 'react';
import {
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
  ReactFlow,
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
};
export function DeviceIcon({ kind, size = 20 }: { kind: DeviceKind; size?: number }) {
  const Icon = icons[kind];
  return <Icon size={size} strokeWidth={1.65} />;
}
export function DeviceBadge({ device }: { device: Device }) {
  return (
    <span className={`device-icon ${device.kind}`}>
      <DeviceIcon kind={device.kind} />
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
  label: string;
  subtitle: string;
  kind: DeviceKind | 'internet' | 'unknown';
  status: string;
  compact: boolean;
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
      <div className="node-symbol">
        <Icon size={data.compact ? 19 : 27} strokeWidth={1.5} />
        <i />
      </div>
      <strong>{data.label}</strong>
      <small>{data.subtitle}</small>
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
}
const nodeTypes = { network: TopologyNode };
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
        (!compact || ['router', 'switch', 'doctor', 'ap', 'nas'].includes(d.id)) &&
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
          subtitle: d.id === 'printer' ? '连接位置待确认' : d.ip,
          kind: d.kind,
          status: d.status,
          compact,
          onOpen: () => onDevice(d.id),
        },
      })),
    ];
    if (snapshot.live) nodes.splice(0, 1);
    const ids = new Set(nodes.map((n) => n.id));
    const edges = snapshot.links
      .filter((l) => ids.has(l.from) && ids.has(l.to))
      .map((l) => ({
        id: l.id,
        source: l.from,
        target: l.to,
        type: 'smoothstep',
        animated: false,
        style: {
          stroke:
            l.availability === 'stale'
              ? '#c3cad5'
              : l.to === 'nas' && snapshot.scenario === 'device'
                ? '#edab4b'
                : '#9fb9d8',
          strokeWidth: 1.4,
          strokeDasharray: l.confidence === 'inferred' ? '6 5' : undefined,
        },
        label: !compact && l.confidence === 'inferred' ? '推测' : undefined,
        labelStyle: { fill: '#8793a4', fontSize: 10 },
      }));
    return { nodes, edges };
  }, [snapshot, compact, filter, onDevice]);
  return (
    <div className={`topology-canvas ${compact ? 'mini' : ''}`} data-testid="topology">
      <ReactFlow
        key={`${snapshot.mode}-${filter}-${compact}`}
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: compact ? 0.12 : 0.18 }}
        minZoom={0.25}
        maxZoom={1.6}
        nodesDraggable={false}
        nodesConnectable={false}
        onNodeClick={(_, node) => node.id !== 'internet' && onDevice(node.id)}
      >
        <Background gap={22} color="#e3e9f0" />
        {!compact && <Controls showInteractive={false} />}
      </ReactFlow>
    </div>
  );
}
