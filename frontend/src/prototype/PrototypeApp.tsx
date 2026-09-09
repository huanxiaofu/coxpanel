import { Alert, Button, Empty, Tag } from 'antd';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import AppShell, { navigation, PageHeader } from './AppShell';
import { Icon } from './Icon';
import { chainSummary, egressSummary, getInbound, protocolLabels, statusLabels } from './model';
import { ThemeProvider } from './ThemeProvider';
import TopologyWorkspace from './TopologyWorkspace';
import ServersPage from './ServersPage';
import { WorkspaceProvider, useWorkspace } from './WorkspaceProvider';
import './prototype.css';

function Overview() {
  const { state } = useWorkspace();
  return <div className="su-standard-page"><PageHeader title="总览" description="从服务器资源到完整代理链，一处掌握工作区。" actions={<Link to="/prototype/topology"><Button type="primary" icon={<Icon name="topology" />}>进入链编辑器</Button></Link>} />
    <Alert type="info" showIcon title="R1 合成工作区 · 以下统计仅来自本次原型会话，不是生产指标。" />
    <div className="su-stats-grid">{[
      ['服务器资源', state.servers.length, `${state.servers.filter(server => server.status === 'online').length} 台在线 · ${state.servers.filter(server => server.status === 'offline').length} 台离线 · ${state.servers.filter(server => server.status === 'maintenance').length} 台维护（模拟）`],
      ['独立入口资源', state.inbounds.length, '同一端口可被多个节点引用'],
      ['已生效 · 模拟', state.proxies.filter(proxy => proxy.published).length, '不代表真实部署回执'],
      ['待应用草稿', state.proxies.filter(proxy => proxy.dirty).length, '不会进入真实订阅'],
    ].map(([label, value, detail]) => <section className="su-stat-card" key={label}><span>{label}</span><strong>{value}</strong><small>{detail}</small></section>)}</div>
    <section className="su-welcome-card"><span className="su-section-eyebrow">YOUR FIRST WORKSPACE</span><h2>代理节点就是出口，一条链一个节点。</h2><p>入口 → 中转（可选）→ 出口（代理节点），横向排列，不分叉。<br />单台服务器可兼入口与出口；每跳可复用已有入站。</p><Link to="/prototype/topology"><Button icon={<Icon name="arrow" />}>编辑代理链</Button></Link></section>
    <div className="su-overview-steps"><div><b>01</b><h3>选择入口</h3><p>第一跳提供客户端订阅入口，仅使用合成资源。</p></div><div><b>02</b><h3>按需追加中转</h3><p>每跳选择服务器并复用或新建入站；顺序即链路。</p></div><div><b>03</b><h3>确认唯一出站</h3><p>最后一跳本机直出，整条链作为一个节点模拟生效。</p></div></div>
  </div>;
}

function Proxies() {
  const { state } = useWorkspace();
  return <div className="su-standard-page"><PageHeader title="代理节点（出口）" description="代理节点 = 出口；客户端节点名即链的出口名。一条链一个条目，首跳提供订阅连接，末跳决定出口服务器。仅合成预览，不生成真实订阅。" actions={<Link to="/prototype/topology"><Button type="primary" icon={<Icon name="plus" />}>前往链编辑器</Button></Link>} />
    {!state.proxies.length ? <div className="su-placeholder-page"><Empty description="创建代理节点就是创建出口：入口 → 中转（可选）→ 出口（代理节点）"><Link to="/prototype/topology"><Button type="primary">创建第一条代理链</Button></Link></Empty></div> : <div className="su-proxy-table-wrap"><table className="su-proxy-table"><thead><tr><th>出口名称（代理节点）</th><th>出口服务器 / 协议（草稿）</th><th>线性链路（草稿）</th><th>订阅入口协议 / 端口</th><th>状态</th><th>操作</th></tr></thead><tbody>{state.proxies.map(proxy => {
      const inbound = proxy.chain[0] && getInbound(state.inbounds, proxy.chain[0]);
      const lastHop = proxy.chain.at(-1);
      const exitInbound = lastHop && getInbound(state.inbounds, lastHop);
      const exitServer = state.servers.find(server => server.id === lastHop?.serverId);
      return <tr key={proxy.id}><td><Tag color="green">出口 · 代理节点</Tag><strong className="su-proxy-egress-name"><Icon name="proxy" size={15} /> {proxy.name || '未命名出口'}</strong><small>{proxy.chain.length} 跳 · {Math.max(0, proxy.chain.length - 2)} 个中转{proxy.chain.length === 1 ? ' · 入口与出口同机' : ' · 末跳为出口'}</small></td><td className="su-proxy-egress-cell"><strong>{exitServer?.name ?? '待选择出口服务器'}</strong><small>{exitInbound ? `${protocolLabels[exitInbound.config.protocol]} / :${exitInbound.config.listenPort} · ${exitInbound.config.name}` : '待选择出口入站'}</small><small>{egressSummary(proxy.egress)}</small></td><td><strong className="su-chain-table-summary">{chainSummary(proxy, state.servers)}</strong>{proxy.published && <small>已发布 · 模拟：{proxy.published.name} · {chainSummary(proxy.published, state.servers)}</small>}</td><td>{inbound ? `${protocolLabels[inbound.config.protocol]} / ${inbound.config.listenPort}` : '待配置订阅入口'}</td><td><span className={`su-status su-status-${proxy.status}`}><span />{statusLabels[proxy.status]}</span></td><td><Link to={`/prototype/topology?chain=${encodeURIComponent(proxy.id)}`}>编辑此链</Link></td></tr>;
    })}</tbody></table></div>}
  </div>;
}

