import { useState } from 'react';
import { Alert, Button, Empty, Modal, Progress, Switch } from 'antd';
import { useSearchParams } from 'react-router-dom';
import { PageHeader } from './AppShell';
import ChainEditor from './ChainEditor';
import EgressConfigForm from './EgressConfigForm';
import { Icon } from './Icon';
import { chainSummary, defaultEgress, egressSummary, getInbound, inboundReferences, protocolLabels, statusLabels } from './model';
import type { ChainHop, ProxyConfig, ProxyDraft, ServerAsset } from './model';
import ProxyConfigForm from './ProxyConfigForm';
import ServerAssetPanel from './ServerAssetPanel';
import { useWorkspace, workspaceApplyError, workspaceReducer } from './WorkspaceProvider';
import type { WorkspaceAction } from './WorkspaceProvider';

export default function TopologyWorkspace() {
  const { state, busy, dispatch, simulateFailure, setSimulateFailure, applyError } = useWorkspace();
  const [searchParams, setSearchParams] = useSearchParams();
  const [selectedPosition, setSelectedPosition] = useState(0);
  const [editing, setEditing] = useState<ChainHop>();
  const [egressOpen, setEgressOpen] = useState(false);
  const [applyIds, setApplyIds] = useState<string[]>();
  const [error, setError] = useState('');
  const [assetsVisible, setAssetsVisible] = useState(true);
  const [modal, modalContext] = Modal.useModal();
  const selected = state.proxies.find(proxy => proxy.id === searchParams.get('chain')) ?? state.proxies[0];
  const locked = busy || Boolean(editing) || egressOpen || Boolean(applyIds);
  const position = Math.min(selectedPosition, Math.max(0, (selected?.chain.length ?? 1) - 1));
  const selectChain = (id: string) => { setSearchParams({ chain: id }); setSelectedPosition(0); setError(''); };
  const createChain = (server?: ServerAsset) => {
    if (locked) return;
    const proxy: ProxyDraft = { id: crypto.randomUUID(), name: `代理链 ${state.proxies.length + 1}`, chain: [{ position: 0, serverId: server?.id ?? '' }], egress: defaultEgress(), status: 'draft', dirty: true };
    dispatch({ type: 'add', proxy });
    selectChain(proxy.id);
  };
  const setServer = (hopPosition: number, server: ServerAsset) => {
    if (locked) return;
    if (server.status !== 'online' || !server.capabilitiesKnown || (hopPosition > 0 && !server.chainTarget)) { setError('服务器离线、能力未知或不支持中转，无法填入此跳。'); return; }
    if (!selected) { createChain(server); return; }
    dispatch({ type: 'select-server', id: selected.id, position: hopPosition, serverId: server.id });
    setSelectedPosition(hopPosition);
    setError('');
  };
  const requestApply = (ids: string[]) => {
    const reason = applyError(ids);
    if (reason) { setError(reason); return; }
    setError('');
    setApplyIds(ids);
  };
  const saveHop = (config: ProxyConfig, apply: boolean) => {
    if (!selected || !editing) return;
    const action: WorkspaceAction = { type: 'save-hop', id: selected.id, position: editing.position, config, resourceId: editing.inboundId ?? crypto.randomUUID() };
    const next = workspaceReducer(state, action);
    if (next === state) { setError('入站未保存：请检查同机端口冲突、服务器状态或入口容量。'); return; }
    dispatch(action);
    setEditing(undefined);
    setError('');
    if (apply) {
      const reason = workspaceApplyError(next, [selected.id]);
      if (reason) setError(`该跳已保存。${reason}`);
      else setApplyIds([selected.id]);
    }
  };
  return <div className="su-topology-page su-linear-page">
    {modalContext}
    <PageHeader title="代理节点链编辑器" description="一个代理节点 = 一条链：入口 → 中转（可选）→ 出站。不分叉。" actions={<><Button disabled={locked} onClick={() => setAssetsVisible(!assetsVisible)}>{assetsVisible ? '收起资产' : '显示资产'}</Button><Button disabled={locked || !state.proxies.some(proxy => proxy.dirty)} onClick={() => requestApply(state.proxies.map(proxy => proxy.id))}>应用全部更改</Button><Button type="primary" disabled={locked} icon={<Icon name="plus" />} onClick={() => createChain()}>新建代理节点</Button></>} />
    <div className="su-prototype-banner"><span><strong>R1 合成原型</strong> · 刷新清空，仅模拟整条链部署</span><span>不接后端 · 不生成真实订阅 · 停在 R1 等你确认</span></div>
    {error && <Alert className="su-workspace-error" type="error" showIcon closable onClose={() => setError('')} title={error} />}
    <div className={`su-workspace su-linear-workspace ${!assetsVisible ? 'assets-hidden' : ''} ${editing ? 'has-editor' : ''}`}>
      {assetsVisible && <ServerAssetPanel proxies={state.proxies} inbounds={state.inbounds} busy={locked} onAdd={server => setServer(position, server)} onLocate={id => { if (!locked) selectChain(id); }} />}
      <div className="su-chain-content">
        <nav className="su-chain-list" aria-label="代理节点链列表"><strong>代理节点 <span>{state.proxies.length}</span></strong><div>{state.proxies.map(proxy => <button key={proxy.id} disabled={locked} aria-current={selected?.id === proxy.id ? 'true' : undefined} onClick={() => selectChain(proxy.id)}><span><Icon name="proxy" size={15} />{proxy.name || '未命名链'}</span><small>{chainSummary(proxy, state.servers)}</small><span className={`su-status su-status-${proxy.status}`}><span />{statusLabels[proxy.status]}</span></button>)}</div></nav>
        {selected ? <><ChainEditor proxy={selected} selectedPosition={position} locked={locked} onSelect={setSelectedPosition} onServer={setServer} onConfigure={(hop, create) => { setSelectedPosition(hop.position); setEditing(create ? { position: hop.position, serverId: hop.serverId } : { ...hop }); }} onEgress={() => setEgressOpen(true)} onApply={() => requestApply([selected.id])} /><div className="su-chain-bottom"><span>资产面板「填入链跳」作用于第 {position + 1} 跳。拖动服务器只能填入指定卡片。</span><Button danger size="small" disabled={locked || Boolean(selected.published)} title={selected.published ? 'R1 不模拟真实下线，仅可删除未发布草稿链' : undefined} onClick={() => modal.confirm({ title: '删除这条草稿链？', content: '只删除链条；已有入站资源保留，可供其他链复用。', okText: '删除草稿链', cancelText: '取消', onOk: () => { dispatch({ type: 'remove', id: selected.id }); setSelectedPosition(0); } })}>删除草稿链</Button></div></> : <div className="su-chain-empty"><Empty description="一个代理节点 = 一条链：入口 → 中转（可选）→ 出站"><p>一台服务器、一个入口、本机直出，即可完成一个节点。<br />多节点分别编辑；同机入站可以跨链复用。</p><Button type="primary" onClick={() => createChain()}>创建第一条代理链</Button></Empty></div>}
      </div>
      {editing && selected && <div className="su-editor-panel"><ProxyConfigForm key={`${selected.id}-${editing.position}-${editing.inboundId ?? 'new'}`} hop={editing} inbounds={state.inbounds} referenceCount={inboundReferences(state.proxies, editing.inboundId ?? '', true).length} egressLabel={egressSummary(selected.egress)} onSave={saveHop} onClose={() => setEditing(undefined)} /></div>}
    </div>
    <footer className="su-deployment-bar" aria-live="polite"><div className="su-deployment-summary"><span className="su-deployment-icon"><Icon name="topology" /></span><div><strong>{busy ? '正在模拟部署整条链' : state.operation?.phase === 'active' ? '整条链模拟部署完成' : state.operation?.phase === 'failed' ? '模拟失败，草稿保留，可重试' : '每跳就绪后，确认应用整条链'}</strong><small>共享入站只保存一份；真实分流、聚合和 ACK 留待 R2</small></div></div><div className="su-deployment-progress"><div><span>01 准备</span><span>02 应用</span><span>03 已生效</span></div><Progress percent={state.operation?.phase === 'active' ? 100 : state.operation?.phase === 'applying' || state.operation?.phase === 'failed' ? 66 : state.operation ? 25 : 0} status={state.operation?.phase === 'failed' ? 'exception' : busy ? 'active' : undefined} showInfo={false} size="small" /></div><div className="su-deployment-options"><label><Switch size="small" checked={simulateFailure} disabled={locked} onChange={setSimulateFailure} aria-label="模拟应用失败" />模拟失败</label></div></footer>
    {egressOpen && selected && <EgressConfigForm key={selected.id} proxy={selected} onClose={() => setEgressOpen(false)} onSave={(name, egress) => { dispatch({ type: 'egress', id: selected.id, name, egress }); setEgressOpen(false); setError(''); }} />}
    <Modal title="确认应用完整代理链 · 合成模拟" open={Boolean(applyIds)} onCancel={() => setApplyIds(undefined)} okText="确认模拟应用" cancelText="继续编辑" onOk={() => { if (applyIds) { const reason = applyError(applyIds); if (reason) setError(reason); else dispatch({ type: 'start', ids: applyIds, fail: simulateFailure }); setApplyIds(undefined); } }}><Alert type="info" showIcon title="每条链作为一个节点发布，所有入站跳一起校验。" /><ul className="su-apply-list">{state.proxies.filter(proxy => applyIds?.includes(proxy.id)).map(proxy => <li key={proxy.id}><strong>{proxy.name}</strong><span>{chainSummary(proxy, state.servers)}</span>{proxy.chain.map(hop => {
      const inbound = getInbound(state.inbounds, hop);
      return <span key={hop.position}>第 {hop.position + 1} 跳 · {state.servers.find(server => server.id === hop.serverId)?.name} · {inbound ? `${protocolLabels[inbound.config.protocol]} :${inbound.config.listenPort} · ${inboundReferences(state.proxies, inbound.id, true).length} 条链引用` : '待配置入站'}</span>;
    })}</li>)}</ul><p className="su-muted">不调用后端、不代表真实 ACK；共享监听的身份与路由分流仍需 R2 验证。失败时旧模拟发布不变。</p></Modal>
  </div>;
}
