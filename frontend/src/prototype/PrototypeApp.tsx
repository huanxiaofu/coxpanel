import { Alert, Button, Empty, Tag } from 'antd';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import AppShell, { navigation, PageHeader } from './AppShell';
import { Icon } from './Icon';
import { getServer, protocolLabels, serverAssets, statusLabels } from './model';
import { ThemeProvider } from './ThemeProvider';
import TopologyWorkspace from './TopologyWorkspace';
import { WorkspaceProvider, useWorkspace } from './WorkspaceProvider';
import './prototype.css';

function Overview() {
  const { state } = useWorkspace();
  return <div className="su-standard-page"><PageHeader title="总览" description="从服务器资源到代理服务，一处掌握工作区。" actions={<Link to="/prototype/topology"><Button type="primary" icon={<Icon name="topology" />}>进入拓扑编排</Button></Link>} />
    <Alert type="info" showIcon title="R1 合成工作区 · 以下统计仅来自本次原型会话，不是生产指标。" />
    <div className="su-stats-grid">{[
      ['服务器资源', serverAssets.length, '3 台在线 · 1 台离线（模拟）'],
      ['代理入口', state.proxies.filter(proxy => proxy.config).length, '同一服务器，多种独立服务'],
      ['已生效 · 模拟', state.proxies.filter(proxy => proxy.published).length, '不代表真实部署回执'],
      ['待应用草稿', state.proxies.filter(proxy => proxy.dirty).length, '不会进入真实订阅'],
    ].map(([label, value, detail]) => <section className="su-stat-card" key={label}><span>{label}</span><strong>{value}</strong><small>{detail}</small></section>)}</div>
    <section className="su-welcome-card"><span className="su-section-eyebrow">YOUR FIRST WORKSPACE</span><h2>一台服务器，就是起点。</h2><p>从 HK-zouter 创建一个 Reality 入口，不连线即本机直出。<br />需要链式转发时，再把入口连接到另一张内部入口卡。</p><Link to="/prototype/topology"><Button icon={<Icon name="arrow" />}>开始编排</Button></Link></section>
    <div className="su-overview-steps"><div><b>01</b><h3>选择资源</h3><p>full / lite、NAT、能力一目了然。</p></div><div><b>02</b><h3>配置入口</h3><p>基础、协议、安全、高级分区编辑。</p></div><div><b>03</b><h3>确认应用</h3><p>先有草稿，再确认模拟生效。</p></div></div>
  </div>;
}

function Servers() {
  const { state } = useWorkspace();
  return <div className="su-standard-page"><PageHeader title="服务器节点" description="服务器是资源，不是订阅中的代理。能力和在线状态均为合成演示。" actions={<Link to="/prototype/topology"><Button type="primary">从画布创建入口</Button></Link>} />
    <div className="su-server-grid">{serverAssets.map(server => <section className="su-server-detail" key={server.id}><div className="su-server-detail-title"><span className="su-region-icon">{server.country}</span><div><h2>{server.name}</h2><p>{server.region} · {server.address}</p></div></div><div><Tag color={server.profile === 'full' ? 'blue' : 'purple'}>{server.profile}</Tag>{server.network === 'nat' && <Tag color="orange">NAT</Tag>}<Tag color={server.online ? 'green' : undefined}>{server.online ? '在线 · 模拟' : '离线 · 模拟'}</Tag></div><dl><div><dt>协议能力</dt><dd>{server.protocols.map(protocol => protocolLabels[protocol]).join(' / ') || '未知，不放行'}</dd></div><div><dt>网络 / 内存</dt><dd>{server.udp ? 'TCP + UDP' : '仅 TCP'} / {server.memory}</dd></div><div><dt>入口数量</dt><dd>{state.proxies.filter(proxy => proxy.serverId === server.id).length} / {server.maxProxies || '未知'}</dd></div><div><dt>链路能力</dt><dd>{server.profile === 'lite' ? 'direct-only · 末端未准入' : server.chainTarget ? '内部入口可作为下一跳' : '未验证'}</dd></div></dl>{server.network === 'nat' && <Alert type="warning" title="合成 NAT 映射 · 10080 → 21080、10081 → 21081；TCP。未验证用户计量、租约或真实内存。" />}</section>)}</div>
  </div>;
}

function Proxies() {
  const { state } = useWorkspace();
  return <div className="su-standard-page"><PageHeader title="代理节点" description="每一个入口都有独立身份、端口与发布状态。以下仅为合成工作区视图。" actions={<Link to="/prototype/topology"><Button type="primary" icon={<Icon name="plus" />}>在画布创建</Button></Link>} />
    {!state.proxies.length ? <div className="su-placeholder-page"><Empty description="尚未创建代理入口"><Link to="/prototype/topology"><Button type="primary">前往拓扑编排</Button></Link></Empty></div> : <div className="su-proxy-table-wrap"><table className="su-proxy-table"><thead><tr><th>代理名称</th><th>所属服务器</th><th>协议 / 端口</th><th>出口（草稿）</th><th>状态</th><th>操作</th></tr></thead><tbody>{state.proxies.map(proxy => <tr key={proxy.id}><td><strong>{proxy.config?.name ?? '待配置入口'}</strong><small>{proxy.config?.exposure === 'internal' ? '内部入口' : proxy.config ? '订阅入口' : '占位草稿'}</small></td><td>{getServer(proxy.serverId).name}</td><td>{proxy.config ? `${protocolLabels[proxy.config.protocol]} / ${proxy.config.listenPort}` : '—'}</td><td>{state.links.some(link => link.source === proxy.id) ? `下一跳 → ${state.proxies.find(target => target.id === state.links.find(link => link.source === proxy.id)?.target)?.config?.name}` : '本机直出'}</td><td><span className={`su-status su-status-${proxy.status}`}><span />{statusLabels[proxy.status]}</span></td><td><Link to="/prototype/topology">在画布编辑</Link></td></tr>)}</tbody></table></div>}
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
  return <div className="su-standard-page"><PageHeader title={page.label} description={page.description} /><Alert type="info" showIcon title="R1 信息架构占位 · 本页未接入真实业务，也不提供伪造成功的管理操作。" /><div className="su-placeholder-page"><span className="su-placeholder-page-icon"><Icon name={page.icon} size={32} /></span><Tag>后续阶段</Tag><h2>{page.label}，预留在这里。</h2><p>产品方案已定案；本阶段先确认入口创建与拓扑编排的手感。</p><div className="su-feature-preview">{placeholderContent[page.path]?.map(([title, text]) => <section key={title}><h3>{title}</h3><p>{text}</p></section>)}</div><Button onClick={() => navigate('/prototype/topology')} icon={<Icon name="arrow" />}>回到可操作原型</Button></div></div>;
}

export default function PrototypeApp() {
  return <BrowserRouter><ThemeProvider><WorkspaceProvider><AppShell><Routes>
    <Route path="/prototype/topology" element={<TopologyWorkspace />} />
    <Route path="/prototype/overview" element={<Overview />} />
    <Route path="/prototype/servers" element={<Servers />} />
    <Route path="/prototype/proxies" element={<Proxies />} />
    <Route path="/prototype/users" element={<Placeholder />} />
    <Route path="/prototype/subscriptions" element={<Placeholder />} />
    <Route path="/prototype/traffic" element={<Placeholder />} />
    <Route path="/prototype/settings" element={<Placeholder />} />
    <Route path="*" element={<Navigate to="/prototype/topology" replace />} />
  </Routes></AppShell></WorkspaceProvider></ThemeProvider></BrowserRouter>;
}
