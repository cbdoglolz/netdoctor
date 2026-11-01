import type {Device} from './data';
export function compareDevices(a:Device,b:Device,key:string,dir:number){
 let x: string|number|null=null,y:string|number|null=null;
 const get=(d:Device):string|number|null=>{if(key==='ip')return d.ip;if(key==='name')return d.name;if(key==='status')return ({offline:0,unresponsive:1,online:2,unknown:3})[d.status]??3;const o=key==='traffic'?d.traffic:d.latency;return o.availability==='available'?o.value:null;};x=get(a);y=get(b);
 if(x===null||y===null)return x===y?a.ip.localeCompare(b.ip,undefined,{numeric:true}):x===null?1:-1;
 const n=typeof x==='number'&&typeof y==='number'?x-y:String(x).localeCompare(String(y),'zh-CN',{numeric:true});return n*dir||a.ip.localeCompare(b.ip,undefined,{numeric:true});
}
