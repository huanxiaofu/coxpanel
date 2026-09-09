import { useEffect, useRef, useState } from "react";
import type { ReactElement } from "react";
import {
  Alert,
  Button,
  Drawer,
  Form,
  Grid,
  Input,
  InputNumber,
  Modal,
  Radio,
  Space,
  Tabs,
  Tag,
  Tooltip,
  Typography,
  theme,
} from "antd";
import {
  defaultConfig,
  getInbound,
  getServer,
  inboundPortConflict,
  protocolCatalog,
  protocolLabels,
} from "./model";
import type { InboundResource, Protocol, ProxyConfig, ProxyDraft, ServerAsset } from "./model";
import InboundProtocolFields from "./InboundProtocolFields";
import type { InboundFormSection } from "./InboundProtocolFields";

export interface ProxyConfigFormProps {
  proxy: ProxyDraft;
  inbounds: InboundResource[];
  referenceCount: number;
  egressLabel: string;
  onSave: (config: ProxyConfig, apply: boolean) => void;
  onClose: () => void;
}

type SubmitIntent = "draft" | "apply";
type FormPath = Array<string | number>;

const IMPLEMENTED_PROTOCOLS: Protocol[] = ["vless-reality", "shadowsocks", "hysteria2"];
const SECTION_ITEMS: Array<{ key: InboundFormSection | "basic" | "listen"; label: string }> = [
  { key: "basic", label: "基础" },
  { key: "listen", label: "监听" },
  { key: "protocol", label: "协议" },
  { key: "security", label: "安全" },
  { key: "tls", label: "TLS" },
  { key: "advanced", label: "高级" },
];

const HOSTNAME_PATTERN = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$/;

function isPortNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 1 && value <= 65535;
}

function isValidHostname(value: string): boolean {
  const normalized = value.trim();
  return normalized.length > 0 && normalized.length <= 253 && HOSTNAME_PATTERN.test(normalized);
}

function isValidIpv4(value: string): boolean {
  const octets = value.split(".");
  return octets.length === 4 && octets.every((octet) => {
    if (!/^\d{1,3}$/.test(octet)) return false;
    const number = Number(octet);
    return number >= 0 && number <= 255 && String(number) === octet.replace(/^0+(?=\d)/, "");
  });
}

function isValidIpv6(value: string): boolean {
  const normalized = value.trim();
  if (!normalized.includes(":")) return false;
  if (normalized.includes("%") || normalized.includes("/") || normalized.includes(":::")) return false;
  const sections = normalized.split("::");
  if (sections.length > 2) return false;
  const left = sections[0] ? sections[0].split(":") : [];
  const right = sections.length === 2 && sections[1] ? sections[1].split(":") : [];
  const groups = [...left, ...right];
  const embeddedIpv4Index = groups.findIndex((group) => group.includes("."));
  const validGroups = groups.every((group, index) => {
    if (!group.includes(".")) return /^[0-9A-Fa-f]{1,4}$/.test(group);
    return index === groups.length - 1 && isValidIpv4(group);
  });
  if (!validGroups) return false;
  const addressGroupCount = groups.length + (embeddedIpv4Index >= 0 ? 1 : 0);
  return sections.length === 2 ? addressGroupCount < 8 : addressGroupCount === 8;
}

function isValidIpAddress(value: string): boolean {
  const normalized = value.trim();
  return isValidIpv4(normalized) || isValidIpv6(normalized);
}

function isValidAddress(value: string): boolean {
  const normalized = value.trim();
  const unwrapped = normalized.startsWith("[") && normalized.endsWith("]")
    ? normalized.slice(1, -1)
    : normalized;
  return isValidIpAddress(unwrapped) || isValidHostname(unwrapped);
}

