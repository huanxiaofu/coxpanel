import { useEffect, useState } from 'react';
import { Table, Button, Modal, Form, Input, Select, Space, message, Tag, Popconfirm, Typography } from 'antd';
import { api } from '../api';
import type { Subscription } from '../api';

const { Paragraph, Text } = Typography;

export default function Subscriptions() {
  const [subs, setSubs] = useState<Subscription[]>([]);
  const [loading, setLoading] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [form] = Form.useForm();

  const load = async () => {
    setLoading(true);
    try {
      setSubs(await api.listSubs());
    } catch (e: any) {
      message.error(e.message);
    }
    setLoading(false);
  };

  useEffect(() => { load(); }, []);

  const onCreate = async (v: any) => {
    try {
      await api.createSub(v.name, v.format);
      message.success('订阅已创建');
      setCreateOpen(false);
      form.resetFields();
      load();
    } catch (e: any) {
      message.error(e.message);
    }
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '名称', dataIndex: 'name' },
    {
      title: '格式', dataIndex: 'format', width: 110,
      render: (f: string) => <Tag color="purple">{f}</Tag>,
    },
    {
      title: '订阅链接',
      dataIndex: 'token',
      render: (t: string) => (
        <Paragraph copyable={{ text: api.subUrl(t) }} style={{ marginBottom: 0 }}>
          <Text code>/sub/{t}</Text>
        </Paragraph>
      ),
    },
    {
      title: '操作', width: 90,
      render: (_: any, s: Subscription) => (
        <Popconfirm title="删除订阅？" onConfirm={async () => { await api.deleteSub(s.id); load(); }}>
          <Button size="small" danger>删除</Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <h2 style={{ margin: 0 }}>我的订阅</h2>
        <Button type="primary" onClick={() => setCreateOpen(true)}>创建订阅</Button>
      </Space>

      <Table rowKey="id" dataSource={subs} columns={columns} loading={loading} pagination={false} />

      <Modal title="创建订阅" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={() => form.submit()}>
        <Form form={form} layout="vertical" onFinish={onCreate} initialValues={{ format: 'mihomo' }}>
          <Form.Item name="name" label="订阅名称" rules={[{ required: true }]}>
            <Input placeholder="我的订阅" />
          </Form.Item>
          <Form.Item name="format" label="客户端格式">
            <Select options={[
              { value: 'mihomo', label: 'mihomo / Clash YAML' },
              { value: 'base64', label: 'Base64 (v2rayN 通用)' },
              { value: 'sing-box', label: 'sing-box JSON (P2)' },
            ]} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
