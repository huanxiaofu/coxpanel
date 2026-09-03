import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import Login from './pages/Login';
import Register from './pages/Register';
import Dashboard from './pages/Dashboard';
import Nodes from './pages/Nodes';
import Subscriptions from './pages/Subscriptions';
import AppLayout from './components/AppLayout';

function RequireAuth({ children }: { children: React.ReactNode }) {
  const t = localStorage.getItem('coxpanel_token');
  if (!t) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

export default function App() {
  return (
    <ConfigProvider locale={zhCN}>
      <BrowserRouter>
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
            <Route path="nodes" element={<Nodes />} />
            <Route path="subscriptions" element={<Subscriptions />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ConfigProvider>
  );
}
