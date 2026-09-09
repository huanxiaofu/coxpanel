import { useCallback, useId, useMemo, useRef, useState } from 'react';
import { Alert, App, Button, Dropdown, Modal, Progress, Select, Switch, Tag } from 'antd';
import { Background, Controls, Handle, MarkerType, Position, ReactFlow, ReactFlowProvider, useReactFlow } from '@xyflow/react';
import type { Connection, Edge, Node, NodeProps } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { PageHeader } from './AppShell';
import { Icon } from './Icon';
import { connectionError, defaultEgress, egressSummary, getInbound, getServer, inboundReferences, protocolLabels, serverAssets, statusLabels } from './model';
import type { InboundResource, ProxyConfig, ProxyDraft, ServerAsset } from './model';
import ProxyConfigForm from './ProxyConfigForm';
import EgressConfigForm from './EgressConfigForm';
import ServerAssetPanel, { SERVER_DRAG_TYPE } from './ServerAssetPanel';
import { useTheme } from './ThemeProvider';
import { useWorkspace, workspaceApplyError, workspaceReducer } from './WorkspaceProvider';

interface CardData extends Record<string, unknown> {
  proxy: ProxyDraft;
  inbound?: InboundResource;
  references: number;
  available: number;
  egress: string;
  publishedEgress?: string;
  busy: boolean;
  onConfigure: (id: string) => void;
  onEgress: (id: string) => void;
  onReuse: (id: string) => void;
  onRevert: (id: string) => void;
  onRemove: (id: string) => void;
}
type ProxyFlowNode = Node<CardData, 'proxy'>;

