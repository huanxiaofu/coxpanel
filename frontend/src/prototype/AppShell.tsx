import { useState } from 'react';
import type { ReactNode } from 'react';
import { Button, Drawer, Modal, Select, Tag } from 'antd';
import { NavLink, useLocation } from 'react-router-dom';
import { Icon } from './Icon';
import type { IconName } from './Icon';
import { useTheme } from './ThemeProvider';

export const navigation: Array<{ path: string; label: string; icon: IconName; description: string }> = [
  { path: 'overview', label: '总览', icon: 'overview', description: '从服务器资源到代理服务，一处掌握工作区。' },
  { path: 'servers', label: '服务器节点', icon: 'server', description: '服务器是资源。一台服务器可以承载多个代理入口。' },
  { path: 'proxies', label: '代理节点', icon: 'proxy', description: '代理是服务。每个入口拥有独立配置、端口与发布状态。' },
  { path: 'topology', label: '拓扑编排', icon: 'topology', description: '拖入服务器创建入口，用连线定义流量的下一跳。' },
  { path: 'users', label: '用户与授权', icon: 'users', description: '管理用户、内部资源组与外部客户分组。' },
  { path: 'subscriptions', label: '订阅与模板', icon: 'subscription', description: '客户端模板与服务器运行配置分离。' },
  { path: 'traffic', label: '流量与通知', icon: 'traffic', description: '查看用户计量、流量周期与运行通知。' },
  { path: 'settings', label: '设置', icon: 'settings', description: '管理工作区偏好与产品设置。' },
];

function Sidebar({ onNavigate }: { onNavigate: () => void }) {
  return <div className="su-sidebar-inner">
    <NavLink className="su-brand" to="/prototype/topology" onClick={onNavigate} aria-label="sing-ui 拓扑工作区"><span className="su-brand-mark"><Icon name="topology" size={23} /></span><span>sing-ui<small>NETWORK CONTROL</small></span></NavLink>
    <div className="su-nav-label">工作空间 <span>演示</span></div>
    <nav aria-label="主导航">{navigation.map((item, index) => <NavLink key={item.path} to={`/prototype/${item.path}`} onClick={onNavigate} className={({ isActive }) => `su-nav-item ${isActive ? 'is-active' : ''} ${index === 4 ? 'su-nav-divider' : ''}`}><Icon name={item.icon} /><span>{item.label}</span>{item.path === 'topology' && <small>R1</small>}</NavLink>)}</nav>
    <div className="su-sidebar-note"><span className="su-live-dot" /><strong>安全的演示工作区</strong><p>仅使用合成数据<br />未连接真实 Agent 或业务 API</p></div>
    <div className="su-sidebar-footer"><span className="su-avatar">演</span><span>演示管理员<small>本地交互原型 · 无需登录</small></span></div>
  </div>;
}

function Topbar({ title, onMenu }: { title: string; onMenu: () => void }) {
  const { preference, setPreference } = useTheme();
  const [help, setHelp] = useState(false);
  return <header className="su-topbar">
    <div className="su-topbar-location"><Button className="su-mobile-menu" aria-label="打开导航" icon={<Icon name="menu" />} onClick={onMenu} /><span className="su-breadcrumb-root">工作空间</span><Icon name="chevron" size={12} /><strong>{title}</strong></div>
    <div className="su-topbar-actions"><Tag color="blue">R1 交互原型</Tag><Select aria-label="主题模式" value={preference} onChange={setPreference} popupMatchSelectWidth={false} options={[{ value: 'system', label: '跟随系统' }, { value: 'light', label: '浅色主题' }, { value: 'dark', label: '深色主题' }]} /><Button type="text" aria-label="原型体验指南" icon={<Icon name="help" />} onClick={() => setHelp(true)} /></div>
    <Modal title="欢迎体验 sing-ui" open={help} onCancel={() => setHelp(false)} footer={<Button type="primary" onClick={() => setHelp(false)}>开始体验</Button>}>
      <ol className="su-guide"><li>把 HK-zouter 拖到画布，点击「新建入口」，或「选择已有入口」。</li><li>选择 VLESS / Reality，填写 54321、SNI / dest，在安全区生成合成材料引用，然后应用。</li><li>本机直出即可完成单服务器闭环。再次拖入 HK，选择同一个 54321 入口创建第二个节点。</li><li>在 SG-edge 创建 SS 入口；HK 第二个节点「配置出站」选择下一跳引用它，也可拖线到它。</li><li>直出节点保留原出站；共享入口不重复占端口。应用更改仅推进模拟发布。</li></ol><p>所有地址、能力、证书和成功状态都是合成演示，不具备真实连接能力。刷新清空工作区。NAT / 轻量 agent 后续再议；停在 R1 等你确认，不进入 R2。</p>
    </Modal>
  </header>;
}

export function PageHeader({ title, description, actions }: { title: string; description: string; actions?: ReactNode }) {
  return <section className="su-page-header"><div><h1>{title}</h1><p>{description}</p></div>{actions && <div className="su-page-actions">{actions}</div>}</section>;
}

export default function AppShell({ children }: { children: ReactNode }) {
  const [mobileMenu, setMobileMenu] = useState(false);
  const location = useLocation();
  const current = navigation.find(item => location.pathname === `/prototype/${item.path}`) ?? navigation[3];
  return <div className="su-app-shell">
    <a href="#workspace-content" className="su-skip-link">跳转到工作区</a>
    <aside className="su-sidebar"><Sidebar onNavigate={() => setMobileMenu(false)} /></aside>
    <Drawer title="sing-ui 导航" placement="left" size={260} open={mobileMenu} onClose={() => setMobileMenu(false)} styles={{ body: { padding: 0 } }}><Sidebar onNavigate={() => setMobileMenu(false)} /></Drawer>
    <div className="su-main-shell"><Topbar title={current.label} onMenu={() => setMobileMenu(true)} /><main id="workspace-content" className="su-main" tabIndex={-1}>{children}</main></div>
  </div>;
}
