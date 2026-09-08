import { Form, Input, Button, Card, Alert, message } from 'antd';
import { useNavigate, Link } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth/AuthContext';
import { useP2Data } from '../components/P2Data';

export default function Register() {
  const nav = useNavigate();
  const { establishSession } = useAuth();
	const [form] = Form.useForm();
	const status = useP2Data<{emailVerificationRequired: boolean}>('/api/auth/email-verifications/status');
	const verify = async () => { try { const values = await form.validateFields(['email', 'inviteCode']); await api.request('/api/auth/email-verifications', 'POST', values); message.success('若邮箱和邀请码有效，验证邮件已入队，请在邮件页面点击确认'); } catch (error) { message.error(error instanceof Error ? error.message : '请检查邮箱与邀请码'); } };

  const onFinish = async (v: any) => {
    try {
      const res = await api.register(v.username, v.password, v.email, v.inviteCode, sessionStorage.getItem('coxpanel_registration_ticket') || undefined);
			sessionStorage.removeItem('coxpanel_registration_ticket');
      establishSession(res.token);
      message.success('注册成功');
      nav('/');
    } catch (e: any) {
      message.error(e.message);
    }
  };

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh', background: '#f0f2f5' }}>
      <Card title="注册（邀请制）" style={{ width: 400 }}>
				{status.data?.emailVerificationRequired && <Alert type="info" title="先验证邮箱，再使用相同邮箱和邀请码完成注册" />}
        <Form form={form} onFinish={onFinish} layout="vertical">
          <Form.Item name="inviteCode" label="邀请码" rules={[{ required: true, message: '必须填写邀请码' }]}>
            <Input placeholder="CXP-xxxxxx" />
          </Form.Item>
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="password" label="密码（至少 8 位）" rules={[{ required: true, min: 8 }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="email" label="邮箱" rules={[{ required: true, type: 'email' }]}>
            <Input />
          </Form.Item>
          <Button type="primary" htmlType="submit" block>注册</Button>
					<Button onClick={() => void verify()} block style={{marginTop: 8}}>发送注册邮箱验证邮件</Button>
          <div style={{ marginTop: 12, textAlign: 'center' }}>
            <Link to="/login">已有账号？登录</Link>
          </div>
        </Form>
      </Card>
    </div>
  );
}
