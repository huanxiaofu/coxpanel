import { createContext, useContext, useEffect, useReducer, useState } from 'react';
import type { ReactNode } from 'react';
import { getServer } from './model';
import type { ProxyConfig, ProxyDraft, ProxyLink } from './model';

interface Operation {
  ids: string[];
  phase: 'preparing' | 'applying' | 'active' | 'failed';
  fail: boolean;
}

interface WorkspaceState {
  proxies: ProxyDraft[];
  links: ProxyLink[];
  publishedLinks: ProxyLink[];
  operation?: Operation;
}

type Action =
  | { type: 'add'; proxy: ProxyDraft }
  | { type: 'move'; positions: Array<{ id: string; position: ProxyDraft['position'] }> }
  | { type: 'remove'; id: string }
  | { type: 'save'; id: string; config: ProxyConfig }
  | { type: 'connect'; connection: ProxyLink }
  | { type: 'disconnect'; id: string }
  | { type: 'revert-links' }
  | { type: 'start'; ids: string[]; fail: boolean }
  | { type: 'advance' }
  | { type: 'reset' };

const initialState: WorkspaceState = { proxies: [], links: [], publishedLinks: [] };

function reducer(state: WorkspaceState, action: Action): WorkspaceState {
  const busy = state.operation?.phase === 'preparing' || state.operation?.phase === 'applying';
  if (busy && action.type !== 'advance' && action.type !== 'move') return state;
  switch (action.type) {
    case 'add': return { ...state, proxies: [...state.proxies, action.proxy] };
    case 'move': return { ...state, proxies: state.proxies.map(proxy => {
      const changed = action.positions.find(position => position.id === proxy.id);
      return changed ? { ...proxy, position: changed.position } : proxy;
    }) };
    case 'remove': return { ...state, proxies: state.proxies.filter(proxy => proxy.id !== action.id) };
    case 'save': return { ...state, proxies: state.proxies.map(proxy => proxy.id === action.id ? { ...proxy, config: action.config, status: 'draft', dirty: true, failure: undefined } : proxy) };
    case 'connect': return {
      ...state, links: [...state.links.filter(link => link.source !== action.connection.source), action.connection],
      proxies: state.proxies.map(proxy => proxy.id === action.connection.source ? { ...proxy, dirty: true, status: 'draft' } : proxy),
    };
    case 'disconnect': return {
      ...state, links: state.links.filter(link => link.source !== action.id),
      proxies: state.proxies.map(proxy => proxy.id === action.id ? { ...proxy, dirty: true, status: 'draft' } : proxy),
    };
    case 'revert-links': return {
      ...state, links: state.publishedLinks,
      proxies: state.proxies.map(proxy => {
        const dirty = !proxy.published || JSON.stringify(proxy.config) !== JSON.stringify(proxy.published);
        return { ...proxy, dirty, status: dirty ? 'draft' : 'active', failure: undefined };
      }),
    };
    case 'start': return {
      ...state, operation: { ids: action.ids, phase: 'preparing', fail: action.fail },
      proxies: state.proxies.map(proxy => action.ids.includes(proxy.id) ? { ...proxy, status: 'preparing', failure: undefined } : proxy),
    };
    case 'advance': {
      if (!state.operation || !busy) return state;
      const phase = state.operation.phase === 'preparing' ? 'applying' : state.operation.fail ? 'failed' : 'active';
      const ids = state.operation.ids;
      return {
        ...state, operation: { ...state.operation, phase },
        publishedLinks: phase === 'active' ? [...state.publishedLinks.filter(link => !ids.includes(link.source)), ...state.links.filter(link => ids.includes(link.source))] : state.publishedLinks,
        proxies: state.proxies.map(proxy => ids.includes(proxy.id) ? {
          ...proxy, status: phase, dirty: phase !== 'active',
          published: phase === 'active' ? proxy.config : proxy.published,
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
  dispatch: React.Dispatch<Action>;
  applyError: (ids: string[]) => string | undefined;
}

const WorkspaceContext = createContext<WorkspaceContextValue | null>(null);

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState);
  const [simulateFailure, setSimulateFailure] = useState(false);
  const busy = state.operation?.phase === 'preparing' || state.operation?.phase === 'applying';
  useEffect(() => {
    if (!busy) return;
    const timer = window.setTimeout(() => dispatch({ type: 'advance' }), state.operation?.phase === 'preparing' ? 1000 : 1600);
    return () => window.clearTimeout(timer);
  }, [busy, state.operation?.phase]);
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (state.proxies.length) event.preventDefault();
    };
    window.addEventListener('beforeunload', beforeUnload);
    return () => window.removeEventListener('beforeunload', beforeUnload);
  }, [state.proxies.length]);
  const applyError = (ids: string[]) => {
    for (const id of ids) {
      const proxy = state.proxies.find(item => item.id === id);
      if (!proxy?.config) return '请先配置所有选中的入口。';
      const server = getServer(proxy.serverId);
      if (!server.online || !server.capabilitiesKnown) return `${server.name} 离线或能力未知，不能应用。`;
      if (state.links.some(link => link.source === id && state.proxies.find(item => item.id === link.target)?.dirty && !ids.includes(link.target))) return '下一跳仍有草稿，请在工作区一起应用相关变更。';
      if (state.links.some(link => link.target === id && state.proxies.find(item => item.id === link.source)?.dirty && !ids.includes(link.source))) return '上游仍有草稿，请在工作区一起应用相关变更。';
    }
    return undefined;
  };
  return <WorkspaceContext.Provider value={{ state, busy, dispatch, simulateFailure, setSimulateFailure, applyError }}>{children}</WorkspaceContext.Provider>;
}

export function useWorkspace() {
  const context = useContext(WorkspaceContext);
  if (!context) throw new Error('WorkspaceProvider is required');
  return context;
}
