import { useEffect, useState } from 'react';
import { Alert, Button, Empty, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Table, Tag, message } from 'antd';
import { api } from '../api';
import type { Inbound, Node } from '../api';

const supportedProtocols = [
  { value: 'vless-reality', label: 'VLESS-Reality' },
  { value: 'shadowsocks', label: 'Shadowsocks 2022' },
  { value: 'hysteria2', label: 'Hysteria2' },
];

const portRule = { type: 'number' as const, min: 1, max: 65535, transform: (value: string) => Number(value) };

function protocolParameters(protocol: string | undefined, root: 'config' | 'extParams') {
  if (protocol === 'vless-reality') {
    return (
      <Space direction="vertical" style={{ width: '100%' }}>
        <Form.Item name={[root, 'uuid']} label="用户 UUID"><Input /></Form.Item>
        <Form.Item name={[root, 'sni']} label="SNI"><Input /></Form.Item>
        <Form.Item name={[root, 'publicKey']} label="Reality 公钥"><Input /></Form.Item>
        {root === 'config' && <Form.Item name={[root, 'privateKey']} label="Reality 私钥"><Input.Password /></Form.Item>}
        {root === 'config' && <Form.Item name={[root, 'target']} label="Reality 握手目标"><Input placeholder="example.test:443" /></Form.Item>}
        <Form.Item name={[root, 'shortId']} label="Short ID"><Input /></Form.Item>
        <Form.Item name={[root, 'fingerprint']} label="指纹" initialValue="chrome"><Input /></Form.Item>
        <Form.Item name={[root, 'flow']} label="流控" initialValue="xtls-rprx-vision"><Input /></Form.Item>
      </Space>
    );
  }
  if (protocol === 'shadowsocks') {
    return (
      <Space direction="vertical" style={{ width: '100%' }}>
        <Form.Item name={[root, 'method']} label="SS2022 方法" initialValue="2022-blake3-aes-128-gcm"><Input /></Form.Item>
        <Form.Item name={[root, 'password']} label="服务端密钥"><Input.Password /></Form.Item>
      </Space>
    );
  }
  if (protocol === 'hysteria2') {
    return (
      <Space direction="vertical" style={{ width: '100%' }}>
        <Form.Item name={[root, 'sni']} label="SNI"><Input /></Form.Item>
        <Form.Item name={[root, 'password']} label="服务端密码"><Input.Password /></Form.Item>
        <Form.Item name={[root, 'obfs']} label="混淆"><Input placeholder="salamander（可选）" /></Form.Item>
        <Form.Item name={[root, 'obfsPassword']} label="混淆密码"><Input.Password /></Form.Item>
        <Form.Item name={[root, 'insecure']} label="跳过证书验证"><Select options={[{ value: false, label: '否' }, { value: true, label: '是' }]} /></Form.Item>
        {root === 'config' && <Form.Item name={[root, 'certificatePath']} label="证书路径"><Input placeholder="/etc/coxpanel/tls/server.crt" /></Form.Item>}
        {root === 'config' && <Form.Item name={[root, 'keyPath']} label="私钥路径"><Input placeholder="/etc/coxpanel/tls/server.key" /></Form.Item>}
      </Space>
    );
  }
  return null;
}

