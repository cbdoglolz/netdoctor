import {makeTaskID} from './control-utils';
import {useEffect, useState} from 'react';
import type {Device, Snapshot} from './data';

type Node = {id:string;name:string;status?:string;payload?:{snapshot:Snapshot}};
type Policy = {id:string;ip:string;mac:string;enabled:boolean;until:string;start?:string;end?:string;status:string;detail:string};
type Task = {request:{id:string;kind:string;ip?:string;target?:string;service?:string};state:string;at:string;output:string};
type Status = {nodeID:string;managerID:string;enabled:boolean;canManage:boolean;scope:string;error:string;state:{policies:Policy[];tasks:Task[]};services:string[]};
const taskLabels:Record<string,string>={pause:'暂停上网',schedule:'每日定时',policy:'策略开关','delete-policy':'删除策略',diagnose:'网络诊断',dns:'DNS 查询','service-status':'服务状态','service-restart':'重启服务'};
const statusLabels:Record<string,string>={queued:'已接收',running:'执行中',completed:'已执行',failed:'执行失败',interrupted:'中断，需核查',installed:'规则已安装',inactive:'当前未生效',expired:'已到期',error:'更新失败',pending:'等待安装',unavailable:'条件不满足'};
const fmt=(v:string)=>new Date(v).toLocaleString('zh-CN');

