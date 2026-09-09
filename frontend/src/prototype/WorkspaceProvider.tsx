import { createContext, useContext, useEffect, useReducer, useState } from 'react';
import type { ReactNode } from 'react';
import { connectionError, getInbound, getServer, inboundPortConflict, inboundReadiness, inboundReferences, serverAssets, validateServerTraffic } from './model';
import type { EgressConfig, InboundResource, ProxyConfig, ProxyDraft, ServerAsset, ServerTraffic } from './model';

interface Operation {
  ids: string[];
  phase: 'preparing' | 'applying' | 'active' | 'failed';
  fail: boolean;
}

export interface WorkspaceState {
  servers: ServerAsset[];
  proxies: ProxyDraft[];
  inbounds: InboundResource[];
  operation?: Operation;
}

export type WorkspaceAction =
  | { type: 'server-save'; id: string; tags: string[]; traffic: ServerTraffic }
  | { type: 'server-toggle'; id: string }
  | { type: 'add'; proxy: ProxyDraft }
  | { type: 'move'; positions: Array<{ id: string; position: ProxyDraft['position'] }> }
  | { type: 'remove'; id: string }
  | { type: 'save'; id: string; config: ProxyConfig }
  | { type: 'reuse'; id: string; inboundId: string }
  | { type: 'egress'; id: string; name: string; egress: EgressConfig }
  | { type: 'revert-egress'; id: string }
  | { type: 'start'; ids: string[]; fail: boolean }
  | { type: 'advance' }
  | { type: 'reset' };

export const initialState: WorkspaceState = { servers: serverAssets, proxies: [], inbounds: [] };
const asDraft = (proxy: ProxyDraft): ProxyDraft => ({ ...proxy, status: 'draft', dirty: true, failure: undefined });

export function workspaceApplyError(state: WorkspaceState, ids: string[]): string | undefined {
  if (!ids.length) return '没有可应用的节点。';
  for (const id of ids) {
    const proxy = state.proxies.find(item => item.id === id);
    const inbound = proxy && getInbound(state.inbounds, proxy);
    if (!proxy || !inbound) return '请先配置所有选中节点的入口。';
    if (proxy.egress.type === 'block') return 'block 仅预留，不可应用。';
    if (proxy.egress.type === 'next-hop') {
      const reason = connectionError(state.proxies, state.inbounds, id, proxy.egress.targetInboundId ?? '', state.servers);
      if (reason) return reason;
    }
    for (const resourceId of [inbound.id, proxy.egress.targetInboundId].filter(Boolean)) {
      const resource = state.inbounds.find(item => item.id === resourceId);
      if (!resource) return '目标入口不存在。';
      const reason = inboundReadiness(resource, state.servers);
      if (reason) return `${resource.config.name}：${reason}`;
      if (inboundPortConflict(state.inbounds, resource.serverId, resource.config.listenPort, resource.id)) return '同机端口已被其他入口占用。';
      const resourceDirty = JSON.stringify(resource.config) !== JSON.stringify(resource.published);
      if (resourceDirty && inboundReferences(state.proxies, resource.id, true).some(reference => !ids.includes(reference.id))) return '共享入口变更影响其他节点，请使用「应用更改」一起确认全部引用。';
    }
  }
}

