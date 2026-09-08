import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Button, Card, Drawer, Empty, Popconfirm, Select, Space, Table, Tabs, Tag, Typography } from 'antd';
import { Background, Controls, Handle, Position, ReactFlow, useEdgesState, useNodesState, type Connection, type Edge, type Node as FlowNode, type NodeProps } from '@xyflow/react';
import { useParams } from 'react-router-dom';
import '@xyflow/react/dist/style.css';
import { api } from '../api';

interface Inbound { id: number; nodeId: number; name: string; role: string; egressMode: string; protocol: string }
interface GraphEdge { fromInboundId: number; toNodeId: number; toInboundId: number }
interface GraphNode { nodeId: number; name?: string; revision: number; edges: GraphEdge[]; inbounds: Inbound[]; layout?: Record<string, any> }
interface Graph { graphRevision: number; nodes: GraphNode[]; issues?: Array<Record<string, any>> }
type InboundNode = FlowNode<{ label: string; role: string; mode: string }, 'inbound'>;

function InboundCard({ data }: NodeProps<InboundNode>) {
  return <Card size="small" style={{ width: 200, border: data.role === 'relay' ? '2px solid #d48806' : undefined }}>
    {data.role !== 'entry' && <Handle type="target" position={Position.Left} />}
    <strong>{data.label}</strong><div><Tag>{data.role}</Tag><Tag>{data.mode === 'chain' ? '中转链' : '直连'}</Tag></div>
    {data.role !== 'landing' && <Handle type="source" position={Position.Right} />}
  </Card>;
}
const nodeTypes = { inbound: InboundCard };

