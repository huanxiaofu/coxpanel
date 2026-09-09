import { useState } from 'react';
import { Alert, Descriptions, Form, Input, Modal, Radio, Select, Switch, Tabs, Tag } from 'antd';
import { egressSummary } from './model';
import type { EgressConfig, ProxyDraft } from './model';
import { useWorkspace } from './WorkspaceProvider';

export interface EgressConfigFormProps {
  proxy: ProxyDraft;
  onSave: (name: string, egress: EgressConfig) => void;
  onClose: () => void;
}

export default function EgressConfigForm({ proxy, onSave, onClose }: EgressConfigFormProps) {
  const { state: { servers } } = useWorkspace();
  const [form] = Form.useForm();
  const [config, setConfig] = useState<EgressConfig>({ ...proxy.egress, dns: { ...proxy.egress.dns } });
  const terminalHop = proxy.chain[proxy.chain.length - 1];
  const terminalServer = terminalHop ? servers.find(server => server.id === terminalHop.serverId) : undefined;
  const canSave = config.type === 'direct';
  const chainLabel = proxy.chain.length
    ? proxy.chain.map((hop, index) => `${index === 0 ? '入口' : `中转 ${index}`} · ${servers.find(server => server.id === hop.serverId)?.name ?? '待选服务器'}`).join(' → ')
    : '尚未选择链跳';
  const update = (patch: Partial<EgressConfig>) => setConfig(current => ({ ...current, ...patch }));

  return <Modal open title="代理链终端出站" width={640} onCancel={onClose} cancelText="取消（不保存）" okText="保存代理链出站草稿" okButtonProps={{ disabled: !canSave }} onOk={() => { if (!canSave) return; void form.validateFields().then(values => onSave(String(values.name).trim(), config)).catch(() => undefined); }}>
    <Form form={form} layout="vertical" initialValues={{ name: proxy.name }}>
      <Form.Item name="name" label="整条代理链名称" rules={[{ required: true, whitespace: true, message: '请输入代理链名称' }, { max: 80 }]}><Input maxLength={80} /></Form.Item>
      <Descriptions size="small" column={1} bordered items={[
        { key: 'chain', label: '线性链路', children: chainLabel },
        { key: 'terminal', label: '终端出网服务器', children: terminalServer ? `${terminalServer.name} · ${terminalServer.region}` : '尚未选择最后一跳服务器' },
      ]} />
      <Form.Item label="终端出站类型"><Radio.Group value={config.type} onChange={event => {
        const type = event.target.value as EgressConfig['type'];
        update({ type, tag: type === 'direct' ? 'direct' : 'external_out' });
      }} options={[
        { value: 'direct', label: '最后一跳本机直出 · direct' },
        { value: 'external', label: '外部指定出口 · 后续支持', disabled: true },
      ]} /></Form.Item>
      {config.type === 'external' && <Alert type="warning" showIcon title="外部指定出口暂不可用" description="仅保留 external 配置意图；当前不会伪装外部出口的真实运行能力。" />}
      <Tabs items={[
        { key: 'dial', label: '拨号与标识', children: <>
          <Form.Item label="逻辑 outbound tag"><Input aria-label="出站 tag" readOnly value={config.tag} /><small className="su-muted">direct 表示由最后一跳服务器本机出网；external 仅为预留标识。</small></Form.Item>
          <Form.Item label="域名解析策略"><Select aria-label="出站域名策略" value={config.domainStrategy} onChange={domainStrategy => update({ domainStrategy })} options={['prefer_ipv4', 'prefer_ipv6', 'ipv4_only', 'ipv6_only'].map(value => ({ value, label: value }))} /></Form.Item>
          <Form.Item label="绑定网卡（可选）"><Input aria-label="出站绑定网卡" maxLength={32} placeholder="例如 eth0，仅保存意图" value={config.bindInterface} onChange={event => update({ bindInterface: event.target.value.trim() })} /></Form.Item>
        </> },
        { key: 'route', label: '路由 / DNS', children: <>
          <Form.Item label="协议嗅探 · action=sniff"><Switch aria-label="出站协议嗅探" checked={config.sniff} onChange={sniff => update({ sniff })} /></Form.Item>
          <Form.Item label="DNS type"><Select aria-label="DNS 类型" value={config.dns.type} onChange={type => update({ dns: { type, server: '' } })} options={['udp', 'tcp', 'https', 'tls', 'hosts'].map(value => ({ value, label: value }))} /></Form.Item>
          <Form.Item label={config.dns.type === 'hosts' ? 'hosts 合成映射引用' : 'DNS 服务端（合成）'}><Input aria-label="DNS 服务端或引用" value={config.dns.server} maxLength={180} placeholder={config.dns.type === 'hosts' ? 'demo-hosts' : 'dns.example.invalid'} onChange={event => update({ dns: { ...config.dns, server: event.target.value } })} /></Form.Item>
          <Form.Item label="rule_set 引用（可选）"><Input aria-label="规则集引用" value={config.ruleSet} maxLength={80} placeholder="demo-rule-set，不使用 geoip / geosite" onChange={event => update({ ruleSet: event.target.value })} /></Form.Item>
          <p className="su-muted">仅保存拨号、路由和 DNS 意图，不下载规则集、不请求 DNS；实际运行字段留待后续适配。</p>
        </> },
      ]} />
      <div className="su-egress-preview"><strong>{egressSummary(config)}</strong><p>{config.sniff && <Tag>action=sniff</Tag>}<Tag>action=route → {config.tag}</Tag>{config.ruleSet && <Tag>rule_set: {config.ruleSet}</Tag>}</p><small>预留动作：hijack-dns / reject / resolve；当前不生成外部出口运行配置。</small></div>
      <Alert type="info" showIcon title="仅保存终端出站意图" description="代理链按第 1 跳入口 → 零或多中转跳 → 最后一跳服务器本机出网；当前不会调用后端或宣称真实连通。" />
    </Form>
  </Modal>;
}
