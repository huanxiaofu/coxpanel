import { Empty, Tag } from 'antd';
import { Link } from 'react-router-dom';
import { egressSummary, getServerEgressRefs, getServerInbounds, protocolLabels, statusLabels } from './model';
import type { EgressConfig, ProxyConfig, ServerAsset, ServerChainReference } from './model';
import { useWorkspace } from './WorkspaceProvider';

function ReferenceState({ reference }: { reference: ServerChainReference }) {
  return <span className="su-server-ref-state">
    {reference.draft && <Tag>当前草稿{reference.dirty ? ' · 待应用' : ''}</Tag>}
    {reference.published && <Tag color="green">已发布 · 模拟</Tag>}
    <span className={`su-status su-status-${reference.status}`}><span />链状态：{statusLabels[reference.status]}</span>
  </span>;
}

function ReferenceLink({ reference }: { reference: ServerChainReference }) {
  return <div className="su-server-ref-name"><Link to={`/prototype/topology?chain=${encodeURIComponent(reference.proxyId)}`}>{reference.name || '未命名链'}</Link>
    {reference.publishedName && reference.publishedName !== reference.name && <small>已发布链名：{reference.publishedName}</small>}
    {!reference.draft && <small>仅旧发布仍使用此服务器资源；跳转查看当前草稿。</small>}
  </div>;
}

function InboundSummary({ config }: { config: ProxyConfig }) {
  return <div className="su-server-inbound-meta"><Tag color="blue">{protocolLabels[config.protocol]}</Tag><span>端口 <code>{config.listenPort}</code></span><span>SNI <code>{config.sni || '未配置 / 不适用'}</code></span><Tag>{config.exposure === 'subscription' ? '订阅入口' : '内部入口'}</Tag></div>;
}

function EgressSummary({ egress, label, server }: { egress: EgressConfig; label: string; server: ServerAsset }) {
  return <div className="su-server-egress-config"><h4>{label}</h4><dl className="su-server-facts">
    <div><dt>目标</dt><dd>{egressSummary(egress)}<small>{egress.type === 'direct' ? `${server.name} · ${server.address}` : '外部目标尚不可配置'}</small></dd></div>
    <div><dt>出站 tag</dt><dd>{egress.tag || '未设置'}</dd></div>
    <div><dt>域名策略 / 网卡</dt><dd>{egress.domainStrategy} / {egress.bindInterface || '自动选择'}</dd></div>
    <div><dt>DNS</dt><dd>{egress.dns.type.toUpperCase()} · {egress.dns.server || '未设置'}</dd></div>
    <div><dt>嗅探 / 规则集</dt><dd>{egress.sniff ? '已开启' : '已关闭'} / {egress.ruleSet || '未设置'}</dd></div>
  </dl></div>;
}

export default function ServerInOutView({ server }: { server: ServerAsset }) {
  const { state } = useWorkspace();
  const inbounds = getServerInbounds(server.id, state.inbounds, state.proxies);
  const egressRefs = getServerEgressRefs(server.id, state.proxies);
  return <div className="su-server-inout" aria-label={`${server.name} 入口与出口维护视图`}>
    <p className="su-server-note">本次合成工作区 · 引用按链去重，保留当前草稿与旧模拟发布。仅查看维护摘要，不回显密钥。</p>
    <details className="su-server-io-section" open>
      <summary><span><strong>入口区</strong><small>INBOUNDS · 本服务器所有入站</small></span><Tag color="blue">{inbounds.length} 个入口</Tag></summary>
      <div className="su-server-io-content">{inbounds.length ? inbounds.map(({ inbound, refs }) => <details className="su-server-io-item" key={inbound.id}>
        <summary><div><strong>{inbound.config.name || '未命名入口'}</strong><InboundSummary config={inbound.config} /><span className="su-server-ref-count">被 {refs.length} 条链引用 · 展开查看</span></div></summary>
        <div className="su-server-io-item-body">
          {inbound.published && <div className="su-server-published-inbound"><small>已发布入口快照 · 模拟：{inbound.published.name}</small><InboundSummary config={inbound.published} /></div>}
          {refs.length ? <ul className="su-server-ref-list" aria-label={`${inbound.config.name} 被引用链`}>{refs.map(reference => <li key={reference.proxyId}><ReferenceLink reference={reference} /><ReferenceState reference={reference} /></li>)}</ul> : <p className="su-server-note">暂未被任何链引用；该入口仍保留，可在链编辑器中复用。</p>}
        </div>
      </details>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="本服务器暂无入口；在链编辑器中新建后会显示在这里。" />}</div>
    </details>
    <details className="su-server-io-section" open>
      <summary><span><strong>出口区（代理节点）</strong><small>OUTBOUND / EGRESS · 仅统计链的最后一跳</small></span><Tag color="green">{egressRefs.length} 条链</Tag></summary>
      <div className="su-server-io-content"><p className="su-server-note">本服务器出站配置摘要按链展示；当前模型没有独立的服务器全局出站配置。仅作入口或中转的链不计入出口。</p>
        {egressRefs.length ? egressRefs.map(reference => {
          const egress = reference.draftEgress ?? reference.publishedEgress;
          const sharedConfig = reference.draftEgress && reference.publishedEgress && JSON.stringify(reference.draftEgress) === JSON.stringify(reference.publishedEgress);
          return <details className="su-server-io-item" key={reference.proxyId}>
            <summary><div><strong>{reference.name || '未命名链'}</strong><small>出口目标：{egress ? egressSummary(egress) : '待配置'} · {server.name}{!reference.draft ? ' · 仅旧发布' : ''}</small><ReferenceState reference={reference} /></div></summary>
            <div className="su-server-io-item-body"><ReferenceLink reference={reference} />
              {reference.draftEgress && <EgressSummary egress={reference.draftEgress} label={sharedConfig ? '当前草稿 / 已发布 · 模拟' : '当前草稿出站'} server={server} />}
              {reference.publishedEgress && !sharedConfig && <EgressSummary egress={reference.publishedEgress} label="已发布出站 · 模拟（不受草稿修改影响）" server={server} />}
            </div>
          </details>;
        }) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无以本服务器为出口的链；可将其已有入站选为链的最后一跳。" />}
      </div>
    </details>
  </div>;
}
