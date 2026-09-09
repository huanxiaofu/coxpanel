export type Protocol = 'wireguard' | 'mixed' | 'vless-reality' | 'vmess' | 'trojan' | 'shadowsocks' | 'hysteria2' | 'tuic' | 'naive' | 'shadowtls' | 'anytls' | 'http';
export type DeploymentStatus = 'draft' | 'preparing' | 'applying' | 'active' | 'failed';
export type ServerStatus = 'online' | 'offline' | 'maintenance';

export interface ServerTraffic {
  limitBytes: number;
  usedBytes: number;
  resetDay: number;
  resetCycle: 'daily' | 'weekly' | 'monthly' | 'custom';
  customDays: number;
  resetAnchor: string;
  statsMode: 'total' | 'outbound';
}

export interface ServerAsset {
  id: string;
  name: string;
  region: string;
  country: string;
  profile: 'full';
  network: 'public';
  status: ServerStatus;
  resumeStatus?: 'online' | 'offline';
  tags: string[];
  publicIp: string;
  privateIp: string;
  agentVersion: string;
  lastHeartbeat: string;
  resources?: { cpuPercent: number; memUsedMb: number; memTotalMb: number; load: number };
  traffic: ServerTraffic;
  capabilitiesKnown: boolean;
  protocols: Protocol[];
  methods: string[];
  udp: boolean;
  chainTarget: boolean;
  maxProxies: number;
  memory: string;
  address: string;
  certificates: Array<{ id: string; name: string; domain: string }>;
}

export interface ProxyConfig {
  name: string;
  protocol: Protocol;
  exposure: 'subscription' | 'internal';
  listenPort: number;
  listenAddress: string;
  advertisedAddress: string;
  advertisedPort: number;
  sni: string;
  target: string;
  materialReady: boolean;
  shortIdReady: boolean;
  method: string;
  network: 'tcp' | 'tcp-udp';
  certificateRef: string;
  fingerprint: string;
  utls: boolean;
  alpn: string;
  obfs: 'none' | 'salamander';
  obfsReady: boolean;
  upMbps: number;
  downMbps: number;
  ignoreClientBandwidth: boolean;
  tcpFastOpen: boolean;
  multiplex: boolean;
}

export interface InboundResource {
  id: string;
  serverId: string;
  config: ProxyConfig;
  published?: ProxyConfig;
}

export interface EgressConfig {
  type: 'direct' | 'external';
  tag: string;
  domainStrategy: 'prefer_ipv4' | 'prefer_ipv6' | 'ipv4_only' | 'ipv6_only';
  bindInterface: string;
  sniff: boolean;
  dns: { type: 'udp' | 'tcp' | 'https' | 'tls' | 'hosts'; server: string };
  ruleSet: string;
}

export interface ChainHop {
  position: number;
  serverId: string;
  inboundId?: string;
  newInboundDraft?: ProxyConfig;
}

export interface ProxyDraft {
  id: string;
  name: string;
  chain: ChainHop[];
  egress: EgressConfig;
  published?: { name: string; chain: ChainHop[]; egress: EgressConfig };
  status: DeploymentStatus;
  dirty: boolean;
  failure?: string;
}

export const protocolCatalog: Array<{ value: Protocol; label: string; fields: string[] }> = [
  { value: 'wireguard', label: 'WireGuard', fields: ['监听 / MTU', '地址', 'Peer 公钥引用 / Allowed IPs', '保活'] },
  { value: 'mixed', label: 'Mixed', fields: ['HTTP + SOCKS 监听', '用户认证引用', '系统代理'] },
  { value: 'vless-reality', label: 'VLESS / Reality', fields: ['UUID 引用 / flow', 'TCP', 'Reality / TLS', 'uTLS'] },
  { value: 'vmess', label: 'VMess', fields: ['UUID 引用 / alterId', '传输', 'TLS', '复用'] },
  { value: 'trojan', label: 'Trojan', fields: ['密码引用', '传输', 'TLS / 证书', '复用'] },
  { value: 'shadowsocks', label: 'Shadowsocks / SS2022', fields: ['method', '密码引用', 'TCP / UDP', '复用'] },
  { value: 'hysteria2', label: 'Hysteria2', fields: ['UDP 监听', '认证 / obfs', '带宽', 'TLS / 证书'] },
  { value: 'tuic', label: 'TUIC', fields: ['UUID / 密码引用', '拥塞控制', 'UDP relay', 'TLS / ALPN'] },
  { value: 'naive', label: 'Naive', fields: ['用户认证引用', '网络', 'TLS / 证书'] },
  { value: 'shadowtls', label: 'ShadowTLS', fields: ['版本', '用户密码引用', '握手目标', 'strict mode'] },
  { value: 'anytls', label: 'AnyTLS', fields: ['用户密码引用', 'padding', 'TLS / 证书', '会话参数'] },
  { value: 'http', label: 'HTTP', fields: ['监听', '用户认证引用', 'TLS / 证书'] },
];