function isValidTarget(value: string): boolean {
  const normalized = value.trim();
  const bracketed = /^\[([^\]]+)\]:(\d+)$/.exec(normalized);
  if (bracketed) return isValidIpAddress(bracketed[1]) && isPortNumber(Number(bracketed[2]));
  const separator = normalized.lastIndexOf(":");
  if (separator <= 0 || separator === normalized.length - 1) return false;
  const host = normalized.slice(0, separator);
  return !host.includes(":") && isValidAddress(host) && isPortNumber(Number(normalized.slice(separator + 1)));
}

function targetHost(value: string): string | undefined {
  const normalized = value.trim();
  const bracketed = /^\[([^\]]+)\]:\d+$/.exec(normalized);
  if (bracketed) return bracketed[1];
  const separator = normalized.lastIndexOf(":");
  return separator > 0 ? normalized.slice(0, separator) : undefined;
}

function fieldPath(value: unknown): FormPath | null {
  if (typeof value === "string" || typeof value === "number") return [value];
  if (!Array.isArray(value)) return null;
  const path = value.filter((part): part is string | number => typeof part === "string" || typeof part === "number");
  return path.length > 0 ? path : null;
}

function sectionForPath(path: FormPath): string {
  switch (path[0]) {
    case "listenPort":
    case "listenAddress":
    case "advertisedAddress":
    case "advertisedPort":
      return "listen";
    case "protocol":
    case "method":
    case "network":
    case "sni":
    case "target":
      return "protocol";
    case "certificateRef":
    case "fingerprint":
    case "utls":
    case "alpn":
      return "tls";
    case "materialReady":
    case "shortIdReady":
    case "obfs":
    case "obfsReady":
      return "security";
    case "upMbps":
    case "downMbps":
    case "ignoreClientBandwidth":
    case "tcpFastOpen":
    case "multiplex":
      return "advanced";
    default:
      return "basic";
  }
}

function createInitialConfig(proxy: ProxyDraft, inbounds: InboundResource[]): ProxyConfig {
  const server = getServer(proxy.serverId);
  const inbound = getInbound(inbounds, proxy);
  const config = { ...(inbound?.config ?? defaultConfig(server, inbounds)) };
  return config;
}

function normalizeConfig(config: ProxyConfig): ProxyConfig {
  const normalizedConfig = {
    ...config,
    name: config.name.trim(),
    listenAddress: config.listenAddress.trim(),
    advertisedAddress: config.advertisedAddress.trim(),
    sni: config.sni.trim(),
    target: config.target.trim(),
    certificateRef: (config.certificateRef ?? "").trim(),
    fingerprint: config.fingerprint.trim(),
    alpn: config.alpn.trim(),
  };
  if (normalizedConfig.protocol === "hysteria2") {
    normalizedConfig.network = "tcp-udp";
    normalizedConfig.tcpFastOpen = false;
    normalizedConfig.multiplex = false;
  }
  if (normalizedConfig.protocol === "vless-reality") normalizedConfig.network = "tcp";
  return normalizedConfig;
}

function protocolGate(protocol: Protocol, server: ServerAsset, published: boolean): string | undefined {
  if (published) return "已发布入口锁定协议。";
  if (!IMPLEMENTED_PROTOCOLS.includes(protocol)) return "该协议仅展示规划字段，当前原型不能保存或应用。";
  if (!server.capabilitiesKnown) return "服务器能力未知，不能应用协议。";
  if (!server.protocols.includes(protocol)) return `服务器未声明支持${protocolLabels[protocol]}。`;
  return undefined;
}