export default function DeviceControl({initialIP=''}:{initialIP?:string}) {
 const [nodes,setNodes]=useState<Node[]>([]),[manager,setManager]=useState(false),[selected,setSelected]=useState(''),[reports,setReports]=useState<Record<string,Status>>({}),[error,setError]=useState(''),[notice,setNotice]=useState(''),[busy,setBusy]=useState(false);
 const [deviceIP,setDeviceIP]=useState(initialIP),[minutes,setMinutes]=useState(15),[start,setStart]=useState('22:00'),[end,setEnd]=useState('07:00'),[target,setTarget]=useState('1.1.1.1'),[domain,setDomain]=useState('example.com'),[service,setService]=useState('nlbwmon');
 useEffect(()=>{let active=true;async function load(){try{
  const r=await fetch('/api/peers',{cache:'no-store',signal:AbortSignal.timeout(8000)});if(!r.ok)throw Error('读取节点失败，请检查登录状态');const v=await r.json();const all:Node[]=[{id:v.local.id,name:v.local.name,payload:v.local},...v.peers];
  const root=await fetch('/api/control',{cache:'no-store',signal:AbortSignal.timeout(8000)});if(!root.ok)throw Error('读取管理授权失败');const rootStatus:Status=await root.json();
  const id=selected||all.find(n=>n.payload?.snapshot.devices.some(d=>d.ip===initialIP&&d.bypassPath?.state==='recent'))?.id||v.local.id;
  const response=id===v.local.id?root:await fetch('/api/control?node='+encodeURIComponent(id),{cache:'no-store',signal:AbortSignal.timeout(14000)});
  const summary=id===v.local.id?rootStatus:await response.json();if(!response.ok)throw Error(summary.error||'节点控制状态读取失败');
  if(active){setNodes(all);setManager(rootStatus.canManage&&rootStatus.enabled);setSelected(id);setReports(old=>({...old,[id]:summary}));setError('')}
 }catch(e){if(active)setError(String(e))}}void load();const t=setInterval(load,5000);return()=>{active=false;clearInterval(t)}},[selected,initialIP]);
 const node=nodes.find(n=>n.id===selected),report=reports[selected];
 const devices:Device[]=node?.payload?.snapshot.devices.filter(d=>d.mac&&d.bypassPath?.state==='recent'&&!['gateway','router','switch','ap'].includes(d.kind)&&d.id!=='doctor')||[];
 const device=devices.find(d=>d.ip===deviceIP);
 const allowed=manager&&!!report?.enabled&&!error&&!busy;
 async function submit(kind:string,fields:Record<string,unknown>,nodeID=selected){
  const r=await fetch('/api/control',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:makeTaskID(),node:nodeID,kind,...fields}),signal:AbortSignal.timeout(15000)});const v=await r.json();if(!r.ok)throw Error(v.error||'提交失败');return v;
 }
 async function act(kind:string,fields:Record<string,unknown>,confirmation?:string){
  if(confirmation&&!window.confirm(confirmation))return;
  setBusy(true);setError('');try{const t=await submit(kind,fields);setNotice(`任务已接收：${t.request.id}。请查看下方执行结果；接收不等于执行成功。`)}catch(e){setError(String(e))}finally{setBusy(false)}
 }
 async function compare(){setBusy(true);setError('');try{for(const n of nodes.filter(n=>n.id===nodes[0]?.id||n.status==='online'))await submit('diagnose',{target},n.id);setNotice('已向在线节点提交同一目标诊断。切换节点查看各自结果，测试有少量启动时间差，不自动判定故障归属。')}catch(e){setError(String(e)+'；部分节点可能已接收，请先查看记录。')}finally{setBusy(false)}}
 return <div className="control-page">
  <section className="panel"><div className="panel-heading"><h2>设备控制</h2><span className="control-kind">IPv4 · 首版</span></div>
   <p className="coverage-note inset">由管理中心发起，目标节点本机执行。当前支持入口为 eth0 的旁路由；不修改终端网关、DNS、代理或 IPv6。</p>
   <div className="inventory-controls"><label>执行节点<select aria-label="执行节点" value={selected} onChange={e=>{setSelected(e.target.value);setDeviceIP('');setNotice('')}}>{nodes.map(n=><option key={n.id} value={n.id}>{n.name}{n.status&&n.status!=='online'?'（不可用或过期）':''}</option>)}</select></label></div>
   {!manager&&<p className="control-message">当前节点未获管理权。请从指定管理中心进入；若尚未配置控制授权，需要先由管理员配置。</p>}
   {error&&<p className="control-message error" role="alert">{error}</p>}{notice&&<p className="control-message" role="status">{notice}</p>}
   {report?.error&&<p className="control-message error">规则执行异常：{report.error}</p>}
  </section>
  <div className="control-grid">
   <section className="panel"><div className="panel-heading"><h2>上网控制</h2></div><div className="control-form">
    <label>有本节点近期路径证据的终端<select aria-label="控制设备" value={deviceIP} onChange={e=>setDeviceIP(e.target.value)}><option value="">选择设备</option>{devices.map(d=><option key={d.id} value={d.ip}>{d.name} · {d.ip}</option>)}</select></label>
    <small>{device?`目标 ${device.ip} · ${device.mac} → ${node?.name}`:'没有近期路径证据的设备不接受新建策略。已暂停设备可在下方策略列表恢复。'}</small>
    <label>临时暂停时长<select value={minutes} onChange={e=>setMinutes(Number(e.target.value))}>{[1,5,15,30,60,120,1440].map(n=><option key={n} value={n}>{n<60?`${n} 分钟`:`${n/60} 小时`}</option>)}</select></label>
    <button className="button primary" disabled={!allowed||!device} onClick={()=>void act('pause',{ip:device?.ip,mac:device?.mac,minutes},`确认暂停 ${device?.name}（${deviceIP}）经过 ${node?.name} 的 IPv4 公网请求 ${minutes} 分钟？内网访问保留，绕行流量不受此规则控制。`)}>暂停上网 · 到期恢复</button>
    <div className="control-time"><label>每日暂停开始<input aria-label="每日暂停开始" type="time" value={start} onChange={e=>setStart(e.target.value)}/></label><label>每日恢复时间<input aria-label="每日恢复时间" type="time" value={end} onChange={e=>setEnd(e.target.value)}/></label></div>
    <small>北京时间，支持跨午夜；按约 10 秒周期核对。与临时暂停重叠时，任一有效策略仍可阻断。</small>
    <button className="button" disabled={!allowed||!device} onClick={()=>void act('schedule',{ip:device?.ip,mac:device?.mac,start,end},`确认每天 ${start} 至 ${end} 暂停 ${deviceIP} 经过 ${node?.name} 的 IPv4 公网请求？`)}>添加每日策略</button>
   </div></section>
   <section className="panel"><div className="panel-heading"><h2>主动诊断与服务</h2></div><div className="control-form">
    <label>探测目标 IPv4<input aria-label="探测目标 IPv4" value={target} onChange={e=>setTarget(e.target.value)}/></label>
    <div className="button-row"><button className="button" disabled={!allowed} onClick={()=>void act('diagnose',{target})}>本节点 Ping / 路由追踪</button><button className="button" disabled={!allowed} onClick={()=>void compare()}>两节点对比探测</button></div>
    <label>DNS 查询域名<input aria-label="DNS 查询域名" value={domain} onChange={e=>setDomain(e.target.value)}/></label><button className="button" disabled={!allowed} onClick={()=>void act('dns',{target:domain})}>使用本节点系统解析器查询</button>
    <label>允许管理的服务<select value={service} onChange={e=>setService(e.target.value)}><option value="nlbwmon">nlbwmon · 流量计量</option><option value="avahi-daemon">avahi-daemon · 名称发现（可选）</option></select></label>
    <div className="button-row"><button className="button" disabled={!allowed} onClick={()=>void act('service-status',{service})}>检查状态</button><button className="button" disabled={!allowed} onClick={()=>void act('service-restart',{service},`确认在 ${node?.name} 重启 ${service}？短时间内相关采集可能中断。`)}>确认后重启服务</button></div>
   </div></section>
  </div>
  <section className="panel"><div className="panel-heading"><h2>本节点策略与恢复入口</h2></div><p className="coverage-note inset">{report?.scope||'正在读取…'}</p><div className="control-form">
   {!report?.state.policies.length&&<p>暂无策略。停用的策略会保留，方便恢复；到期的临时策略需重新创建。</p>}
   {report?.state.policies.map(p=><article className="control-record" key={p.id}><strong>{p.ip} · {p.start?`每日 ${p.start}—${p.end}`:`截至 ${fmt(p.until)}`}</strong><p>{statusLabels[p.status]||p.status} · {p.enabled?'已启用':'已停用'}</p><small>{p.mac} · {p.detail}</small><div className="button-row"><button className="button small" disabled={!allowed||(!p.enabled&&!p.start&&new Date(p.until).getTime()<Date.now())} onClick={()=>void act('policy',{policyID:p.id,enabled:!p.enabled},p.enabled?undefined:`确认重新启用 ${p.ip} 的暂停策略？`)}>{p.enabled?'停用此策略／恢复上网':'重新启用'}</button>{(!p.enabled||p.status==='expired')&&<button className="button small" disabled={!allowed} onClick={()=>void act('delete-policy',{policyID:p.id},`确认删除 ${p.ip} 的此条策略？执行历史保留。`)}>删除策略</button>}</div></article>)}
  </div></section>
  <section className="panel"><div className="panel-heading"><h2>执行记录 · {node?.name}</h2></div><div className="control-form">
   {!report?.state.tasks.length&&<p>暂无操作记录。</p>}
   {report?.state.tasks.slice().reverse().slice(0,30).map(t=><details className="control-record" key={t.request.id}><summary><strong>{taskLabels[t.request.kind]||t.request.kind} · {t.request.ip||t.request.target||t.request.service||'策略'}</strong> — {statusLabels[t.state]||t.state}<small>{fmt(t.at)} · {t.request.id}</small><p>{t.output.split('\n')[0]}</p></summary><pre>{t.output}</pre></details>)}
  </div></section>
 </div>;
}
