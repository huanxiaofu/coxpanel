import { useCallback, useMemo, useRef, useState } from 'react';
import { Alert, App, Button, Dropdown, Modal, Progress, Select, Switch, Tag } from 'antd';
import { Background, Controls, Handle, MarkerType, Position, ReactFlow, ReactFlowProvider, useReactFlow } from '@xyflow/react';
import type { Connection, Edge, Node, NodeProps } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { PageHeader } from './AppShell';
import { Icon } from './Icon';
import { connectionError, getServer, protocolLabels, serverAssets, statusLabels } from './model';
import type { ProxyConfig, ProxyDraft, ProxyLink, ServerAsset } from './model';
import ProxyConfigForm from './ProxyConfigForm';
import ServerAssetPanel, { SERVER_DRAG_TYPE } from './ServerAssetPanel';
import { useTheme } from './ThemeProvider';
import { useWorkspace } from './WorkspaceProvider';

interface CardData extends Record<string, unknown> {
  proxy: ProxyDraft;
  egress: string;
  publishedEgress?: string;
  busy: boolean;
  onConfigure: (id: string) => void;
  onConnect: (id: string) => void;
  onDirect: (id: string) => void;
  onRemove: (id: string) => void;
}
type ProxyFlowNode = Node<CardData, 'proxy'>;

function ProxyNodeCard({ data, selected }: NodeProps<ProxyFlowNode>) {
  const { proxy } = data;
  const server = getServer(proxy.serverId);
  const canConnect = Boolean(proxy.config) && server.profile === 'full' && !data.busy;
  const items = [
    { key: 'configure', label: proxy.config ? '编辑入口' : '配置入口', disabled: data.busy, onClick: () => data.onConfigure(proxy.id) },
    { key: 'connect', label: '连接到…', disabled: !canConnect, onClick: () => data.onConnect(proxy.id) },
    { key: 'direct', label: '改为本机直出', disabled: !data.egress.startsWith('下一跳') || data.busy, onClick: () => data.onDirect(proxy.id) },
    ...(!proxy.published ? [{ key: 'remove', label: proxy.config ? '删除草稿' : '取消创建', danger: true, disabled: data.busy, onClick: () => data.onRemove(proxy.id) }] : []),
  ];
  return <Dropdown menu={{ items }} trigger={['contextMenu']}>
    <article className={`su-proxy-card ${!proxy.config ? 'is-placeholder' : ''} ${selected ? 'is-selected' : ''}`} data-proxy-id={proxy.id} aria-label={proxy.config?.name ?? `${server.name} 待配置入口`}>
      <Handle type="target" position={Position.Left} isConnectable={Boolean(proxy.config?.exposure === 'internal' && server.chainTarget && !data.busy)} aria-label="连接目标" />
      <div className="su-proxy-card-heading"><span className={`su-protocol-icon protocol-${proxy.config?.protocol ?? 'draft'}`}><Icon name={proxy.config ? 'proxy' : 'plus'} size={19} /></span><div><h3>{proxy.config?.name ?? '新建代理入口'}</h3><span><Icon name="server" size={11} /> {server.name}</span></div><Dropdown menu={{ items }} trigger={['click']}><Button className="nodrag nopan" type="text" size="small" aria-label={`${proxy.config?.name ?? server.name} 操作菜单`}>•••</Button></Dropdown></div>
      {proxy.config ? <>
        <div className="su-proxy-badges"><Tag color={proxy.config.protocol === 'vless-reality' ? 'blue' : proxy.config.protocol === 'shadowsocks' ? 'purple' : 'cyan'}>{protocolLabels[proxy.config.protocol]}</Tag><Tag>{proxy.config.exposure === 'internal' ? '内部入口' : '订阅入口'}</Tag><span className={`su-status su-status-${proxy.status}`}><span />{statusLabels[proxy.status]}</span></div>
        <dl className="su-proxy-fields"><div><dt>入口</dt><dd title={`${proxy.config.advertisedAddress}:${proxy.config.advertisedPort}`}>{proxy.config.advertisedAddress}:{proxy.config.advertisedPort}</dd></div><div><dt>监听</dt><dd>{proxy.config.protocol === 'hysteria2' ? 'UDP' : proxy.config.network === 'tcp-udp' ? 'TCP / UDP' : 'TCP'} · {proxy.config.listenPort}</dd></div></dl>
        <div className={`su-egress ${proxy.dirty ? 'is-draft' : ''}`}><Icon name="arrow" size={14} /><span>{data.egress}</span>{proxy.dirty && <small>草稿</small>}</div>
        {proxy.published && proxy.dirty && <div className="su-published-note">当前模拟生效：{data.publishedEgress} · {proxy.published.listenPort}</div>}
        {proxy.failure && <p className="su-card-error" role="alert">{proxy.failure}</p>}
        <div className="su-card-actions nodrag nopan"><Button type="text" size="small" disabled={data.busy} onClick={() => data.onConfigure(proxy.id)}>编辑入口</Button><Button type="text" size="small" disabled={!canConnect} title={server.profile === 'lite' ? 'lite 仅支持本机直出' : '用画布命令选择下一跳'} onClick={() => data.onConnect(proxy.id)}>连接到…</Button></div>
      </> : <><p className="su-placeholder-copy">服务器已就位，配置一个入口。<br />双击、右键或点击下方按钮。</p><div className="su-placeholder-bottom"><span className="su-status su-status-draft"><span />待配置</span><Button type="primary" size="small" className="nodrag nopan" disabled={data.busy} onClick={() => data.onConfigure(proxy.id)}>配置入口</Button></div></>}
      <Handle type="source" position={Position.Right} isConnectable={canConnect} aria-label="拖线设置下一跳" title={server.profile === 'lite' ? 'lite 首版 direct-only' : '拖到内部入口，定义下一跳'} />
    </article>
  </Dropdown>;
}
const nodeTypes = { proxy: ProxyNodeCard };