export const protocolLabels = Object.fromEntries(protocolCatalog.map(protocol => [protocol.value, protocol.label])) as Record<Protocol, string>;
export const statusLabels: Record<DeploymentStatus, string> = {
  draft: '草稿', preparing: '准备中 · 模拟', applying: '应用中 · 模拟', active: '已生效 · 模拟', failed: '应用失败 · 模拟',
};

export const serverStatusLabels: Record<ServerStatus, string> = { online: '在线', offline: '离线', maintenance: '维护中' };
const gibibyte = 1024 ** 3;
const fixtureTime = Date.now();
const heartbeatBefore = (minutes: number) => new Date(fixtureTime - minutes * 60_000).toISOString();
const trafficAnchor = new Date(new Date(fixtureTime).setUTCHours(0, 0, 0, 0)).toISOString();
const defaultTraffic: ServerTraffic = {
  limitBytes: 1024 * gibibyte, usedBytes: 0, resetDay: 1, resetCycle: 'monthly',
  customDays: 30, resetAnchor: trafficAnchor, statsMode: 'total',
};

export const serverAssets: ServerAsset[] = [
  {
    id: 'server-hk', name: 'HK-zouter', region: '香港', country: 'HK', profile: 'full', network: 'public',
    status: 'online', capabilitiesKnown: true, protocols: ['vless-reality', 'shadowsocks', 'hysteria2'],
    tags: ['HK', '旗舰', 'CN2'], publicIp: '203.0.113.10', privateIp: '10.0.1.10', agentVersion: '1.0.0-demo',
    lastHeartbeat: heartbeatBefore(0.2), resources: { cpuPercent: 24, memUsedMb: 768, memTotalMb: 2048, load: 0.42 },
    traffic: { ...defaultTraffic, limitBytes: 2 * 1024 * gibibyte, usedBytes: 812 * gibibyte, resetDay: 15 },
    methods: ['2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305'],
    udp: true, chainTarget: true, maxProxies: 12, memory: '2 GB', address: 'hk.example.invalid',
    certificates: [{ id: 'demo-cert-hk', name: 'HK 演示证书（合成引用）', domain: 'hk.example.invalid' }],
  },
  {
    id: 'server-sg', name: 'SG-edge', region: '新加坡', country: 'SG', profile: 'full', network: 'public',
    status: 'online', capabilitiesKnown: true, protocols: ['vless-reality', 'shadowsocks', 'hysteria2'],
    tags: ['SG', '标准'], publicIp: '198.51.100.20', privateIp: '10.0.2.20', agentVersion: '1.0.0-demo',
    lastHeartbeat: heartbeatBefore(0.5), resources: { cpuPercent: 68, memUsedMb: 846, memTotalMb: 1024, load: 1.28 },
    traffic: { ...defaultTraffic, usedBytes: 936 * gibibyte, resetCycle: 'weekly', resetDay: 1, statsMode: 'outbound' },
    methods: ['2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm'], udp: true, chainTarget: true,
    maxProxies: 8, memory: '1 GB', address: 'sg.example.invalid', certificates: [],
  },
  {
    id: 'server-us', name: 'US-standby', region: '美国', country: 'US', profile: 'full', network: 'public',
    status: 'offline', capabilitiesKnown: false, protocols: [], methods: [], udp: false, chainTarget: false,
    tags: ['US', '备用'], publicIp: '192.0.2.30', privateIp: '10.0.3.30', agentVersion: '未上报',
    lastHeartbeat: heartbeatBefore(180), traffic: { ...defaultTraffic, limitBytes: 0, usedBytes: 42 * gibibyte, resetCycle: 'daily' },
    maxProxies: 0, memory: '未上报', address: 'us.example.invalid', certificates: [],
  },
  {
    id: 'server-jp', name: 'JP-transit', region: '日本', country: 'JP', profile: 'full', network: 'public',
    status: 'maintenance', resumeStatus: 'online', capabilitiesKnown: true, protocols: ['vless-reality', 'shadowsocks'],
    tags: ['JP', '备用', '标准'], publicIp: '203.0.113.40', privateIp: '10.0.4.40', agentVersion: '1.0.0-demo',
    lastHeartbeat: heartbeatBefore(25), resources: { cpuPercent: 8, memUsedMb: 512, memTotalMb: 4096, load: 0.12 },
    traffic: { ...defaultTraffic, limitBytes: 500 * gibibyte, usedBytes: 523 * gibibyte, resetCycle: 'custom', customDays: 14 },
    methods: ['2022-blake3-aes-128-gcm'], udp: true, chainTarget: true,
    maxProxies: 16, memory: '4 GB', address: 'jp.example.invalid', certificates: [],
  },
];

