import { useState } from 'react';
import { Alert, Descriptions, Form, Input, Modal, Radio, Select, Switch, Tabs, Tag } from 'antd';
import { connectionError, egressSummary, getServer, inboundReferences, protocolLabels } from './model';
import type { EgressConfig, InboundResource, ProxyDraft } from './model';
import { useWorkspace } from './WorkspaceProvider';

export default function EgressConfigForm({ proxy, inbounds, proxies, onSave, onClose }: {
  proxy: ProxyDraft;
  inbounds: InboundResource[];
  proxies: ProxyDraft[];
  onSave: (name: string, egress: EgressConfig) => void;
  onClose: () => void;
}) {
  const { state: { servers } } = useWorkspace();
  const [form] = Form.useForm();
  const [config, setConfig] = useState<EgressConfig>({ ...proxy.egress, dns: { ...proxy.egress.dns } });
  const target = inbounds.find(inbound => inbound.id === config.targetInboundId);
  const reason = config.type === 'next-hop' ? connectionError(proxies, inbounds, proxy.id, config.targetInboundId ?? '', servers) : undefined;
  const update = (patch: Partial<EgressConfig>) => setConfig(current => ({ ...current, ...patch }));
  return <Modal open title="出站配置" width={640} onCancel={onClose} cancelText="取消（不保存）" okText="保存出站草稿" okButtonProps={{ disabled: Boolean(reason) }} onOk={() => void form.validateFields().then(values => onSave(String(values.name).trim(), config)).catch(() => undefined)}>
    <Form form={form} layout="vertical" initialValues={{ name: proxy.name }}>
      <Form.Item name="name" label="节点名称" rules={[{ required: true, whitespace: true, message: '请输入节点名称' }, { max: 160 }]}><Input maxLength={160} /></Form.Item>
      <Form.Item label="出站类型"><Radio.Group value={config.type} onChange={event => update({ type: event.target.value, targetInboundId: undefined, tag: event.target.value === 'direct' ? 'direct' : 'proxy_out' })} options={[{ value: 'direct', label: '本机直出 · direct' }, { value: 'next-hop', label: '下一跳 · 已有入口' }, { value: 'block', label: 'block / reject（预留）', disabled: true }]} /></Form.Item>
      {config.type === 'next-hop' && <>
        <Form.Item label="目标入站资源" validateStatus={reason ? 'warning' : undefined} help={reason ?? '订阅入口、内部入口均可复用；无需重新创建监听。'}>
          <Select aria-label="选择下一跳入口" placeholder="选择任意服务器的已有入口" value={config.targetInboundId} onChange={targetInboundId => update({ targetInboundId })} options={inbounds.map(inbound => {
            const disabledReason = connectionError(proxies, inbounds, proxy.id, inbound.id, servers);
            return { value: inbound.id, disabled: Boolean(disabledReason), label: `${inbound.config.name} · ${getServer(inbound.serverId, servers).name} :${inbound.config.listenPort}${disabledReason ? `（${disabledReason}）` : ''}` };
          })} />
        </Form.Item>
        {target && <Descriptions size="small" column={1} bordered items={[
          { key: 'protocol', label: '出站协议（推导）', children: protocolLabels[target.config.protocol] },
          { key: 'endpoint', label: '服务端地址 / 端口', children: `${target.config.advertisedAddress}:${target.config.advertisedPort}` },
          { key: 'security', label: '协议参数', children: target.config.protocol === 'shadowsocks' ? target.config.method : `SNI: ${target.config.sni} · ${target.config.protocol === 'hysteria2' ? 'UDP / TLS' : 'TCP / Reality'}` },
          { key: 'references', label: '资源引用', children: `当前被 ${inboundReferences(proxies, target.id).length} 个节点引用；不会创建新监听` },
          { key: 'material', label: '认证材料', children: '引用目标入口受控材料，不复制、不展示' },
        ]} />}
      </>}
      <Tabs items={[
        { key: 'dial', label: '拨号与标识', children: <>
          <Form.Item label="逻辑 outbound tag"><Input aria-label="出站 tag" readOnly value={config.tag} /><small className="su-muted">下一跳使用 proxy_out；R2 服务器聚合需生成唯一 tag，不能直接拼接多个同名出站。</small></Form.Item>
          <Form.Item label="域名解析策略"><Select aria-label="出站域名策略" value={config.domainStrategy} onChange={domainStrategy => update({ domainStrategy })} options={['prefer_ipv4', 'prefer_ipv6', 'ipv4_only', 'ipv6_only'].map(value => ({ value, label: value }))} /></Form.Item>
          <Form.Item label="绑定网卡（可选）"><Input aria-label="出站绑定网卡" maxLength={32} placeholder="例如 eth0，仅保存意图" value={config.bindInterface} onChange={event => update({ bindInterface: event.target.value.trim() })} /></Form.Item>
        </> },
        { key: 'route', label: '路由 / DNS', children: <>
          <Form.Item label="协议嗅探 · action=sniff"><Switch aria-label="出站协议嗅探" checked={config.sniff} onChange={sniff => update({ sniff })} /></Form.Item>
          <Form.Item label="DNS type"><Select aria-label="DNS 类型" value={config.dns.type} onChange={type => update({ dns: { type, server: '' } })} options={['udp', 'tcp', 'https', 'tls', 'hosts'].map(value => ({ value, label: value }))} /></Form.Item>
          <Form.Item label={config.dns.type === 'hosts' ? 'hosts 合成映射引用' : 'DNS 服务端（合成）'}><Input aria-label="DNS 服务端或引用" value={config.dns.server} maxLength={180} placeholder={config.dns.type === 'hosts' ? 'demo-hosts' : 'dns.example.invalid'} onChange={event => update({ dns: { ...config.dns, server: event.target.value } })} /></Form.Item>
          <Form.Item label="rule_set 引用（可选）"><Input aria-label="规则集引用" value={config.ruleSet} maxLength={80} placeholder="demo-rule-set，不使用 geoip / geosite" onChange={event => update({ ruleSet: event.target.value })} /></Form.Item>
          <p className="su-muted">仅保存引用，不下载规则集、不请求 DNS。DNS、网卡及域名策略均为 R1 意图，运行时字段留待 R2 适配。</p>
        </> },
      ]} />
      <div className="su-egress-preview"><strong>{egressSummary(config, inbounds)}</strong><p>{config.sniff && <Tag>action=sniff</Tag>}<Tag>action=route → {config.tag}</Tag>{config.ruleSet && <Tag>rule_set: {config.ruleSet}</Tag>}</p><small>预留动作：hijack-dns / reject / resolve。block 不生成旧式运行时出站。</small></div>
      <Alert type="info" showIcon title="仅模拟出站意图" description="A→B 引用 B 的入口，不继承 B 节点的出站。共享监听的不同出站如何按身份/路由分流，留待 R2 设计及验证。" />
    </Form>
  </Modal>;
}