function WorkspaceCanvas() {
  const { state, dispatch, busy, simulateFailure, setSimulateFailure, applyError } = useWorkspace();
  const { dark } = useTheme();
  const { modal, message } = App.useApp();
  const flow = useReactFlow<ProxyFlowNode>();
  const canvas = useRef<HTMLDivElement>(null);
  const [editorId, setEditorId] = useState<string>();
  const [connectingId, setConnectingId] = useState<string>();
  const [targetId, setTargetId] = useState<string>();
  const [error, setError] = useState('');
  const [showAssets, setShowAssets] = useState(true);
  const [selectedId, setSelectedId] = useState<string>();
  const [draggingOver, setDraggingOver] = useState(false);
  const [applyIds, setApplyIds] = useState<string[]>();
  const focusOrigin = useRef<HTMLElement | null>(null);
  const dirty = state.proxies.filter(proxy => proxy.config && proxy.dirty);
  const linksChanged = JSON.stringify(state.links) !== JSON.stringify(state.publishedLinks);
  const editing = state.proxies.find(proxy => proxy.id === editorId);

  const egress = useCallback((id: string, published = false) => {
    const links = published ? state.publishedLinks : state.links;
    const target = state.proxies.find(proxy => proxy.id === links.find(link => link.source === id)?.target);
    return target ? `下一跳 → ${(published ? target.published?.name : target.config?.name) ?? '待配置'}` : '本机直出';
  }, [state.links, state.publishedLinks, state.proxies]);

  const configure = useCallback((id: string) => {
    if (busy) return;
    focusOrigin.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setEditorId(id);
  }, [busy]);

  const restoreFocus = () => window.setTimeout(() => {
    if (focusOrigin.current?.isConnected) focusOrigin.current.focus();
    else canvas.current?.focus();
  }, 250);

  const addServer = (server: ServerAsset, position?: ProxyDraft['position']) => {
    if (busy || !server.online || !server.capabilitiesKnown) return;
    if (state.proxies.filter(proxy => proxy.serverId === server.id).length >= server.maxProxies) { setError('该服务器已达到演示容量上限。'); return; }
    const id = `draft:${crypto.randomUUID()}`;
    const count = state.proxies.length;
    const placement = position ?? { x: 70 + (count % 2) * 380, y: 95 + Math.floor(count / 2) * 330 };
    dispatch({ type: 'add', proxy: { id, serverId: server.id, position: placement, status: 'draft', dirty: false } });
    setSelectedId(id);
    setError('');
    if (!position) window.setTimeout(() => void flow.fitView({ duration: 250, padding: 0.22, maxZoom: 1 }), 80);
  };

  const remove = useCallback((id: string) => {
    const proxy = state.proxies.find(item => item.id === id);
    if (!proxy || proxy.published || busy) return;
    if ([...state.links, ...state.publishedLinks].some(link => link.source === id || link.target === id)) { setError('该入口存在连线依赖，请先明确断开关联连线。'); return; }
    if (!proxy.config) { dispatch({ type: 'remove', id }); return; }
    modal.confirm({ title: '删除这个草稿？', content: `${proxy.config.name} 尚未发布。此操作仅删除本地草稿。`, okText: '删除草稿', okButtonProps: { danger: true }, cancelText: '保留', onOk: () => dispatch({ type: 'remove', id }) });
  }, [busy, dispatch, modal, state.links, state.proxies, state.publishedLinks]);

  const disconnect = useCallback((id: string) => {
    if (busy) return;
    modal.confirm({ title: '将流量改为本机直出？', content: '清除出边后，草稿的流量将改为由所属服务器直接出网。当前模拟生效链路保持不变，直到你确认应用。', okText: '确认清除出边', cancelText: '保留连线', onOk: () => dispatch({ type: 'disconnect', id }) });
  }, [busy, dispatch, modal]);

  const connect = (connection: ProxyLink) => {
    if (busy) return false;
    const reason = connectionError(state.proxies, state.links, connection);
    if (reason) { setError(reason); return false; }
    dispatch({ type: 'connect', connection });
    setError('');
    void message.info('连线已保存为草稿，请确认应用。');
    return true;
  };

  const openConnection = useCallback((id: string) => { setConnectingId(id); setTargetId(undefined); setError(''); }, []);
  const nodes: ProxyFlowNode[] = useMemo(() => state.proxies.map(proxy => ({
    id: proxy.id, type: 'proxy', position: proxy.position, selected: proxy.id === selectedId,
    ariaLabel: `${proxy.config?.name ?? getServer(proxy.serverId).name + ' 待配置入口'}，按 Enter 配置`,
    data: { proxy, busy, egress: egress(proxy.id), publishedEgress: egress(proxy.id, true), onConfigure: configure, onConnect: openConnection, onDirect: disconnect, onRemove: remove },
  })), [state.proxies, selectedId, busy, egress, configure, openConnection, disconnect, remove]);
  const edges: Edge[] = state.links.map(link => ({
    id: `${link.source}->${link.target}`, source: link.source, target: link.target,
    type: 'smoothstep', markerEnd: { type: MarkerType.ArrowClosed, color: dark ? '#6b9eff' : '#0958d9' },
    label: '下一跳', animated: busy && !window.matchMedia('(prefers-reduced-motion: reduce)').matches,
    style: { stroke: dark ? '#6b9eff' : '#0958d9', strokeWidth: 2, strokeDasharray: state.publishedLinks.some(published => published.source === link.source && published.target === link.target) ? undefined : '6 4' },
    labelStyle: { fill: dark ? '#c2d4fa' : '#0958d9', fontSize: 11 },
    labelBgStyle: { fill: dark ? '#23252b' : '#fff' },
  }));

  const requestApply = (ids: string[]) => {
    const reason = applyError(ids);
    if (reason) { setError(reason); return; }
    setApplyIds(ids);
  };

  const save = (config: ProxyConfig, apply: boolean) => {
    if (!editing || busy) return;
    if (config.exposure !== 'internal' && [...state.links, ...state.publishedLinks].some(link => link.target === editing.id)) { void message.error('此入口被上游引用，必须保持内部用途。请先在画布处理连线依赖。'); return; }
    dispatch({ type: 'save', id: editing.id, config });
    setEditorId(undefined);
    restoreFocus();
    if (apply) {
      const hasDependencies = [...state.links, ...state.publishedLinks].some(link => link.target === editing.id || (link.source === editing.id && state.proxies.find(proxy => proxy.id === link.target)?.dirty));
      if (hasDependencies) { setError('入口草稿已保存。此变更涉及关联入口，请使用工作区「应用更改」确认完整范围。'); return; }
      dispatch({ type: 'start', ids: [editing.id], fail: simulateFailure });
    } else void message.success('入口草稿已保存，仅保留在本次浏览器会话。');
  };

  const closeEditor = () => {
    if (editing && !editing.config) dispatch({ type: 'remove', id: editing.id });
    setEditorId(undefined);
    restoreFocus();
  };

  const reset = () => modal.confirm({ title: '重置演示工作区？', content: '清空本次会话的所有合成入口和连线。不影响任何真实服务。', okText: '重置演示', cancelText: '保留', okButtonProps: { danger: true }, onOk: () => { dispatch({ type: 'reset' }); setError(''); } });

  return <div className="su-topology-page">
    <PageHeader title="拓扑编排" description="拖入服务器创建入口，用连线定义流量的下一跳。" actions={<><Button onClick={() => setShowAssets(value => !value)} icon={<Icon name="server" />}>{showAssets ? '收起资产' : '服务器资产'}</Button><Button disabled={busy || !state.proxies.length} icon={<Icon name="reset" size={16} />} onClick={reset} aria-label="重置演示工作区">重置</Button><Button type="primary" disabled={busy || !dirty.length} onClick={() => requestApply(dirty.map(proxy => proxy.id))}>应用更改{dirty.length ? ` (${dirty.length})` : ''}</Button></>} />
    <div className="su-prototype-banner"><span className="su-demo-pill">SANDBOX</span><p>这是合成交互原型。所有部署状态均为模拟，不会更改服务器；刷新后工作区清空。</p><span>R1 · 等待你的体验确认</span></div>
    {error && <Alert className="su-workspace-error" type="warning" showIcon title={error} closable onClose={() => setError('')} />}
    <div className={`su-workspace ${showAssets ? '' : 'assets-hidden'} ${editing ? 'has-editor' : ''}`}>
      {showAssets && <ServerAssetPanel proxies={state.proxies} busy={busy} onAdd={server => addServer(server)} onLocate={id => { setSelectedId(id); void flow.fitView({ nodes: [{ id }], duration: 300, maxZoom: 1, padding: 0.4 }); }} />}
      <div className={`su-canvas ${draggingOver ? 'is-drag-over' : ''}`} ref={canvas} role="region" aria-label="拓扑画布" tabIndex={0}
        onKeyDown={event => {
          const flowNode = (event.target as HTMLElement).closest('.react-flow__node');
          if (event.key === 'Enter' && flowNode && event.target === flowNode && flowNode.getAttribute('data-id')) configure(flowNode.getAttribute('data-id')!);
        }}
        onDragOver={event => { if (event.dataTransfer.types.includes(SERVER_DRAG_TYPE)) { event.preventDefault(); event.dataTransfer.dropEffect = 'copy'; setDraggingOver(true); } }}
        onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as globalThis.Node | null)) setDraggingOver(false); }}
        onDrop={event => {
          event.preventDefault(); setDraggingOver(false);
          const serverId = event.dataTransfer.getData(SERVER_DRAG_TYPE);
          const server = serverAssets.find(asset => asset.id === serverId);
          if (server) addServer(server, flow.screenToFlowPosition({ x: event.clientX - 155, y: event.clientY - 38 }));
        }}>
        <div className="su-canvas-label"><span className="su-live-dot" />默认工作区 <span>/</span> <span>{state.proxies.length} 个入口 · {state.links.length} 条连线</span></div>
        <ReactFlow<ProxyFlowNode> nodes={nodes} edges={edges} nodeTypes={nodeTypes} colorMode={dark ? 'dark' : 'light'} minZoom={0.25} maxZoom={1.6} defaultViewport={{ x: 0, y: 0, zoom: 1 }} deleteKeyCode={null} zoomOnDoubleClick={false} nodesConnectable={!busy}
          onNodesChange={changes => { const positions = changes.flatMap(change => change.type === 'position' && change.position ? [{ id: change.id, position: change.position }] : []); if (positions.length) dispatch({ type: 'move', positions }); }}
          onNodeClick={(_event, node) => setSelectedId(node.id)}
          onNodeDoubleClick={(_event, node) => configure(node.id)}
          onPaneClick={() => setSelectedId(undefined)}
          onConnect={(connection: Connection) => connect({ source: connection.source, target: connection.target })}
          onEdgeDoubleClick={(_event, edge) => disconnect(edge.source)}
          ariaLabelConfig={{ 'controls.zoomIn.ariaLabel': '放大画布', 'controls.zoomOut.ariaLabel': '缩小画布', 'controls.fitView.ariaLabel': '适配全部入口', 'controls.interactive.ariaLabel': '切换画布交互' }}>
          <Background gap={22} size={1} color={dark ? '#3c3e49' : '#d5dae3'} /><Controls showInteractive={false} position="bottom-left" />
        </ReactFlow>
        {!state.proxies.length && <div className="su-canvas-empty"><div className="su-empty-illustration"><span><Icon name="server" size={30} /></span><div>······ <Icon name="arrow" size={18} /> ······</div><span><Icon name="plus" size={29} /></span></div><Tag color="blue">从一台服务器开始</Tag><h2>把服务器，变成可用的代理。</h2><p>从左侧拖入 HK-zouter，创建你的第一个 Reality 入口。<br />无需预建入站，也无需创建另一个出口。</p><Button type="primary" icon={<Icon name="plus" />} onClick={() => addServer(serverAssets[0])}>添加 HK-zouter 到画布</Button><small>也支持键盘和「添加」按钮操作</small></div>}
        <div className="su-canvas-legend"><span><i className="is-draft" />草稿</span><span><i className="is-deploying" />部署中</span><span><i className="is-active" />已生效</span><span><i className="is-failed" />失败</span><span className="su-muted">均为模拟</span></div>
      </div>
      {editing && <div className="su-editor-panel"><ProxyConfigForm key={editing.id} proxy={editing} proxies={state.proxies} egressLabel={egress(editing.id)} onSave={save} onClose={closeEditor} /></div>}
    </div>
    <footer className="su-deployment-bar" aria-live="polite"><div className="su-deployment-summary"><span className="su-deployment-icon"><Icon name="topology" /></span><div><strong>{busy ? '正在模拟部署' : state.operation?.phase === 'failed' ? '模拟应用失败，草稿已保留' : state.operation?.phase === 'active' ? '模拟部署完成' : '准备好创建第一个入口'}</strong><small>{busy ? '仅演示进度，不会联系真实 Agent' : state.operation ? '模拟状态不代表真实 ACK 或公网可连接' : '无连线 = 本机直出 · 连线 = 下一跳'}</small></div></div><div className="su-deployment-progress"><div><span>01 准备</span><span>02 应用</span><span>03 已生效</span></div><Progress percent={state.operation?.phase === 'active' ? 100 : state.operation?.phase === 'applying' || state.operation?.phase === 'failed' ? 66 : state.operation ? 25 : 0} status={state.operation?.phase === 'failed' ? 'exception' : busy ? 'active' : undefined} showInfo={false} size="small" /></div><div className="su-deployment-options">{linksChanged && <Button size="small" disabled={busy} onClick={() => dispatch({ type: 'revert-links' })}>撤销连线草稿</Button>}<label><Switch size="small" checked={simulateFailure} disabled={busy} onChange={setSimulateFailure} aria-label="模拟应用失败" /> 模拟失败</label></div></footer>
    <Modal title="连接到内部入口" open={Boolean(connectingId)} onCancel={() => { setConnectingId(undefined); setError(''); }} okText="保存连线草稿" cancelText="取消" okButtonProps={{ disabled: !targetId }} onOk={() => { if (connectingId && targetId && connect({ source: connectingId, target: targetId })) setConnectingId(undefined); }}>
      <p className="su-modal-intro">从 {state.proxies.find(proxy => proxy.id === connectingId)?.config?.name} 出发。只有内部入口可作为下一跳，连线不会立即应用。</p>
      <Select aria-label="选择下一跳入口" placeholder="选择内部入口" style={{ width: '100%' }} value={targetId} onChange={setTargetId} options={state.proxies.filter(proxy => proxy.id !== connectingId && proxy.config).map(proxy => { const reason = connectionError(state.proxies, state.links, { source: connectingId ?? '', target: proxy.id }); return { value: proxy.id, disabled: Boolean(reason), label: `${proxy.config?.name} · ${getServer(proxy.serverId).name}${reason ? `（${reason}）` : ''}` }; })} />
      {!state.proxies.some(proxy => proxy.config?.exposure === 'internal') && <Alert style={{ marginTop: 16 }} type="info" title="先将 SG-edge 添加到画布，创建用途为「内部入口」的代理并保存草稿。" />}
    </Modal>
    <Modal title="确认应用合成变更" open={Boolean(applyIds)} onCancel={() => setApplyIds(undefined)} okText="确认模拟应用" cancelText="继续编辑" onOk={() => { if (applyIds) { dispatch({ type: 'start', ids: applyIds, fail: simulateFailure }); setApplyIds(undefined); } }}>
      <Alert type="info" showIcon title="仅模拟准备、应用与生效，不调用后端。" /><ul className="su-apply-list">{state.proxies.filter(proxy => applyIds?.includes(proxy.id)).map(proxy => <li key={proxy.id}><strong>{proxy.config?.name}</strong><span>{getServer(proxy.serverId).name} · {proxy.config?.listenPort} · {egress(proxy.id)}</span></li>)}</ul><p className="su-muted">同机其他入口会继续保留。真实服务器级聚合、依赖发布与 ACK 验证留待 R2 / R3。</p>
    </Modal>
  </div>;
}

export default function TopologyWorkspace() {
  return <ReactFlowProvider><WorkspaceCanvas /></ReactFlowProvider>;
}
