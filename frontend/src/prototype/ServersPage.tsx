import { useEffect, useState } from 'react';
import { Alert, App, Button, Drawer, Empty, Form, Input, InputNumber, Popconfirm, Select, Tabs, Tag, Tooltip } from 'antd';
import { Link } from 'react-router-dom';
import { PageHeader } from './AppShell';
import { Icon } from './Icon';
import { ServerResources, ServerStatusBadge, ServerTags, ServerTrafficSummary } from './ServerPresentation';
import { filterServers, formatBytes, inboundReferences, nextTrafficReset, protocolLabels, serverStatusLabels, trafficCycleLabel, validateServerTraffic } from './model';
import type { InboundResource, ProxyDraft, ServerAsset, ServerFilters, ServerTraffic } from './model';
import { useWorkspace } from './WorkspaceProvider';

const unitBytes = { GB: 1024 ** 3, TB: 1024 ** 4 };
type TrafficUnit = keyof typeof unitBytes;
type EditorTab = 'details' | 'traffic' | 'tags';
const defaultFilters: ServerFilters = { query: '', status: 'all', tag: '', region: '', sort: 'name' };
const weekdays = ['周一', '周二', '周三', '周四', '周五', '周六', '周日'];

function utcDate(value: string | Date) {
  const date = new Date(value);
  return Number.isFinite(date.getTime()) ? `${date.toISOString().slice(0, 19).replace('T', ' ')} UTC` : '未上报';
}

function normalizeTags(tags: string[]) {
  return [...new Set(tags.map(tag => tag.trim()).filter(Boolean))];
}

function tagsError(tags: string[]) {
  if (tags.length > 12) return '最多保留 12 个标签，请删除多余标签后保存。';
  if (tags.some(tag => tag.length > 24)) return '每个标签最多 24 字，请缩短超长标签后保存。';
}

function serverCounts(serverId: string, inbounds: InboundResource[], proxies: ProxyDraft[]) {
  const owned = inbounds.filter(inbound => inbound.serverId === serverId);
  return {
    inbounds: owned.length,
    nodes: proxies.filter(proxy => proxy.chain.some(hop => hop.serverId === serverId)).length,
    references: owned.reduce((total, inbound) => total + inboundReferences(proxies, inbound.id).length, 0),
  };
}

function ServerAddresses({ server }: { server: ServerAsset }) {
  const { message } = App.useApp();
  async function copyAddress(address: string, label: string) {
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(address);
      void message.success(`已复制${label} · 合成地址`);
    } catch {
      void message.error('复制失败：浏览器未授予剪贴板权限，请手动选择地址复制。');
    }
  }
  return <div className="su-server-addresses">{[
    ['公网 IP', server.publicIp], ['内网 IP', server.privateIp],
  ].map(([label, address]) => <div key={label}>
    <span>{label}</span><code>{address || '未上报'}</code>
    <Tooltip title={`复制${label}`}><Button type="text" size="small" disabled={!address} aria-label={`复制 ${server.name} ${label}`} icon={<Icon name="copy" size={14} />} onClick={() => void copyAddress(address, label)} /></Tooltip>
  </div>)}</div>;
}

function ServerUsage({ serverId, inbounds, proxies }: { serverId: string; inbounds: InboundResource[]; proxies: ProxyDraft[] }) {
  const counts = serverCounts(serverId, inbounds, proxies);
  return <dl className="su-server-usage" aria-label="当前会话入口与节点统计">
    <div><dt>独立入口</dt><dd>{counts.inbounds}</dd></div>
    <div><dt>代理节点</dt><dd>{counts.nodes}</dd></div>
    <Tooltip title="按各入口的节点引用数求和，包含监听与下一跳引用；同一节点可引用多个入口。"><div><dt>入口引用</dt><dd>{counts.references}</dd></div></Tooltip>
  </dl>;
}

