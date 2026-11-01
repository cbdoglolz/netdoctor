import {useEffect,useState} from 'react';
type SyncStatus={enabled:boolean;isManager:boolean;managerURL:string;revision:number;hash:string;updatedAt:string;savedAt:string;lastContact:string;managerOnline:boolean;allBackedUp:boolean;devices:number;layouts:number;error:string;scope:string;replicas:{id:string;name:string;revision:number;hash:string;at:string;error?:string}[]};
const time=(v:string)=>v&&!v.startsWith('0001')?new Date(v).toLocaleString('zh-CN'):'尚未完成';
export default function CatalogSync(){
 const [data,setData]=useState<SyncStatus|null>(null),[error,setError]=useState('');
 useEffect(()=>{let active=true;async function poll(){try{const r=await fetch('/api/catalog',{cache:'no-store',signal:AbortSignal.timeout(8000)});const v=await r.json();if(!r.ok)throw Error(v.error||'读取同步状态失败');if(active){setData(v);setError('')}}catch(e){if(active)setError(String(e))}}void poll();const t=setInterval(poll,5000);return()=>{active=false;clearInterval(t)}},[]);
 if(!data)return <p role="status">{error||'正在读取公共资料同步状态…'}</p>;
 const title=!data.enabled?'公共资料同步未启用':data.isManager?(data.allBackedUp?'公共资料已保存并备份':'公共资料仅本机保存 · 等待备份'):(data.managerOnline?'公共资料自动同步中':'管理中心失联 · 使用本机已保存副本');
 return <div className="control-page"><section className="panel"><div className="control-form"><h2>公共资料同步与备份</h2><strong role="status">{error?'同步状态读取失败，不能确认当前备份状态':title}</strong>{error&&<p role="alert">{error}</p>}{data.error&&<p role="alert">{data.error}</p>}<p>备注、设备类型、型号、分组、上传图标和已保存的拓扑布局只在管理中心编辑。节点独立保存副本，断联或重启不清空。</p>
 <dl className="catalog-facts"><dt>当前角色</dt><dd>{data.isManager?'管理中心':'受管节点／公共资料副本'}</dd><dt>公共版本</dt><dd>v{data.revision} · {data.devices} 条条目 · {data.layouts} 层布局</dd><dt>资料修改时间</dt><dd>{time(data.updatedAt)}</dd><dt>本机持久保存时间</dt><dd>{time(data.savedAt)}</dd>{!data.isManager&&<><dt>最近联系管理中心</dt><dd>{time(data.lastContact)}</dd></>}</dl>
 {data.enabled&&!data.isManager&&<p><a className="button" href={data.managerURL+'/#devices'} target="_blank" rel="noreferrer">前往管理中心编辑资料（独立登录）</a></p>}
 <p className="coverage-note">后台每约 5 秒核对版本，只在内容变化时传输完整公共资料；失败自动重试。保留最近 8 个旧版本文件供管理员恢复，尚无网页历史恢复入口。</p>
 </div></section>
 {data.isManager&&<section className="panel"><div className="control-form"><h2>备用副本确认</h2>{!data.replicas.length&&<p>尚无备用节点确认，不能视为已备份。</p>}{data.replicas.map(p=><article className="control-record" key={p.id}><strong>{p.name}</strong><p>{p.error||error?'待同步／状态未确认':p.revision===data.revision&&p.hash===data.hash&&Date.now()-new Date(p.at).getTime()<20000?'已持久保存当前版本':'待同步'}</p><small>已确认版本 v{p.revision} · 最近确认 {time(p.at)}</small>{p.error&&<p>{p.error}</p>}</article>)}</div></section>}
 <section className="panel"><div className="control-form"><h2>备份范围</h2><p>{data.scope}</p><p>本阶段提供公共资料的自动持久复制，不会让备用节点自动取得管理权；同一台 ESXi 或共同存储故障仍可能同时影响两台。</p></div></section></div>;
}
