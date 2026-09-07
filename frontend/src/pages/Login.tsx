import { Form, Input, Button, Card, message } from 'antd';
import { useNavigate, Link } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth/AuthContext';

export default function Login() {
  const nav = useNavigate();
  const { establishSession } = useAuth();

  const onFinish = async (v: { username: string; password: string }) => {
    try {
      const res = await api.login(v.username, v.password);
      establishSession(res.token);
      message.success('登录成功');
      nav('/');
    } catch (e: any) {
      message.error(e.message);
    }
  };

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh', background: '#f0f2f5' }}>
      <Card title="Coxpanel 登录" style={{ width: 380 }}>
        <Form onFinish={onFinish} layout="vertical">
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input.Password />
          </Form.Item>
          <Button type="primary" htmlType="submit" block>登录</Button>
          <div style={{ marginTop: 12, textAlign: 'center' }}>
            <Link to="/register">没有账号？凭邀请码注册</Link>
          </div>
        </Form>
      </Card>
    </div>
  );
}
