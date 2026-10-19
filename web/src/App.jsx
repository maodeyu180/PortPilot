import React, { useEffect, useState } from 'react';
import { Activity, ArrowRight, BookOpen, CheckCircle2, CircleHelp, FileCode2, FolderSync, LayoutDashboard, Loader2, LogOut, Menu, Network, PanelLeftClose, PanelLeftOpen, Plus, RefreshCw, ShieldCheck, SlidersHorizontal, SquareTerminal, TriangleAlert, X } from 'lucide-react';
import { api, copyText, Logo, Button, IconButton, Badge } from './ui.jsx';
import { Access, Settings, Logs, SubscriptionModal, NodeModal, ExportModal, ConfirmModal, ConfigModal, Guide } from './panels.jsx';
import { Login, Setup } from './Auth.jsx';
import Nodes from './Nodes.jsx';
import Overview from './Overview.jsx';
import Subscriptions from './Subscriptions.jsx';

const routes = [
  { id: 'overview', label: '服务概览', icon: LayoutDashboard },
  { id: 'nodes', label: '节点管理', icon: Network },
  { id: 'subscriptions', label: '订阅管理', icon: FolderSync },
  { id: 'access', label: '访问认证', icon: ShieldCheck },
  { id: 'settings', label: '运行配置', icon: SlidersHorizontal },
  { id: 'logs', label: '操作日志', icon: SquareTerminal },
];
const pageInfo = {
  overview: ['服务概览', '你的代理服务运行情况，一目了然。'],
  nodes: ['节点管理', '管理每个节点的固定入口，直接导出 HTTP 与 SOCKS5H 地址。'],
  subscriptions: ['订阅管理', '统一管理订阅来源，更新节点时保留原有端口。'],
  access: ['访问认证', '设置所有代理入口共用的账号与密码。'],
  settings: ['运行配置', '配置服务器入口、监听端口与 Mihomo 控制器。'],
  logs: ['操作日志', '订阅同步、节点调整与配置应用的完整记录。'],
};
const routeFromHash = () => routes.some(r => r.id === location.hash.slice(1)) ? location.hash.slice(1) : 'overview';
const EMPTY_IDS = [];