export function getServer(serverId: string, servers: ServerAsset[] = serverAssets): ServerAsset {
  const server = servers.find(asset => asset.id === serverId);
  if (!server) throw new Error('找不到合成服务器');
  return server;
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const exponent = bytes > 0 ? Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1) : 0;
  return `${Number((bytes / 1024 ** exponent).toFixed(exponent >= 3 ? 2 : 0))} ${units[exponent]}`;
}

export function validateServerTraffic(traffic: ServerTraffic): string | undefined {
  if (![traffic.limitBytes, traffic.usedBytes].every(value => Number.isSafeInteger(value) && value >= 0)) return '流量必须是非负有限值，且不能超过安全整数范围。';
  if (!['daily', 'weekly', 'monthly', 'custom'].includes(traffic.resetCycle)) return '请选择有效刷新周期。';
  if (!['total', 'outbound'].includes(traffic.statsMode)) return '请选择有效流量统计方式。';
  if (!Number.isInteger(traffic.resetDay) || traffic.resetDay < 1 || traffic.resetDay > (traffic.resetCycle === 'weekly' ? 7 : 31)) return traffic.resetCycle === 'weekly' ? '请选择周一至周日。' : '每月刷新日须为 1–31 的整数。';
  if (!Number.isInteger(traffic.customDays) || traffic.customDays < 1 || traffic.customDays > 365) return '自定义周期须为 1–365 天的整数。';
  if (!Number.isFinite(Date.parse(traffic.resetAnchor))) return '刷新周期起点无效。';
}

export function nextTrafficReset(traffic: ServerTraffic, now = new Date()): Date {
  if (validateServerTraffic(traffic) || !Number.isFinite(now.getTime())) return new Date(NaN);
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  const dayMilliseconds = 86_400_000;
  if (traffic.resetCycle === 'daily') return new Date(today + dayMilliseconds);
  if (traffic.resetCycle === 'weekly') {
    const untilNext = (traffic.resetDay % 7 - now.getUTCDay() + 7) % 7 || 7;
    return new Date(today + untilNext * dayMilliseconds);
  }
  if (traffic.resetCycle === 'monthly') {
    const monthlyDate = (offset: number) => {
      const month = now.getUTCMonth() + offset;
      const lastDay = new Date(Date.UTC(now.getUTCFullYear(), month + 1, 0)).getUTCDate();
      return new Date(Date.UTC(now.getUTCFullYear(), month, Math.min(traffic.resetDay, lastDay)));
    };
    const candidate = monthlyDate(0);
    return candidate > now ? candidate : monthlyDate(1);
  }
  const anchorDate = new Date(traffic.resetAnchor);
  const anchor = Date.UTC(anchorDate.getUTCFullYear(), anchorDate.getUTCMonth(), anchorDate.getUTCDate());
  const period = traffic.customDays * dayMilliseconds;
  const intervals = Math.max(1, Math.floor((now.getTime() - anchor) / period) + 1);
  return new Date(anchor + intervals * period);
}

export function trafficCycleLabel(traffic: ServerTraffic): string {
  if (traffic.resetCycle === 'daily') return '每天';
  if (traffic.resetCycle === 'weekly') return `每周${['一', '二', '三', '四', '五', '六', '日'][traffic.resetDay - 1]}`;
  if (traffic.resetCycle === 'custom') return `每 ${traffic.customDays} 天`;
  return `每月 ${traffic.resetDay} 日`;
}

