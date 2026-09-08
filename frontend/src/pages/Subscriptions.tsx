import { useEffect, useState } from 'react';
import { Alert, Button, Empty, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography, message } from 'antd';
import { api } from '../api';
import type { Subscription, SupportedSubscriptionFormat } from '../api';
import { Link } from 'react-router-dom';
import { useP2Data } from '../components/P2Data';

const { Paragraph, Text } = Typography;

const supportedFormats: Array<{ value: SupportedSubscriptionFormat; label: string }> = [
  { value: 'mihomo', label: 'mihomo / Clash YAML' },
  { value: 'base64', label: 'Base64（标准 URI 列表）' },
];

export default function Subscriptions() {
  const [subs, setSubs] = useState<Subscription[]>([]);
  const [groups, setGroups] = useState<Array<{ id: number; name: string }>>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [form] = Form.useForm();
	const templates = useP2Data<Array<Record<string, any>>>('/api/templates');
	const [switching, setSwitching] = useState<Subscription | null>(null);
	const [switchForm] = Form.useForm();
	const [switchPreview, setSwitchPreview] = useState<unknown>(null);
	const [versions, setVersions] = useState<Array<{version: number}>>([]);

  const load = async () => {
    setLoading(true);
    setError(null);
    try {
      const [subscriptionResult, groupResult] = await Promise.all([
        api.listSubs(),
        api.listMyGroups(),
      ]);
      setSubs(Array.isArray(subscriptionResult) ? subscriptionResult : []);
      setGroups(Array.isArray(groupResult) ? groupResult : []);
    } catch (reason: unknown) {
      setError(reason instanceof Error ? reason.message : '订阅读取失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const onCreate = async (values: { name: string; format: SupportedSubscriptionFormat; nodeGroupId: number }) => {
    try {
      await api.createSub(values.name, values.format, values.nodeGroupId);
      message.success('订阅已创建');
      setCreateOpen(false);
      form.resetFields();
      await load();
    } catch (reason: unknown) {
      message.error(reason instanceof Error ? reason.message : '订阅创建失败');
    }
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', render: (value: string | null) => value || '-' },
    {
      title: '格式', dataIndex: 'format', width: 180,
      render: (value: string | null) => {
        const label = supportedFormats.find((format) => format.value === value)?.label;
        return label ? <Tag color="purple">{label}</Tag> : <Tag>未知格式</Tag>;
      },
    },
    {
      title: '订阅链接', dataIndex: 'token',
      render: (value: string | null) => value ? (
        <Paragraph copyable={{ text: api.subUrl(value) }} style={{ marginBottom: 0 }}>
          <Text code>{api.subUrl(value)}</Text>
        </Paragraph>
      ) : <Text type="secondary">暂无链接</Text>,
    },
    {
      title: '操作', width: 260,
      render: (_value: unknown, subscription: Subscription) => (
        <Space wrap><Link to={`/subscriptions/${subscription.id}/overrides`}>编辑覆写</Link><Button size="small" onClick={() => { setSwitching(subscription); setSwitchPreview(null); setVersions([]); switchForm.setFieldsValue({ ...subscription, templateId: subscription.templateId ?? undefined, templateVersion: subscription.templateVersion ?? undefined }); }}>切换模板</Button><Popconfirm title="删除订阅？" onConfirm={async () => { await api.deleteSub(subscription.id); await load(); }}>
          <Button size="small" danger>删除</Button>
        </Popconfirm></Space>
      ),
    },
  ];

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <h2 style={{ margin: 0 }}>我的订阅</h2>
        <Button type="primary" disabled={loading || groups.length === 0} onClick={() => setCreateOpen(true)}>创建订阅</Button>
      </Space>
      {error && <Alert type="error" showIcon message="订阅读取失败" description={error} action={<a onClick={load}>重试</a>} />}
      {!loading && !error && groups.length === 0 && <Empty description="暂无授权用户组，无法创建订阅" />}
      {!loading && !error && subs.length === 0 && <Empty description="暂无订阅" />}
      <Table rowKey="id" dataSource={subs} columns={columns} loading={loading} pagination={false} locale={{ emptyText: '暂无订阅' }} />

      <Modal title="创建订阅" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={() => form.submit()}>
        <Form form={form} layout="vertical" onFinish={onCreate} initialValues={{ format: 'mihomo' }}>
          <Form.Item name="name" label="订阅名称" rules={[{ required: true, message: '请输入订阅名称' }]}><Input placeholder="我的订阅" /></Form.Item>
          <Form.Item name="format" label="客户端格式" rules={[{ required: true, message: '请选择格式' }]}>
            <Select options={supportedFormats} />
          </Form.Item>
          <Form.Item name="nodeGroupId" label="授权用户组" rules={[{ required: true, message: '请选择授权用户组' }]}>
            <Select options={groups.map((group) => ({ value: group.id, label: group.name || `用户组 ${group.id}` }))} placeholder="选择用户组" />
          </Form.Item>
          <Text type="secondary">仅支持 mihomo YAML 与标准 Base64 URI 列表；不提供未实现的 JSON 格式。</Text>
        </Form>
      </Modal>
			<Modal title="切换模板（不更换订阅 token）" open={switching !== null} width={720} onCancel={() => setSwitching(null)} onOk={async () => { try { const values = await switchForm.validateFields(); await api.request(`/api/my/subscriptions/${switching!.id}`, 'PUT', { ...values, templateId: values.templateId ?? null, templateVersion: values.templateVersion ?? null, expectedRevision: switching?.revision }); setSwitching(null); await load(); } catch (reason) { message.error(reason instanceof Error ? reason.message : '切换失败'); } }} okButtonProps={{disabled: switchPreview === null}}><Form form={switchForm} layout="vertical" onValuesChange={() => setSwitchPreview(null)}><Form.Item name="name" label="名称"><Input /></Form.Item><Form.Item name="format" label="格式"><Select options={supportedFormats} /></Form.Item><Form.Item name="nodeGroupId" label="授权节点组"><Select options={groups.map(group => ({value: group.id, label: group.name}))} /></Form.Item><Form.Item name="templateId" label="已发布模板（空=内置）"><Select allowClear options={(templates.data || []).filter(template => template.format === switchForm.getFieldValue('format')).map(template => ({value: template.id, label: `${template.name} / v${template.publishedVersion}`}))} onChange={async value => { switchForm.setFieldValue('templateVersion', undefined); setVersions(value ? await api.request(`/api/templates/${value}/versions`) : []); }} /></Form.Item><Form.Item name="templateVersion" label="版本（空=跟随最新）"><Select allowClear options={versions.map(version => ({value: version.version, label: `固定 v${version.version}`}))} /></Form.Item></Form><Button onClick={async () => { try { const values = switchForm.getFieldsValue(); setSwitchPreview(await api.request(`/api/my/subscriptions/${switching!.id}/preview`, 'POST', { ...values, templateId: values.templateId ?? null, templateVersion: values.templateVersion ?? null })); } catch (reason) { message.error(reason instanceof Error ? reason.message : '预览失败'); } }}>先预览引用与配置</Button>{switchPreview !== null && <pre style={{whiteSpace:'pre-wrap'}}>{JSON.stringify(switchPreview, null, 2)}</pre>}</Modal>
    </div>
  );
}
