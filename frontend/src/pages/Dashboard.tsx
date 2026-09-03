import { useEffect, useState } from 'react';
import { Card, Row, Col, Statistic } from 'antd';
import { api } from '../api';

export default function Dashboard() {
  const [user, setUser] = useState<any>(null);
  const [nodes, setNodes] = useState<any[]>([]);
  const [subs, setSubs] = useState<any[]>([]);

  useEffect(() => {
    api.me().then(setUser).catch(() => {});
    api.listNodes().then(setNodes).catch(() => {});
    api.listSubs().then(setSubs).catch(() => {});
  }, []);

  const online = nodes.filter((n) => n.status === 'online').length;

  return (
    <div>
      <h2>总览</h2>
      <Row gutter={16}>
        <Col span={6}>
          <Card>
            <Statistic title="节点总数" value={nodes.length} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="在线节点" value={online} valueStyle={{ color: '#3f8600' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="我的订阅" value={subs.length} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="当前用户" value={user?.username || '-'} />
          </Card>
        </Col>
      </Row>
      <Card title="欢迎使用 Coxpanel" style={{ marginTop: 16 }}>
        <p>一个图形化编排的代理节点管理面板。当前为 P1 版本：节点管理 + 订阅分发已可用。</p>
      </Card>
    </div>
  );
}
