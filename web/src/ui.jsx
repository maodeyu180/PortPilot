import React, { useEffect, useRef } from 'react';
import { Activity, ArrowDown, ArrowDownToLine, ArrowRight, ArrowUpRight, BookOpen, Check, CheckCircle2, ChevronDown, ChevronLeft, ChevronRight, Circle, CircleHelp, Clipboard, Clock3, Code2, Copy, Download, Ellipsis, ExternalLink, FileCode2, FileText, FolderSync, Globe2, KeyRound, Layers3, LayoutGrid, Link2, ListFilter, Loader2, LogOut, Menu, Network, Plus, Power, Radio, RefreshCw, Search, Server, Settings2, ShieldCheck, SlidersHorizontal, Sparkles, SquareTerminal, Trash2, TriangleAlert, Upload, Waypoints, X, Zap } from 'lucide-react';
async function api(path, method = 'GET', body) {
  const response = await fetch(`/api${path}`, { method, headers: body !== undefined ? { 'Content-Type': 'application/json' } : {}, body: body !== undefined ? JSON.stringify(body) : undefined });
  const data = await response.json();
  if (!response.ok) { if (response.status === 401 && !['/login', '/setup'].includes(path)) window.dispatchEvent(new Event('session-expired')); const error = new Error(data.error || '请求失败，请重试'); error.status = response.status; throw error; }
  return data;
}
const formatTime = value => value ? new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }) : '尚未更新';
const bytes = n => n ? `${(n / 1048576).toFixed(1)} MB` : '—';
function country(name) {
  const map = [[/香港|🇭🇰|Hong.?Kong|\bHK\b/i, '🇭🇰', '香港'], [/日本|🇯🇵|Japan|\bJP\b|东京|大阪/i, '🇯🇵', '日本'], [/新加坡|狮城|🇸🇬|Singapore|\bSG\b/i, '🇸🇬', '新加坡'], [/美国|🇺🇸|United.?States|\bUS\b|洛杉矶|硅谷/i, '🇺🇸', '美国'], [/台湾|台灣|🇹🇼|Taiwan|\bTW\b/i, '🇹🇼', '台湾'], [/韩国|🇰🇷|Korea|\bKR\b/i, '🇰🇷', '韩国'], [/德国|🇩🇪|Germany/i, '🇩🇪', '德国'], [/英国|🇬🇧|Britain|\bUK\b/i, '🇬🇧', '英国'], [/加拿大|🇨🇦|Canada/i, '🇨🇦', '加拿大']];
  return map.find(([re]) => re.test(name))?.slice(1) || ['🌐', '其他地区'];
}
function download(content, name, type = 'text/plain') { const url = URL.createObjectURL(new Blob([content], { type })); const a = document.createElement('a'); a.href = url; a.download = name; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000); }
function Logo({ small = false }) { return <div className={`logo ${small ? 'small' : ''}`}><div className="logo-mark"><Waypoints size={24} strokeWidth={2.4} /></div>{!small && <span>Port<span className="logo-blue">Pilot</span><small>PROXY CONTROL</small></span>}</div>; }
function Button({ type = 'button', children, variant = '', loading = false, disabled = false, className = '', ...props }) { return <button type={type} className={`button ${variant} ${className}`} disabled={loading || disabled} {...props}>{loading && <Loader2 size={15} className="spin" />}{children}</button>; }
function IconButton({ title, children, ...props }) { return <button className="icon-button" title={title} aria-label={title} {...props}>{children}</button>; }
function Badge({ children, tone = '' }) { return <span className={`badge ${tone}`}>{children}</span>; }
function Switch({ checked, onChange, label, disabled = false }) { return <button type="button" role="switch" aria-checked={checked} aria-label={label} className={`switch ${checked ? 'on' : ''}`} disabled={disabled} onClick={() => onChange(!checked)}><span /></button>; }
function Field({ label, hint, children, wide = false }) { return <label className={`field ${wide ? 'wide' : ''}`}><span className="field-label">{label}</span>{children}{hint && <span className="field-hint">{hint}</span>}</label>; }
function Empty({ title = '还没有节点', text = '导入 Clash / Mihomo 订阅，为每个节点分配一个固定入口。', action }) { return <div className="empty"><div className="empty-icon"><Network size={30} /></div><h3>{title}</h3><p>{text}</p>{action}</div>; }

function Modal({ title, subtitle, children, onClose, wide = false, footer }) {
  const ref = useRef(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    const previous = document.activeElement;
    const focusables = () => [...ref.current.querySelectorAll('button:not([disabled]), input, textarea, select, a[href]')];
    (ref.current.querySelector('input:not([type=checkbox]), textarea, select') || focusables()[0])?.focus();
    const key = e => { if (e.key === 'Escape') closeRef.current(); if (e.key === 'Tab') { const elements = focusables(); const first = elements[0]; const last = elements.at(-1); if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); } else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); } } };
    document.addEventListener('keydown', key); const oldOverflow = document.body.style.overflow; document.body.style.overflow = 'hidden';
    return () => { document.removeEventListener('keydown', key); document.body.style.overflow = oldOverflow; previous?.focus(); };
  }, []);
  return <div className="modal-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose(); }}><section className={`modal ${wide ? 'modal-wide' : ''}`} role="dialog" aria-modal="true" aria-labelledby="modal-title" ref={ref}><div className="modal-header"><div><h2 id="modal-title">{title}</h2>{subtitle && <p>{subtitle}</p>}</div><IconButton title="关闭" onClick={onClose}><X size={19} /></IconButton></div><div className="modal-body">{children}</div>{footer && <div className="modal-footer">{footer}</div>}</section></div>;
}

export async function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    try { await navigator.clipboard.writeText(text); return; }
    catch { /* Fall through to the local clipboard fallback. */ }
  }
  const previous = document.activeElement;
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.cssText = 'position:fixed;left:-9999px;top:0;opacity:0';
  document.body.appendChild(textarea);
  textarea.select();
  try { if (!document.execCommand('copy')) throw new Error('copy unavailable'); }
  finally { textarea.remove(); previous?.focus(); }
}
export { api, formatTime, bytes, country, download, Logo, Button, IconButton, Badge, Switch, Field, Empty, Modal };
