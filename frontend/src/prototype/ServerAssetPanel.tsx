import { useState } from 'react';
import { Button, Input, Tag, Tooltip } from 'antd';
import { Icon } from './Icon';
import { protocolLabels, serverAssets } from './model';
import type { ProxyDraft, ServerAsset } from './model';

export const SERVER_DRAG_TYPE = 'application/sing-ui-server';

export default function ServerAssetPanel({ proxies, busy, onAdd, onLocate }: { proxies: ProxyDraft[]; busy: boolean; onAdd: (server: ServerAsset) => void; onLocate: (id: string) => void }) {
  const [query, setQuery] = useState('');
  const filtered = serverAssets.filter(server => `${server.name} ${server.region} ${server.profile}`.toLowerCase().includes(query.toLowerCase()));
  return <aside className="su-assets" aria-label="服务器资产">
    <div className="su-assets-heading"><h2>服务器资产 <span>{serverAssets.length}</span></h2><Tag>合成</Tag></div>
    <p className="su-muted">拖到画布 · 创建新的代理入口</p>
    <Input allowClear aria-label="搜索服务器" placeholder="搜索名称、地区…" value={query} onChange={event => setQuery(event.target.value)} />
    <div className="su-asset-list">{filtered.map(server => {
      const owned = proxies.filter(proxy => proxy.serverId === server.id);
      const disabled = busy || !server.online || !server.capabilitiesKnown || owned.length >= server.maxProxies;
      const reason = !server.online ? '服务器离线，能力未知，暂不可创建' : owned.length >= server.maxProxies ? '达到演示容量上限，请先删除未发布草稿' : '添加到画布';
      return <article key={server.id} className={`su-asset ${disabled ? 'is-unavailable' : ''}`} data-server-id={server.id} draggable={!disabled} onDragStart={event => {
        if (disabled) { event.preventDefault(); return; }
        event.dataTransfer.setData(SERVER_DRAG_TYPE, server.id);
        event.dataTransfer.effectAllowed = 'copy';
      }}>
        <div className="su-asset-title"><span className="su-region-icon">{server.country}</span><div><strong>{server.name}</strong><small>{server.region} · {server.memory}</small></div><span className="su-drag-grip"><Icon name="grip" size={15} /></span></div>
        <div className="su-asset-tags"><Tag color={server.profile === 'full' ? 'blue' : 'purple'}>{server.profile}</Tag>{server.network === 'nat' && <Tag color="orange">NAT</Tag>}<span className={`su-status ${server.online ? 'su-status-active' : 'su-status-draft'}`}><span />{server.online ? '在线 · 模拟' : '离线 · 模拟'}</span></div>
        <div className="su-capabilities">{server.capabilitiesKnown ? server.protocols.map(protocol => protocolLabels[protocol]).join(' / ') : '能力未知 · 不予放行'}</div>
        {server.profile === 'lite' && <small className="su-muted">仅 TCP · direct-only · 客户分配未验证</small>}
        <div className="su-asset-bottom"><span>{owned.length} / {server.maxProxies || '—'} 个入口</span><Tooltip title={reason}><Button size="small" disabled={disabled} aria-label={`添加 ${server.name} 到画布`} icon={<Icon name="plus" size={13} />} onClick={() => onAdd(server)}>添加</Button></Tooltip></div>
        {owned.length > 0 && <div className="su-owned-proxies">{owned.map(proxy => <button key={proxy.id} onClick={() => onLocate(proxy.id)}><span>↳</span> {proxy.config?.name ?? '待配置入口'} <Icon name="chevron" size={11} /></button>)}</div>}
      </article>;
    })}{!filtered.length && <div className="su-inline-empty">没有匹配的服务器</div>}</div>
    <div className="su-asset-hint"><Icon name="help" size={16} /><p>同一台服务器可以多次拖入，每次创建独立入口。</p></div>
  </aside>;
}
