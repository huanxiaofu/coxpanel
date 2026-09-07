import { Form, Input, Button, Card, message } from 'antd';
import { useNavigate, Link } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth/AuthContext';

export default function Register() {
  const nav = useNavigate();
  const { establishSession } = useAuth();

  const onFinish = async (v: any) => {
    try {
      const res = await api.register(v.username, v.password, v.email, v.inviteCode);
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
        <Form onFinish={onFinish} layout="vertical">
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
          <div style={{ marginTop: 12, textAlign: 'center' }}>
            <Link to="/login">已有账号？登录</Link>
          </div>
        </Form>
      </Card>
    </div>
  );
}
