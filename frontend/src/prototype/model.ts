export type Protocol = "vless-reality" | "shadowsocks" | "hysteria2";
export type DeploymentStatus = "draft" | "preparing" | "applying" | "active" | "failed";

export interface ServerAsset {
  id: string;
  name: string;
  region: string;
  country: string;
  profile: "full" | "lite";
  network: "public" | "nat";
  online: boolean;
  capabilitiesKnown: boolean;
  protocols: Protocol[];
  methods: string[];
  udp: boolean;
  chainTarget: boolean;
  maxProxies: number;
  memory: string;
  address: string;
  certificates: Array<{ id: string; name: string; domain: string }>;
  portMappings: Array<{ listenPort: number; publicPort: number }>;
}

export interface ProxyConfig {
  name: string;
  protocol: Protocol;
  exposure: "subscription" | "internal";
  listenPort: number;
  listenAddress: string;
  advertisedAddress: string;
  advertisedPort: number;
  sni: string;
  target: string;
  materialReady: boolean;
  shortIdReady: boolean;
  method: string;
  network: "tcp" | "tcp-udp";
  certificateRef: string;
}

export interface ProxyDraft {
  id: string;
  serverId: string;
  position: { x: number; y: number };
  config?: ProxyConfig;
  published?: ProxyConfig;
  status: DeploymentStatus;
  dirty: boolean;
  failure?: string;
}

export interface ProxyLink {
  source: string;
  target: string;
}

export const protocolLabels: Record<Protocol, string> = {
  "vless-reality": "Reality",
  shadowsocks: "Shadowsocks",
  hysteria2: "Hysteria2",
};

export const statusLabels: Record<DeploymentStatus, string> = {
  draft: "草稿",
  preparing: "准备中 · 模拟",
  applying: "应用中 · 模拟",
  active: "已生效 · 模拟",
  failed: "应用失败 · 模拟",
};

export const serverAssets: ServerAsset[] = [
  {
    id: "server-hk", name: "HK-zouter", region: "香港", country: "HK", profile: "full",
    network: "public", online: true, capabilitiesKnown: true,
    protocols: ["vless-reality", "shadowsocks", "hysteria2"],
    methods: ["2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305"],
    udp: true, chainTarget: true, maxProxies: 12, memory: "2 GB", address: "hk.example.invalid",
    certificates: [{ id: "demo-cert-hk", name: "HK 演示证书（合成引用）", domain: "hk.example.invalid" }], portMappings: [],
  },
  {
    id: "server-sg", name: "SG-edge", region: "新加坡", country: "SG", profile: "full",
    network: "public", online: true, capabilitiesKnown: true,
    protocols: ["vless-reality", "shadowsocks", "hysteria2"],
    methods: ["2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"],
    udp: true, chainTarget: true, maxProxies: 8, memory: "1 GB", address: "sg.example.invalid",
    certificates: [], portMappings: [],
  },
  {
    id: "server-jp", name: "NAT-JP", region: "日本", country: "JP", profile: "lite",
    network: "nat", online: true, capabilitiesKnown: true, protocols: ["shadowsocks"],
    methods: ["2022-blake3-aes-128-gcm"], udp: false, chainTarget: false,
    maxProxies: 2, memory: "64 MB · 目标档", address: "jp.example.invalid", certificates: [],
    portMappings: [{ listenPort: 10080, publicPort: 21080 }, { listenPort: 10081, publicPort: 21081 }],
  },
  {
    id: "server-us", name: "US-standby", region: "美国", country: "US", profile: "full",
    network: "public", online: false, capabilitiesKnown: false, protocols: [], methods: [],
    udp: false, chainTarget: false, maxProxies: 0, memory: "未上报", address: "us.example.invalid",
    certificates: [], portMappings: [],
  },
];

export function getServer(serverId: string): ServerAsset {
  const server = serverAssets.find(asset => asset.id === serverId);
  if (!server) throw new Error("找不到合成服务器");
  return server;
}

export function defaultConfig(server: ServerAsset, proxies: ProxyDraft[]): ProxyConfig {
  const usedPorts = new Set(proxies.filter(proxy => proxy.serverId === server.id).flatMap(proxy => [proxy.config?.listenPort, proxy.published?.listenPort]));
  let listenPort = server.profile === "lite" ? 10080 : 443;
  while (usedPorts.has(listenPort)) listenPort += 1;
  if (server.network === "nat") listenPort = server.portMappings.find(mapping => !usedPorts.has(mapping.listenPort))?.listenPort ?? 10080;
  const protocol = server.protocols[0] ?? "vless-reality";
  return {
    name: `${server.name}-${protocolLabels[protocol]}`, protocol, exposure: server.profile === "lite" ? "internal" : "subscription",
    listenPort, listenAddress: "::", advertisedAddress: server.address,
    advertisedPort: server.portMappings.find(mapping => mapping.listenPort === listenPort)?.publicPort ?? listenPort,
    sni: "example.invalid", target: "example.invalid:443", materialReady: false, shortIdReady: false,
    method: server.methods[0] ?? "", network: "tcp", certificateRef: "",
  };
}

export function connectionError(proxies: ProxyDraft[], links: ProxyLink[], connection: ProxyLink): string | undefined {
  const source = proxies.find(proxy => proxy.id === connection.source);
  const target = proxies.find(proxy => proxy.id === connection.target);
  if (!source?.config || !target?.config) return "请先配置并保存两张入口卡，再建立连线。";
  if (source.id === target.id) return "入口不能连接到自己。";
  if (getServer(source.serverId).profile === "lite") return "lite 首版仅支持本机直出，不能作为连线源。";
  if (!getServer(source.serverId).online || !getServer(target.serverId).online) return "服务器离线，不能建立新连线。";
  if (target.config.exposure !== "internal") return "下一跳必须是独立的内部入口，不能使用订阅入口。";
  if (!getServer(target.serverId).chainTarget) return "目标尚未通过链路能力门禁；lite 联合发布留待 R3 验证。";
  if (links.some(link => link.target === target.id && link.source !== source.id)) return "该内部入口已有上游，请创建独立内部入口。";
  const candidate = [...links.filter(link => link.source !== source.id), connection];
  for (const start of proxies) {
    const visitedProxies = new Set<string>();
    const visitedServers = new Set<string>();
    let cursor: ProxyDraft | undefined = start;
    while (cursor) {
      if (visitedProxies.has(cursor.id)) return "不允许形成有向环。";
      if (visitedServers.has(cursor.serverId)) return "同一条链不能重复经过同一台服务器。";
      visitedProxies.add(cursor.id);
      visitedServers.add(cursor.serverId);
      if (visitedProxies.size > 8) return "一条链最多支持 8 个入口。";
      const nextId = candidate.find(link => link.source === cursor?.id)?.target;
      cursor = proxies.find(proxy => proxy.id === nextId);
    }
  }
  return undefined;
}
