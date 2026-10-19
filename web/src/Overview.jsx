import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Activity, ArrowDownToLine, ArrowRight, ArrowUpRight, Check, CheckCircle2, Clock3, Copy, FolderSync, Globe2, Network, Plus, Radio, RefreshCw, ShieldCheck, TriangleAlert, Zap } from 'lucide-react';
import { Badge, Button, IconButton, country, formatTime } from './ui.jsx';

function Metric({ label, value, suffix, note, icon: Icon, tone = 'blue', badge, onClick }) {
  return <button className={`metric-card ${tone}`} onClick={onClick} type="button">
    <div className="metric-top"><span className="metric-icon"><Icon size={24} strokeWidth={1.8} /></span>{badge && <span className="metric-badge">{badge}</span>}<ArrowUpRight className="metric-arrow" size={17} /></div>
    <div className="metric-value">{value}{suffix && <span>{suffix}</span>}</div>
    <div className="metric-label">{label}</div><div className="metric-note">{note}</div>
  </button>;
}

function LatencyChart({ nodes }) {
  const [sort, setSort] = useState('port');
  const [hovered, setHovered] = useState(null);
  const chartRef = useRef(null);
  const [chartWidth, setChartWidth] = useState(1100);
  const data = useMemo(() => [...nodes].sort((a, b) => sort === 'port' ? a.port - b.port : a.latency - b.latency), [nodes, sort]);
  useEffect(() => {
    if (!chartRef.current) return;
    const observer = new ResizeObserver(entries => setChartWidth(Math.round(entries[0].contentRect.width)));
    observer.observe(chartRef.current);
    return () => observer.disconnect();
  }, [data.length]);
  const max = Math.max(1000, Math.ceil(Math.max(0, ...data.map(n => n.latency)) / 1000) * 1000);
  const width = Math.max(300, chartWidth), height = 260, left = 47, top = 18, bottom = 48, right = 15;
  const innerWidth = width - left - right, innerHeight = height - top - bottom;
  const spacing = innerWidth / Math.max(data.length, 1), barWidth = Math.min(36, spacing * .55);
  const selected = hovered !== null ? data[hovered] : null;
  const latencies = data.map(n => n.latency).sort((a, b) => a - b);
  const median = latencies.length ? Math.round((latencies[Math.floor((latencies.length - 1) / 2)] + latencies[Math.ceil((latencies.length - 1) / 2)]) / 2) : null;
  return <section className="panel latency-panel">
    <div className="card-heading"><div><h2>节点响应延迟</h2><p>已通过测试的节点 · 最近一次连接测试结果</p></div><div className="chart-controls"><span className="chart-legend"><i />连接延迟</span><select aria-label="延迟图排序" value={sort} onChange={e => { setSort(e.target.value); setHovered(null); }}><option value="port">按入口端口</option><option value="latency">按响应延迟</option></select></div></div>
    {data.length ? <>
      <div className="latency-chart" ref={chartRef}>
        <span className="chart-unit">毫秒 / ms</span>
        <svg viewBox={`0 0 ${width} ${height}`} style={{ height }} role="img" aria-label={`${data.length} 个节点的实际连接延迟，最低 ${latencies[0]} 毫秒，最高 ${latencies.at(-1)} 毫秒`}>
          <defs><linearGradient id="latency-fill" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="#4c9af2" stopOpacity=".9" /><stop offset="100%" stopColor="#4c9af2" stopOpacity=".22" /></linearGradient></defs>
          {[0, 1, 2, 3, 4].map(i => { const y = top + innerHeight * i / 4; return <g key={i}><line x1={left} x2={width-right} y1={y} y2={y} stroke="#edf0f5" strokeDasharray="3 5" /><text x={left-12} y={y+4} textAnchor="end" className="axis-label">{Math.round(max * (1-i/4)).toLocaleString()}</text></g>; })}
          {data.map((node, i) => {
            const x = left + spacing * (i + .5), h = node.latency / max * innerHeight;
            return <g key={node.id}>
              <rect x={x-spacing*.46} y={top} width={spacing*.92} height={innerHeight} fill={hovered === i ? '#f7faff' : 'transparent'} rx="5" />
              <rect x={x-barWidth/2} y={top+innerHeight-h} width={barWidth} height={Math.max(3,h)} rx="5" fill={hovered === i ? '#3c8ff1' : 'url(#latency-fill)'} tabIndex="0" role="graphics-symbol" aria-label={`${node.name}，入口 ${node.port}，延迟 ${node.latency} 毫秒`} onMouseEnter={() => setHovered(i)} onMouseLeave={() => setHovered(null)} onFocus={() => setHovered(i)} onBlur={() => setHovered(null)}><title>{node.name} · :{node.port} · {node.latency} ms</title></rect>
              {(data.length <= 8 || i % Math.ceil(data.length/(width < 600 ? 4 : 10)) === 0 || i === data.length-1) && <text className="axis-label" textAnchor="middle" x={x} y={height-19}>{node.port}</text>}
            </g>;
          })}
        </svg>
        {selected && <div className="chart-tooltip" style={{ left: `${Math.max(14, Math.min(85, (left + spacing * (hovered + .5)) / width * 100))}%` }}><strong>{country(selected.name)[0]} {selected.name}</strong><span>:{selected.port}<b>{selected.latency.toLocaleString()} ms</b></span></div>}
      </div>
      <div className="chart-footer"><span><i className="chart-footer-dot" />{data.length} 个节点通过测试</span><div><span>最低 <strong>{latencies[0].toLocaleString()} <small>ms</small></strong></span><span>中位数 <strong>{median.toLocaleString()} <small>ms</small></strong></span><span>最高 <strong>{latencies.at(-1).toLocaleString()} <small>ms</small></strong></span></div></div>
    </> : <div className="chart-empty"><Activity size={34} /><h3>还没有可展示的测试结果</h3><p>连接 Mihomo 并测试节点后，这里会显示实际响应延迟。</p></div>}
  </section>;
}