export function workspaceReducer(state: WorkspaceState, action: WorkspaceAction): WorkspaceState {
  const busy = state.operation?.phase === 'preparing' || state.operation?.phase === 'applying';
  if (busy && action.type !== 'advance' && action.type !== 'move') return state;
  switch (action.type) {
    case 'server-save': {
      const tags = [...new Set(action.tags.map(tag => tag.trim()).filter(Boolean))];
      if (validateServerTraffic(action.traffic) || tags.length > 12 || tags.some(tag => tag.length > 24)) return state;
      return { ...state, servers: state.servers.map(server => server.id === action.id ? { ...server, tags, traffic: { ...action.traffic, usedBytes: server.traffic.usedBytes } } : server) };
    }
    case 'server-toggle': return { ...state, servers: state.servers.map(server => server.id === action.id ? {
      ...server,
      status: server.status === 'maintenance' ? server.resumeStatus ?? 'offline' : 'maintenance',
      resumeStatus: server.status === 'maintenance' ? undefined : server.status,
    } : server) };
    case 'add': {
      const server = state.servers.find(item => item.id === action.proxy.serverId);
      if (!server || server.status !== 'online' || !server.capabilitiesKnown) return state;
      return { ...state, proxies: [...state.proxies, action.proxy] };
    }
    case 'move': return { ...state, proxies: state.proxies.map(proxy => {
      const changed = action.positions.find(position => position.id === proxy.id);
      return changed ? { ...proxy, position: changed.position } : proxy;
    }) };
    case 'remove': return { ...state, proxies: state.proxies.filter(proxy => proxy.id !== action.id || proxy.published) };
    case 'save': {
      const proxy = state.proxies.find(item => item.id === action.id);
      if (!proxy || inboundPortConflict(state.inbounds, proxy.serverId, action.config.listenPort, proxy.inboundId)) return state;
      const existing = getInbound(state.inbounds, proxy);
      if (existing?.published && existing.config.protocol !== action.config.protocol) return state;
      if (!existing && state.inbounds.filter(item => item.serverId === proxy.serverId).length >= getServer(proxy.serverId, state.servers).maxProxies) return state;
      const inboundId = existing?.id ?? `inbound:${proxy.id}`;
      const resource = { ...existing, id: inboundId, serverId: proxy.serverId, config: action.config };
      const affected = new Set(inboundReferences(state.proxies, inboundId, true).map(item => item.id));
      affected.add(proxy.id);
      return { ...state, inbounds: [...state.inbounds.filter(item => item.id !== inboundId), resource], proxies: state.proxies.map(item => affected.has(item.id) ? asDraft({ ...item, ...(item.id === proxy.id ? { inboundId, name: existing ? item.name : action.config.name } : {}) }) : item) };
    }
    case 'reuse': {
      const inbound = state.inbounds.find(item => item.id === action.inboundId);
      if (!inbound) return state;
      return { ...state, proxies: state.proxies.map(proxy => proxy.id === action.id && !proxy.inboundId && proxy.serverId === inbound.serverId ? asDraft({ ...proxy, inboundId: inbound.id, name: `${inbound.config.name} · 引用 ${state.proxies.filter(item => item.inboundId === inbound.id).length + 1}` }) : proxy) };
    }
    case 'egress': {
      if (action.egress.type === 'block') return state;
      if (action.egress.type === 'next-hop' && connectionError(state.proxies, state.inbounds, action.id, action.egress.targetInboundId ?? '', state.servers)) return state;
      const egress = { ...action.egress, targetInboundId: action.egress.type === 'next-hop' ? action.egress.targetInboundId : undefined };
      return { ...state, proxies: state.proxies.map(proxy => proxy.id === action.id ? asDraft({ ...proxy, name: action.name, egress }) : proxy) };
    }
    case 'revert-egress': return { ...state, proxies: state.proxies.map(proxy => {
      if (proxy.id !== action.id || !proxy.published) return proxy;
      const inbound = getInbound(state.inbounds, proxy);
      const target = state.inbounds.find(item => item.id === proxy.published?.egress.targetInboundId);
      const dirty = [inbound, target].some(item => item && JSON.stringify(item.config) !== JSON.stringify(item.published));
      return { ...proxy, egress: proxy.published.egress, name: proxy.published.name, dirty, status: dirty ? 'draft' : 'active', failure: undefined };
    }) };
    case 'start': {
      if (workspaceApplyError(state, action.ids)) return state;
      return { ...state, operation: { ids: action.ids, phase: 'preparing', fail: action.fail }, proxies: state.proxies.map(proxy => action.ids.includes(proxy.id) ? { ...proxy, status: 'preparing', failure: undefined } : proxy) };
    }
    case 'advance': {
      if (!state.operation || !busy) return state;
      const phase = state.operation.phase === 'preparing' ? 'applying' : state.operation.fail ? 'failed' : 'active';
      const ids = state.operation.ids;
      const resources = new Set(state.proxies.filter(proxy => ids.includes(proxy.id)).flatMap(proxy => [proxy.inboundId, proxy.egress.targetInboundId]));
      return { ...state, operation: { ...state.operation, phase },
        inbounds: phase === 'active' ? state.inbounds.map(inbound => resources.has(inbound.id) ? { ...inbound, published: { ...inbound.config } } : inbound) : state.inbounds,
        proxies: state.proxies.map(proxy => ids.includes(proxy.id) ? { ...proxy, status: phase, dirty: phase !== 'active',
          published: phase === 'active' && proxy.inboundId ? { inboundId: proxy.inboundId, name: proxy.name, egress: { ...proxy.egress, dns: { ...proxy.egress.dns } } } : proxy.published,
          failure: phase === 'failed' ? '模拟应用失败；草稿已保留，旧模拟发布不变。可重试。' : undefined,
        } : proxy),
      };
    }
    case 'reset': return initialState;
  }
}

interface WorkspaceContextValue {
  state: WorkspaceState;
  busy: boolean;
  simulateFailure: boolean;
  setSimulateFailure: (value: boolean) => void;
  dispatch: React.Dispatch<WorkspaceAction>;
  applyError: (ids: string[]) => string | undefined;
}

const WorkspaceContext = createContext<WorkspaceContextValue | null>(null);

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(workspaceReducer, initialState);
  const [simulateFailure, setSimulateFailure] = useState(false);
  const busy = state.operation?.phase === 'preparing' || state.operation?.phase === 'applying';
  useEffect(() => {
    if (!busy) return;
    const timer = window.setTimeout(() => dispatch({ type: 'advance' }), state.operation?.phase === 'preparing' ? 1000 : 1600);
    return () => window.clearTimeout(timer);
  }, [busy, state.operation?.phase]);
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => { if (state.proxies.length || state.inbounds.length || state.servers !== initialState.servers) event.preventDefault(); };
    window.addEventListener('beforeunload', beforeUnload);
    return () => window.removeEventListener('beforeunload', beforeUnload);
  }, [state.proxies.length, state.inbounds.length, state.servers]);
  return <WorkspaceContext.Provider value={{ state, busy, dispatch, simulateFailure, setSimulateFailure, applyError: ids => workspaceApplyError(state, ids) }}>{children}</WorkspaceContext.Provider>;
}

export function useWorkspace() {
  const context = useContext(WorkspaceContext);
  if (!context) throw new Error('WorkspaceProvider is required');
  return context;
}
