import { describe, expect, it } from 'vitest';
import {
  defaults,
  demoService,
  displayMetric,
  normalizeSettings,
  scenarioLabels,
  type Mode,
  type Scenario,
} from './data';

describe('观测覆盖与拓扑', () => {
  it('旁路由不位于其他设备到互联网的必经路径', () => {
    const s = demoService.getSnapshot('bypass', 'normal', '24h');
    expect(s.links.find((l) => l.to === 'doctor')?.from).toBe('switch');
    expect(s.links.filter((l) => l.from === 'doctor')).toHaveLength(0);
    expect(s.links.find((l) => l.to === 'router')?.from).toBe('internet');
  });
  it('主路由模式替换核心角色并扩大可见范围', () => {
    const s = demoService.getSnapshot('gateway', 'normal', '24h');
    expect(s.devices.some((d) => d.id === 'router')).toBe(false);
    expect(s.links.find((l) => l.to === 'doctor')?.from).toBe('internet');
    expect(s.links.find((l) => l.to === 'switch')?.from).toBe('doctor');
    expect(s.capabilities.some((c) => c.name === '爱快管理数据')).toBe(false);
  });
  it('不可见流量使用 null，不能伪装成零', () => {
    const d = demoService
      .getSnapshot('bypass', 'normal', '24h')
      .devices.find((d) => d.id === 'speaker')!;
    expect(d.traffic.value).toBeNull();
    expect(displayMetric(d.traffic, ' GB')).toBe('不可见');
  });
  it('不猜测未知连接，保留推测连接证据', () => {
    const s = demoService.getSnapshot('bypass', 'normal', '24h');
    expect(s.links.some((l) => l.to === 'printer')).toBe(false);
    expect(s.links.find((l) => l.to === 'tv')?.confidence).toBe('inferred');
    expect(s.links.every((l) => Boolean(l.source && l.observedAt))).toBe(true);
  });
  it('终端负载和未授权路由器数据不伪造', () => {
    const s = demoService.getSnapshot('bypass', 'normal', '24h');
    expect(s.devices.find((d) => d.id === 'phone')?.cpu.availability).toBe('unsupported');
    expect(s.devices.find((d) => d.id === 'router')?.cpu.availability).toBe('denied');
  });
});
describe('诊断场景一致性', () => {
  for (const mode of ['bypass', 'gateway'] as Mode[]) {
    for (const scenario of Object.keys(scenarioLabels) as Scenario[]) {
      it(`${mode}/${scenario} 数据固定且关系完整`, () => {
        const s = demoService.getSnapshot(mode, scenario, '24h');
        expect(s).toEqual(demoService.getSnapshot(mode, scenario, '24h'));
        const ids = new Set(['internet', ...s.devices.map((d) => d.id)]);
        expect(s.links.every((l) => ids.has(l.from) && ids.has(l.to))).toBe(true);
        expect(s.alerts.every((a) => a.deviceIds.every((id) => ids.has(id)))).toBe(true);
        expect(s.alerts.filter((a) => !a.recovered)).toHaveLength(scenario === 'normal' ? 0 : 1);
      });
    }
  }
  it('无响应不等于已确认离线', () => {
    const s = demoService.getSnapshot('bypass', 'device', '24h');
    expect(s.devices.find((d) => d.id === 'nas')?.status).toBe('unresponsive');
    expect(s.alerts[0].evidence.join('')).toContain('3 次');
  });
  it('采集中断时移除当前指标，设备和拓扑统一过期', () => {
    const s = demoService.getSnapshot('bypass', 'stale', '24h');
    expect(s.devices.every((d) => d.status === 'unknown')).toBe(true);
    expect(s.links.every((l) => l.availability === 'stale')).toBe(true);
    expect(
      Object.values(s.metrics).every((m) => m.value === null && m.availability === 'stale'),
    ).toBe(true);
    expect(s.samples.every((p) => p.download === null && p.latency === null)).toBe(true);
    expect(s.traffic).toHaveLength(0);
  });
  it('丢包和拥塞场景包含对应指标证据', () => {
    expect(demoService.getSnapshot('bypass', 'loss', '24h').metrics.loss.value).toBe(8);
    expect(demoService.getSnapshot('gateway', 'congestion', '24h').metrics.download.value).toBe(
      926,
    );
  });
  it('时间范围切换同时改变趋势和设备累计数据', () => {
    const day = demoService.getSnapshot('bypass', 'normal', '24h');
    const week = demoService.getSnapshot('bypass', 'normal', '7d');
    expect(day.samples).toHaveLength(24);
    expect(week.samples).toHaveLength(7);
    expect(week.devices.find((d) => d.id === 'nas')!.traffic.value).toBeGreaterThan(
      day.devices.find((d) => d.id === 'nas')!.traffic.value!,
    );
    expect(week.traffic[0].requests).toBe(day.traffic[0].requests * 7);
  });
});
describe('本地设置恢复', () => {
  it('缺失或损坏设置安全回退', () => {
    expect(normalizeSettings(null)).toEqual(defaults);
    expect(
      normalizeSettings({
        mode: 'invalid',
        cache: -1,
        storage: '512',
        interval: NaN,
        retention: Infinity,
      }),
    ).toEqual(defaults);
  });
  it('有效自定义值可被完整恢复', () => {
    const settings = { mode: 'gateway', interval: 30, cache: 128, storage: 1024, retention: 60 };
    expect(normalizeSettings(settings)).toEqual(settings);
  });
});