export default function TopologyGraph() {
  const params = useParams();
  const [graph, setGraph] = useState<Graph | null>(null);
  const [nodes, setNodes, onNodesChange] = useNodesState<InboundNode>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [root, setRoot] = useState<number>(Number(params.nodeId) || 0);
  const [source, setSource] = useState<number>();
  const [target, setTarget] = useState<number>();
  const [modes, setModes] = useState<Record<number, string>>({});
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [preview, setPreview] = useState<Record<string, any> | null>(null);
  const [release, setRelease] = useState<Record<string, any> | null>(null);
  const [drawer, setDrawer] = useState(false);
  const epoch = useRef(0);
  const inbounds = useMemo(() => new Map((graph?.nodes || []).flatMap(node => node.inbounds.map(inbound => [inbound.id, { ...inbound, nodeId: node.nodeId }] as const))), [graph]);

  const load = useCallback(async () => {
    setLoading(true); const generation = ++epoch.current;
    try {
      const result: Graph = await api.request('/api/topology/graph');
      if (generation !== epoch.current) return;
      setGraph(result); setRoot(current => current || result.nodes[0]?.nodeId || 0); setModes({}); setDirty(false); setPreview(null); setError('');
      setNodes(result.nodes.flatMap((node, nodeIndex) => node.inbounds.map((inbound, inboundIndex) => ({ id: String(inbound.id), type: 'inbound', position: node.layout?.positions?.[inbound.id] || { x: nodeIndex * 280, y: inboundIndex * 150 }, data: { label: `${node.name || node.nodeId} / ${inbound.name}`, role: inbound.role, mode: inbound.egressMode || (inbound.role === 'relay' ? 'chain' : 'direct') } }))));
      setEdges(result.nodes.flatMap(node => node.edges.map(edge => ({ id: `${edge.fromInboundId}:${edge.toInboundId}`, source: String(edge.fromInboundId), target: String(edge.toInboundId), label: '下一跳' }))));
    } catch (reason) { setError(reason instanceof Error ? reason.message : '全图读取失败'); }
    finally { if (generation === epoch.current) setLoading(false); }
  }, [setNodes, setEdges]);
  useEffect(() => { void load(); return () => { epoch.current++; }; }, [load]);
  useEffect(() => {
    if (!release?.id && !release?.releaseId) return;
    const poll = async () => { if (document.hidden) return; try { const result = await api.request(`/api/topology/releases/${release.id || release.releaseId}`); setRelease(result); } catch (reason) { setError(reason instanceof Error ? reason.message : '发布状态查询失败'); } };
    const timer = window.setInterval(() => void poll(), 3000); document.addEventListener('visibilitychange', poll);
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', poll); };
  }, [release?.id, release?.releaseId]);

  const invalidate = () => { setDirty(true); setPreview(null); };
  const connect = (connection: Connection) => {
    const from = inbounds.get(Number(connection.source)), to = inbounds.get(Number(connection.target));
    if (!from || !to) return;
    let problem = '';
    if (from.id === to.id || from.nodeId === to.nodeId) problem = '不允许自环或同物理节点互转';
    else if (from.role === 'landing' || to.role === 'entry') problem = '入口只能出边，落地只能入边';
    else if ((modes[from.id] || from.egressMode) !== 'chain' && from.role !== 'relay') problem = '请先将入口切换到中转链';
    else if (edges.some(edge => edge.source === connection.source)) problem = '每个入站只能有一个下一跳';
    else if (to.role === 'relay' && edges.some(edge => edge.target === connection.target)) problem = '同一个 relay 入站不能被多个上游共用';
    const next = [...edges, { id: `${from.id}:${to.id}`, source: String(from.id), target: String(to.id) }];
    for (const start of inbounds.values()) {
      let cursor: Inbound | undefined = start; const visited = new Set<number>(); const machines = new Set<number>(); let depth = 0;
      while (cursor) {
        if (visited.has(cursor.id)) { problem = '拒绝有向环：' + [...visited, cursor.id].join(' → '); break; }
        if (machines.has(cursor.nodeId)) { problem = '同一条链不能重入物理节点'; break; }
        visited.add(cursor.id); machines.add(cursor.nodeId); depth++;
        if (depth > 8) { problem = '链深最多8级'; break; }
        const edge = next.find(item => Number(item.source) === cursor!.id); cursor = edge ? inbounds.get(Number(edge.target)) : undefined;
      }
    }
    if (problem) { setError(problem); return; }
    setEdges(next); invalidate(); setError('');
  };
  const save = async () => {
    if (!graph) return;
    setLoading(true);
    try {
      const changes = graph.nodes.map(node => ({ nodeId: node.nodeId, expectedRevision: node.revision, edges: edges.filter(edge => inbounds.get(Number(edge.source))?.nodeId === node.nodeId).map(edge => ({ fromInboundId: Number(edge.source), toNodeId: inbounds.get(Number(edge.target))!.nodeId, toInboundId: Number(edge.target) })), layout: { positions: Object.fromEntries(nodes.filter(item => inbounds.get(Number(item.id))?.nodeId === node.nodeId).map(item => [item.id, item.position])) }, inboundModes: node.inbounds.filter(inbound => modes[inbound.id]).map(inbound => ({ inboundId: inbound.id, egressMode: modes[inbound.id] })) }));
      await api.request('/api/topology/graph', 'PUT', { expectedGraphRevision: graph.graphRevision, changes }); await load();
    } catch (reason) { setError(`${reason instanceof Error ? reason.message : '保存失败'}。本地编辑已保留；409 时请导出副本后重新载入，不能强制覆盖。`); }
    finally { setLoading(false); }
  };
  const previewGraph = async () => { try { setPreview(await api.request(`/api/topology/${root}/preview`, 'POST')); setError(''); } catch (reason) { setError(reason instanceof Error ? reason.message : '预览失败'); } };
  const deploy = async () => { if (!preview) return; try { const result = await api.request(`/api/topology/${root}/deploy`, 'POST', { previewId: preview.previewId, expectedGraphRevision: preview.graphRevision }); setRelease(result); setDrawer(true); } catch (reason) { setError(reason instanceof Error ? reason.message : '部署失败'); } };
  const exportDraft = () => { const url = URL.createObjectURL(new Blob([JSON.stringify({ graphRevision: graph?.graphRevision, nodes: nodes.map(node => ({ id: node.id, position: node.position })), edges, modes }, null, 2)], { type: 'application/json' })); const anchor = document.createElement('a'); anchor.href = url; anchor.download = 'topology-local-draft.json'; anchor.click(); URL.revokeObjectURL(url); };

  return <Space orientation="vertical" size="middle" style={{ width: '100%' }}><Typography.Title level={2}>多级拓扑编排</Typography.Title><Alert showIcon type="info" title={`全图版本 ${graph?.graphRevision ?? '—'} · ${dirty ? '有未保存更改' : '已保存草稿'}`} description="保存不应用；预览有效10分钟；发布按下游到入口执行 prepare / apply。真正连线端点为入站，单条链最多8级且不重复物理节点。" />{error && <Alert showIcon type="error" title={error} />}<Space wrap><Select aria-label="发布入口物理节点" value={root || undefined} onChange={value => { setRoot(value); setPreview(null); }} options={(graph?.nodes || []).map(node => ({ value: node.nodeId, label: node.name || `节点 ${node.nodeId}` }))} /><Button loading={loading} onClick={() => void save()} disabled={!graph || !dirty}>保存草稿</Button><Button disabled={dirty || !root} onClick={() => void previewGraph()}>全链预览</Button><Popconfirm title="显式发布冻结预览？分布式发布不是瞬时原子切换。" onConfirm={() => deploy()}><Button type="primary" disabled={!preview?.previewId || Date.parse(preview.expiresAt) <= Date.now()}>部署</Button></Popconfirm><Button onClick={exportDraft}>导出本地布局副本</Button><Popconfirm title="重新载入会丢弃当前未保存内容，先导出副本？" onConfirm={() => load()}><Button>重新载入</Button></Popconfirm>{release && <Button onClick={() => setDrawer(true)}>查看发布状态</Button>}</Space>
    <Space wrap><Select aria-label="源入站" style={{ minWidth: 200 }} value={source} onChange={setSource} options={[...inbounds.values()].filter(inbound => inbound.role !== 'landing').map(inbound => ({ value: inbound.id, label: `${inbound.nodeId}/${inbound.name}` }))} /><Select aria-label="目标入站" style={{ minWidth: 200 }} value={target} onChange={setTarget} options={[...inbounds.values()].filter(inbound => inbound.role !== 'entry').map(inbound => ({ value: inbound.id, label: `${inbound.nodeId}/${inbound.name}`, disabled: inbound.nodeId === inbounds.get(source || 0)?.nodeId }))} /><Button disabled={!source || !target} onClick={() => connect({ source: String(source), target: String(target), sourceHandle: null, targetHandle: null })}>添加下一跳（键盘可用）</Button></Space>
    <div style={{ height: 500, border: '1px solid #d9d9d9', borderRadius: 8 }}>{graph?.nodes.length ? <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} onNodesChange={changes => { onNodesChange(changes); if (changes.some(change => change.type === 'position')) invalidate(); }} onEdgesChange={changes => { onEdgesChange(changes); if (changes.some(change => change.type !== 'select')) invalidate(); }} onConnect={connect} fitView><Background /><Controls /></ReactFlow> : <Empty description={loading ? '正在读取全图…' : '暂无受管入站，外部节点不参与中转图'} />}</div>
    <Table size="small" rowKey="id" dataSource={[...inbounds.values()].filter(inbound => inbound.role === 'entry')} columns={[{ title: '入口', dataIndex: 'name' }, { title: '出站模式', render: (_, inbound) => <Select aria-label={`${inbound.name} 出站模式`} value={modes[inbound.id] || inbound.egressMode || 'direct'} options={[{ value: 'direct', label: '直连' }, { value: 'chain', label: '中转链' }]} onChange={value => { setModes(current => ({ ...current, [inbound.id]: value })); setNodes(current => current.map(node => node.id === String(inbound.id) ? { ...node, data: { ...node.data, mode: value } } : node)); invalidate(); }} /> }]} />
    {preview && <Tabs items={(Array.isArray(preview.configs) ? preview.configs : Object.entries(preview.configs || {}).map(([nodeId, config]) => ({ nodeId, config }))).map((entry: any) => ({ key: String(entry.nodeId), label: `节点 ${entry.nodeId}`, children: <pre style={{ whiteSpace: 'pre-wrap' }}>{JSON.stringify(entry.config || entry, null, 2)}</pre> }))} />}
    <Drawer title="逐节点发布与恢复状态" open={drawer} onClose={() => setDrawer(false)} size="large"><Alert showIcon type="info" title={`发布状态：${release?.status || '正在读取'}`} description="prepare 仅检查候选；真实应用回执完成后才显示 succeeded。离线或回滚失败需要人工处理。" /><pre style={{ whiteSpace: 'pre-wrap' }}>{JSON.stringify(release, null, 2)}</pre>{['failed', 'manual_required'].includes(release?.status) && <Popconfirm title="请求恢复该失败批次的旧路由？仍需真实节点回执。" onConfirm={async () => setRelease(await api.request(`/api/topology/releases/${release?.id || release?.releaseId}/rollback`, 'POST'))}><Button danger>请求回滚</Button></Popconfirm>}</Drawer>
  </Space>;
}
