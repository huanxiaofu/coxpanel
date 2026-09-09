import { createContext, useContext, useEffect, useReducer, useState } from 'react';
import type { ReactNode } from 'react';
import { chainReadiness, getInbound, inboundPortConflict, inboundReferences, serverAssets, validateServerTraffic } from './model';
import type { ChainHop, EgressConfig, InboundResource, ProxyConfig, ProxyDraft, ServerAsset, ServerTraffic } from './model';

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
  | { type: 'remove'; id: string }
  | { type: 'rename'; id: string; name: string }
  | { type: 'append-hop'; id: string }
  | { type: 'remove-hop'; id: string; position: number }
  | { type: 'move-hop'; id: string; position: number; direction: -1 | 1 }
  | { type: 'select-server'; id: string; position: number; serverId: string }
  | { type: 'save-hop'; id: string; position: number; config: ProxyConfig; resourceId: string }
  | { type: 'reuse-hop'; id: string; position: number; inboundId: string }
  | { type: 'egress'; id: string; name: string; egress: EgressConfig }
  | { type: 'revert-chain'; id: string }
  | { type: 'start'; ids: string[]; fail: boolean }
  | { type: 'advance' }
  | { type: 'reset' };

export const initialState: WorkspaceState = { servers: serverAssets, proxies: [], inbounds: [] };
const asDraft = (proxy: ProxyDraft): ProxyDraft => ({ ...proxy, status: 'draft', dirty: true, failure: undefined });
const ordered = (chain: ChainHop[]) => chain.map((hop, position) => ({ ...hop, position }));
const resourceDirty = (resource: InboundResource) => JSON.stringify(resource.config) !== JSON.stringify(resource.published);

export function workspaceApplyError(state: WorkspaceState, ids: string[]): string | undefined {
  if (!ids.length) return '没有可应用的代理链。';
  for (const id of ids) {
    const proxy = state.proxies.find(item => item.id === id);
    if (!proxy) return '代理链不存在。';
    const reason = chainReadiness(proxy, state.inbounds, state.servers);
    if (reason) return `${proxy.name || '未命名节点'}：${reason}`;
    for (const hop of proxy.chain) {
      const resource = getInbound(state.inbounds, hop)!;
      if (resourceDirty(resource) && inboundReferences(state.proxies, resource.id, true).some(reference => !ids.includes(reference.id))) {
        return '共享入站变更影响其他链，请使用「应用全部更改」一起确认全部引用。';
      }
    }
  }
}