function applyBlockers(config: ProxyConfig, server: ServerAsset, inbound: InboundResource | undefined, inbounds: InboundResource[]): string[] {
  const blockers: string[] = [];
  if (!IMPLEMENTED_PROTOCOLS.includes(config.protocol)) blockers.push("当前协议仍在规划中，不能应用。");
  if (!server.online) blockers.push("服务器离线，暂不能应用。");
  if (!server.capabilitiesKnown) blockers.push("服务器能力未知，暂不能应用。");
  if (!server.protocols.includes(config.protocol)) blockers.push(`服务器未声明支持${protocolLabels[config.protocol]}。`);
  if (server.maxProxies > 0 && inbounds.filter((candidate) => candidate.serverId === server.id && candidate.id !== inbound?.id).length >= server.maxProxies) blockers.push(`服务器入口资源容量已满（${server.maxProxies}）。`);
  if (!isPortNumber(config.listenPort)) blockers.push("监听端口必须是 1-65535 的整数。");
  if (isPortNumber(config.listenPort) && inboundPortConflict(inbounds, server.id, config.listenPort, inbound?.id)) blockers.push("同机端口已被其它入口资源占用。");

  if (config.protocol === "vless-reality") {
    if (!isValidHostname(config.sni)) blockers.push("Reality SNI 必须是合法域名。");
    if (!isValidTarget(config.target)) blockers.push("Reality dest 必须是合法 host:port。");
    if (!config.materialReady) blockers.push("Reality 合成密钥 / UUID 尚未就绪。");
    if (!config.shortIdReady) blockers.push("Reality 合成 Short ID 尚未就绪。");
    if (!config.fingerprint.trim()) blockers.push("Reality 指纹不能为空。");
  }
  if (config.protocol === "shadowsocks") {
    if (!config.method || !server.methods.includes(config.method)) blockers.push("Shadowsocks 方法必须来自服务器能力清单。");
    if (!config.materialReady) blockers.push("Shadowsocks 合成密码尚未就绪。");
    if (config.network === "tcp-udp" && !server.udp) blockers.push("服务器未声明 UDP 能力。");
  }
  if (config.protocol === "hysteria2") {
    if (!server.udp) blockers.push("Hysteria2 需要服务器 UDP 能力。");
    if (!isValidHostname(config.sni)) blockers.push("Hysteria2 SNI 必须是合法域名。");
    if (!config.materialReady) blockers.push("Hysteria2 合成认证密码尚未就绪。");
    if (!server.certificates.some((certificate) => certificate.id === config.certificateRef)) blockers.push("Hysteria2 必须引用服务器证书清单中的合成证书。");
    if (config.obfs === "salamander" && !config.obfsReady) blockers.push("salamander 混淆密码尚未就绪。");
    if (!Number.isFinite(config.upMbps) || config.upMbps < 0 || !Number.isFinite(config.downMbps) || config.downMbps < 0) blockers.push("Hysteria2 带宽必须是非负数字。");
  }
  return blockers;
}

function normalizeProtocolValue(protocol: Protocol, server: ServerAsset, current: Partial<ProxyConfig>): Partial<ProxyConfig> {
  if (protocol === "vless-reality") return { protocol, network: "tcp", method: "", target: "", certificateRef: "", obfs: "none" };
  if (protocol === "shadowsocks") return { protocol, network: current.network === "tcp-udp" && server.udp ? "tcp-udp" : "tcp", method: server.methods.includes(current.method ?? "") ? current.method : server.methods[0] ?? "", sni: "", target: "", certificateRef: "", obfs: "none" };
  if (protocol === "hysteria2") return { protocol, network: "tcp-udp", method: "", target: "", certificateRef: server.certificates.some((certificate) => certificate.id === current.certificateRef) ? current.certificateRef : server.certificates[0]?.id ?? "", obfs: "none", tcpFastOpen: false, multiplex: false };
  return { protocol, network: "tcp", method: "", sni: "", target: "", certificateRef: "", obfs: "none" };
}

function HiddenReadinessFields(): ReactElement {
  return (
    <>
      <Form.Item name="protocol" hidden><Input /></Form.Item>
      <Form.Item name="materialReady" valuePropName="checked" hidden><input type="checkbox" aria-hidden="true" tabIndex={-1} /></Form.Item>
      <Form.Item name="shortIdReady" valuePropName="checked" hidden><input type="checkbox" aria-hidden="true" tabIndex={-1} /></Form.Item>
      <Form.Item name="obfsReady" valuePropName="checked" hidden><input type="checkbox" aria-hidden="true" tabIndex={-1} /></Form.Item>
    </>
  );
}

