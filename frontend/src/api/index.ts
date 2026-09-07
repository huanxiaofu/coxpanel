const BASE = import.meta.env.VITE_API_BASE || '';

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

export type UserRole = 'user' | 'admin' | 'owner' | string;

export interface AuthUser {
  id: number;
  username: string;
  email?: string | null;
  role: UserRole;
}

export interface Node {
  id: number;
  name: string;
  type: 'managed' | 'external' | string;
  publicIp?: string | null;
  easyIp?: string | null;
  coreVersion?: string | null;
  status?: string | null;
  extProtocol?: string | null;
  extParams?: Record<string, unknown> | null;
}

export interface Inbound {
  id: number;
  nodeId: number;
  name: string;
  protocol: 'vless-reality' | 'shadowsocks' | 'hysteria2' | string;
  role: 'entry' | 'landing' | 'relay' | string;
  listenAddr?: string | null;
  listenPort?: number | null;
  config?: Record<string, unknown> | null;
}

export type SupportedSubscriptionFormat = 'mihomo' | 'base64';

export interface Subscription {
  id: number;
  name: string;
  token?: string | null;
  format: SupportedSubscriptionFormat | string;
  nodeGroupId?: number | null;
}

export interface TopologyEdge {
  fromInboundId: number;
  toNodeId: number;
  toInboundId: number;
}

export interface TopologyDraft {
  nodeId: number;
  edges: TopologyEdge[];
  revision?: string | number | null;
}

export interface TopologyPreview {
  version: string;
  config: unknown;
}

export interface NodeGroup {
  id: number;
  name: string;
  nodeIds?: number[] | null;
}

export interface AdminUser {
  id: number;
  username: string;
  email?: string | null;
  role?: UserRole | null;
  groupIds?: number[] | null;
}

export interface Invite {
  id?: number;
  code: string;
  nodeGroupId?: number | null;
  maxUses?: number | null;
  usedCount?: number | null;
  expiresAt?: string | null;
}

function token(): string {
  return localStorage.getItem('coxpanel_token') || '';
}

async function req(path: string, method = 'GET', body?: unknown): Promise<any> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const t = token();
  if (t) headers.Authorization = `Bearer ${t}`;
  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401) {
    localStorage.removeItem('coxpanel_token');
    localStorage.removeItem('coxpanel_user');
    window.location.href = '/login';
    throw new Error('未登录');
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError(res.status, data?.error?.message || `HTTP ${res.status}`);
  }
  return data;
}

export const api = {
  login: (username: string, password: string) =>
    req('/api/auth/login', 'POST', { username, password }),
  register: (username: string, password: string, email: string, inviteCode: string) =>
    req('/api/auth/register', 'POST', { username, password, email, inviteCode }),
  me: (): Promise<AuthUser> => req('/api/me'),

  listNodes: (): Promise<Node[]> => req('/api/nodes/'),
  createNode: (data: Record<string, unknown>) => req('/api/nodes/', 'POST', data),
  updateNode: (id: number, data: Record<string, unknown>) => req(`/api/nodes/${id}/`, 'PUT', data),
  deleteNode: (id: number) => req(`/api/nodes/${id}/`, 'DELETE'),
  listInbounds: (nodeId: number): Promise<Inbound[]> => req(`/api/nodes/${nodeId}/inbounds`),
  createInbound: (nodeId: number, data: Record<string, unknown>) =>
    req(`/api/nodes/${nodeId}/inbounds`, 'POST', data),
  deleteInbound: (nodeId: number, inboundId: number) =>
    req(`/api/nodes/${nodeId}/inbounds/${inboundId}`, 'DELETE'),

  listInvites: (): Promise<Invite[]> => req('/api/invites/'),
  genInvite: (groupId: number) => req('/api/invites/', 'POST', { groupId }),

  listSubs: (): Promise<Subscription[]> => req('/api/my/subscriptions'),
  listMyGroups: (): Promise<Array<{ id: number; name: string }>> => req('/api/my/groups'),
  createSub: (name: string, format: SupportedSubscriptionFormat, nodeGroupId: number) =>
    req('/api/my/subscriptions', 'POST', { name, format, nodeGroupId }),
  deleteSub: (id: number) => req(`/api/my/subscriptions/${id}`, 'DELETE'),
  saveOverride: (subId: number, nodeId: number, data: Record<string, unknown>) =>
    req(`/api/my/subscriptions/${subId}/overrides/${nodeId}`, 'PUT', data),

  getTopology: (nodeId: number): Promise<TopologyDraft> => req(`/api/topology/${nodeId}`),
  saveTopology: (nodeId: number, draft: TopologyDraft): Promise<TopologyDraft> =>
    req(`/api/topology/${nodeId}`, 'PUT', draft),
  previewTopology: (nodeId: number): Promise<TopologyPreview> =>
    req(`/api/topology/${nodeId}/preview`, 'POST'),
  deployTopology: (nodeId: number, version: string): Promise<{ version: string; deployed: boolean }> =>
    req(`/api/topology/${nodeId}/deploy`, 'POST', { version }),

  listGroups: (): Promise<NodeGroup[]> => req('/api/groups'),
  createGroup: (name: string): Promise<NodeGroup> => req('/api/groups', 'POST', { name }),
  setGroupNodes: (groupId: number, nodeIds: number[]) =>
    req(`/api/groups/${groupId}/nodes`, 'PUT', { nodeIds }),
  listUsers: (): Promise<AdminUser[]> => req('/api/users'),
  setUserGroups: (userId: number, groupIds: number[]) =>
    req(`/api/users/${userId}/groups`, 'PUT', { groupIds }),

  subUrl: (t: string) => `${(BASE || window.location.origin).replace(/\/$/, '')}/sub/${encodeURIComponent(t)}`,
};