export function workspaceReducer(state: WorkspaceState, action: WorkspaceAction): WorkspaceState {
  const busy = state.operation?.phase === 'preparing' || state.operation?.phase === 'applying';
  if (busy && action.type !== 'advance') return state;
  const updateProxy = (id: string, update: (proxy: ProxyDraft) => ProxyDraft): WorkspaceState => ({
    ...state, operation: undefined, proxies: state.proxies.map(proxy => proxy.id === id ? asDraft(update(proxy)) : proxy),
  });
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
      if (!action.proxy.chain.length || state.proxies.some(proxy => proxy.id === action.proxy.id)) return state;
      if (action.proxy.chain.some(hop => hop.serverId && !state.servers.some(server => server.id === hop.serverId && server.status === 'online' && server.capabilitiesKnown))) return state;
      return { ...state, operation: undefined, proxies: [...state.proxies, asDraft({ ...action.proxy, chain: ordered(action.proxy.chain) })] };
    }
    case 'remove': return { ...state, operation: undefined, proxies: state.proxies.filter(proxy => proxy.id !== action.id || proxy.published) };
    case 'rename': return updateProxy(action.id, proxy => ({ ...proxy, name: action.name.slice(0, 80) }));
    case 'append-hop': return updateProxy(action.id, proxy => ({ ...proxy, chain: [...proxy.chain, { position: proxy.chain.length, serverId: '' }] }));
    case 'remove-hop': return updateProxy(action.id, proxy => proxy.chain.length <= 1 ? proxy : ({ ...proxy, chain: ordered(proxy.chain.filter(hop => hop.position !== action.position)) }));
    case 'move-hop': return updateProxy(action.id, proxy => {
      const target = action.position + action.direction;
      if (!proxy.chain[action.position] || !proxy.chain[target]) return proxy;
      const chain = [...proxy.chain];
      [chain[action.position], chain[target]] = [chain[target], chain[action.position]];
      return { ...proxy, chain: ordered(chain) };
    });
    case 'select-server': {
      const server = state.servers.find(item => item.id === action.serverId);
      if (!server || server.status !== 'online' || !server.capabilitiesKnown || (action.position > 0 && !server.chainTarget)) return state;
      return updateProxy(action.id, proxy => ({ ...proxy, chain: proxy.chain.map(hop => hop.position === action.position && hop.serverId !== server.id ? { position: hop.position, serverId: server.id } : hop) }));
    }
    case 'save-hop': {
      const proxy = state.proxies.find(item => item.id === action.id);
      const hop = proxy?.chain[action.position];
      const server = state.servers.find(item => item.id === hop?.serverId);
      if (!proxy || !hop || !server || server.status !== 'online' || !server.capabilitiesKnown) return state;
      const existing = state.inbounds.find(resource => resource.id === action.resourceId);
      if (existing && (existing.id !== hop.inboundId || existing.serverId !== hop.serverId)) return state;
      if (existing?.published && existing.config.protocol !== action.config.protocol) return state;
      if (!existing && server.maxProxies > 0 && state.inbounds.filter(resource => resource.serverId === server.id).length >= server.maxProxies) return state;
      if (inboundPortConflict(state.inbounds, hop.serverId, action.config.listenPort, existing?.id)) return state;
      const resource: InboundResource = { id: action.resourceId, serverId: hop.serverId, config: { ...action.config }, published: existing?.published };
      const affected = new Set([proxy.id, ...inboundReferences(state.proxies, resource.id, true).map(reference => reference.id)]);
      return { ...state, operation: undefined, inbounds: [...state.inbounds.filter(item => item.id !== resource.id), resource], proxies: state.proxies.map(item => affected.has(item.id) ? asDraft({
        ...item, chain: item.id === proxy.id ? item.chain.map(candidate => candidate.position === action.position ? { position: candidate.position, serverId: hop.serverId, inboundId: resource.id } : candidate) : item.chain,
      }) : item) };
    }
    case 'reuse-hop': {
      const proxy = state.proxies.find(item => item.id === action.id);
      const hop = proxy?.chain[action.position];
      const inbound = state.inbounds.find(item => item.id === action.inboundId);
      if (!proxy || !hop || !inbound || inbound.serverId !== hop.serverId) return state;
      if (action.position === 0 && inbound.config.exposure !== 'subscription') return state;
      if (proxy.chain.some(candidate => candidate.position !== action.position && candidate.inboundId === inbound.id)) return state;
      return updateProxy(action.id, item => ({ ...item, chain: item.chain.map(candidate => candidate.position === action.position ? { position: candidate.position, serverId: inbound.serverId, inboundId: inbound.id } : candidate) }));
    }
    case 'egress': {
      if (action.egress.type !== 'direct') return state;
      return updateProxy(action.id, proxy => ({ ...proxy, name: action.name, egress: { ...action.egress, dns: { ...action.egress.dns } } }));
    }
    case 'revert-chain': return { ...state, operation: undefined, proxies: state.proxies.map(proxy => {
      if (proxy.id !== action.id || !proxy.published) return proxy;
      const restored = { ...proxy, ...structuredClone(proxy.published) };
      const dirty = restored.chain.some(hop => { const resource = getInbound(state.inbounds, hop); return !resource || resourceDirty(resource); });
      return { ...restored, dirty, status: dirty ? 'draft' : 'active', failure: undefined };
    }) };
    case 'start': {
      if (workspaceApplyError(state, action.ids)) return state;
      return { ...state, operation: { ids: [...action.ids], phase: 'preparing', fail: action.fail }, proxies: state.proxies.map(proxy => action.ids.includes(proxy.id) ? { ...proxy, status: 'preparing', failure: undefined } : proxy) };
    }
    case 'advance': {
      if (!state.operation || !busy) return state;
      const phase = state.operation.phase === 'preparing' ? 'applying' : state.operation.fail ? 'failed' : 'active';
      const ids = state.operation.ids;
      const resources = new Set(state.proxies.filter(proxy => ids.includes(proxy.id)).flatMap(proxy => proxy.chain.map(hop => hop.inboundId)));
      return { ...state, operation: { ...state.operation, phase },
        inbounds: phase === 'active' ? state.inbounds.map(inbound => resources.has(inbound.id) ? { ...inbound, published: { ...inbound.config } } : inbound) : state.inbounds,
        proxies: state.proxies.map(proxy => ids.includes(proxy.id) ? { ...proxy, status: phase, dirty: phase !== 'active',
          published: phase === 'active' ? structuredClone({ name: proxy.name, chain: proxy.chain, egress: proxy.egress }) : proxy.published,
          failure: phase === 'failed' ? '模拟应用失败；整条链草稿已保留，旧模拟发布不变。可重试。' : undefined,
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