const placeholderContent: Record<string, Array<[string, string]>> = {
  users: [['用户生命周期', '到期、限额、流量重置和批量管理。'], ['内部 / 外部分组', '资源池与客户授权分开，同机代理不自动共享授权。'], ['独立运行授权', '订阅可见性与用户凭据、计量、租约共同验证。']],
  subscriptions: [['订阅模板', 'Mihomo、Sing-box、Base64、Xray JSON 按适配矩阵交付。'], ['客户覆写工作台', '只修改已有授权范围的客户端参数，不改变服务器。'], ['每客户最多 10 个方案', '草稿和暂停都占名额，发布生成独立链接。']],
  traffic: [['用户计量', '保留历史与周期，未知遥测不显示为零流量。'], ['通知与告警', '部署失败、离线与流量异常有可解释的状态。'], ['周期与重置', '月度、间隔、单次计划与迟到流量按方案验证。']],
  settings: [['主题偏好', '顶栏切换浅色 / 深色 / 跟随系统，主题保存在本机。'], ['原型数据隔离', '配置仅在浏览器内存；刷新清空，不读取真实登录信息。'], ['后续实施', 'DDNS、运行标识迁移与生产部署均未开启。']],
};

function Placeholder() {
  const location = useLocation();
  const navigate = useNavigate();
  const page = navigation.find(item => location.pathname === `/prototype/${item.path}`) ?? navigation[7];
  return <div className="su-standard-page"><PageHeader title={page.label} description={page.description} /><Alert type="info" showIcon title="R1 信息架构占位 · 本页未接入真实业务，也不提供伪造成功的管理操作。" /><div className="su-placeholder-page"><span className="su-placeholder-page-icon"><Icon name={page.icon} size={32} /></span><Tag>后续阶段</Tag><h2>{page.label}，预留在这里。</h2><p>本阶段先确认线性链编辑与入站复用的手感。</p><div className="su-feature-preview">{placeholderContent[page.path]?.map(([title, text]) => <section key={title}><h3>{title}</h3><p>{text}</p></section>)}</div><Button onClick={() => navigate('/prototype/topology')} icon={<Icon name="arrow" />}>回到可操作原型</Button></div></div>;
}

export default function PrototypeApp() {
  return <BrowserRouter><ThemeProvider><WorkspaceProvider><AppShell><Routes>
    <Route path="/prototype/topology" element={<TopologyWorkspace />} />
    <Route path="/prototype/overview" element={<Overview />} />
    <Route path="/prototype/servers" element={<ServersPage />} />
    <Route path="/prototype/proxies" element={<Proxies />} />
    <Route path="/prototype/users" element={<Placeholder />} />
    <Route path="/prototype/subscriptions" element={<Placeholder />} />
    <Route path="/prototype/traffic" element={<Placeholder />} />
    <Route path="/prototype/settings" element={<Placeholder />} />
    <Route path="*" element={<Navigate to="/prototype/topology" replace />} />
  </Routes></AppShell></WorkspaceProvider></ThemeProvider></BrowserRouter>;
}
