import { useEffect, useState } from 'react';
import { Alert, Button, Card, Empty, Form, Input, Select, Space, Table, Tag, message } from 'antd';
import { api } from '../api';
import type { AdminUser, Invite, Node, NodeGroup } from '../api';

interface SectionError {
  groups?: string;
  users?: string;
  invites?: string;
}

function errorText(reason: unknown, fallback: string): string {
  return reason instanceof Error ? reason.message : fallback;
}

export default function Administration() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [groups, setGroups] = useState<NodeGroup[]>([]);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [invites, setInvites] = useState<Invite[]>([]);
  const [loading, setLoading] = useState(true);
  const [errors, setErrors] = useState<SectionError>({});
  const [creatingGroup, setCreatingGroup] = useState(false);
  const [groupForm] = Form.useForm<{ name: string }>();
  const [inviteForm] = Form.useForm<{ groupId: number }>();

  const load = async () => {
    setLoading(true);
    setErrors({});
    const [nodeResult, groupResult, userResult, inviteResult] = await Promise.allSettled([
      api.listNodes(),
      api.listGroups(),
      api.listUsers(),
      api.listInvites(),
    ]);
    if (nodeResult.status === 'fulfilled') setNodes(Array.isArray(nodeResult.value) ? nodeResult.value : []);
    if (groupResult.status === 'fulfilled') setGroups(Array.isArray(groupResult.value) ? groupResult.value : []);
    if (userResult.status === 'fulfilled') setUsers(Array.isArray(userResult.value) ? userResult.value : []);
    if (inviteResult.status === 'fulfilled') setInvites(Array.isArray(inviteResult.value) ? inviteResult.value : []);
    setErrors({
      groups: groupResult.status === 'rejected' ? errorText(groupResult.reason, '用户组读取失败') : undefined,
      users: userResult.status === 'rejected' ? errorText(userResult.reason, '用户读取失败') : undefined,
      invites: inviteResult.status === 'rejected' ? errorText(inviteResult.reason, '邀请码读取失败') : undefined,
    });
    setLoading(false);
  };

  useEffect(() => { load(); }, []);

  const createGroup = async (values: { name: string }) => {
    setCreatingGroup(true);
    try {
      await api.createGroup(values.name);
      message.success('用户组已创建');
      groupForm.resetFields();
      await load();
    } catch (reason: unknown) {
      message.error(errorText(reason, '用户组创建失败'));
    } finally {
      setCreatingGroup(false);
    }
  };

  const saveGroupNodes = async (groupId: number, nodeIds: number[]) => {
    try {
      await api.setGroupNodes(groupId, nodeIds);
      message.success('用户组节点权限已更新');
      await load();
    } catch (reason: unknown) {
      message.error(errorText(reason, '节点权限更新失败'));
    }
  };

  const saveUserGroups = async (userId: number, groupIds: number[]) => {
    try {
      await api.setUserGroups(userId, groupIds);
      message.success('用户组授权已更新');
      await load();
    } catch (reason: unknown) {
      message.error(errorText(reason, '用户组授权更新失败'));
    }
  };

  const createInvite = async (values: { groupId: number }) => {
    try {
      await api.genInvite(values.groupId);
      message.success('邀请码已生成');
      inviteForm.resetFields();
      await load();
    } catch (reason: unknown) {
      message.error(errorText(reason, '邀请码生成失败'));
    }
  };

  const nodeOptions = nodes.map((node) => ({ value: node.id, label: node.name || `节点 ${node.id}` }));
  const groupOptions = groups.map((group) => ({ value: group.id, label: group.name || `用户组 ${group.id}` }));

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <h2 style={{ margin: 0 }}>权限管理</h2>
        <Button onClick={load} loading={loading}>刷新</Button>
      </Space>

      <Card title="用户组与节点权限" loading={loading} style={{ marginBottom: 16 }}>
        {errors.groups && <Alert type="error" showIcon message="用户组读取失败" description={errors.groups} />}
        {!loading && !errors.groups && groups.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无用户组" />}
        <Table
          rowKey="id"
          dataSource={groups}
          pagination={false}
          locale={{ emptyText: '暂无用户组' }}
          columns={[
            { title: '用户组', dataIndex: 'name', render: (value: string | null, group: NodeGroup) => value || `用户组 ${group.id}` },
            {
              title: '可访问节点',
              render: (_value: unknown, group: NodeGroup) => (
                <Select
                  mode="multiple"
                  allowClear
                  style={{ minWidth: 280 }}
                  value={group.nodeIds || []}
                  options={nodeOptions}
                  placeholder="选择节点"
                  onChange={(values: number[]) => saveGroupNodes(group.id, values)}
                />
              ),
            },
          ]}
        />
        <Form form={groupForm} layout="inline" onFinish={createGroup} style={{ marginTop: 16 }}>
          <Form.Item name="name" rules={[{ required: true, message: '请输入用户组名称' }]}><Input placeholder="新用户组名称" /></Form.Item>
          <Button type="primary" htmlType="submit" loading={creatingGroup}>创建用户组</Button>
        </Form>
      </Card>

      <Card title="用户授权" loading={loading} style={{ marginBottom: 16 }}>
        {errors.users && <Alert type="error" showIcon message="用户读取失败" description={errors.users} />}
        {!loading && !errors.users && users.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无用户" />}
        <Table
          rowKey="id"
          dataSource={users}
          pagination={false}
          locale={{ emptyText: '暂无用户' }}
          columns={[
            { title: '用户名', dataIndex: 'username', render: (value: string | null) => value || '-' },
            { title: '角色', dataIndex: 'role', render: (value: string | null) => <Tag>{value || 'user'}</Tag> },
            {
              title: '用户组',
              render: (_value: unknown, user: AdminUser) => (
                <Select
                  mode="multiple"
                  allowClear
                  style={{ minWidth: 280 }}
                  value={user.groupIds || []}
                  options={groupOptions}
                  placeholder="选择用户组"
                  onChange={(values: number[]) => saveUserGroups(user.id, values)}
                />
              ),
            },
          ]}
        />
      </Card>

      <Card title="邀请注册" loading={loading}>
        {errors.invites && <Alert type="error" showIcon message="邀请码读取失败" description={errors.invites} />}
        {!loading && !errors.invites && invites.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无邀请码" />}
        <Form form={inviteForm} layout="inline" onFinish={createInvite} style={{ marginBottom: 16 }}>
          <Form.Item name="groupId" rules={[{ required: true, message: '必须选择用户组' }]}>
            <Select style={{ width: 240 }} options={groupOptions} placeholder="选择注册后用户组" />
          </Form.Item>
          <Button type="primary" htmlType="submit" disabled={groups.length === 0}>生成邀请码</Button>
        </Form>
        <Table
          rowKey={(invite) => invite.id || invite.code}
          dataSource={invites}
          pagination={false}
          locale={{ emptyText: '暂无邀请码' }}
          columns={[
            { title: '邀请码', dataIndex: 'code', render: (value: string | null) => value || '-' },
            { title: '用户组', dataIndex: 'nodeGroupId', render: (value: number | null) => groups.find((group) => group.id === value)?.name || value || '-' },
            {
              title: '剩余次数',
              render: (_value: unknown, invite: Invite) => invite.maxUses != null && invite.usedCount != null ? Math.max(invite.maxUses - invite.usedCount, 0) : '-',
            },
            { title: '到期时间', dataIndex: 'expiresAt', render: (value: string | null) => value || '-' },
          ]}
        />
      </Card>
    </div>
  );
}
