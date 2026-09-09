import { useState } from 'react';
import { Button, Input, Tag, Tooltip } from 'antd';
import { Icon } from './Icon';
import { filterServers, protocolLabels, serverStatusLabels } from './model';
import { ServerStatusBadge, ServerTags } from './ServerPresentation';
import { useWorkspace } from './WorkspaceProvider';
import type { InboundResource, ProxyDraft, ServerAsset } from './model';

export const SERVER_DRAG_TYPE = 'application/sing-ui-server';

export default function ServerAssetPanel({ proxies, inbounds, busy, onAdd, onLocate }: { proxies: ProxyDraft[]; inbounds: InboundResource[]; busy: boolean; onAdd: (server: ServerAsset) => void; onLocate: (id: string) => void }) {
  const [query, setQuery] = useState('');
  const { state: { servers } } = useWorkspace();
  const filtered = filterServers(servers, { query, status: 'all', tag: '', region: '', sort: 'name' });
  return <aside className="su-assets" aria-label="服务器资产">
    <div className="su-assets-heading"><h2>服务器资产 <span>{servers.length}</span></h2><Tag>合成</Tag></div>
    <p className="su-muted">拖入链编辑器 · 填入选中的链跳</p>
    <Input allowClear aria-label="搜索服务器" placeholder="搜索名称、IP、标签…" value={query} onChange={event => setQuery(event.target.value)} />
    <div className="su-asset-list">{filtered.map(server => {
      const owned = proxies.filter(proxy => proxy.chain.some(hop => hop.serverId === server.id));
      const inboundCount = inbounds.filter(inbound => inbound.serverId === server.id).length;
      const disabled = busy || server.status !== 'online' || !server.capabilitiesKnown;
      const reason = server.status !== 'online' ? `服务器${serverStatusLabels[server.status]}，暂不可填入` : !server.capabilitiesKnown ? '能力未知，暂不可填入' : '填入当前选中的链跳；复用入口不占新端口或入口容量';
      return <article key={server.id} className={`su-asset ${disabled ? 'is-unavailable' : ''}`} data-server-id={server.id} draggable={!disabled} onDragStart={event => {
        if (disabled) { event.preventDefault(); return; }
        event.dataTransfer.setData(SERVER_DRAG_TYPE, server.id);
        event.dataTransfer.effectAllowed = 'link';
      }}>
        <div className="su-asset-title"><span className="su-region-icon">{server.country}</span><div><strong>{server.name}</strong><small>{server.region} · {server.memory}</small></div><span className="su-drag-grip"><Icon name="grip" size={15} /></span></div>
        <div className="su-asset-tags"><Tag color="blue">full</Tag><ServerStatusBadge status={server.status} /></div>
        <ServerTags tags={server.tags} />
        <div className="su-capabilities">{server.capabilitiesKnown ? server.protocols.map(protocol => protocolLabels[protocol]).join(' / ') : '能力未知 · 不予放行'}</div>
        <div className="su-asset-bottom"><span>{inboundCount} / {server.maxProxies || '—'} 个入口 · {owned.length} 条链引用</span><Tooltip title={reason}><Button size="small" disabled={disabled} aria-label={`将 ${server.name} 填入选中的链跳`} icon={<Icon name="plus" size={13} />} onClick={() => onAdd(server)}>填入链跳</Button></Tooltip></div>
        {owned.length > 0 && <div className="su-owned-proxies">{owned.map(proxy => <button key={proxy.id} onClick={() => onLocate(proxy.id)}><span>↳</span> {proxy.name || '待配置代理链'} <Icon name="chevron" size={11} /></button>)}</div>}
      </article>;
    })}{!filtered.length && <div className="su-inline-empty">没有匹配的服务器</div>}</div>
    <div className="su-asset-hint"><Icon name="help" size={16} /><p>选择一条链后再填入服务器；每个链跳只能按入口 → 中转 → 出站顺序编辑，不支持自由连线。</p></div>
  </aside>;
}