export interface ServerFilters {
  query: string;
  status: 'all' | ServerStatus;
  tag: string;
  region: string;
  sort: 'name' | 'status' | 'memory';
}

export function filterServers(servers: ServerAsset[], filters: ServerFilters): ServerAsset[] {
  const query = filters.query.trim().toLocaleLowerCase();
  const statusOrder: Record<ServerStatus, number> = { online: 0, maintenance: 1, offline: 2 };
  const memoryPercent = (server: ServerAsset) => server.resources && server.resources.memTotalMb > 0 ? server.resources.memUsedMb / server.resources.memTotalMb : -1;
  return servers.filter(server => {
    const searchable = [server.name, server.address, server.publicIp, server.privateIp, server.region, server.country, ...server.tags].join(' ').toLocaleLowerCase();
    return searchable.includes(query) && (filters.status === 'all' || server.status === filters.status) &&
      (!filters.tag || server.tags.includes(filters.tag)) && (!filters.region || server.country === filters.region);
  }).sort((first, second) => {
    const difference = filters.sort === 'status' ? statusOrder[first.status] - statusOrder[second.status] :
      filters.sort === 'memory' ? memoryPercent(second) - memoryPercent(first) : 0;
    return difference || first.name.localeCompare(second.name, 'zh-CN', { numeric: true });
  });
}

export function getInbound(inbounds: InboundResource[], hop: ChainHop) {
  return inbounds.find(inbound => inbound.id === hop.inboundId && inbound.serverId === hop.serverId);
}

export function inboundPortConflict(inbounds: InboundResource[], serverId: string, listenPort: number, exceptInboundId?: string) {
  return inbounds.find(inbound => inbound.id !== exceptInboundId && inbound.serverId === serverId &&
    [inbound.config.listenPort, inbound.published?.listenPort].includes(listenPort));
}

export function defaultConfig(server: ServerAsset, inbounds: InboundResource[]): ProxyConfig {
  let listenPort = 443;
  while (inboundPortConflict(inbounds, server.id, listenPort)) listenPort += 1;
  const protocol = server.protocols[0] ?? 'vless-reality';
  return {
    name: `${server.name}-${protocolLabels[protocol]}`, protocol, exposure: 'subscription', listenPort,
    listenAddress: '::', advertisedAddress: server.address, advertisedPort: listenPort,
    sni: 'example.invalid', target: 'example.invalid:443', materialReady: false, shortIdReady: false,
    method: server.methods[0] ?? '', network: 'tcp', certificateRef: '', fingerprint: 'chrome', utls: true,
    alpn: '', obfs: 'none', obfsReady: false, upMbps: 100, downMbps: 100, ignoreClientBandwidth: false,
    tcpFastOpen: false, multiplex: false,
  };
}

export function defaultEgress(): EgressConfig {
  return { type: 'direct', tag: 'direct', domainStrategy: 'prefer_ipv4', bindInterface: '', sniff: false, dns: { type: 'udp', server: 'dns.example.invalid' }, ruleSet: '' };
}

export function inboundReferences(proxies: ProxyDraft[], inboundId: string, includePublished = false): ProxyDraft[] {
  return proxies.filter(proxy => proxy.chain.some(hop => hop.inboundId === inboundId) ||
    (includePublished && proxy.published?.chain.some(hop => hop.inboundId === inboundId)));
}

export function egressSummary(egress: EgressConfig): string {
  if (egress.type === 'direct') return '本机直出 · direct';
  return '指定外部出口（后续支持）';
}

export function chainSummary(proxy: Pick<ProxyDraft, 'chain' | 'egress'>, servers: ServerAsset[] = serverAssets): string {
  return [...proxy.chain.map(hop => servers.find(server => server.id === hop.serverId)?.name ?? '待选服务器'), egressSummary(proxy.egress)].join(' → ');
}

