import React, { useEffect, useMemo, useState } from 'react';
import { ArrowDownToLine, ArrowUpRight, CheckCircle2, ChevronDown, ChevronLeft, ChevronRight, Copy, Ellipsis, FolderSync, Layers3, ListFilter, Plus, Search, Settings2, ShieldCheck, Trash2, X, Zap } from 'lucide-react';
import { Badge, Button, IconButton, Switch, Empty, country, formatTime } from './ui.jsx';

function statusMatches(node, status) {
  if(status==='all') return true;
  if(status==='enabled') return node.enabled && node.available;
  if(status==='disabled') return !node.enabled;
  if(status==='missing') return !node.available;
  if(status==='tested') return !!node.testedAt && node.latency!==null;
  if(status==='failed') return !!node.testedAt && node.latency===null;
  if(status==='untested') return !node.testedAt;
  return true;
}

export default function Nodes({ state, core, mutate, busy, run, setModal, exportNode, testNodes, initialFilter }) {
  const [search,setSearch]=useState('');
  const [type,setType]=useState('all');
  const [status,setStatus]=useState(initialFilter || 'all');
  const [subscription,setSubscription]=useState('all');
  const [selected,setSelected]=useState(new Set());
  const [page,setPage]=useState(1);
  const [menu,setMenu]=useState(null);
  const nodes=state.nodes;
  const enabled=nodes.filter(n=>n.enabled&&n.available).length;
  const filtered=useMemo(()=>nodes.filter(n=>(!search||`${n.name} ${n.port} ${n.server}`.toLowerCase().includes(search.toLowerCase()))&&(type==='all'||n.type===type)&&(subscription==='all'||n.subscriptionId===subscription)&&statusMatches(n,status)).sort((a,b)=>a.port-b.port),[nodes,search,type,status,subscription]);
  useEffect(()=>setStatus(initialFilter || 'all'),[initialFilter]);
  useEffect(()=>setPage(1),[search,type,status,subscription]);
  useEffect(()=>setSelected(prev=>new Set([...prev].filter(id=>nodes.some(n=>n.id===id)))),[nodes]);
  const pages=Math.max(1,Math.ceil(filtered.length/10)), currentPage=Math.min(page,pages);
  const visible=filtered.slice((currentPage-1)*10,currentPage*10);
  const allChecked=visible.length>0&&visible.every(n=>selected.has(n.id));
  const tabs=[['all','全部节点',nodes.length],['tested','测试通过',nodes.filter(n=>statusMatches(n,'tested')).length],['failed','未通过',nodes.filter(n=>statusMatches(n,'failed')).length],['disabled','已停用',nodes.filter(n=>!n.enabled).length]];
  function select(id){setSelected(prev=>{const next=new Set(prev);next.has(id)?next.delete(id):next.add(id);return next;});}
  function resetFilters(){setSearch('');setType('all');setStatus('all');setSubscription('all');}
  const subName=id=>state.subscriptions.find(s=>s.id===id)?.name||'独立节点';

  return <>
    <section className="panel node-panel">
      <div className="panel-toolbar"><div><h2>代理节点 <Badge tone="neutral">{nodes.length}</Badge><Badge tone="green"><ShieldCheck size={13}/>需账密认证</Badge></h2><p>{enabled} 个入口已启用<span className="inline-divider">·</span>固定端口在订阅刷新后保持不变</p></div><div className="toolbar-actions"><Button disabled={!!busy||!nodes.length} loading={busy==='test'} onClick={()=>testNodes([...selected])}><Zap size={16}/>{selected.size?'测试所选':'连接测试'}</Button><Button variant="primary-soft" disabled={!enabled} onClick={()=>setModal({type:'export',ids:[...selected]})}><ArrowDownToLine size={16}/>导出代理</Button></div></div>
      <div className="node-tabs" role="group" aria-label="节点连接状态">{tabs.map(([id,label,count])=><button key={id} className={status===id?'active':''} onClick={()=>setStatus(id)} aria-pressed={status===id}>{label}<span>{count}</span></button>)}<span className="node-tabs-status"><span className={`status-dot ${core.connected?'green':'gray'}`}/>{core.connected?'内核已连接':'内核待连接'}</span></div>
      <div className="filter-bar"><div className="search-field"><Search size={17}/><input value={search} onChange={e=>setSearch(e.target.value)} placeholder="搜索节点名称、端口或服务器" aria-label="搜索节点"/>{search&&<button aria-label="清空搜索" onClick={()=>setSearch('')}><X size={14}/></button>}</div><div className="filters"><div className="select-wrap"><Layers3 size={15}/><select aria-label="协议筛选" value={type} onChange={e=>setType(e.target.value)}><option value="all">全部协议</option>{[...new Set(nodes.map(n=>n.type))].map(t=><option key={t} value={t}>{t.toUpperCase()}</option>)}</select><ChevronDown size={13}/></div><div className="select-wrap"><FolderSync size={15}/><select aria-label="订阅筛选" value={subscription} onChange={e=>setSubscription(e.target.value)}><option value="all">全部订阅</option>{state.subscriptions.map(s=><option key={s.id} value={s.id}>{s.name}</option>)}<option value="">独立节点</option></select><ChevronDown size={13}/></div><div className="select-wrap"><ListFilter size={15}/><select aria-label="状态筛选" value={status} onChange={e=>setStatus(e.target.value)}><option value="all">全部状态</option><option value="tested">测试通过</option><option value="failed">测试未通过</option><option value="untested">尚未测试</option><option value="enabled">已启用</option><option value="disabled">已停用</option><option value="missing">来源失效</option></select><ChevronDown size={13}/></div></div></div>
      {selected.size>0&&<div className="batch-bar"><span>已选择 <strong>{selected.size}</strong> 个节点</span><button onClick={()=>run('batch',()=>mutate('/nodes/batch','POST',{ids:[...selected],enabled:true},'所选节点已启用'))} disabled={!!busy}>批量启用</button><button onClick={()=>run('batch',()=>mutate('/nodes/batch','POST',{ids:[...selected],enabled:false},'所选节点已停用'))} disabled={!!busy}>批量停用</button><button onClick={()=>setSelected(new Set())}>取消选择</button></div>}
      {!nodes.length?<Empty action={<Button variant="primary" onClick={()=>setModal({type:'subscription'})}><Plus size={16}/>导入第一个订阅</Button>}/>:<>
        <div className="table-scroll"><table className="node-table"><thead><tr><th className="check-cell"><input type="checkbox" aria-label="选择本页所有节点" checked={allChecked} onChange={()=>setSelected(prev=>{const next=new Set(prev);visible.forEach(n=>allChecked?next.delete(n.id):next.add(n.id));return next;})}/></th><th>节点名称</th><th>协议</th><th>固定代理入口</th><th>连接测试</th><th>启用</th><th className="action-heading">操作</th></tr></thead><tbody>{visible.map(n=><tr key={n.id} className={selected.has(n.id)?'selected':''}>
          <td className="check-cell"><input type="checkbox" checked={selected.has(n.id)} onChange={()=>select(n.id)} aria-label={`选择 ${n.name}`}/></td>
          <td><div className="node-identity"><div className="country-flag" title={country(n.name)[1]}>{country(n.name)[0]}</div><div><div className="node-name" title={n.name}>{n.name}</div><div className="node-source">{subName(n.subscriptionId)}{!n.available&&<span className="missing-mark"> · 来源失效</span>}</div></div></div></td>
          <td><Badge tone={n.type==='anytls'?'blue':n.type==='vless'?'violet':'neutral'}>{n.type.toUpperCase()}</Badge></td>
          <td><button className="endpoint" title="复制包含认证的 HTTP 代理地址" onClick={()=>exportNode(n)} disabled={!n.enabled||!n.available}><span>{state.settings.publicHost.includes(':')?`[${state.settings.publicHost}]`:state.settings.publicHost}<strong>:{n.port}</strong></span><Copy size={14}/></button><div className="endpoint-protocol">HTTP <span>/</span> SOCKS5</div></td>
          <td><span className={`latency ${n.testedAt?n.latency!==null?'success':'failed':''}`}><span className={`status-dot ${n.testedAt?n.latency!==null?'green':'amber':'gray'}`}/>{n.testedAt?n.latency!==null?`${n.latency.toLocaleString()} ms`:'未通过':'尚未测试'}</span>{n.testedAt&&<div className="tested-time">{formatTime(n.testedAt)}</div>}</td>
          <td><Switch checked={n.enabled} label={`${n.name} 启用状态`} disabled={!!busy||!n.available} onChange={enabled=>run(`toggle-${n.id}`,()=>mutate(`/nodes/${n.id}`,'PATCH',{enabled}))}/></td>
          <td><div className="row-actions"><button className="row-edit" onClick={()=>setModal({type:'node',node:n})}>编辑</button><div className="row-menu-container"><IconButton title={`${n.name} 更多操作`} onClick={()=>setMenu(menu===n.id?null:n.id)}><Ellipsis size={19}/></IconButton>{menu===n.id&&<><div className="menu-dismiss" onClick={()=>setMenu(null)}/><div className="row-menu"><button onClick={()=>{setMenu(null);setModal({type:'export',ids:[n.id]});}}><Copy size={15}/>导出地址</button><button className="danger-text" onClick={()=>{setMenu(null);setModal({type:'delete',title:'删除节点',text:`确定删除「${n.name}」及其端口映射？订阅节点会在下次同步时重新导入；临时停用可使用开关。`,action:()=>mutate(`/nodes/${n.id}`,'DELETE',undefined,'节点已删除')});}}><Trash2 size={15}/>删除节点</button></div></>}</div></div></td>
        </tr>)}</tbody></table>{!filtered.length&&<Empty title="没有找到匹配的节点" text="试试其他关键词，或调整筛选条件。" action={<Button onClick={resetFilters}>重置筛选</Button>}/>}</div>
        <div className="pagination"><span>共 <strong>{filtered.length}</strong> 个节点<span className="inline-divider">·</span>每页 10 条</span><div><span className="page-range">{filtered.length?(currentPage-1)*10+1:0}–{Math.min(currentPage*10,filtered.length)} / {filtered.length}</span><IconButton title="上一页" disabled={currentPage===1} onClick={()=>setPage(currentPage-1)}><ChevronLeft size={17}/></IconButton>{Array.from({length:pages},(_,i)=>i+1).filter(n=>n===1||n===pages||Math.abs(n-currentPage)<=1).map((n,index,arr)=><React.Fragment key={n}>{index>0&&n-arr[index-1]>1&&<span className="page-ellipsis">…</span>}<button className={`page-number ${n===currentPage?'active':''}`} onClick={()=>setPage(n)} aria-label={`第 ${n} 页`} aria-current={n===currentPage?'page':undefined}>{n}</button></React.Fragment>)}<IconButton title="下一页" disabled={currentPage===pages} onClick={()=>setPage(currentPage+1)}><ChevronRight size={17}/></IconButton></div></div>
      </>}
    </section><div className="below-table"><ShieldCheck size={15}/><span>HTTP 与 SOCKS5 共用端口，均启用账号密码认证。</span><button onClick={()=>setModal({type:'guide'})}>查看连接说明<ArrowUpRight size={14}/></button></div>
  </>;
}
