import { useState } from 'react';
import { Alert, Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography, message } from 'antd';
import { useParams } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth/AuthContext';
import { DataState, useP2Data } from '../components/P2Data';

const builtin = { schemaVersion: 1, defaults: { displayNamePattern: '${node.name}-${inbound.name}', params: {} }, variables: {}, groups: [{ id: 'main', name: '节点选择', type: 'select', members: ['$authorizedProxies', 'DIRECT'] }], rules: [{ type: 'MATCH', target: 'group:main' }], dns: { enabled: false } };

export default function Templates() {
  const { user } = useAuth();
  const { id } = useParams();
  const owner = user?.role === 'owner';
  const query = useP2Data<Array<Record<string, any>>>(`/api/templates${owner ? '?includeDrafts=true' : ''}`);
  const [editing, setEditing] = useState<Record<string, any> | null>(null);
  const [versions, setVersions] = useState<Array<Record<string, any>> | null>(null);
  const [validation, setValidation] = useState<unknown>(null);
  const [error, setError] = useState('');
  const [form] = Form.useForm();
  const edit = (template: Record<string, any>) => { setEditing(template); setValidation(null); setError(''); form.setFieldsValue({ name: template.name || '', description: template.description || '', format: template.format || 'mihomo', definition: JSON.stringify(template.definition || builtin, null, 2) }); };
  const action = async (operation: () => Promise<unknown>) => { try { await operation(); setError(''); void query.load(); } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败'); } };
  const save = async () => {
    await action(async () => {
      const values = await form.validateFields();
      const definition = JSON.parse(values.definition);
      await api.request(editing?.id ? `/api/templates/${editing.id}` : '/api/templates', editing?.id ? 'PUT' : 'POST', { ...values, definition, expectedRevision: editing?.revision });
      setEditing(null); message.success('草稿已保存；未发布前不会影响现有订阅');
    });
  };
  return <Space orientation="vertical" size="large" style={{ width: '100%' }}><Typography.Title level={2}>订阅模板</Typography.Title><Alert showIcon type="info" title="模板只改变客户端订阅，不修改节点运行配置" description="发布版本不可变；订阅可跟随最新或固定旧版。内置模板只读，可复制。不支持脚本、任意 URL、远程规则或权限扩张。" /><DataState {...query} retry={query.load} />{error && <Alert type="error" title={error} />}{owner && <Button type="primary" onClick={() => edit({})}>创建模板草稿</Button>}<Table rowKey="id" dataSource={query.data || []} rowClassName={row => String(row.id) === id ? 'ant-table-row-selected' : ''} columns={[{ title: '名称', dataIndex: 'name' }, { title: '格式', dataIndex: 'format' }, { title: '发布版本', dataIndex: 'publishedVersion', render: value => value || '未发布' }, { title: '状态', dataIndex: 'status', render: value => <Tag>{value}</Tag> }, { title: '操作', render: (_, row) => <Space wrap><Button onClick={() => void action(async () => setVersions(await api.request(`/api/templates/${row.id}/versions`)))}>历史版本</Button>{owner && <><Button disabled={row.isBuiltin} onClick={() => edit(row)}>编辑草稿</Button><Button onClick={() => void action(async () => { await api.request('/api/templates', 'POST', { sourceTemplateId: row.id, sourceVersion: row.publishedVersion, name: `${row.name} 副本`, description: row.description }); })}>复制</Button><Popconfirm title="创建不可变新版本？跟随最新的订阅将在刷新后生效。" onConfirm={() => action(async () => { await api.request(`/api/templates/${row.id}/publish`, 'POST', { expectedRevision: row.revision }); })}><Button disabled={row.isBuiltin}>发布</Button></Popconfirm><Popconfirm title="归档模板？仍被订阅引用会拒绝。" onConfirm={() => action(async () => { await api.request(`/api/templates/${row.id}`, 'DELETE', { expectedRevision: row.revision }); })}><Button danger disabled={row.isBuiltin}>归档</Button></Popconfirm></>}</Space> }]} />
    <Modal title="模板草稿编辑" open={editing !== null} width={850} onCancel={() => setEditing(null)} onOk={() => void save()} okText="保存草稿"><Form form={form} layout="vertical"><Form.Item name="name" label="模板名称" rules={[{ required: true }]}><Input maxLength={128} /></Form.Item><Form.Item name="description" label="说明"><Input /></Form.Item><Form.Item name="format" label="客户端格式"><Select options={['mihomo', 'sing-box', 'base64'].map(value => ({ value, label: value }))} onChange={value => { if (value === 'base64') form.setFieldValue('definition', JSON.stringify({ schemaVersion: 1, defaults: {}, variables: {}, groups: [], rules: [] }, null, 2)); }} /></Form.Item><Card size="small" title="常用字段结构化编辑"><Form.Item label="默认显示名模式"><Input onChange={event => { try { const definition = JSON.parse(form.getFieldValue('definition')); definition.defaults = { ...definition.defaults, displayNamePattern: event.target.value }; form.setFieldValue('definition', JSON.stringify(definition, null, 2)); } catch { setError('请先修复高级 JSON'); } }} placeholder="${node.name}-${inbound.name}" /></Form.Item></Card><Form.Item name="definition" label="高级 JSON（与结构化字段共用同一 schema）" rules={[{ required: true }]}><Input.TextArea rows={16} spellCheck={false} /></Form.Item></Form>{editing?.id && <Button onClick={() => void action(async () => setValidation(await api.request(`/api/templates/${editing.id}/validate`, 'POST', { definition: JSON.parse(form.getFieldValue('definition')) })))}>校验当前定义</Button>}{validation !== null && <pre style={{ whiteSpace: 'pre-wrap' }}>{JSON.stringify(validation, null, 2)}</pre>}{error && <Alert type="error" title={error} />}</Modal>
    <Modal title="不可变发布版本" open={versions !== null} onCancel={() => setVersions(null)} footer={null}><Table rowKey="version" dataSource={versions || []} columns={[{ title: '版本', dataIndex: 'version' }, { title: '发布时间', dataIndex: 'publishedAt' }, { title: '校验和', dataIndex: 'checksum', ellipsis: true }]} /></Modal>
  </Space>;
}