export function chainReadiness(proxy: ProxyDraft, inbounds: InboundResource[], servers: ServerAsset[] = serverAssets): string | undefined {
  if (!proxy.name.trim()) return '代理节点名称不能为空。';
  if (!proxy.chain.length) return '至少需要一个订阅入口。';
  if (proxy.egress.type !== 'direct') return '指定外部出口后续支持，R1 请使用本机直出。';
  const seenInbounds = new Set<string>();
  for (const [position, hop] of proxy.chain.entries()) {
    if (hop.position !== position) return '链跳顺序不连续。';
    if (hop.newInboundDraft) return `第 ${position + 1} 跳：请先保存新入口草稿，再应用整条链。`;
    const inbound = getInbound(inbounds, hop);
    if (!inbound) return `第 ${position + 1} 跳：请选择已有入站或新建入口。`;
    if (seenInbounds.has(inbound.id)) return '同一条链不能重复经过同一个入站；可在不同链中复用。';
    seenInbounds.add(inbound.id);
    if (position === 0 && inbound.config.exposure !== 'subscription') return '第一跳必须是订阅入口；请更换或编辑该入站。';
    const server = servers.find(candidate => candidate.id === hop.serverId);
    if (!server) return `第 ${position + 1} 跳：服务器不存在。`;
    if (position > 0 && !server.chainTarget) return `第 ${position + 1} 跳：服务器不支持中转能力。`;
    const reason = inboundReadiness(inbound, servers);
    if (reason) return `第 ${position + 1} 跳：${reason}`;
    if (inboundPortConflict(inbounds, hop.serverId, inbound.config.listenPort, inbound.id)) return `第 ${position + 1} 跳：同机端口已被其他入口占用。`;
  }
}

export function inboundReadiness(inbound: InboundResource, servers: ServerAsset[] = serverAssets): string | undefined {
  const { config } = inbound;
  if (!['vless-reality', 'shadowsocks', 'hysteria2'].includes(config.protocol)) return '当前协议仅规划，不可应用。';
  const server = getServer(inbound.serverId, servers);
  if (server.status !== 'online' || !server.capabilitiesKnown || !server.protocols.includes(config.protocol)) return '服务器离线、维护中或协议能力不可用。';
  if (!config.name?.trim()) return '入口名称不能为空。';
  if (![config.listenPort, config.advertisedPort].every(port => Number.isInteger(port) && port >= 1 && port <= 65535)) return '端口必须为 1-65535 的整数。';
  const hostname = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$/;
  if (!config.listenAddress?.trim() || !config.advertisedAddress?.trim()) return '监听地址与公布地址不能为空。';
  if (config.protocol !== 'shadowsocks' && (!hostname.test(config.sni) || config.sni.length > 253)) return 'SNI 必须是合法域名。';
  if (config.protocol === 'vless-reality') {
    const target = /^(\[[0-9a-fA-F:.]+\]|[A-Za-z0-9.-]+):(\d+)$/.exec(config.target);
    if (!target || !Number.isInteger(Number(target[2])) || Number(target[2]) < 1 || Number(target[2]) > 65535) return 'Reality dest 必须是合法 host:port。';
    if (target[1].startsWith('[')) {
      try { new URL(`https://${config.target}`); } catch { return 'Reality dest 的 IPv6 地址不合法。'; }
    } else if (!hostname.test(target[1]) || target[1].length > 253) return 'Reality dest 主机名不合法。';
    if (!config.fingerprint?.trim() || config.network !== 'tcp') return 'Reality 需要指纹与 TCP 传输。';
  }
  if (!config.materialReady) return '请先生成合成认证材料引用。';
  if (config.protocol === 'vless-reality' && !config.shortIdReady) return 'Reality Short ID 尚未就绪。';
  if (config.protocol === 'shadowsocks' && !server.methods.includes(config.method)) return 'Shadowsocks 方法不在能力清单。';
  if (config.protocol === 'hysteria2' && (!server.udp || !server.certificates.some(certificate => certificate.id === config.certificateRef))) return 'Hysteria2 需要 UDP 能力和可用证书引用。';
  if (config.protocol === 'hysteria2' && config.obfs === 'salamander' && !config.obfsReady) return '请生成 obfs 合成密码引用。';
  if (config.protocol === 'hysteria2' && ![config.upMbps, config.downMbps].every(bandwidth => Number.isFinite(bandwidth) && bandwidth >= 0)) return 'Hysteria2 带宽必须是非负数字。';
  if (config.network === 'tcp-udp' && !server.udp) return '服务器未声明 UDP 能力。';
  return undefined;
}