function HealthCard({ nodes, navigate }) {
  const passed = nodes.filter(n => n.testedAt && n.latency !== null).length;
  const failed = nodes.filter(n => n.testedAt && n.latency === null).length;
  const waiting = nodes.length - passed - failed;
  const total = nodes.length;
  const segments = [{ name: '测试通过', count: passed, color: '#4698f2', filter: 'tested' }, { name: '未通过', count: failed, color: '#f2b37b', filter: 'failed' }, { name: '未测试', count: waiting, color: '#e4eaf2', filter: 'untested' }];
  let offset = 0;
  return <section className="panel health-card"><div className="card-heading"><div><h2>节点连接状态</h2><p>最近一次测试的结果分布</p></div><Activity size={19} className="subtle-icon" /></div><div className="health-ring"><svg viewBox="0 0 170 170" role="img" aria-label={`${passed} 个测试通过，${failed} 个未通过，${waiting} 个未测试`}><circle cx="85" cy="85" r="65" fill="none" stroke="#f0f3f8" strokeWidth="16" />{segments.map(segment => { const length = total ? segment.count/total*100 : 0; const start = offset; offset += length; return <circle key={segment.name} cx="85" cy="85" r="65" fill="none" stroke={segment.color} strokeWidth="16" pathLength="100" strokeDasharray={`${length} ${100-length}`} strokeDashoffset={-start} transform="rotate(-90 85 85)" />; })}</svg><div><strong>{total ? Math.round(passed/total*100) : 0}<small>%</small></strong><span>测试通过率</span></div></div><div className="health-legend">{segments.map(segment => <button key={segment.name} onClick={() => navigate('nodes', segment.filter)}><span><i style={{ background: segment.color }} />{segment.name}</span><strong>{segment.count}<small>个</small></strong><ArrowRight size={13} /></button>)}</div></section>;
}