export default function App() {
  const [session, setSession] = useState(null);
  const [state, setState] = useState(null);
  const [core, setCore] = useState({ connected: false });
  const [route, setRoute] = useState(routeFromHash);
  const [nodeFilter, setNodeFilter] = useState('all');
  const [modal, setModal] = useState(null);
  const [toast, setToast] = useState(null);
  const [busy, setBusy] = useState('');
  const [loadError, setLoadError] = useState('');
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem('portpilot.sidebarCollapsed') === 'true');
  const [refreshedAt, setRefreshedAt] = useState(null);
  const notify = (message, error = false) => setToast({ message, error, key: Date.now() });

  async function load() {
    try { setState(await api('/state')); setRefreshedAt(new Date()); setLoadError(''); }
    catch (error) { setLoadError(error.message); }
  }
  async function refreshCore() {
    try { setCore(await api('/core')); }
    catch (error) { notify(error.message, true); }
  }
  async function refreshAll() { await Promise.all([load(), refreshCore()]); }
  useEffect(() => {
    api('/session').then(setSession).catch(e => setLoadError(e.message));
    const expired = () => { setState(null); api('/session').then(setSession).catch(error => setLoadError(error.message)); };
    const changed = () => setRoute(routeFromHash());
    window.addEventListener('session-expired', expired);
    window.addEventListener('hashchange', changed);
    return () => { window.removeEventListener('session-expired', expired); window.removeEventListener('hashchange', changed); };
  }, []);
  useEffect(() => {
    if (!session?.authenticated) return;
    refreshAll();
    const interval = setInterval(refreshAll, 30000);
    return () => clearInterval(interval);
  }, [session?.authenticated]);
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(null), 4500);
    return () => clearTimeout(timer);
  }, [toast]);
  useEffect(() => { localStorage.setItem('portpilot.sidebarCollapsed', String(collapsed)); }, [collapsed]);

  function navigate(id, filter = 'all') {
    if (id === 'nodes') setNodeFilter(filter);
    setRoute(id); setSidebarOpen(false);
    history.replaceState(null, '', `#${id}`);
    window.scrollTo({ top: 0, behavior: 'instant' });
  }
  async function mutate(path, method, body, message) {
    const result = await api(path, method, body);
    if (result.nodes) setState(result); else await load();
    if (message) notify(message);
    return result;
  }
  async function run(key, fn) {
    if (busy) return;
    setBusy(key);
    try { await fn(); } catch (error) { notify(error.message, true); } finally { setBusy(''); }
  }
  async function copy(text) {
    try { await copyText(text); notify('已复制到剪贴板'); }
    catch { notify('复制失败，请在导出窗口选择地址后手动复制', true); }
  }
  async function exportNode(node, protocol = 'http') {
    try {
      const { links } = await api('/export', 'POST', { protocol, ids: [node.id] });
      if (!links) throw new Error('节点已停用或来源失效，无法导出');
      await copy(links);
    } catch (error) { notify(error.message, true); }
  }
  async function testNodes(ids = []) {
    await run('test', async () => {
      const result = await api('/nodes/test', 'POST', { ids });
      await load();
      notify(`已完成 ${result.tested} 个节点的连接测试`);
    });
  }
  const context = { state, setState, core, mutate, notify, run, busy, setModal, copy, exportNode, refreshCore, navigate, testNodes };

  if (session?.setupRequired) return <Setup onSuccess={setSession} onInitialized={setSession} />;
  if (session && !session.authenticated) return <Login onSuccess={setSession} />;
  if (!state) return <div className="loading-page"><Logo /><div className="loading-label">{loadError ? <><TriangleAlert size={18} />{loadError}<Button onClick={() => location.reload()}>重新加载</Button></> : <><Loader2 size={19} className="spin" />正在连接控制台…</>}</div></div>;
  const pending = state.revision !== state.appliedRevision;
  const enabled = state.nodes.filter(n => n.enabled && n.available).length;

  return <div className={`app-shell ${collapsed ? 'sidebar-collapsed' : ''}`}>
    {sidebarOpen && <div className="sidebar-scrim" onClick={() => setSidebarOpen(false)} />}
    <aside className={`sidebar ${sidebarOpen ? 'is-open' : ''}`}>
      <a href="#overview" className="brand-link" onClick={e => { e.preventDefault(); navigate('overview'); }}><Logo /></a>
      <div className="nav-section-label">控制台</div>
      <nav aria-label="主导航">
        {routes.map(item => <button key={item.id} title={collapsed ? item.label : undefined} className={`nav-item ${route === item.id ? 'active' : ''}`} onClick={() => navigate(item.id)}>
          <item.icon size={19} strokeWidth={1.75} /><span>{item.label}</span>
          {item.id === 'nodes' && <span className="nav-count">{state.nodes.length}</span>}
          {route === item.id && <span className="nav-active-dot" />}
        </button>)}
      </nav>
      <div className="sidebar-bottom">
        <div className="core-card">
          <div className="core-card-title"><span className={`status-dot ${core.connected ? 'green' : 'gray'}`} /><strong>Mihomo 内核</strong><Badge tone={core.connected ? 'green' : 'neutral'}>{core.connected ? '已连接' : '待连接'}</Badge></div>
          <p>{core.connected ? `运行版本 ${core.version}` : '连接内核后开始代理转发'}</p>
          <button className="text-link" onClick={() => navigate('settings')}>管理内核连接 <ArrowRight size={13} /></button>
        </div>
        <div className="sidebar-utility">
          <button title="使用指南" onClick={() => setModal({ type: 'guide' })}><BookOpen size={18} /><span>使用指南</span></button>
          <button title={collapsed ? '展开侧边栏' : '收起侧边栏'} className="collapse-button" onClick={() => setCollapsed(!collapsed)}>{collapsed ? <PanelLeftOpen size={18} /> : <PanelLeftClose size={18} />}<span>收起侧边栏</span></button>
          {session?.passwordRequired && <button title="退出登录" onClick={() => run('logout', async () => { await api('/logout', 'POST', {}); setSession({ authenticated: false, passwordRequired: true }); })}><LogOut size={18} /><span>退出登录</span></button>}
        </div>
        <div className="sidebar-footer"><ShieldCheck size={13} /><span>PortPilot v0.1.2 <span className="footer-divider">·</span> 自托管</span></div>
      </div>
    </aside>
    <div className="workspace">
      <header className="mobile-topbar"><IconButton title="打开导航" onClick={() => setSidebarOpen(true)}><Menu size={22} /></IconButton><Logo /><IconButton title="使用指南" onClick={() => setModal({ type: 'guide' })}><CircleHelp size={20} /></IconButton></header>
      <main>
        <div className="page-heading">
          <div><h1>{pageInfo[route][0]}</h1><p>{pageInfo[route][1]}</p></div>
          <div className="heading-actions">
            <span className={`health-pill ${core.connected ? '' : 'offline'}`}><span className={`status-dot ${core.connected ? 'green' : 'gray'}`} />{core.connected ? '代理内核已连接' : '代理内核待连接'}</span>
            {route === 'nodes' && <Button onClick={() => setModal({ type: 'node' })}><Plus size={16} />添加节点</Button>}
            {['nodes', 'subscriptions'].includes(route) && <Button variant="primary" onClick={() => setModal({ type: 'subscription' })}><Plus size={17} />导入订阅</Button>}
          </div>
        </div>
        {loadError && <div className="connection-notice"><TriangleAlert size={17} /><span>{loadError}</span><button onClick={refreshAll}>重试</button></div>}
        {route === 'overview' && <Overview {...context} refreshedAt={refreshedAt} refreshAll={refreshAll} />}
        {route === 'nodes' && <Nodes {...context} initialFilter={nodeFilter} />}
        {route === 'subscriptions' && <Subscriptions {...context} />}
        {route === 'access' && <Access {...context} />}
        {route === 'settings' && <Settings {...context} />}
        {route === 'logs' && <Logs {...context} />}
        <footer className="page-footer"><span>PortPilot <span className="footer-divider">·</span> 代理服务，由你掌控</span><span>Powered by <a href="https://github.com/MetaCubeX/mihomo" target="_blank" rel="noreferrer">Mihomo</a></span></footer>
      </main>
      {pending && state.nodes.length > 0 && <div className="pending-bar"><div className="pending-icon"><FileCode2 size={20} /></div><div><strong>有未应用的配置更改</strong><span>应用后，节点映射与认证设置才会生效。</span></div><Button variant="primary" disabled={!!busy} onClick={() => setModal({ type: 'apply' })}>应用配置<ArrowRight size={16} /></Button></div>}
    </div>
    {toast && <div className={`toast ${toast.error ? 'error' : ''}`} role="status" key={toast.key}>{toast.error ? <TriangleAlert size={19} /> : <CheckCircle2 size={19} />}<span>{toast.message}</span><button aria-label="关闭提示" onClick={() => setToast(null)}><X size={16} /></button></div>}
    {modal?.type === 'subscription' && <SubscriptionModal {...context} subscription={modal.subscription} onClose={() => setModal(null)} />}
    {modal?.type === 'node' && <NodeModal {...context} node={modal.node} onClose={() => setModal(null)} />}
    {modal?.type === 'export' && <ExportModal {...context} ids={modal.ids || EMPTY_IDS} onClose={() => setModal(null)} />}
    {modal?.type === 'delete' && <ConfirmModal title={modal.title} text={modal.text} confirm="确认删除" danger onClose={() => setModal(null)} onConfirm={modal.action} />}
    {modal?.type === 'apply' && <ConfirmModal title="应用当前配置" text={`将 ${enabled} 个启用映射及当前认证设置发送到 Mihomo。内核会重新加载监听端口，现有连接可能受到影响。`} confirm="应用到 Mihomo" onClose={() => setModal(null)} onConfirm={async () => { await mutate('/core/apply', 'POST', {}, '配置已应用'); await refreshCore(); }} />}
    {modal?.type === 'guide' && <Guide onClose={() => setModal(null)} />}
    {modal?.type === 'config' && <ConfigModal onClose={() => setModal(null)} notify={notify} />}
  </div>;
}
