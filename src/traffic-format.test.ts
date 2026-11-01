import {describe,it,expect} from 'vitest';
import {displayMetric,formatTraffic} from './data';
describe('流量自适应单位',()=>{
 it('小流量不显示长串 GB 小数',()=>{expect(formatTraffic(0.00096)).toBe('960 KB');expect(formatTraffic(0.96)).toBe('960 MB');expect(formatTraffic(1.25)).toBe('1.25 GB');expect(formatTraffic(0)).toBe('0 B');});
 it('不可见和过期仍显示原因而不是零',()=>{expect(displayMetric({value:null,availability:'invisible',source:'test',observedAt:''},' GB')).toBe('不可见');});
});
