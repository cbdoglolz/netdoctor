import { useEffect, useState } from 'react';
import type { Snapshot, Range } from './data';
export function useLiveSnapshot(range: Range = '24h') {
  const [state, setState] = useState<{
    checked: boolean;
    live: boolean;
    snapshot: Snapshot | null;
    error: string;
  }>({ checked: false, live: false, snapshot: null, error: '' });
  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setInterval>;
    const controller = new AbortController();
    async function poll() {
      try {
        const response = await fetch('/api/snapshot', {
          cache: 'no-store',
          signal: AbortSignal.timeout(8000),
        });
        if (!response.ok) throw new Error('采集服务不可用或登录已过期，请重新登录');
        const snapshot = (await response.json()) as Snapshot & { error?: string };
        if (snapshot.error) throw new Error(snapshot.error);
        if (!Array.isArray(snapshot.devices) || !Array.isArray(snapshot.samples))
          throw new Error('无效采集数据');
        const hr = await fetch(`/api/history?range=${range}`, {
          cache: 'no-store',
          signal: AbortSignal.timeout(8000),
        });
        if (hr.ok) {
          const history = await hr.json();
          snapshot.samples = history.samples;
          snapshot.historyStart = history.historyStart;
          for (const d of snapshot.devices) {
            const total = history.usage[d.mac.toLowerCase()] ?? history.usage[d.ip];
            if (total !== undefined)
              d.traffic = {
                value: Number(total.toFixed(6)),
                availability: 'available',
                source: 'nlbwmon · 当前可见路径累计 GB',
                observedAt: snapshot.observedAt,
              };
          }
        }
        if (!Array.isArray(snapshot.devices) || !Array.isArray(snapshot.samples))
          throw new Error('无效采集数据');
        if (!disposed) setState({ checked: true, live: true, snapshot, error: '' });
      } catch (error) {
        if (!disposed)
          setState((s) => ({
            ...s,
            error: error instanceof Error ? error.message : '采集失败',
            snapshot: s.snapshot
              ? {
                  ...s.snapshot,
                  devices: s.snapshot.devices.map((d) => ({
                    ...d,
                    status: 'unknown',
                    latency: { ...d.latency, value: null, availability: 'stale' },
                    cpu: { ...d.cpu, value: null, availability: 'stale' },
                    memory: { ...d.memory, value: null, availability: 'stale' },
                    traffic: { ...d.traffic, value: null, availability: 'stale' },
                  })),
                  metrics: {
                    download: {
                      ...s.snapshot.metrics.download,
                      value: null,
                      availability: 'stale',
                    },
                    upload: { ...s.snapshot.metrics.upload, value: null, availability: 'stale' },
                    latency: { ...s.snapshot.metrics.latency, value: null, availability: 'stale' },
                    loss: { ...s.snapshot.metrics.loss, value: null, availability: 'stale' },
                  },
                  samples: [],
                  traffic: [],
                }
              : null,
          }));
      }
    }
    async function start() {
      try {
        const response = await fetch('/runtime.json', {
          signal: controller.signal,
          cache: 'no-store',
        });
        if (response.redirected || response.status === 401) {
          if (!disposed)
            setState({
              checked: true,
              live: true,
              snapshot: null,
              error: '登录已过期，请重新登录',
            });
          return;
        }
        const config = (await response.json()) as { live?: boolean };
        if (!config.live) throw new Error('static');
        if (disposed) return;
        setState((s) => ({ ...s, checked: true, live: true }));
        await poll();
        timer = setInterval(poll, 10000);
      } catch {
        if (!disposed) setState({ checked: true, live: false, snapshot: null, error: '' });
      }
    }
    void start();
    return () => {
      disposed = true;
      controller.abort();
      clearInterval(timer);
    };
  }, [range]);
  return state;
}
