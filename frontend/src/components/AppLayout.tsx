import { Layout, Menu, Button } from 'antd';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';

const { Header, Content, Sider } = Layout;

export default function AppLayout() {
  const nav = useNavigate();
  const loc = useLocation();
  const user = JSON.parse(localStorage.getItem('coxpanel_user') || '{}');

  const logout = () => {
    localStorage.removeItem('coxpanel_token');
    localStorage.removeItem('coxpanel_user');
    nav('/login');
  };

  const selected = loc.pathname === '/nodes' ? 'nodes' : loc.pathname === '/subscriptions' ? 'subs' : 'dashboard';

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ color: '#fff', fontSize: 18, fontWeight: 600 }}>Coxpanel</div>
        <div>
          <span style={{ color: '#ddd', marginRight: 16 }}>{user.username}</span>
          <Button size="small" onClick={logout}>退出</Button>
        </div>
      </Header>
      <Layout>
        <Sider width={200} theme="light">
          <Menu
            mode="inline"
            selectedKeys={[selected]}
            onClick={(e) => nav(e.key === 'dashboard' ? '/' : `/${e.key}`)}
            items={[
              { key: 'dashboard', label: '总览' },
              { key: 'nodes', label: '节点管理' },
              { key: 'subs', label: '我的订阅' },
            ]}
          />
        </Sider>
        <Content style={{ padding: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
