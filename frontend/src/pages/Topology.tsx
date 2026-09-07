import { useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Button, Card, Empty, Select, Space, Tag, Typography, message } from 'antd';
import { Background, Controls, MiniMap, ReactFlow, addEdge, useEdgesState, useNodesState, type Connection, type Edge, type EdgeChange, type Node as FlowNode } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { ApiError, api } from '../api';
import type { Inbound, Node, TopologyDraft, TopologyEdge, TopologyPreview } from '../api';

const { Paragraph, Text } = Typography;

interface TopologyProps {
  nodeId?: number;
}

function flowNode(id: string, label: string, x: number, y: number, kind: 'source' | 'target'): FlowNode {
  return {
    id,
    position: { x, y },
    data: { label: `${kind === 'source' ? '入口' : '目标'}：${label}` },
    style: { border: kind === 'source' ? '2px solid #1677ff' : '2px solid #52c41a', borderRadius: 8, padding: 10 },
  };
}

async function loadTopologyDraft(nodeId: number): Promise<TopologyDraft | null> {
  try {
    return await api.getTopology(nodeId);
  } catch (reason: unknown) {
    if (reason instanceof ApiError && reason.status === 404) return null;
    throw reason;
  }
}

export default function Topology({ nodeId: selectedNodeId }: TopologyProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>([]);
  const [edges, setEdges, applyFlowEdgeChanges] = useEdgesState<Edge>([]);
  const [adminNodes, setAdminNodes] = useState<Node[]>([]);
  const [inbounds, setInbounds] = useState<Record<number, Inbound[]>>({});
  const [nodeId, setNodeId] = useState<number | undefined>(selectedNodeId);
  const [loading, setLoading] = useState(true);
  const [topologyReady, setTopologyReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [draftSaved, setDraftSaved] = useState(false);
  const [preview, setPreview] = useState<TopologyPreview | null>(null);
  const [saving, setSaving] = useState(false);
  const [previewing, setPreviewing] = useState(false);
  const [deploying, setDeploying] = useState(false);
  const [deployed, setDeployed] = useState(false);
  const requestSequence = useRef(0);
  const graphRevision = useRef(0);

  useEffect(() => {
    const requestId = ++requestSequence.current;
    let active = true;
    setLoading(true);
    setTopologyReady(false);
    setError(null);
    setDraftSaved(false);
    setPreview(null);
    setDeployed(false);
    const isCurrent = () => active && requestSequence.current === requestId;

    const load = async () => {
      try {
        const availableNodes = await api.listNodes();
        if (!isCurrent()) return;
        const safeNodes = Array.isArray(availableNodes) ? availableNodes : [];
        setAdminNodes(safeNodes);
        const chosenNodeId = selectedNodeId || safeNodes[0]?.id;
        setNodeId(chosenNodeId);
        if (!chosenNodeId) return;

        const savedForSelected = selectedNodeId ? await loadTopologyDraft(selectedNodeId) : null;
        if (!isCurrent()) return;
        const sources = await api.listInbounds(chosenNodeId);
        const targetEntries = await Promise.all(safeNodes.map(async (node) => [node.id, await api.listInbounds(node.id)] as const));
        if (!isCurrent()) return;
        const allInbounds = Object.fromEntries(targetEntries);
        setInbounds(allInbounds);
        const draft = (selectedNodeId ? savedForSelected : await loadTopologyDraft(chosenNodeId)) || { nodeId: chosenNodeId, edges: [] };
        if (!isCurrent()) return;
        renderGraph(chosenNodeId, safeNodes, sources, draft.edges || [], Object.fromEntries(targetEntries), setNodes, setEdges);
        setDraftSaved(false);
        setTopologyReady(true);
      } catch (reason: unknown) {
        if (isCurrent()) setError(reason instanceof Error ? reason.message : '拓扑数据读取失败');
      } finally {
        if (isCurrent()) setLoading(false);
      }
    };
    void load();
    return () => { active = false; };
  }, [selectedNodeId]);

  const sourceInbounds = useMemo(() => nodeId ? (inbounds[nodeId] || []) : [], [inbounds, nodeId]);
  const targetOptions = useMemo(() => adminNodes.flatMap((node) => (inbounds[node.id] || []).map((inbound) => ({
    value: `${node.id}:${inbound.id}`,
    label: `${node.name || `节点 ${node.id}`} / ${inbound.name || `入站 ${inbound.id}`}`,
  }))), [adminNodes, inbounds]);

  const invalidateGraphState = () => {
    graphRevision.current += 1;
    setDraftSaved(false);
    setPreview(null);
    setDeployed(false);
  };

  const onEdgesChange = (changes: EdgeChange<Edge>[]) => {
    applyFlowEdgeChanges(changes);
    if (changes.some((change) => change.type !== 'select')) invalidateGraphState();
  };

  const selectNode = async (nextNodeId: number) => {
    const requestId = ++requestSequence.current;
    setNodeId(nextNodeId);
    setLoading(true);
    setTopologyReady(false);
    setDraftSaved(false);
    setPreview(null);
    setDeployed(false);
    setError(null);
    graphRevision.current += 1;
    try {
      const [sources, saved] = await Promise.all([
        api.listInbounds(nextNodeId),
        loadTopologyDraft(nextNodeId),
      ]);
      if (requestSequence.current !== requestId) return;
      const nextInbounds = { ...inbounds, [nextNodeId]: Array.isArray(sources) ? sources : [] };
      setInbounds(nextInbounds);
      renderGraph(nextNodeId, adminNodes, nextInbounds[nextNodeId], saved?.edges || [], nextInbounds, setNodes, setEdges);
      setTopologyReady(true);
    } catch (reason: unknown) {
      if (requestSequence.current === requestId) setError(reason instanceof Error ? reason.message : '所选节点拓扑读取失败');
    } finally {
      if (requestSequence.current === requestId) setLoading(false);
    }
  };

  const onConnect = (connection: Connection) => {
    if (!connection.source || !connection.target) return;
    setEdges((current) => addEdge({ ...connection, animated: true }, current));
    invalidateGraphState();
  };

  const save = async () => {
    if (!nodeId || !topologyReady || error) return;
    const saveNodeId = nodeId;
    const saveRevision = graphRevision.current;
    const saveRequestId = requestSequence.current;
    const draft: TopologyDraft = {
      nodeId: saveNodeId,
      edges: edges.flatMap((edge): TopologyEdge[] => {
        const fromInboundId = Number(edge.source.replace('inbound:', ''));
        const [toNodeId, toInboundId] = edge.target.replace('inbound:', '').split(':').map(Number);
        return Number.isInteger(fromInboundId) && Number.isInteger(toNodeId) && Number.isInteger(toInboundId)
          ? [{ fromInboundId, toNodeId, toInboundId }]
          : [];
      }),
    };
    setSaving(true);
    try {
      await api.saveTopology(saveNodeId, draft);
      if (saveRevision !== graphRevision.current || saveRequestId !== requestSequence.current) return;
      setDraftSaved(true);
      setPreview(null);
      setDeployed(false);
      message.success('拓扑草稿已保存');
    } catch (reason: unknown) {
      message.error(reason instanceof Error ? reason.message : '拓扑保存失败');
    } finally {
      setSaving(false);
    }
  };

  const makePreview = async () => {
    if (!nodeId || !topologyReady || !draftSaved || error) return;
    const previewNodeId = nodeId;
    const previewRevision = graphRevision.current;
    const previewRequestId = requestSequence.current;
    setPreviewing(true);
    try {
      const result = await api.previewTopology(previewNodeId);
      if (previewRevision !== graphRevision.current || previewRequestId !== requestSequence.current) return;
      setPreview(result);
      setDeployed(false);
      message.success('预览已生成；确认后再部署');
    } catch (reason: unknown) {
      message.error(reason instanceof Error ? reason.message : '拓扑预览失败');
    } finally {
      setPreviewing(false);
    }
  };

  const deploy = async () => {
    if (!nodeId || !preview || error) return;
    const deployNodeId = nodeId;
    const deployRevision = graphRevision.current;
    const deployRequestId = requestSequence.current;
    const previewVersion = preview.version;
    setDeploying(true);
    try {
      const result = await api.deployTopology(deployNodeId, previewVersion);
      if (deployRevision !== graphRevision.current || deployRequestId !== requestSequence.current) return;
      setDeployed(result.deployed === true);
      message.success(`已部署版本 ${result.version}`);
    } catch (reason: unknown) {
      message.error(reason instanceof Error ? reason.message : '拓扑部署失败（可能是版本已过期）');
    } finally {
      setDeploying(false);
    }
  };

  if (loading) return <Card loading title="拓扑编排" />;

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <h2 style={{ margin: 0 }}>拓扑编排</h2>
        <Space>
          <Select aria-label="选择节点" value={nodeId} placeholder="选择节点" options={adminNodes.map((node) => ({ value: node.id, label: node.name || `节点 ${node.id}` }))} onChange={selectNode} style={{ width: 180 }} />
          <Button onClick={save} loading={saving} disabled={!nodeId || !topologyReady || Boolean(error)}>保存草稿</Button>
          <Button onClick={makePreview} loading={previewing} disabled={!draftSaved || Boolean(error)}>预览</Button>
          <Button type="primary" onClick={deploy} loading={deploying} disabled={!preview || Boolean(error)}>明确部署</Button>
        </Space>
      </Space>
      {error && <Alert type="error" showIcon message="拓扑读取失败" description={error} />}
      {!error && adminNodes.length === 0 && <Empty description="暂无可编排节点" />}
      {adminNodes.length > 0 && nodeId && (
        <>
          <Card size="small" style={{ marginBottom: 16 }}>
            <Text type="secondary">拖动左侧入口连接到右侧目标入站。保存草稿、预览、明确部署必须按顺序完成。</Text>
            {sourceInbounds.length === 0 && <Alert style={{ marginTop: 12 }} type="info" message="当前节点暂无入口入站" />}
          </Card>
          <div style={{ height: 460, border: '1px solid #d9d9d9', borderRadius: 8 }}>
            <ReactFlow nodes={nodes} edges={edges} onNodesChange={onNodesChange} onEdgesChange={onEdgesChange} onConnect={onConnect} fitView>
              <MiniMap />
              <Controls />
              <Background />
            </ReactFlow>
          </div>
          {preview && (
            <Card title={<Space>服务端预览 <Tag color="blue">版本 {preview.version}</Tag></Space>} style={{ marginTop: 16 }}>
              <Paragraph copyable>{JSON.stringify(preview.config, null, 2)}</Paragraph>
              {deployed && <Alert type="success" message="该预览版本已明确部署" showIcon />}
            </Card>
          )}
          {targetOptions.length === 0 && <Text type="secondary">暂无目标入站；请先在节点管理中创建入站。</Text>}
        </>
      )}
    </div>
  );
}

