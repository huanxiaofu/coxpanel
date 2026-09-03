// 后端 API 封装
const BASE = import.meta.env.VITE_API_BASE || '';

export interface Node {
  id: number;
  name: string;
  type: 'managed' | 'external';
  publicIp?: string;
  easyIp?: string;
  coreVersion?: string;
  status: string;
  extProtocol?: string;
  extParams?: any;
}

export interface Inbound {
  id: number;
  nodeId: number;
  name: string;
  protocol: string;
  role: 'entry' | 'landing' | 'relay';
  listenAddr: string;
  listenPort: number;
  config: any;
  minClientVer: string;
}

export interface Subscription {
  id: number;
  name: string;
  token: string;
  format: string;
}

function token(): string {
  return localStorage.getItem('coxpanel_token') || '';
}

async function req(path: string, method = 'GET', body?: any): Promise<any> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const t = token();
  if (t) headers['Authorization'] = `Bearer ${t}`;
  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401) {
    localStorage.removeItem('coxpanel_token');
    localStorage.removeItem('coxpanel_user');
    window.location.href = '/login';
    throw new Error('未登录');
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data?.error?.message || `HTTP ${res.status}`);
  }
  return data;
}

export const api = {
  // 认证
  login: (username: string, password: string) =>
    req('/api/auth/login', 'POST', { username, password }),
  register: (username: string, password: string, email: string, inviteCode: string) =>
    req('/api/auth/register', 'POST', { username, password, email, inviteCode }),
  me: () => req('/api/me'),

  // 节点（admin）
  listNodes: (): Promise<Node[]> => req('/api/nodes/'),
  createNode: (data: any) => req('/api/nodes/', 'POST', data),
  updateNode: (id: number, data: any) => req(`/api/nodes/${id}/`, 'PUT', data),
  deleteNode: (id: number) => req(`/api/nodes/${id}/`, 'DELETE'),
  listInbounds: (nodeId: number): Promise<Inbound[]> => req(`/api/nodes/${nodeId}/inbounds`),
  createInbound: (nodeId: number, data: any) => req(`/api/nodes/${nodeId}/inbounds`, 'POST', data),
  deleteInbound: (nodeId: number, inboundId: number) =>
    req(`/api/nodes/${nodeId}/inbounds/${inboundId}`, 'DELETE'),

  // 邀请码（admin）
  listInvites: () => req('/api/invites/'),
  genInvite: () => req('/api/invites/', 'POST', {}),

  // 订阅（用户）
  listSubs: (): Promise<Subscription[]> => req('/api/my/subscriptions'),
  createSub: (name: string, format = 'mihomo') =>
    req('/api/my/subscriptions', 'POST', { name, format }),
  deleteSub: (id: number) => req(`/api/my/subscriptions/${id}`, 'DELETE'),
  saveOverride: (subId: number, nodeId: number, data: any) =>
    req(`/api/my/subscriptions/${subId}/overrides/${nodeId}`, 'PUT', data),

  // 订阅 URL
  subUrl: (t: string) => `${BASE}/sub/${t}`,
};