export default function Overview({ state, core, busy, run, refreshAll, refreshedAt, setModal, navigate, exportNode, testNodes }) {
  const nodes = state.nodes;
  const passed = nodes.filter(n => n.testedAt && n.latency !== null);
  const enabled = nodes.filter(n => n.enabled && n.available);
  const ready = passed.filter(n => n.enabled && n.available).sort((a,b) => a.latency-b.latency);
  const failed = nodes.filter(n => n.testedAt && n.latency === null).length;
  const dates = nodes.filter(n => n.testedAt).map(n => n.testedAt).sort();
  const latestTest = dates.at(-1);
  const firstPort = enabled.length ? Math.min(...enabled.map(n => n.port)) : state.settings.portStart;
  const lastPort = enabled.length ? Math.max(...enabled.map(n => n.port)) : state.settings.portEnd;
  return <>
    <div className="overview-toolbar"><div className="overview-live"><span className="status-dot blue" />当前运行概览<span className="toolbar-divider" /><Clock3 size={14} /><span>每 30 秒自动刷新</span></div><div className="overview-buttons"><Button loading={busy === 'refresh'} disabled={!!busy} onClick={() => run('refresh', refreshAll)}><RefreshCw size={16} />刷新状态</Button><Button variant="primary" onClick={() => setModal({ type:'subscription' })}><Plus size={17} />导入订阅</Button></div></div>
    <div className="metrics-grid">
      <Metric label="代理节点总数" value={nodes.length} note={`来自 ${state.subscriptions.length} 个订阅源`} icon={Network} onClick={() => navigate('nodes')} />
      <Metric label="测试通过的节点" value={passed.length} suffix={`/ ${nodes.length}`} note={latestTest ? `最近测试 ${formatTime(latestTest)}` : '连接内核后可开始测试'} icon={CheckCircle2} tone="green" badge={nodes.length ? `${Math.round(passed.length/nodes.length*100)}% 通过率` : undefined} onClick={() => navigate('nodes','tested')} />
      <Metric label="已启用代理入口" value={enabled.length} note={enabled.length ? `端口 ${firstPort}–${lastPort}` : '导入订阅后自动分配端口'} icon={Radio} onClick={() => navigate('nodes','enabled')} />
      <Metric label="订阅来源" value={state.subscriptions.length} note={`${state.subscriptions.filter(s => s.interval>0).length} 个订阅已开启自动更新`} icon={FolderSync} tone="violet" onClick={() => navigate('subscriptions')} />
    </div>
    <LatencyChart nodes={passed} />
    <div className="dashboard-bottom-grid">
      <section className="panel quick-nodes"><div className="card-heading"><div><h2>可用代理入口</h2><p>按最近一次响应延迟排序</p></div><button className="text-link" onClick={() => navigate('nodes','tested')}>查看全部<ArrowRight size={15} /></button></div>
        {ready.length ? <><div className="quick-node-list">{ready.slice(0,4).map(node => <div className="quick-node" key={node.id}><span className="country-flag">{country(node.name)[0]}</span><div className="quick-node-name"><strong title={node.name}>{node.name}</strong><span>固定入口 :{node.port}</span></div><span className="quick-node-delay">{node.latency.toLocaleString()} <small>ms</small></span><IconButton title={`复制 ${node.name} 的 HTTP 代理地址`} onClick={() => exportNode(node)}><Copy size={16} /></IconButton></div>)}</div><button className="card-bottom-action" onClick={() => setModal({type:'export',ids:ready.map(n => n.id)})}><ArrowDownToLine size={16} />导出 {ready.length} 个可用入口<ArrowRight size={15} /></button></> : <div className="small-empty"><Globe2 size={28} /><p>暂无通过测试且已启用的入口</p><Button loading={busy==='test'} disabled={!!busy||!enabled.length} onClick={() => testNodes()}><Zap size={15} />连接测试</Button></div>}
      </section>
      <HealthCard nodes={nodes} navigate={navigate} />
      <section className="panel recent-activity"><div className="card-heading"><div><h2>最近活动</h2><p>你的代理服务更新记录</p></div><button className="text-link" onClick={() => navigate('logs')}>全部<ArrowRight size={15} /></button></div><div className="activity-list">{state.events.slice(0,4).map(event => <div className="activity-item" key={event.id}><span className={`activity-mark ${event.kind}`}>{event.kind==='success' ? <Check size={13} /> : event.kind==='error' ? <TriangleAlert size={12} /> : <RefreshCw size={12} />}</span><div><strong>{event.message}</strong><time>{formatTime(event.time)}</time></div></div>)}</div><button className="card-bottom-action" onClick={() => navigate('settings')}><ShieldCheck size={16} />{state.revision===state.appliedRevision ? '当前配置已应用' : '有待应用的配置'}<ArrowRight size={15} /></button></section>
    </div>
    <div className="overview-note"><ShieldCheck size={16} /><span>所有入口使用账号密码认证，HTTP 与 SOCKS5 共用同一端口。</span><button onClick={() => setModal({type:'guide'})}>连接说明<ArrowUpRight size={14} /></button></div>
  </>;
}