function renderGraph(
  selectedNodeId: number,
  adminNodes: Node[],
  sources: Inbound[],
  savedEdges: TopologyEdge[],
  allInbounds: Record<number, Inbound[]>,
  setNodes: (nodes: FlowNode[]) => void,
  setEdges: (edges: Edge[]) => void,
) {
  const sourceNodes = sources.map((inbound, index) => flowNode(`inbound:${inbound.id}`, inbound.name || `入站 ${inbound.id}`, 40, 60 + index * 90, 'source'));
  const targetNodes = adminNodes.flatMap((node, nodeIndex) => (allInbounds[node.id] || []).map((inbound, inboundIndex) => flowNode(
    `inbound:${node.id}:${inbound.id}`,
    `${node.name || `节点 ${node.id}`} / ${inbound.name || `入站 ${inbound.id}`}`,
    410,
    60 + (nodeIndex * 4 + inboundIndex) * 90,
    'target',
  )));
  const graphEdges = savedEdges.map((edge, index) => ({
    id: `saved:${index}`,
    source: `inbound:${edge.fromInboundId}`,
    target: `inbound:${edge.toNodeId}:${edge.toInboundId}`,
    animated: true,
  }));
  setNodes([...sourceNodes, ...targetNodes]);
  setEdges(graphEdges);
  void selectedNodeId;
}