export default function ProxyConfigForm({ proxy, inbounds, referenceCount, egressLabel, onSave, onClose }: ProxyConfigFormProps): ReactElement {
  const server = getServer(proxy.serverId);
  const inbound = getInbound(inbounds, proxy);
  const [initialConfig] = useState<ProxyConfig>(() => createInitialConfig(proxy, inbounds));
  const [form] = Form.useForm<ProxyConfig>();
  const [activeSection, setActiveSection] = useState("basic");
  const [activeProtocol, setActiveProtocol] = useState<Protocol>(initialConfig.protocol);
  const [hasChanges, setHasChanges] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [submitIntent, setSubmitIntent] = useState<SubmitIntent | null>(null);
  const [syncNotice, setSyncNotice] = useState<string>();
  const [, setFormRevision] = useState(0);
  const returnFocusRef = useRef<HTMLElement | null>(null);
  const advertisedPortManualRef = useRef(initialConfig.advertisedPort !== initialConfig.listenPort);
  const [modal, modalContextHolder] = Modal.useModal();
  const { token } = theme.useToken();
  const screens = Grid.useBreakpoint();
  const isPublished = Boolean(inbound?.published);
  const isNewInbound = !inbound;

  useEffect(() => {
    const activeElement = document.activeElement;
    if (activeElement instanceof HTMLElement && !activeElement.closest(".ant-drawer")) returnFocusRef.current = activeElement;
  }, []);

  const currentConfig = normalizeConfig({ ...initialConfig, ...form.getFieldsValue(true), protocol: activeProtocol });
  const blockers = applyBlockers(currentConfig, server, inbound, inbounds);
  const canApply = blockers.length === 0;
  const canSave = IMPLEMENTED_PROTOCOLS.includes(currentConfig.protocol);
  const resourceCount = inbounds.filter((candidate) => candidate.serverId === server.id).length;
  const occupiedResource = isPortNumber(currentConfig.listenPort) ? inboundPortConflict(inbounds, server.id, currentConfig.listenPort, inbound?.id) : undefined;
  const docked = Boolean(screens.xl);
  const drawerWidth = docked || screens.xs ? "100%" : 560;

  const updateForm = (values: Partial<ProxyConfig>) => {
    form.setFieldsValue(values);
    setHasChanges(true);
    setFormRevision((revision) => revision + 1);
  };

  const handleValuesChange = (changedValues: Partial<ProxyConfig>) => {
    setHasChanges(true);
    setFormRevision((revision) => revision + 1);
    if (Object.prototype.hasOwnProperty.call(changedValues, "target")) setSyncNotice(undefined);
    if (Object.prototype.hasOwnProperty.call(changedValues, "advertisedPort")) advertisedPortManualRef.current = true;
    if (Object.prototype.hasOwnProperty.call(changedValues, "listenPort") && !advertisedPortManualRef.current && isPortNumber(changedValues.listenPort)) {
      form.setFieldsValue({ advertisedPort: changedValues.listenPort });
    }
  };

  const selectProtocol = (nextProtocol: Protocol) => {
    if (isPublished && nextProtocol !== currentConfig.protocol) return;
    updateForm({ ...normalizeProtocolValue(nextProtocol, server, currentConfig), materialReady: false, shortIdReady: false, obfsReady: false });
    setActiveProtocol(nextProtocol);
    setActiveSection("protocol");
  };

  const syncSni = () => {
    const host = targetHost(String(form.getFieldValue("target") ?? ""));
    if (!host || !isValidHostname(host)) {
      setSyncNotice("请先填写带合法域名主机的 dest，再提取 SNI。");
      return;
    }
    updateForm({ sni: host });
    setSyncNotice(`已从 dest 提取 ${host}。`);
  };

  const restoreFocus = () => {
    const element = returnFocusRef.current;
    if (element && document.contains(element)) element.focus({ preventScroll: true });
  };

  const closeImmediately = () => {
    onClose();
    globalThis.setTimeout(restoreFocus, 0);
  };

  const requestClose = () => {
    if (!hasChanges) {
      closeImmediately();
      return;
    }
    modal.confirm({ title: "放弃未保存更改？", content: isNewInbound ? "关闭会取消这个新入口资源。" : "关闭会丢弃当前表单中的未保存更改。", okText: "确认关闭", cancelText: "继续编辑", onOk: closeImmediately });
  };

  const handleFinish = (values: ProxyConfig) => {
    const config = normalizeConfig({ ...initialConfig, ...form.getFieldsValue(true), ...values, protocol: activeProtocol });
    if (!IMPLEMENTED_PROTOCOLS.includes(config.protocol)) {
      setSubmitting(false);
      setSubmitIntent(null);
      return;
    }
    try {
      onSave(config, false);
      setHasChanges(false);
    } finally {
      setSubmitting(false);
      setSubmitIntent(null);
    }
  };

  const focusValidationError = (errorInfo: { errorFields?: Array<{ name: unknown }> }) => {
    const path = fieldPath(errorInfo.errorFields?.[0]?.name);
    if (!path) return;
    setActiveSection(sectionForPath(path));
    globalThis.setTimeout(() => form.scrollToField(path), 0);
  };

  const confirmApply = (config: ProxyConfig) => {
    modal.confirm({
      title: isNewInbound ? "确认创建并应用入口资源？" : "确认应用入口更改？",
      content: (
        <Space orientation="vertical" size={8}>
          <Typography.Text>入口：{config.name}</Typography.Text>
          <Typography.Text>监听端口：{config.listenPort}</Typography.Text>
          <Typography.Text>节点引用：{referenceCount} 个</Typography.Text>
          {referenceCount > 0 && <Alert type="warning" showIcon title="这是共享入口资源" description="应用后会影响所有引用该入口的节点。" />}
          <Typography.Text type="secondary">确认框不展示任何材料、密码或密钥内容。</Typography.Text>
        </Space>
      ),
      okText: isNewInbound ? "确认创建并应用" : "确认应用更改",
      cancelText: "返回编辑",
      onOk: () => {
        try {
          onSave(config, true);
          setHasChanges(false);
        } finally {
          setSubmitting(false);
          setSubmitIntent(null);
        }
      },
      onCancel: () => {
        setSubmitting(false);
        setSubmitIntent(null);
      },
    });
  };

  const submit = (intent: SubmitIntent) => {
    if (intent === "draft" && !canSave) return;
    setSubmitIntent(intent);
    setSubmitting(true);
    if (intent === "draft") {
      form.submit();
      return;
    }
    void form.validateFields()
      .then((values) => {
        const config = normalizeConfig({ ...initialConfig, ...form.getFieldsValue(true), ...values, protocol: activeProtocol });
        if (!IMPLEMENTED_PROTOCOLS.includes(config.protocol) || applyBlockers(config, server, inbound, inbounds).length > 0) {
          setSubmitting(false);
          setSubmitIntent(null);
          return;
        }
        setSubmitting(false);
        confirmApply(config);
      })
      .catch((errorInfo: { errorFields?: Array<{ name: unknown }> }) => {
        setSubmitting(false);
        setSubmitIntent(null);
        focusValidationError(errorInfo);
      });
  };

  const nameRules = [
    { required: true, message: "请输入入口名称" },
    { validator: async (_rule: unknown, value: unknown) => typeof value === "string" && value.trim() ? Promise.resolve() : Promise.reject(new Error("入口名称不能只有空格")) },
  ];
  const portRules = [
    { required: true, message: "请输入监听端口" },
    { validator: async (_rule: unknown, value: unknown) => {
      if (!isPortNumber(value)) return Promise.reject(new Error("端口必须是 1-65535 的整数"));
      return inboundPortConflict(inbounds, server.id, value, inbound?.id) ? Promise.reject(new Error("同机端口已被其它入口资源占用")) : Promise.resolve();
    } },
  ];
  const addressRules = [
    { required: true, message: "请输入监听地址" },
    { validator: async (_rule: unknown, value: unknown) => !value || isValidIpAddress(String(value)) ? Promise.resolve() : Promise.reject(new Error("监听地址必须是合法 IPv4 或 IPv6 地址")) },
  ];
  const advertisedAddressRules = [
    { required: true, message: "请输入公布地址" },
    { validator: async (_rule: unknown, value: unknown) => !value || isValidAddress(String(value)) ? Promise.resolve() : Promise.reject(new Error("公布地址必须是合法 IP 或域名")) },
  ];
  const advertisedPortRules = [
    { required: true, message: "请输入公布端口" },
    { validator: async (_rule: unknown, value: unknown) => isPortNumber(value) ? Promise.resolve() : Promise.reject(new Error("端口必须是 1-65535 的整数")) },
  ];

  const basicSection = (
    <div className="su-proxy-config-section">
      <Form.Item name="name" label="入口名称" rules={nameRules}><Input maxLength={128} placeholder="例如 HK Reality 主入口" /></Form.Item>
      <Form.Item name="exposure" label="用途" rules={[{ required: true, message: "请选择入口用途" }]}><Radio.Group><Radio value="subscription">subscription · 订阅</Radio><Radio value="internal">internal · 内部</Radio></Radio.Group></Form.Item>
      <Form.Item label="服务器"><Input value={`${server.name} · ${server.region} · ${server.id}`} readOnly disabled suffix={isPublished ? <Tag>已发布入口</Tag> : <Tag>合成资产</Tag>} /></Form.Item>
      <Form.Item label="出口"><Input value={egressLabel || "本机直出"} readOnly disabled /></Form.Item>
      {referenceCount > 0 && <Alert type="warning" showIcon title={`该入口资源被 ${referenceCount} 个节点引用`} description="编辑会同步影响所有引用它的节点；如需隔离，请先创建独立入口资源。" />}
      {isPublished && <Alert type="info" showIcon title="已发布入口" description="协议选择已锁定；修改其它字段会先保留为草稿，应用后更新共享入口。" />}
      <Alert type="warning" showIcon title="仅前端合成原型" description="不会发起网络请求、读取凭据或回显任何真实或合成密钥内容。" />
      <Alert type={canApply ? "success" : "warning"} showIcon title={canApply ? "应用前检查通过" : "当前只能保存草稿"} description={blockers.length > 0 ? <Space orientation="vertical" size={2}>{blockers.map((blocker) => <Typography.Text key={blocker}>{blocker}</Typography.Text>)}</Space> : "当前三种实现协议的能力、端口和材料状态均满足应用前检查。"} />
      <Typography.Text type="secondary" className="su-proxy-config-capacity-text">同机入口资源：{resourceCount}{server.maxProxies > 0 ? `/${server.maxProxies}` : ""} · {server.online ? "online" : "offline"} · {server.capabilitiesKnown ? "capabilities known" : "capabilities unknown"}</Typography.Text>
    </div>
  );

  const listenSection = (
    <div className="su-proxy-config-section">
      <Form.Item name="listenAddress" label="监听地址" rules={addressRules}><Input placeholder=":: 或 0.0.0.0" /></Form.Item>
      <Form.Item name="listenPort" label={activeProtocol === "hysteria2" ? "UDP 监听端口" : "监听端口"} rules={portRules}><InputNumber min={1} max={65535} precision={0} style={{ width: "100%" }} /></Form.Item>
      {occupiedResource && <Alert type="error" showIcon title="端口冲突" description={`端口 ${currentConfig.listenPort} 已被入口资源“${occupiedResource.config.name}”占用；同一资源自身不视为冲突。`} />}
      <Form.Item name="advertisedAddress" label="公布地址" rules={advertisedAddressRules}><Input placeholder="example.invalid" /></Form.Item>
      <Form.Item name="advertisedPort" label="公布端口" rules={advertisedPortRules}><InputNumber min={1} max={65535} precision={0} style={{ width: "100%" }} /></Form.Item>
      <Typography.Text type="secondary" className="su-proxy-config-security-note">入口资源独立占用端口；同一入站被多个节点引用不会重复占用端口。</Typography.Text>
    </div>
  );

  const protocolSection = (
    <div className="su-proxy-config-section">
      <Tabs className="su-proxy-config-protocol-tabs" activeKey={activeProtocol} onChange={(value) => selectProtocol(value as Protocol)} items={protocolCatalog.map((entry) => { const protocol = entry.value; const reason = protocolGate(protocol, server, isPublished && protocol !== currentConfig.protocol); return { key: protocol, disabled: isPublished && protocol !== currentConfig.protocol, label: <Tooltip title={reason ?? `${entry.label} · ${entry.fields.length} 个字段`}><span>{entry.label}</span></Tooltip> }; })} />
      <Typography.Text type="secondary" className="su-proxy-config-security-note">当前支持保存：Reality、Shadowsocks、Hysteria2。其它协议保留字段目录与能力门禁，尚不宣称可用。</Typography.Text>
      <InboundProtocolFields section="protocol" protocol={activeProtocol} config={currentConfig} server={server} onUpdate={updateForm} onSyncSni={syncSni} syncNotice={syncNotice} />
    </div>
  );

  const protocolFields = (section: InboundFormSection): ReactElement => <div className="su-proxy-config-section"><InboundProtocolFields section={section} protocol={activeProtocol} config={currentConfig} server={server} onUpdate={updateForm} onSyncSni={syncSni} syncNotice={syncNotice} /></div>;

  const footer = (
    <div className="su-proxy-config-footer">
      <Button onClick={requestClose} disabled={submitting}>取消</Button>
      <Space wrap className="su-proxy-config-submit-actions">
        <Tooltip title={!canSave ? "规划协议只能浏览，不能保存" : undefined}><span><Button disabled={!canSave || submitting} loading={submitting && submitIntent === "draft"} onClick={() => submit("draft")}>保存草稿</Button></span></Tooltip>
        <Tooltip title={!canApply && blockers.length > 0 ? blockers.join(" ") : undefined}><span><Button type="primary" disabled={!canApply || submitting} loading={submitting && submitIntent === "apply"} onClick={() => submit("apply")}>{isNewInbound ? "创建并应用" : "应用更改"}</Button></span></Tooltip>
      </Space>
    </div>
  );

  return (
    <>
      {modalContextHolder}
      <Drawer className="su-proxy-config-drawer" rootClassName={docked ? "su-docked-drawer" : undefined} getContainer={docked ? false : undefined} rootStyle={docked ? { position: "absolute" } : undefined} push={false} open title={isNewInbound ? "配置入站资源" : `编辑入站资源 · ${initialConfig.name}`} size={drawerWidth} keyboard mask={docked ? false : { closable: true }} onClose={requestClose} afterOpenChange={(open) => { if (!open) restoreFocus(); }} footer={footer} styles={{ body: { padding: token.paddingLG }, footer: { padding: token.paddingSM, borderTop: `1px solid ${token.colorBorderSecondary}` } }}>
        <div className="su-proxy-config-form">
          <Form form={form} layout="vertical" initialValues={initialConfig} onValuesChange={handleValuesChange} onFinish={handleFinish} onFinishFailed={(errorInfo) => { setSubmitting(false); setSubmitIntent(null); focusValidationError(errorInfo); }}>
            <HiddenReadinessFields />
            <Tabs className="su-proxy-config-tabs" activeKey={activeSection} onChange={setActiveSection} items={SECTION_ITEMS.map((section) => ({ key: section.key, label: section.label, forceRender: true, children: section.key === "basic" ? basicSection : section.key === "listen" ? listenSection : section.key === "protocol" ? protocolSection : protocolFields(section.key) }))} />
          </Form>
        </div>
      </Drawer>
    </>
  );
}
