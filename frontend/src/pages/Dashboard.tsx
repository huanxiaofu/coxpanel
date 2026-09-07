import { useEffect, useState } from 'react';
import { Alert, Card, Col, Empty, Row, Skeleton, Statistic } from 'antd';
import { api } from '../api';
import type { Node, Subscription } from '../api';
import { isAdminRole, useAuth } from '../auth/AuthContext';

export default function Dashboard() {
  const { user } = useAuth();
  const isAdmin = isAdminRole(user?.role);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [subs, setSubs] = useState<Subscription[]>([]);
  const [nodesLoading, setNodesLoading] = useState(isAdmin);
  const [subsLoading, setSubsLoading] = useState(true);
  const [nodesError, setNodesError] = useState<string | null>(null);
  const [subsError, setSubsError] = useState<string | null>(null);

  const load = () => {
    setSubsLoading(true);
    setSubsError(null);
    api.listSubs()
      .then((result) => setSubs(Array.isArray(result) ? result : []))
      .catch((reason: unknown) => setSubsError(reason instanceof Error ? reason.message : '订阅读取失败'))
      .finally(() => setSubsLoading(false));

    if (!isAdmin) {
      setNodes([]);
      setNodesLoading(false);
      setNodesError(null);
      return;
    }

    setNodesLoading(true);
    setNodesError(null);
    api.listNodes()
      .then((result) => setNodes(Array.isArray(result) ? result : []))
      .catch((reason: unknown) => setNodesError(reason instanceof Error ? reason.message : '节点读取失败'))
      .finally(() => setNodesLoading(false));
  };

  useEffect(() => { load(); }, [isAdmin]);

  const online = nodes.filter((node) => node.status === 'online').length;

  return (
    <div>
      <h2>总览</h2>
      <Row gutter={[16, 16]}>
        {isAdmin && (
          <>
            <Col xs={24} sm={12} lg={6}>
              <Card><Statistic title="节点总数" value={nodesLoading ? undefined : nodes.length} loading={nodesLoading} /></Card>
            </Col>
            <Col xs={24} sm={12} lg={6}>
              <Card><Statistic title="在线节点" value={nodesLoading ? undefined : online} loading={nodesLoading} valueStyle={{ color: '#3f8600' }} /></Card>
            </Col>
          </>
        )}
        <Col xs={24} sm={12} lg={6}>
          <Card><Statistic title="我的订阅" value={subsLoading ? undefined : subs.length} loading={subsLoading} /></Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card><Statistic title="当前用户" value={user?.username || '-'} /></Card>
        </Col>
      </Row>
      {nodesError && isAdmin && <Alert style={{ marginTop: 16 }} type="error" showIcon message="节点读取失败" description={nodesError} action={<a onClick={load}>重试</a>} />}
      {subsError && <Alert style={{ marginTop: 16 }} type="error" showIcon message="订阅读取失败" description={subsError} action={<a onClick={load}>重试</a>} />}
      {!subsLoading && !subsError && subs.length === 0 && (
        <Card style={{ marginTop: 16 }}><Empty description="还没有订阅，可从我的订阅创建" /></Card>
      )}
      {subsLoading && <Card style={{ marginTop: 16 }}><Skeleton active /></Card>}
      <Card title="欢迎使用 Coxpanel" style={{ marginTop: 16 }}>
        <p>{isAdmin ? '管理节点、权限、订阅与两级拓扑。' : '普通用户仅访问自己的订阅与授权内容。'}</p>
      </Card>
    </div>
  );
}