function ProtocolCapabilities({ server }: { server: ServerAsset }) {
  const supported = new Set(server.protocols);
  return <div className="su-server-capabilities">
    <div><span>协议能力</span><strong>{server.capabilitiesKnown ? `${supported.size} 种支持` : '能力未上报'}</strong></div>
    <div className="su-server-protocols">{([
      ['vless-reality', 'Reality'], ['shadowsocks', 'SS'], ['hysteria2', 'Hy2'],
    ] as const).map(([protocol, label]) => <span className={server.capabilitiesKnown && supported.has(protocol) ? 'is-supported' : ''} key={protocol}>
      <Icon name={server.capabilitiesKnown && supported.has(protocol) ? 'check' : 'help'} size={12} />{label}<small>{!server.capabilitiesKnown ? '未知' : supported.has(protocol) ? '支持' : '未开放'}</small>
    </span>)}</div>
  </div>;
}

function ServerEditor({ server, initialTab, now, onClose }: { server: ServerAsset; initialTab: EditorTab; now: Date; onClose: () => void }) {
  const { state, busy, dispatch } = useWorkspace();
  const { message } = App.useApp();
  const [activeTab, setActiveTab] = useState<EditorTab>(initialTab);
  const [traffic, setTraffic] = useState<ServerTraffic>(() => ({ ...server.traffic }));
  const [tags, setTags] = useState(() => [...server.tags]);
  const [tagInput, setTagInput] = useState('');
  const [unit, setUnit] = useState<TrafficUnit>(server.traffic.limitBytes >= unitBytes.TB ? 'TB' : 'GB');
  const [limitInput, setLimitInput] = useState<number | null>(() => server.traffic.limitBytes / unitBytes[server.traffic.limitBytes >= unitBytes.TB ? 'TB' : 'GB']);
  const [saveError, setSaveError] = useState('');
  const normalizedTags = normalizeTags([...tags, tagInput]);
  const currentTagsError = tagsError(normalizedTags);
  const previewTraffic = { ...traffic, usedBytes: server.traffic.usedBytes };
  const trafficError = validateServerTraffic(previewTraffic);
  const nextReset = nextTrafficReset(previewTraffic, now);
  const dirty = JSON.stringify(normalizeTags(server.tags)) !== JSON.stringify(normalizedTags) || JSON.stringify(server.traffic) !== JSON.stringify(previewTraffic);

  function changeTraffic(patch: Partial<ServerTraffic>) {
    setTraffic(current => ({ ...current, ...patch }));
    setSaveError('');
  }

  function save() {
    if (busy) {
      setSaveError('工作区正在模拟应用，暂时无法保存；草稿已保留。');
      return;
    }
    if (currentTagsError || trafficError) {
      setSaveError(currentTagsError || trafficError || '请检查配置。');
      setActiveTab(currentTagsError ? 'tags' : 'traffic');
      return;
    }
    dispatch({ type: 'server-save', id: server.id, tags: normalizedTags, traffic: previewTraffic });
    void message.success('已保存到合成工作区 · 已用流量保留，刷新页面后清空');
    onClose();
  }

  const detailsTab = <div className="su-server-tab-content">
    <section className="su-server-editor-section"><h3>连接与 Agent</h3><ServerAddresses server={server} />
      <dl className="su-server-facts">
        <div><dt>Hostname</dt><dd>{server.address}</dd></div>
        <div><dt>最后心跳</dt><dd>{utcDate(server.lastHeartbeat)}<small>合成心跳时间，不代表当前连通性</small></dd></div>
        <div><dt>Agent / 内核</dt><dd>{server.agentVersion || '未上报'} / {server.profile}<small>lite 规划中 · 不可配置</small></dd></div>
        <div><dt>已上报协议</dt><dd>{server.capabilitiesKnown ? server.protocols.map(protocol => protocolLabels[protocol]).join('、') || '无' : '未知，不放行'}</dd></div>
        <div><dt>网络能力</dt><dd>{server.capabilitiesKnown ? server.udp ? 'TCP + UDP' : 'TCP' : '未上报'}</dd></div>
        <div><dt>入口容量</dt><dd>{server.maxProxies ? `${server.maxProxies} 个` : '未上报'}</dd></div>
        <div><dt>下一跳能力</dt><dd>{server.chainTarget ? '支持复用入口为下一跳' : '未验证'}</dd></div>
      </dl>
    </section>
    <section className="su-server-editor-section"><h3>资源快照</h3><ServerResources server={server} /></section>
    <section className="su-server-editor-section"><h3>当前会话拓扑</h3><ServerUsage serverId={server.id} inbounds={state.inbounds} proxies={state.proxies} /><p className="su-server-note">统计直接来自本次工作区的入口与代理节点，不是生产数据。</p></section>
    <section className="su-server-editor-section"><div className="su-server-section-heading"><h3>流量策略</h3><Button type="link" size="small" disabled={busy} onClick={() => setActiveTab('traffic')}>编辑流量</Button></div><ServerTrafficSummary traffic={server.traffic} now={now} /></section>
    <section className="su-server-editor-section"><div className="su-server-section-heading"><h3>当前标签</h3><Button type="link" size="small" disabled={busy} onClick={() => setActiveTab('tags')}>编辑标签</Button></div><ServerTags tags={server.tags} /></section>
  </div>;

  const trafficTab = <div className="su-server-tab-content">
    <Alert showIcon type="info" title="只修改策略，不清零历史流量" description={`当前已用 ${formatBytes(server.traffic.usedBytes)}。周期与倒计时仅预览，不执行真实刷新；所有日期按 UTC 计算。`} />
    <Form layout="vertical" disabled={busy} className="su-server-editor-form">
      <Form.Item label="流量限额" required extra="0 表示不限额；1 TB = 1024 GB，输入值换算为整数 byte。" validateStatus={!Number.isSafeInteger(traffic.limitBytes) || traffic.limitBytes < 0 ? 'error' : undefined} help={!Number.isSafeInteger(traffic.limitBytes) || traffic.limitBytes < 0 ? '请填写非负流量，换算后须在安全整数范围内。' : undefined}>
        <div className="su-server-limit-input"><InputNumber aria-label="流量限额" min={0} max={Number.MAX_SAFE_INTEGER / unitBytes[unit]} value={limitInput} onChange={value => {
          setLimitInput(value);
          changeTraffic({ limitBytes: value === null ? NaN : Math.round(value * unitBytes[unit]) });
        }} /><Select aria-label="流量单位" value={unit} options={[{ value: 'GB', label: 'GB' }, { value: 'TB', label: 'TB' }]} onChange={(value: TrafficUnit) => {
          setUnit(value);
          setLimitInput(Number.isFinite(traffic.limitBytes) ? traffic.limitBytes / unitBytes[value] : null);
        }} /></div>
      </Form.Item>
      <Form.Item label="刷新周期" required><Select aria-label="刷新周期" value={traffic.resetCycle} onChange={(value: ServerTraffic['resetCycle']) => changeTraffic({ resetCycle: value, resetDay: value === 'weekly' && traffic.resetDay > 7 ? 1 : traffic.resetDay })} options={[
        { value: 'daily', label: '每天' }, { value: 'weekly', label: '每周' }, { value: 'monthly', label: '每月' }, { value: 'custom', label: '自定义天数' },
      ]} /></Form.Item>
      {traffic.resetCycle === 'daily' && <p className="su-server-field-note">每天 00:00 UTC 为计划刷新时间。</p>}
      {traffic.resetCycle === 'weekly' && <Form.Item label="每周刷新日" required extra="所选日期的 00:00 UTC。"><Select aria-label="每周刷新日" value={traffic.resetDay} onChange={value => changeTraffic({ resetDay: value })} options={weekdays.map((label, index) => ({ value: index + 1, label }))} /></Form.Item>}
      {traffic.resetCycle === 'monthly' && <Form.Item label="每月刷新日" required extra="1–31 日，00:00 UTC；当月不足所选日期时，使用当月最后一天。"><InputNumber aria-label="每月刷新日" min={1} max={31} value={Number.isFinite(traffic.resetDay) ? traffic.resetDay : null} onChange={value => changeTraffic({ resetDay: value ?? NaN })} /></Form.Item>}
      {traffic.resetCycle === 'custom' && <div className="su-server-form-columns">
        <Form.Item label="间隔天数" required extra="1–365 天，整数。"><InputNumber aria-label="自定义间隔天数" min={1} max={365} value={Number.isFinite(traffic.customDays) ? traffic.customDays : null} onChange={value => changeTraffic({ customDays: value ?? NaN })} /></Form.Item>
        <Form.Item label="周期起点（UTC）" required extra="从所选日期 00:00 UTC 起计算。"><Input aria-label="周期起点 UTC" type="date" value={traffic.resetAnchor.slice(0, 10)} onChange={event => changeTraffic({ resetAnchor: event.target.value ? `${event.target.value}T00:00:00.000Z` : '' })} /></Form.Item>
      </div>}
      <Form.Item label="流量统计方式" required extra="只保存策略选择；不回算、不删除已有流量。"><Select aria-label="流量统计方式" value={traffic.statsMode} onChange={(value: ServerTraffic['statsMode']) => changeTraffic({ statsMode: value })} options={[{ value: 'total', label: '入站 + 出站合计' }, { value: 'outbound', label: '仅出站' }]} /></Form.Item>
    </Form>
    <section className="su-server-reset-preview" aria-live="polite"><span className="su-section-eyebrow">策略预览 · UTC</span><h3>{trafficError ? '请完善流量配置' : `${trafficCycleLabel(traffic)} · 下次刷新`}</h3><p>{Number.isFinite(nextReset.getTime()) ? utcDate(nextReset) : trafficError}</p>{!trafficError && <ServerTrafficSummary traffic={previewTraffic} now={now} />}</section>
  </div>;

  const tagsTab = <div className="su-server-tab-content">
    <p className="su-server-note">用标签组织地区、线路与用途。修改只影响本次合成会话，不改变入口或节点。</p>
    <Form layout="vertical" disabled={busy} className="su-server-editor-form"><Form.Item label={`服务器标签 · ${normalizedTags.length} / 12`} validateStatus={currentTagsError ? 'error' : undefined} help={currentTagsError} extra="输入后按回车添加，也可直接保存；自动去除首尾空格、去重。最多 12 个，每个最多 24 字。">
      <Select mode="tags" aria-label="服务器标签" value={tags} searchValue={tagInput} onSearch={setTagInput} onChange={values => { setTags(normalizeTags(values)); setTagInput(''); setSaveError(''); }} options={[...new Set(state.servers.flatMap(asset => asset.tags))].map(tag => ({ value: tag, label: tag }))} placeholder="例如：旗舰、CN2、备用" />
    </Form.Item></Form>
    <section className="su-server-editor-section"><h3>保存后预览</h3><ServerTags tags={normalizedTags} /></section>
    <Alert showIcon type="info" title="取消即丢弃本次编辑" description="详情、流量和标签共用一份草稿，切换标签页不会写入工作区。点击保存后统一生效。" />
  </div>;

  return <Drawer open title="服务器详情" size={600} rootClassName="su-server-drawer" onClose={onClose} footer={<div className="su-server-drawer-footer"><span>{busy ? '模拟应用中 · 编辑已锁定' : dirty ? '有未保存更改 · 仅会话内存' : '仅合成数据 · 刷新页面后清空'}</span><div><Button onClick={onClose}>取消</Button><Button type="primary" disabled={busy || !dirty} onClick={save}>保存更改</Button></div></div>}>
    <div className="su-server-drawer-identity"><span className="su-region-icon">{server.country}</span><div><h2>{server.name}</h2><p>{server.region} · {server.address}</p></div><ServerStatusBadge status={server.status} /></div>
    {busy && <Alert className="su-server-editor-alert" showIcon type="warning" title="工作区正在模拟应用，暂时禁止编辑与保存；当前草稿保留。" />}
    {saveError && <div role="alert" className="su-server-editor-alert"><Alert showIcon type="error" title={saveError} /></div>}
    <Tabs activeKey={activeTab} onChange={key => setActiveTab(key as EditorTab)} items={[{ key: 'details', label: '详情', children: detailsTab }, { key: 'traffic', label: '流量配置', children: trafficTab }, { key: 'tags', label: '标签', children: tagsTab }]} />
  </Drawer>;
}

