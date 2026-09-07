import { Button, ConfigProvider, Result, Spin } from 'antd';
import { BrowserRouter, Navigate, Route, Routes, useNavigate } from 'react-router-dom';
import zhCN from 'antd/locale/zh_CN';
import { AuthProvider, isAdminRole, useAuth } from './auth/AuthContext';
import Login from './pages/Login';
import Register from './pages/Register';
import Dashboard from './pages/Dashboard';
import Nodes from './pages/Nodes';
import Subscriptions from './pages/Subscriptions';
import Topology from './pages/Topology';
import Administration from './pages/Administration';
import AppLayout from './components/AppLayout';

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { user, loading, error, signOut } = useAuth();
  if (loading) return <Spin fullscreen tip="读取用户权限…" />;
  if (!user) {
    if (error) {
      return (
        <Result
          status="error"
          title="无法读取用户权限"
          subTitle={error}
          extra={<Button onClick={signOut}>返回登录</Button>}
        />
      );
    }
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}

function RequireAdmin({ children }: { children: React.ReactNode }) {
  const { user } = useAuth();
  const nav = useNavigate();
  if (!isAdminRole(user?.role)) {
    return (
      <Result
        status="403"
        title="无权访问"
        subTitle="此页面仅对管理员开放。"
        extra={<Button onClick={() => nav('/')}>返回总览</Button>}
      />
    );
  }
  return <>{children}</>;
}

export default function App() {
  return (
    <ConfigProvider locale={zhCN}>
      <BrowserRouter>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/register" element={<Register />} />
            <Route
              path="/"
              element={
                <RequireAuth>
                  <AppLayout />
                </RequireAuth>
              }
            >
              <Route index element={<Dashboard />} />
              <Route path="nodes" element={<RequireAdmin><Nodes /></RequireAdmin>} />
              <Route path="subscriptions" element={<Subscriptions />} />
              <Route path="topology" element={<RequireAdmin><Topology /></RequireAdmin>} />
              <Route path="administration" element={<RequireAdmin><Administration /></RequireAdmin>} />
              <Route path="subs" element={<Navigate to="/subscriptions" replace />} />
            </Route>
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </AuthProvider>
      </BrowserRouter>
    </ConfigProvider>
  );
}
