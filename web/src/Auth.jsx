import React, { useState } from 'react';
import { ArrowRight, ShieldCheck } from 'lucide-react';
import { api, Button, Field, Logo } from './ui.jsx';

function AuthFrame({ setup = false, children }) {
  return <div className="login-page">
    <div className="login-decoration"><div /><div /><div /></div>
    <div className={`login-card ${setup ? 'setup-card' : ''}`}>
      <Logo />
      <div className="eyebrow">{setup ? 'FIRST-TIME SETUP' : 'WELCOME BACK'}</div>
      <h1>{setup ? '设置管理密码' : '欢迎回到控制台'}</h1>
      <p>{setup ? '为你的控制台创建密码，完成后即可开始管理节点。' : '使用管理密码，连接你的代理工作空间。'}</p>
      {children}
      <span className="login-foot"><ShieldCheck size={14} />{setup ? '管理密码保存在本机，设置后用于登录控制台。' : '轻量，自托管，由你掌控。'}</span>
    </div>
  </div>;
}

export function Login({ onSuccess }) {
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  async function submit(event) {
    event.preventDefault(); setLoading(true); setError('');
    try { onSuccess(await api('/login', 'POST', { password })); }
    catch (error) { setError(error.message); }
    finally { setLoading(false); }
  }
  return <AuthFrame><form onSubmit={submit}>
    <Field label="管理密码"><input type="password" value={password} onChange={event => setPassword(event.target.value)} required maxLength={1024} autoComplete="current-password" autoFocus /></Field>
    {error && <div className="inline-error" role="alert">{error}</div>}
    <Button variant="primary" type="submit" loading={loading}>进入控制台<ArrowRight size={16} /></Button>
  </form></AuthFrame>;
}

export function Setup({ onSuccess, onInitialized }) {
  const [form, setForm] = useState({ password: '', confirmPassword: '' });
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const change = (name, value) => setForm(previous => ({ ...previous, [name]: value }));
  async function submit(event) {
    event.preventDefault(); setError('');
    if (form.password !== form.confirmPassword) { setError('两次输入的密码不一致'); return; }
    setLoading(true);
    try { onSuccess(await api('/setup', 'POST', form)); }
    catch (error) {
      if (error.status === 409) {
        try { onInitialized(await api('/session')); }
        catch (sessionError) { setError(sessionError.message); }
      } else setError(error.message);
    } finally { setLoading(false); }
  }
  return <AuthFrame setup>
    <form onSubmit={submit}>
      <div className="auth-fields">
        <Field label="管理密码" hint="至少 12 个字符，用于登录面板，与代理账号密码独立。"><input type="password" value={form.password} onChange={event => change('password', event.target.value)} required minLength={12} maxLength={256} autoComplete="new-password" autoFocus /></Field>
        <Field label="确认管理密码"><input type="password" value={form.confirmPassword} onChange={event => change('confirmPassword', event.target.value)} required minLength={12} maxLength={256} autoComplete="new-password" /></Field>
      </div>
      {error && <div className="inline-error" role="alert">{error}</div>}
      <Button variant="primary" type="submit" loading={loading}>保存密码并进入<ArrowRight size={16} /></Button>
    </form>
  </AuthFrame>;
}