export default function Nodes() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [inboundNode, setInboundNode] = useState<Node | null>(null);
  const [inbounds, setInbounds] = useState<Inbound[]>([]);
  const [inboundLoading, setInboundLoading] = useState(false);
  const [inboundError, setInboundError] = useState<string | null>(null);
  const [createForm] = Form.useForm();
  const [inboundForm] = Form.useForm();
  const protocol = Form.useWatch('protocol', inboundForm);
  const nodeType = Form.useWatch('type', createForm);
  const externalProtocol = Form.useWatch('extProtocol', createForm);

  const load = async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await api.listNodes();
      setNodes(Array.isArray(result) ? result : []);
    } catch (reason: unknown) {
      setError(reason instanceof Error ? reason.message : '节点读取失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const onCreate = async (values: Record<string, unknown>) => {
    try {
      const payload = values.type === 'external'
        ? values
        : Object.fromEntries(Object.entries(values).filter(([key]) => key !== 'extProtocol' && key !== 'extParams'));
      const result = await api.createNode(payload);
      setCreateOpen(false);
      createForm.resetFields();
      message.success(result?.agentCredential ? '节点已创建；Agent 凭据仅显示本次，请立即保存' : '节点已创建');
      if (result?.agentCredential) {
        Modal.info({ title: 'Agent 凭据（仅显示一次）', content: <Input.Password value={result.agentCredential} readOnly /> });
      }
      await load();
    } catch (reason: unknown) {
      message.error(reason instanceof Error ? reason.message : '节点创建失败');
    }
  };

  const openInbounds = async (node: Node) => {
    setInboundNode(node);
    setInboundLoading(true);
    setInboundError(null);
    try {
      const result = await api.listInbounds(node.id);
      setInbounds(Array.isArray(result) ? result : []);
    } catch (reason: unknown) {
      setInboundError(reason instanceof Error ? reason.message : '入站读取失败');
    } finally {
      setInboundLoading(false);
    }
  };

  const onCreateInbound = async (values: Record<string, unknown>) => {
    if (!inboundNode) return;
    try {
      const config = (values.config || {}) as Record<string, unknown>;
      await api.createInbound(inboundNode.id, {
        ...values,
        listenPort: Number(values.listenPort),
        config,
      });
      message.success('入站已创建');
      inboundForm.resetFields();
      await openInbounds(inboundNode);
    } catch (reason: unknown) {
      message.error(reason instanceof Error ? reason.message : '入站创建失败');
    }
  };

  const onDeleteInbound = async (inbound: Inbound) => {
    if (!inboundNode) return;
    try {
      await api.deleteInbound(inboundNode.id, inbound.id);
      message.success('入站已删除');
      await openInbounds(inboundNode);
    } catch (reason: unknown) {
      message.error(reason instanceof Error ? reason.message : '入站删除失败');
    }
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', render: (value: string | null) => value || '-' },
    {
      title: '类型', dataIndex: 'type', width: 100,
      render: (value: string | null) => value === 'external' ? <Tag color="orange">外部</Tag> : <Tag color="blue">受管</Tag>,
    },
    { title: '公网 IP', dataIndex: 'publicIp', render: (value: string | null) => value || '-' },
    {
      title: '状态', dataIndex: 'status', width: 90,
      render: (value: string | null) => value === 'online' ? <Tag color="green">在线</Tag> : <Tag>离线</Tag>,
    },
    {
      title: '操作', width: 180,
      render: (_value: unknown, node: Node) => (
        <Space>
          <Button size="small" onClick={() => openInbounds(node)}>入站</Button>
          <Popconfirm title="删除节点？" onConfirm={async () => { await api.deleteNode(node.id); await load(); }}>
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
      {error && <Alert type="error" showIcon message="节点读取失败" description={error} action={<a onClick={load}>重试</a>} />}
      {!loading && !error && nodes.length === 0 && <Empty description="暂无节点" />}
      <Table rowKey="id" dataSource={nodes} columns={columns} loading={loading} pagination={false} locale={{ emptyText: '暂无节点' }} />

      <Modal title="添加节点" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={() => createForm.submit()}>
        <Form form={createForm} layout="vertical" onFinish={onCreate} initialValues={{ type: 'managed' }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}><Input /></Form.Item>
          <Form.Item name="type" label="类型"><Select options={[{ value: 'managed', label: '受管节点（agent + sing-box）' }, { value: 'external', label: '外部节点（静态参数）' }]} /></Form.Item>
          <Form.Item name="publicIp" label="公网 IP"><Input placeholder="example.test" /></Form.Item>
          <Form.Item name="easyIp" label="EasyTier 内网 IP"><Input placeholder="10.14.14.x" /></Form.Item>
          {nodeType === 'external' && (
            <>
              <Form.Item name="extProtocol" label="外部协议" rules={[{ required: true, message: '请选择协议' }]}>
                <Select options={supportedProtocols} placeholder="协议" />
              </Form.Item>
              <Form.Item name={['extParams', 'server']} label="服务端地址" rules={[{ required: true, message: '请输入服务端地址' }]}><Input /></Form.Item>
              <Form.Item name={['extParams', 'port']} label="服务端端口" rules={[{ required: true, message: '端口必须为 1-65535' }, portRule]}>
                <InputNumber min={1} max={65535} precision={0} placeholder="1-65535" style={{ width: '100%' }} />
              </Form.Item>
              {protocolParameters(externalProtocol, 'extParams')}
            </>
          )}
        </Form>
      </Modal>

      <Modal
        title={`${inboundNode?.name || '节点'} 的入站`}
        open={!!inboundNode}
        onCancel={() => { setInboundNode(null); setInbounds([]); inboundForm.resetFields(); }}
        footer={null}
        width={720}
      >
        {inboundError && <Alert type="error" showIcon message="入站读取失败" description={inboundError} />}
        {!inboundLoading && !inboundError && inbounds.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无入站" />}
        <Table
          rowKey="id"
          dataSource={inbounds}
          loading={inboundLoading}
          pagination={false}
          size="small"
          locale={{ emptyText: '暂无入站' }}
          columns={[
            { title: '名称', dataIndex: 'name', render: (value: string | null) => value || '-' },
            { title: '协议', dataIndex: 'protocol', width: 140, render: (value: string | null) => value || '-' },
            { title: '角色', dataIndex: 'role', width: 90, render: (value: string | null) => value || '-' },
            { title: '端口', dataIndex: 'listenPort', width: 80, render: (value: number | null) => value ?? '-' },
            { title: '操作', width: 90, render: (_value: unknown, inbound: Inbound) => <Popconfirm title="删除入站？" onConfirm={() => onDeleteInbound(inbound)}><Button size="small" danger>删除</Button></Popconfirm> },
          ]}
        />
        <Form form={inboundForm} layout="vertical" onFinish={onCreateInbound} style={{ marginTop: 16 }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}><Input /></Form.Item>
          <Space wrap>
            <Form.Item name="protocol" label="协议" rules={[{ required: true, message: '请选择协议' }]}><Select placeholder="协议" style={{ width: 180 }} options={supportedProtocols} /></Form.Item>
            <Form.Item name="role" label="角色" initialValue="entry"><Select style={{ width: 120 }} options={[{ value: 'entry', label: '入口' }, { value: 'landing', label: '落地' }, { value: 'relay', label: '中转' }]} /></Form.Item>
            <Form.Item name="listenPort" label="端口" rules={[{ required: true, message: '端口必须为 1-65535' }, portRule]}><InputNumber min={1} max={65535} precision={0} placeholder="1-65535" /></Form.Item>
          </Space>
          {protocolParameters(protocol, 'config')}
          <Button type="primary" htmlType="submit">添加入站</Button>
        </Form>
      </Modal>
    </div>
  );
}
