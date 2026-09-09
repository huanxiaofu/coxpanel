import { Alert, Button, Input, Select, Tag } from 'antd';
import { useEffect, useRef } from 'react';
import type { DragEvent } from 'react';
import { Icon } from './Icon';
import { chainReadiness, egressSummary, getInbound, inboundReferences, protocolLabels, statusLabels } from './model';
import type { ChainHop, ProxyDraft, ServerAsset } from './model';
import { SERVER_DRAG_TYPE } from './ServerAssetPanel';
import { useWorkspace } from './WorkspaceProvider';

interface ChainEditorProps {
  proxy: ProxyDraft;
  selectedPosition: number;
  locked: boolean;
  onSelect: (position: number) => void;
  onServer: (position: number, server: ServerAsset) => void;
  onConfigure: (hop: ChainHop, create: boolean) => void;
  onEgress: () => void;
  onApply: () => void;
}

export default function ChainEditor({ proxy, selectedPosition, locked, onSelect, onServer, onConfigure, onEgress, onApply }: ChainEditorProps) {
  const { state, dispatch } = useWorkspace();
  const selectedHop = useRef<HTMLLIElement>(null);
  useEffect(() => {
    selectedHop.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [selectedPosition, proxy.id, proxy.chain.length]);
  const reason = chainReadiness(proxy, state.inbounds, state.servers);
  const dropServer = (event: DragEvent, position: number) => {
    event.preventDefault();
    if (locked) return;
    const server = state.servers.find(candidate => candidate.id === event.dataTransfer.getData(SERVER_DRAG_TYPE));
    if (server) onServer(position, server);
  };
  const lastServer = state.servers.find(server => server.id === proxy.chain.at(-1)?.serverId);
  return <section className="su-chain-editor" aria-label="单条代理链编辑区">
    <div className="su-chain-heading"><div><Tag color="blue">代理节点 = 出口 · 一个节点一条链</Tag><h2>入口 → 中转 → 出口（代理节点）</h2><p>最后一跳就是出口，客户端节点名即出口名；首跳仍是订阅连接入口。不分叉，不合并。</p></div><span className={`su-status su-status-${proxy.status}`}><span />{statusLabels[proxy.status]}</span></div>
    <div className="su-chain-toolbar"><label htmlFor="chain-name">出口名称（代理节点 / 链名）<Input id="chain-name" value={proxy.name} disabled={locked} maxLength={80} onChange={event => dispatch({ type: 'rename', id: proxy.id, name: event.target.value })} /></label><Button disabled={locked} icon={<Icon name="plus" />} onClick={() => { dispatch({ type: 'append-hop', id: proxy.id }); onSelect(proxy.chain.length); }}>追加一跳</Button><Button type="primary" disabled={locked || !proxy.dirty} onClick={onApply}>{proxy.published ? '应用当前链' : '创建并应用整条链'}</Button></div>
    <div className="su-chain-scroll" tabIndex={0} role="region" aria-label="横向线性卡片链，可横向滚动">
      <ol className="su-chain-track" aria-label="入口到出站的有序链">
        {proxy.chain.map((hop, position) => {
          const inbound = getInbound(state.inbounds, hop);
          const server = state.servers.find(candidate => candidate.id === hop.serverId);
          const available = state.inbounds.filter(resource => resource.serverId === hop.serverId);
          const isEgress = position === proxy.chain.length - 1;
          const hopLabel = isEgress ? '出口（代理节点）' : position === 0 ? '入口 · 订阅入口' : `中转 ${position}`;
          return <li key={position} ref={selectedPosition === position ? selectedHop : undefined} className="su-chain-step" data-hop-position={position}>
            <article className={`su-chain-hop ${isEgress ? 'is-egress' : ''} ${selectedPosition === position ? 'is-selected' : ''}`} onDragOver={event => { if (!locked && event.dataTransfer.types.includes(SERVER_DRAG_TYPE)) event.preventDefault(); }} onDrop={event => dropServer(event, position)} aria-label={`第 ${position + 1} 跳${hopLabel}`}>
              <div className="su-chain-hop-heading"><button disabled={locked} aria-pressed={selectedPosition === position} onClick={() => onSelect(position)}><span>{String(position + 1).padStart(2, '0')}</span>{hopLabel}</button><Tag color={isEgress ? 'green' : undefined}>{inbound ? '已选入站' : '待配置'}</Tag></div>
              {isEgress && <p className="su-chain-egress-note">{position === 0 ? '单跳：同时作为订阅入口与出口。' : '复用此服务器的已有入站作为出口，无需重复创建监听。'}</p>}
              <label>服务器<Select aria-label={`第 ${position + 1} 跳服务器`} showSearch optionFilterProp="label" placeholder="选择或拖入服务器" value={hop.serverId || undefined} disabled={locked} onChange={serverId => { const target = state.servers.find(candidate => candidate.id === serverId); if (target) onServer(position, target); }} options={state.servers.map(candidate => ({ value: candidate.id, label: candidate.name, disabled: candidate.status !== 'online' || !candidate.capabilitiesKnown || (position > 0 && !candidate.chainTarget) }))} /></label>
              <div className="su-chain-inbound-summary"><Icon name="server" size={23} /><div><strong>{server?.name ?? '将资产拖到这一跳'}</strong><small>{inbound ? `${protocolLabels[inbound.config.protocol]} · :${inbound.config.listenPort}` : '先选服务器，再复用或新建入站'}</small></div></div>
              <label>{isEgress ? '选择已有入站作为出口' : '复用已有入站'}<Select aria-label={`第 ${position + 1} 跳已有入站`} showSearch optionFilterProp="label" placeholder={available.length ? '选择已有入站' : '此服务器尚无入站'} value={inbound?.id} disabled={locked || !server} onChange={inboundId => dispatch({ type: 'reuse-hop', id: proxy.id, position, inboundId })} options={available.map(resource => {
                const duplicate = proxy.chain.some(candidate => candidate.position !== position && candidate.inboundId === resource.id);
                const internal = position === 0 && resource.config.exposure !== 'subscription';
                return { value: resource.id, disabled: duplicate || internal, label: `${resource.config.name} · ${protocolLabels[resource.config.protocol]} :${resource.config.listenPort}${duplicate ? '（本链已使用）' : internal ? '（仅内部入口）' : ''}` };
              })} /></label>
              <small className="su-chain-resource-note">{inbound ? `被 ${inboundReferences(state.proxies, inbound.id).length} 条链复用 · ${inbound.config.materialReady ? '合成材料已就绪' : '材料待配置'}` : '复用不复制监听，也不重复占用端口。'}</small>
              <div className="su-chain-hop-actions"><Button size="small" disabled={locked || !server} onClick={() => onConfigure(hop, true)} aria-label={`第 ${position + 1} 跳新建入口`}>新建入口</Button><Button size="small" disabled={locked || !inbound} onClick={() => onConfigure(hop, false)} aria-label={`第 ${position + 1} 跳配置入站`}>配置入站</Button></div>
              <div className="su-chain-reorder"><Button size="small" disabled={locked || position === 0} aria-label={`第 ${position + 1} 跳左移`} onClick={() => { dispatch({ type: 'move-hop', id: proxy.id, position, direction: -1 }); onSelect(position - 1); }}>← 左移</Button><Button size="small" disabled={locked || position === proxy.chain.length - 1} aria-label={`第 ${position + 1} 跳右移`} onClick={() => { dispatch({ type: 'move-hop', id: proxy.id, position, direction: 1 }); onSelect(position + 1); }}>右移 →</Button><Button size="small" danger disabled={locked || proxy.chain.length === 1} aria-label={`删除第 ${position + 1} 跳`} onClick={() => { dispatch({ type: 'remove-hop', id: proxy.id, position }); onSelect(Math.max(0, position - 1)); }}>删除</Button></div>
            </article><span className="su-chain-arrow" aria-hidden="true">⟶</span>
          </li>;
        })}
        <li className="su-chain-step"><article className="su-chain-hop su-chain-terminal" aria-label="唯一出站"><Tag color="green">出口的出站配置 · 唯一</Tag><span className="su-chain-terminal-icon"><Icon name="proxy" size={28} /></span><h3>{egressSummary(proxy.egress)}</h3><p>出口名：{proxy.name || '未命名出口'}<br />由 {lastServer?.name ?? '最后一跳服务器'} 出网</p><Button disabled={locked} onClick={onEgress}>配置出站</Button><Button disabled>指定外部出口 · 后续支持</Button><small>这是末跳的出网配置，不是额外一跳。<br />最短链：单台服务器兼入口与出口。</small></article></li>
      </ol>
    </div>
    <Alert type={reason ? 'warning' : 'success'} showIcon title={reason ? '链完整性待完善' : '链完整性检查通过'} description={reason ?? `${proxy.chain.length} 跳入站 → 唯一出站；第一跳作为订阅入口。仅模拟部署。`} />
    {proxy.failure && <Alert type="error" showIcon title={proxy.failure} />}
    {proxy.published && <div className="su-chain-published"><span>已有模拟发布 · {proxy.published.chain.length} 跳；编辑草稿不改变旧发布。恢复链不会撤销共享入站的编辑。</span><Button size="small" disabled={locked || !proxy.dirty} onClick={() => dispatch({ type: 'revert-chain', id: proxy.id })}>恢复已发布链</Button></div>}
  </section>;
}