function ProxyNodeCard({ data, selected }: NodeProps<ProxyFlowNode>) {
  const { proxy, inbound } = data;
  const server = getServer(proxy.serverId);
  const config = inbound?.config;
  const items = [
    { key: 'configure', label: config ? '编辑共享入口' : '新建入口', disabled: data.busy, onClick: () => data.onConfigure(proxy.id) },
    { key: 'egress', label: '配置出站 / 连接到…', disabled: !config || data.busy, onClick: () => data.onEgress(proxy.id) },
    ...(proxy.published && proxy.dirty ? [{ key: 'revert', label: '撤销节点名称与出站草稿', disabled: data.busy, onClick: () => data.onRevert(proxy.id) }] : []),
    ...(!config && data.available ? [{ key: 'reuse', label: '选择已有入口', disabled: data.busy, onClick: () => data.onReuse(proxy.id) }] : []),
    ...(!proxy.published ? [{ key: 'remove', label: '移除节点草稿', danger: true, disabled: data.busy, onClick: () => data.onRemove(proxy.id) }] : []),
  ];
  return <Dropdown menu={{ items }} trigger={['contextMenu']}>
    <article className={`su-proxy-card ${!config ? 'is-placeholder' : ''} ${selected ? 'is-selected' : ''}`} data-proxy-id={proxy.id} data-inbound-id={inbound?.id} aria-label={config ? proxy.name : `${server.name} 待配置入口`}>
      <Handle type="target" position={Position.Left} isConnectable={Boolean(config && server.chainTarget && !data.busy)} aria-label="入口引用目标" />
      <div className="su-proxy-card-heading"><span className={`su-protocol-icon protocol-${config?.protocol ?? 'draft'}`}><Icon name={config ? 'proxy' : 'plus'} size={19} /></span><div><h3>{config ? proxy.name : '创建节点'}</h3><span><Icon name="server" size={11} /> {server.name}</span></div><Dropdown menu={{ items }} trigger={['click']}><Button className="nodrag nopan" type="text" size="small" aria-label={`${proxy.name || server.name} 操作菜单`}>•••</Button></Dropdown></div>
      {config ? <>
        <div className="su-proxy-badges"><Tag color="blue">{protocolLabels[config.protocol]}</Tag><Tag>{config.exposure === 'internal' ? '内部入口' : '订阅入口'}</Tag><Tag>:{config.listenPort}</Tag></div>
        <p className="su-inbound-name">入口资源：{config.name}</p>
        <div className="su-card-egress"><Icon name="arrow" size={14} /><span>{data.egress}</span></div>
        {proxy.published && proxy.dirty && <small className="su-published-egress">当前模拟生效：{data.publishedEgress}</small>}
        <div className="su-proxy-status-row"><span className={`su-status su-status-${proxy.status}`}><span />{statusLabels[proxy.status]}</span>{proxy.dirty && <small>待应用</small>}</div>
        {proxy.failure && <p className="su-card-failure">{proxy.failure}</p>}
        <div className="su-proxy-card-actions nodrag nopan"><Button type="text" size="small" disabled={data.busy} onClick={() => data.onConfigure(proxy.id)}>编辑入口</Button><Button type="text" size="small" disabled={data.busy} onClick={() => data.onEgress(proxy.id)}>配置出站</Button></div>
        <small className="su-reference-count">入口被 {data.references} 个节点引用 · 不重复占用端口</small>
      </> : <><p className="su-placeholder-copy">一台服务器 + 一个入口 + 本机直出<br />即可完成单机闭环。</p><div className="su-placeholder-bottom nodrag nopan"><Button type="primary" size="small" disabled={data.busy} onClick={() => data.onConfigure(proxy.id)}>新建入口</Button>{data.available > 0 && <Button size="small" disabled={data.busy} onClick={() => data.onReuse(proxy.id)}>选择已有入口 ({data.available})</Button>}</div></>}
      <Handle type="source" position={Position.Right} isConnectable={Boolean(config && !data.busy)} aria-label="拖线引用下一跳入口" title="拖到已有入口，设置本节点出站" />
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
  const draftIdPrefix = useId();
  const nextDraftId = useRef(0);
  const [editorId, setEditorId] = useState<string>();
  const [egressId, setEgressId] = useState<string>();
  const [reuseId, setReuseId] = useState<string>();
  const [inboundId, setInboundId] = useState<string>();
  const [error, setError] = useState('');
  const [showAssets, setShowAssets] = useState(true);
  const [selectedId, setSelectedId] = useState<string>();
  const [draggingOver, setDraggingOver] = useState(false);
  const [applyIds, setApplyIds] = useState<string[]>();
  const focusOrigin = useRef<HTMLElement | null>(null);
  const dirty = state.proxies.filter(proxy => proxy.inboundId && proxy.dirty);
  const editing = state.proxies.find(proxy => proxy.id === editorId);
  const egressProxy = state.proxies.find(proxy => proxy.id === egressId);
  const reuseProxy = state.proxies.find(proxy => proxy.id === reuseId);

  const configure = useCallback((id: string) => {
    if (busy) return;
    focusOrigin.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setEditorId(id);
  }, [busy]);
  const restoreFocus = () => window.setTimeout(() => {
    if (focusOrigin.current?.isConnected) focusOrigin.current.focus();
    else canvas.current?.focus();
  }, 250);
  const openReuse = useCallback((id: string) => { setReuseId(id); setInboundId(undefined); }, []);
  const openEgress = useCallback((id: string) => setEgressId(id), []);
  const revertEgress = useCallback((id: string) => dispatch({ type: 'revert-egress', id }), [dispatch]);
  const fitNodes = () => window.setTimeout(() => void flow.fitView({ duration: 250, padding: 0.22, maxZoom: 1 }), 120);
  const addServer = (server: ServerAsset, position?: ProxyDraft['position']) => {
    if (busy || !server.online || !server.capabilitiesKnown) return;
    const id = `draft:${draftIdPrefix}:${nextDraftId.current++}`;
    const count = state.proxies.length;
    dispatch({ type: 'add', proxy: { id, serverId: server.id, name: '', egress: defaultEgress(), position: position ?? { x: 70 + (count % 2) * 380, y: 95 + Math.floor(count / 2) * 365 }, status: 'draft', dirty: false } });
    setSelectedId(id);
    setError('');
    if (!position) fitNodes();
  };
  const remove = useCallback((id: string) => {
    const proxy = state.proxies.find(item => item.id === id);
    if (!proxy || proxy.published || busy) return;
    if (!proxy.inboundId) { dispatch({ type: 'remove', id }); return; }
    modal.confirm({ title: '移除这个节点草稿？', content: '只移除节点，不删除独立入口资源；其它节点引用和下一跳保持不变。可再次拖入服务器选择该入口。', okText: '移除节点', cancelText: '保留', onOk: () => dispatch({ type: 'remove', id }) });
  }, [busy, dispatch, modal, state.proxies]);
  const disconnect = (id: string) => {
    const proxy = state.proxies.find(item => item.id === id);
    if (!proxy || busy) return;
    modal.confirm({ title: '将出站改为本机直出？', content: '只修改此节点出站草稿，不删除或锁定目标入口。', okText: '确认直出', cancelText: '保留连线', onOk: () => dispatch({ type: 'egress', id, name: proxy.name, egress: { ...proxy.egress, type: 'direct', tag: 'direct', targetInboundId: undefined } }) });
  };
  const connect = (connection: Connection) => {
    if (busy) return;
    const source = state.proxies.find(proxy => proxy.id === connection.source);
    const target = state.proxies.find(proxy => proxy.id === connection.target);
    const reason = connectionError(state.proxies, state.inbounds, connection.source, target?.inboundId ?? '');
    if (reason || !source) { setError(reason ?? '节点不存在'); return; }
    dispatch({ type: 'egress', id: source.id, name: source.name, egress: { ...source.egress, type: 'next-hop', tag: 'proxy_out', targetInboundId: target?.inboundId } });
    setError('');
    void message.info('已引用目标入口作为出站，请确认应用草稿。');
  };
  const nodes: ProxyFlowNode[] = useMemo(() => state.proxies.map(proxy => ({
    id: proxy.id, type: 'proxy', position: proxy.position, selected: proxy.id === selectedId,
    ariaLabel: `${proxy.name || getServer(proxy.serverId).name + ' 待配置入口'}，按 Enter 配置`,
    data: { proxy, inbound: getInbound(state.inbounds, proxy), references: inboundReferences(state.proxies, proxy.inboundId ?? '').length,
      available: state.inbounds.filter(inbound => inbound.serverId === proxy.serverId).length, busy,
      egress: egressSummary(proxy.egress, state.inbounds), publishedEgress: proxy.published ? egressSummary(proxy.published.egress, state.inbounds, true) : undefined,
      onConfigure: configure, onEgress: openEgress, onReuse: openReuse, onRevert: revertEgress, onRemove: remove },
  })), [state.proxies, state.inbounds, selectedId, busy, configure, openEgress, openReuse, revertEgress, remove]);
  const edges: Edge[] = state.proxies.flatMap(proxy => {
    if (proxy.egress.type !== 'next-hop') return [];
    const target = state.proxies.find(item => item.inboundId === proxy.egress.targetInboundId);
    if (!target) return [];
    return [{ id: `${proxy.id}->${target.id}`, source: proxy.id, target: target.id, type: 'smoothstep', markerEnd: { type: MarkerType.ArrowClosed, color: dark ? '#6b9eff' : '#0958d9' },
      label: '引用入口 · 下一跳', animated: busy && !window.matchMedia('(prefers-reduced-motion: reduce)').matches,
      style: { stroke: dark ? '#6b9eff' : '#0958d9', strokeWidth: 2, strokeDasharray: proxy.published?.egress.targetInboundId === proxy.egress.targetInboundId ? undefined : '6 4' },
      labelStyle: { fill: dark ? '#c2d4fa' : '#0958d9', fontSize: 11 }, labelBgStyle: { fill: dark ? '#23252b' : '#fff' } }];
  });
  const requestApply = (ids: string[]) => {
    const reason = applyError(ids);
    if (reason) { setError(reason); return; }
    setApplyIds(ids);
  };
  const save = (config: ProxyConfig, apply: boolean) => {
    if (!editing || busy) return;
    const action = { type: 'save' as const, id: editing.id, config };
    const next = workspaceReducer(state, action);
    if (next === state) { setError('保存被拒绝：请检查端口占用、容量或已发布协议锁定。'); return; }
    dispatch(action);
    setEditorId(undefined);
    fitNodes();
    restoreFocus();
    if (apply) {
      const reason = workspaceApplyError(next, [editing.id]);
      if (reason) { setError(`入口草稿已保存。${reason}`); return; }
      dispatch({ type: 'start', ids: [editing.id], fail: simulateFailure });
    } else void message.success('入口草稿已保存；共享引用同步更新，发布快照保持不变。');
  };
  const reset = () => modal.confirm({ title: '重置演示工作区？', content: '清空本次会话的合成入口和节点，不影响真实服务。', okText: '重置演示', cancelText: '保留', onOk: () => { dispatch({ type: 'reset' }); setEditorId(undefined); setError(''); } });

  return <div className="su-topology-page">
    <PageHeader title="拓扑编排" description="入口独立复用，节点引用入口并配置出站；一台服务器即可闭环。" actions={<><Button onClick={() => setShowAssets(value => !value)} icon={<Icon name="server" />}>{showAssets ? '收起资产' : '服务器资产'}</Button><Button disabled={busy || (!state.proxies.length && !state.inbounds.length)} onClick={reset} aria-label="重置演示工作区">重置</Button><Button type="primary" disabled={busy || !dirty.length} onClick={() => requestApply(dirty.map(proxy => proxy.id))}>应用更改{dirty.length ? ` (${dirty.length})` : ''}</Button></>} />
    <div className="su-prototype-banner"><span className="su-demo-pill">SANDBOX</span><p>R1 合成原型 · 仅 full agent · NAT / 轻量 agent 后续再议。刷新清空工作区，不修改服务器。</p><span>等待用户确认，不进入 R2</span></div>
    {error && <Alert className="su-workspace-error" type="warning" showIcon title={error} closable onClose={() => setError('')} />}
    <div className={`su-workspace ${showAssets ? '' : 'assets-hidden'} ${editing ? 'has-editor' : ''}`}>
      {showAssets && <ServerAssetPanel proxies={state.proxies} inbounds={state.inbounds} busy={busy} onAdd={server => addServer(server)} onLocate={id => { setSelectedId(id); void flow.fitView({ nodes: [{ id }], duration: 300, maxZoom: 1, padding: 0.4 }); }} />}
      <div className={`su-canvas ${draggingOver ? 'is-drag-over' : ''}`} ref={canvas} role="region" aria-label="拓扑画布" tabIndex={0}
        onKeyDown={event => { const node = (event.target as HTMLElement).closest('.react-flow__node'); if (event.key === 'Enter' && node && event.target === node && node.getAttribute('data-id')) configure(node.getAttribute('data-id')!); }}
        onDragOver={event => { if (!busy && Array.from(event.dataTransfer.types).includes(SERVER_DRAG_TYPE)) { event.preventDefault(); event.dataTransfer.dropEffect = 'copy'; setDraggingOver(true); } }}
        onDragLeave={event => { if (!(event.relatedTarget instanceof globalThis.Node) || !event.currentTarget.contains(event.relatedTarget)) setDraggingOver(false); }}
        onDrop={event => { event.preventDefault(); setDraggingOver(false); if (busy || !Array.from(event.dataTransfer.types).includes(SERVER_DRAG_TYPE)) return; const server = serverAssets.find(asset => asset.id === event.dataTransfer.getData(SERVER_DRAG_TYPE)); if (server) { const position = flow.screenToFlowPosition({ x: event.clientX, y: event.clientY }); addServer(server, { x: position.x - 159, y: position.y - 38 }); } }}>
        <div className="su-canvas-label"><span className="su-live-dot" />默认工作区 <span>/</span><span>{state.inbounds.length} 个入口资源 · {state.proxies.length} 个节点 · {state.proxies.filter(proxy => proxy.egress.type === 'next-hop').length} 个下一跳引用</span></div>
        <ReactFlow<ProxyFlowNode> nodes={nodes} edges={edges} nodeTypes={nodeTypes} colorMode={dark ? 'dark' : 'light'} minZoom={0.25} maxZoom={1.6} deleteKeyCode={null} zoomOnDoubleClick={false} nodesConnectable={!busy}
          onNodesChange={changes => { const positions = changes.flatMap(change => change.type === 'position' && change.position ? [{ id: change.id, position: change.position }] : []); if (positions.length) dispatch({ type: 'move', positions }); }}
          onNodeClick={(_event, node) => setSelectedId(node.id)} onNodeDoubleClick={(_event, node) => configure(node.id)} onPaneClick={() => setSelectedId(undefined)} onConnect={connect} onEdgeDoubleClick={(_event, edge) => disconnect(edge.source)}
          ariaLabelConfig={{ 'controls.zoomIn.ariaLabel': '放大画布', 'controls.zoomOut.ariaLabel': '缩小画布', 'controls.fitView.ariaLabel': '适配全部入口', 'controls.interactive.ariaLabel': '切换画布交互' }}>
          <Background gap={22} size={1} color={dark ? '#3c3e49' : '#d5dae3'} /><Controls showInteractive={false} position="bottom-left" />
        </ReactFlow>
        {!state.proxies.length && <div className="su-canvas-empty"><div className="su-empty-illustration"><span><Icon name="server" size={30} /></span><div>→</div><span><Icon name="proxy" size={29} /></span></div><Tag color="blue">一台服务器即可完成</Tag><h2>一个入口，多种出站选择。</h2><p>拖入 HK-zouter，新建或复用已有入口。<br />本机直出即可完成节点，不需要第二台机器。</p><Button type="primary" onClick={() => addServer(serverAssets[0])}>添加 HK-zouter 到画布</Button><small>同一入口可被多个节点和下一跳引用</small></div>}
        <div className="su-canvas-legend"><span><i className="is-draft" />草稿</span><span><i className="is-active" />已生效</span><span className="su-muted">均为模拟 · 连线仅引用目标入口</span></div>
      </div>
      {editing && <div className="su-editor-panel"><ProxyConfigForm key={editing.id} proxy={editing} inbounds={state.inbounds} referenceCount={inboundReferences(state.proxies, editing.inboundId ?? '', true).length} egressLabel={egressSummary(editing.egress, state.inbounds)} onSave={save} onClose={() => { setEditorId(undefined); restoreFocus(); }} /></div>}
    </div>
    <footer className="su-deployment-bar" aria-live="polite"><div className="su-deployment-summary"><span className="su-deployment-icon"><Icon name="topology" /></span><div><strong>{busy ? '正在模拟部署' : state.operation?.phase === 'failed' ? '模拟应用失败，草稿已保留' : state.operation?.phase === 'active' ? '模拟部署完成' : '入口与出站就绪后，确认应用'}</strong><small>不代表真实 ACK；共享监听分流策略留待 R2 验证</small></div></div><div className="su-deployment-progress"><div><span>01 准备</span><span>02 应用</span><span>03 已生效</span></div><Progress percent={state.operation?.phase === 'active' ? 100 : state.operation?.phase === 'applying' || state.operation?.phase === 'failed' ? 66 : state.operation ? 25 : 0} status={state.operation?.phase === 'failed' ? 'exception' : busy ? 'active' : undefined} showInfo={false} size="small" /></div><div className="su-deployment-options"><label><Switch size="small" checked={simulateFailure} disabled={busy} onChange={setSimulateFailure} aria-label="模拟应用失败" /> 模拟失败</label></div></footer>
    <Modal title="选择已有入口" open={Boolean(reuseProxy)} onCancel={() => setReuseId(undefined)} okText="引用入口创建节点" cancelText="取消" okButtonProps={{ disabled: !inboundId }} onOk={() => { if (reuseProxy && inboundId) { dispatch({ type: 'reuse', id: reuseProxy.id, inboundId }); setReuseId(undefined); fitNodes(); } }}>
      <p>只创建节点引用，不复制监听、端口或材料。随后可以为这个节点单独配置出站。</p>
      <Select aria-label="选择服务器已有入口" style={{ width: '100%' }} value={inboundId} onChange={setInboundId} options={state.inbounds.filter(inbound => inbound.serverId === reuseProxy?.serverId).map(inbound => ({ value: inbound.id, label: `${inbound.config.name} · ${protocolLabels[inbound.config.protocol]} :${inbound.config.listenPort} · ${inboundReferences(state.proxies, inbound.id).length} 个引用` }))} />
    </Modal>
    {egressProxy && <EgressConfigForm key={egressProxy.id} proxy={egressProxy} inbounds={state.inbounds} proxies={state.proxies} onClose={() => setEgressId(undefined)} onSave={(name, egress) => { dispatch({ type: 'egress', id: egressProxy.id, name, egress }); setEgressId(undefined); setError(''); fitNodes(); }} />}
    <Modal title="确认应用合成变更" open={Boolean(applyIds)} onCancel={() => setApplyIds(undefined)} okText="确认模拟应用" cancelText="继续编辑" onOk={() => { if (applyIds) { const reason = applyError(applyIds); if (reason) setError(reason); else dispatch({ type: 'start', ids: applyIds, fail: simulateFailure }); setApplyIds(undefined); } }}>
      <Alert type="info" showIcon title="只模拟节点出站意图与共享入口发布，不调用后端。" /><ul className="su-apply-list">{state.proxies.filter(proxy => applyIds?.includes(proxy.id)).map(proxy => <li key={proxy.id}><strong>{proxy.name}</strong><span>{getServer(proxy.serverId).name} · 入口 :{getInbound(state.inbounds, proxy)?.config.listenPort} · {egressSummary(proxy.egress, state.inbounds)}</span></li>)}</ul><p className="su-muted">共享入口只保留一份监听资源。多节点不同出站的真实身份/路由分流、服务器聚合和 ACK 均留待 R2。</p>
    </Modal>
  </div>;
}

export default function TopologyWorkspace() {
  return <ReactFlowProvider><WorkspaceCanvas /></ReactFlowProvider>;
}
