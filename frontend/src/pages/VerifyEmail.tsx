import { useState } from 'react';
import { Alert, Button, Card, Space, Typography } from 'antd';
import { Link } from 'react-router-dom';
import { api } from '../api';

export default function VerifyEmail() {
  const [parameters] = useState(() => new URLSearchParams(window.location.hash.slice(1)));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [status, setStatus] = useState('');
  const confirm = async () => {
    setBusy(true); setError('');
    try {
      const result = await api.request('/api/auth/email-verifications/confirm', 'POST', { challengeId: parameters.get('challengeId'), token: parameters.get('token') });
      if (result.registrationTicket) sessionStorage.setItem('coxpanel_registration_ticket', result.registrationTicket);
      window.history.replaceState(null, '', '/verify-email');
      setStatus(result.status);
    } catch (reason) { setError(reason instanceof Error ? reason.message : '验证失败'); }
    finally { setBusy(false); }
  };
  return <Card title="确认邮箱验证" style={{ maxWidth: 560, margin: '60px auto' }}><Space orientation="vertical"><Typography.Paragraph>仅点击确认后消费一次性验证链接。验证链接不是登录凭证，不会自动登录。</Typography.Paragraph>{error && <Alert type="error" title={error} />}{status ? <><Alert type="success" title={status === 'existing_email_verified' ? '本人邮箱已验证' : '邮箱已验证，请在10分钟内完成注册'} /><Link to={status === 'existing_email_verified' ? '/alerts' : '/register'}>继续</Link></> : <Button type="primary" loading={busy} disabled={!parameters.get('challengeId') || !parameters.get('token')} onClick={() => void confirm()}>确认验证此邮箱</Button>}</Space></Card>;
}
