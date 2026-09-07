import { Button, Layout, Menu } from 'antd';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { isAdminRole, useAuth } from '../auth/AuthContext';

const { Header, Content, Sider } = Layout;

export default function AppLayout() {
  const nav = useNavigate();
  const loc = useLocation();
  const { user, signOut } = useAuth();
  const isAdmin = isAdminRole(user?.role);

  const selected = loc.pathname.startsWith('/nodes')
    ? 'nodes'
    : loc.pathname.startsWith('/subscriptions') || loc.pathname.startsWith('/subs')
      ? 'subscriptions'
      : loc.pathname.startsWith('/topology')
        ? 'topology'
        : loc.pathname.startsWith('/administration')
          ? 'administration'
          : 'dashboard';

  const items = [
    { key: 'dashboard', label: '总览' },
    ...(isAdmin ? [
      { key: 'nodes', label: '节点管理' },
      { key: 'topology', label: '拓扑编排' },
      { key: 'administration', label: '权限管理' },
    ] : []),
    { key: 'subscriptions', label: '我的订阅' },
  ];

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ color: '#fff', fontSize: 18, fontWeight: 600 }}>Coxpanel</div>
        <div>
          <span style={{ color: '#ddd', marginRight: 16 }}>
            {user?.username || '-'}{user?.role ? `（${user.role}）` : ''}
          </span>
          <Button size="small" onClick={() => { signOut(); nav('/login'); }}>退出</Button>
        </div>
      </Header>
      <Layout>
        <Sider width={200} theme="light">
          <Menu
            mode="inline"
            selectedKeys={[selected]}
            onClick={(event) => nav(event.key === 'dashboard' ? '/' : `/${event.key}`)}
            items={items}
          />
        </Sider>
        <Content style={{ padding: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