export default function ServersPage() {
  const { state, busy, dispatch } = useWorkspace();
  const { message } = App.useApp();
  const [filters, setFilters] = useState<ServerFilters>(defaultFilters);
  const [editor, setEditor] = useState<{ id: string; tab: EditorTab } | null>(null);
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 60_000);
    return () => window.clearInterval(timer);
  }, []);
  const servers = filterServers(state.servers, filters);
  const selected = state.servers.find(server => server.id === editor?.id);
  const tagOptions = [...new Set(state.servers.flatMap(server => server.tags))].sort((first, second) => first.localeCompare(second, 'zh-CN'));
  const regionOptions = [...new Map(state.servers.map(server => [server.country, server.region])).entries()];
  const hasFilters = filters.query !== '' || filters.status !== 'all' || filters.tag !== '' || filters.region !== '' || filters.sort !== 'name';
  const statusCount = (status: ServerAsset['status']) => state.servers.filter(server => server.status === status).length;
  const nearLimit = state.servers.filter(server => server.traffic.limitBytes > 0 && server.traffic.usedBytes / server.traffic.limitBytes >= 0.8).length;
  const overview = [
    { label: '服务器总数', value: state.servers.length, detail: `${regionOptions.length} 个地区 · 仅 full agent`, icon: 'server' as const, tone: 'total' },
    { label: '在线', value: statusCount('online'), detail: '合成状态 · 不是实时连通性', icon: 'check' as const, tone: 'online' },
    { label: '离线 / 维护', value: `${statusCount('offline')} / ${statusCount('maintenance')}`, detail: '恢复维护前状态，不伪造上线', icon: 'pause' as const, tone: 'maintenance' },
    { label: '需关注流量', value: nearLimit, detail: '已用 ≥ 80% · 仅提示，不阻断', icon: 'traffic' as const, tone: 'traffic' },
  ];

  function updateFilters(patch: Partial<ServerFilters>) {
    setFilters(current => ({ ...current, ...patch }));
  }

  function toggleServer(server: ServerAsset) {
    if (busy) return;
    dispatch({ type: 'server-toggle', id: server.id });
    void message.info(server.status === 'maintenance' ? '已模拟退出维护，恢复原在线/离线状态；未执行真实启用。' : '已模拟进入维护，入口与节点完整保留；未执行真实停用。');
  }

  return <div className="su-standard-page su-servers-page">
    <PageHeader title="服务器节点" description="掌握节点状态、资源与流量策略，用标签快速找到下一台服务器。" actions={<Link to="/prototype/topology"><Button type="primary" icon={<Icon name="topology" />}>前往拓扑编排</Button></Link>} />
    <Alert showIcon type="info" title="R1 合成工作区 · 所有数据及操作均为模拟" description="修改仅存于本次会话，刷新页面后清空。资源与心跳非实时；时间统一为 UTC，流量刷新仅预览，不会执行真实刷新或清零历史。lite 规划中，不可配置。" />
    <div className="su-servers-overview">{overview.map(stat => <section key={stat.label} className={`su-servers-stat is-${stat.tone}`}><div><span>{stat.label}</span><Icon name={stat.icon} size={18} /></div><strong>{stat.value}</strong><small>{stat.detail}</small></section>)}</div>
    <section className="su-server-toolbar" aria-label="服务器筛选">
      <div className="su-server-toolbar-top"><Input className="su-server-search" aria-label="搜索服务器" allowClear placeholder="搜索名称、hostname、IP、标签或地区" value={filters.query} onChange={event => updateFilters({ query: event.target.value })} /><Select aria-label="服务器排序" value={filters.sort} onChange={(sort: ServerFilters['sort']) => updateFilters({ sort })} options={[{ value: 'name', label: '名称排序' }, { value: 'status', label: '状态排序' }, { value: 'memory', label: '内存占用：高到低' }]} /></div>
      <div className="su-server-filter-row">
        <label><span>状态</span><Select aria-label="状态筛选" value={filters.status} onChange={(status: ServerFilters['status']) => updateFilters({ status })} options={[{ value: 'all', label: '全部状态' }, ...Object.entries(serverStatusLabels).map(([value, label]) => ({ value, label }))]} /></label>
        <label><span>标签</span><Select aria-label="标签筛选" showSearch allowClear placeholder="全部标签" value={filters.tag || undefined} onChange={tag => updateFilters({ tag: tag || '' })} options={tagOptions.map(tag => ({ value: tag, label: tag }))} /></label>
        <label><span>地区</span><Select aria-label="地区筛选" allowClear placeholder="全部地区" value={filters.region || undefined} onChange={region => updateFilters({ region: region || '' })} options={regionOptions.map(([country, region]) => ({ value: country, label: `${region} · ${country}` }))} /></label>
        <Button type="text" disabled={!hasFilters} icon={<Icon name="reset" size={14} />} onClick={() => setFilters(defaultFilters)}>清空筛选</Button>
      </div>
      <div className="su-server-result-meta"><span role="status" aria-live="polite">显示 <strong>{servers.length}</strong> / {state.servers.length} 台服务器{hasFilters ? ' · 已应用筛选或排序' : ''}</span><span>工作区统计 · UTC</span></div>
    </section>
    {busy && <Alert className="su-servers-busy" type="warning" showIcon title="模拟应用进行中：可以查看详情，编辑与停用 / 启用暂不可用。" />}
    {servers.length ? <div className="su-servers-grid">{servers.map(server => <article className={`su-server-card is-${server.status}`} key={server.id} aria-label={`${server.name} 服务器`}>
      <div className="su-server-card-heading"><span className="su-region-icon">{server.country}</span><div><h2>{server.name}</h2><p>{server.region} · {server.address}</p></div><ServerStatusBadge status={server.status} /></div>
      <div className="su-server-agent"><span>Agent {server.agentVersion || '未上报'}</span><Tag>{server.profile}</Tag><span>lite 规划中</span></div>
      <p className="su-server-heartbeat">最后心跳 <time dateTime={server.lastHeartbeat}>{utcDate(server.lastHeartbeat)}</time></p>
      <ServerAddresses server={server} />
      <ProtocolCapabilities server={server} />
      <ServerResources server={server} />
      <ServerUsage serverId={server.id} inbounds={state.inbounds} proxies={state.proxies} />
      <ServerTrafficSummary traffic={server.traffic} now={now} />
      <div className="su-server-card-tags"><ServerTags tags={server.tags} /></div>
      <footer className="su-server-card-actions"><Button size="small" onClick={() => setEditor({ id: server.id, tab: 'details' })}>详情</Button><Button size="small" disabled={busy} onClick={() => setEditor({ id: server.id, tab: 'tags' })}>编辑标签</Button>
        <Popconfirm disabled={busy} title={server.status === 'maintenance' ? '模拟启用此服务器？' : '模拟停用此服务器？'} description={<div className="su-server-toggle-description">{server.status === 'maintenance' ? '仅退出维护并恢复原来的在线 / 离线状态，不代表真实上线。' : '仅将状态改为维护，不执行真实停机。'}不删除任何入口或代理节点，刷新页面后清空。</div>} okText={server.status === 'maintenance' ? '确认模拟启用' : '确认模拟停用'} cancelText="取消" okButtonProps={{ disabled: busy }} onConfirm={() => toggleServer(server)}><Button size="small" danger={server.status !== 'maintenance'} disabled={busy} icon={<Icon name={server.status === 'maintenance' ? 'check' : 'pause'} size={12} />}>{server.status === 'maintenance' ? '启用' : '停用'}</Button></Popconfirm>
      </footer>
    </article>)}</div> : <section className="su-server-empty"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={state.servers.length ? '没有匹配的服务器，试试其他名称、标签或地区。' : '当前合成工作区暂无服务器。'}>{hasFilters && <Button onClick={() => setFilters(defaultFilters)}>清空筛选</Button>}</Empty></section>}
    <p className="su-servers-footer-note"><Icon name="help" size={14} />独立入口与代理节点可复用；引用数基于工作区草稿。服务器维护不会删除拓扑。</p>
    {selected && editor && <ServerEditor key={selected.id} server={selected} initialTab={editor.tab} now={now} onClose={() => setEditor(null)} />}
  </div>;
}
