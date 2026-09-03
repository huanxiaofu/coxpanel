import { useEffect, useState } from 'react';
import { Table, Button, Modal, Form, Input, Select, Space, message, Tag, Popconfirm } from 'antd';
import { api } from '../api';
import type { Node, Inbound } from '../api';

export default function Nodes() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [inboundNode, setInboundNode] = useState<Node | null>(null);
  const [inbounds, setInbounds] = useState<Inbound[]>([]);
  const [createForm] = Form.useForm();
  const [inboundForm] = Form.useForm();

  const load = async () => {
    setLoading(true);
    try {
      setNodes(await api.listNodes());
    } catch (e: any) {
      message.error(e.message);
    }
    setLoading(false);
  };

  useEffect(() => { load(); }, []);

  const onCreate = async (v: any) => {
    try {
      await api.createNode(v);
      message.success('节点已创建');
      setCreateOpen(false);
      createForm.resetFields();
      load();
    } catch (e: any) {
      message.error(e.message);
    }
  };

  const openInbounds = async (n: Node) => {
    setInboundNode(n);
    try {
      setInbounds(await api.listInbounds(n.id));
    } catch (e: any) {
      message.error(e.message);
    }
  };

  const onCreateInbound = async (v: any) => {
    if (!inboundNode) return;
    try {
      await api.createInbound(inboundNode.id, v);
      message.success('入站已创建');
      inboundForm.resetFields();
      setInbounds(await api.listInbounds(inboundNode.id));
    } catch (e: any) {
      message.error(e.message);
    }
  };

  const onDeleteInbound = async (ib: Inbound) => {
    if (!inboundNode) return;
    try {
      await api.deleteInbound(inboundNode.id, ib.id);
      message.success('入站已删除');
      setInbounds(await api.listInbounds(inboundNode.id));
    } catch (e: any) {
      message.error(e.message);
    }
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '名称', dataIndex: 'name' },
    {
      title: '类型',
      dataIndex: 'type',
      width: 100,
      render: (t: string) => (t === 'external' ? <Tag color="orange">外部</Tag> : <Tag color="blue">受管</Tag>),
    },
    { title: '公网 IP', dataIndex: 'publicIp', render: (v: string) => v || '-' },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      render: (s: string) => (s === 'online' ? <Tag color="green">在线</Tag> : <Tag>离线</Tag>),
    },
    {
      title: '操作',
      width: 260,
      render: (_: any, n: Node) => (
        <Space>
          <Button size="small" onClick={() => openInbounds(n)}>入站</Button>
          <Popconfirm title="删除节点？" onConfirm={async () => { await api.deleteNode(n.id); load(); }}>
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <h2 style={{ margin: 0 }}>节点管理</h2>
        <Button type="primary" onClick={() => setCreateOpen(true)}>添加节点</Button>
      </Space>

      <Table rowKey="id" dataSource={nodes} columns={columns} loading={loading} pagination={false} />

      {/* 创建节点 */}
      <Modal title="添加节点" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={() => createForm.submit()}>
        <Form form={createForm} layout="vertical" onFinish={onCreate} initialValues={{ type: 'managed' }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="type" label="类型">
            <Select
              options={[
                { value: 'managed', label: '受管节点（agent + sing-box）' },
                { value: 'external', label: '外部节点（静态参数）' },
              ]}
            />
          </Form.Item>
          <Form.Item name="publicIp" label="公网 IP">
            <Input placeholder="45.89.219.222" />
          </Form.Item>
          <Form.Item name="easyIp" label="EasyTier 内网 IP">
            <Input placeholder="10.14.14.x" />
          </Form.Item>
        </Form>
      </Modal>

      {/* 入站管理 */}
      <Modal
        title={`${inboundNode?.name} 的入站`}
        open={!!inboundNode}
        onCancel={() => setInboundNode(null)}
        footer={null}
        width={640}
      >
        <Table
          rowKey="id"
          dataSource={inbounds}
          pagination={false}
          size="small"
          columns={[
            { title: '名称', dataIndex: 'name' },
            { title: '协议', dataIndex: 'protocol', width: 140 },
            {
              title: '角色', dataIndex: 'role', width: 90,
              render: (r: string) => (r === 'entry' ? <Tag color="blue">入口</Tag> : r === 'landing' ? <Tag color="green">落地</Tag> : <Tag>中转</Tag>),
            },
            { title: '端口', dataIndex: 'listenPort', width: 80 },
            {
              title: '操作', width: 90,
              render: (_: any, ib: Inbound) => (
                <Popconfirm title="删除入站？" onConfirm={() => onDeleteInbound(ib)}>
                  <Button size="small" danger>删除</Button>
                </Popconfirm>
              ),
            },
          ]}
        />
        <Form form={inboundForm} layout="vertical" onFinish={onCreateInbound} style={{ marginTop: 16 }}>
          <Space.Compact block>
            <Form.Item name="name" rules={[{ required: true }]} style={{ width: 160 }}>
              <Input placeholder="名称" />
            </Form.Item>
            <Form.Item name="protocol" rules={[{ required: true }]} style={{ width: 180 }}>
              <Select
                placeholder="协议"
                options={[
                  { value: 'vless-reality', label: 'VLESS-Reality' },
                  { value: 'shadowsocks', label: 'Shadowsocks' },
                  { value: 'hysteria2', label: 'Hysteria2' },
                ]}
              />
            </Form.Item>
            <Form.Item name="role" initialValue="entry" style={{ width: 120 }}>
              <Select options={[
                { value: 'entry', label: '入口' },
                { value: 'landing', label: '落地' },
                { value: 'relay', label: '中转' },
              ]} />
            </Form.Item>
            <Form.Item name="listenPort" rules={[{ required: true }]} style={{ width: 100 }}>
              <Input placeholder="端口" />
            </Form.Item>
            <Button type="primary" htmlType="submit">添加</Button>
          </Space.Compact>
        </Form>
      </Modal>
    </div>
  );
}
