import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties } from "react";
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
  Select,
  Space,
  Switch,
  Tabs,
  Tag,
  Tooltip,
  Typography,
  theme,
} from "antd";

import {
  defaultConfig,
  getServer,
  protocolLabels,
} from "./model";
import type {
  Protocol,
  ProxyConfig,
  ProxyDraft,
  ServerAsset,
} from "./model";

export interface ProxyConfigFormProps {
  proxy: ProxyDraft;
  proxies: ProxyDraft[];
  egressLabel: string;
  onSave: (config: ProxyConfig, apply: boolean) => void;
  onClose: () => void;
}

type FieldPath = Array<string | number>;
type SubmitIntent = "draft" | "apply";

const PROTOCOL_CHOICES: Array<{
  value: Protocol;
  label: string;
  description: string;
}> = [
  {
    value: "vless-reality",
    label: "Reality",
    description: "VLESS + Reality，使用合成演示目标。",
  },
  {
    value: "shadowsocks",
    label: "Shadowsocks",
    description: "Shadowsocks 2022，方法来自服务器能力清单。",
  },
  {
    value: "hysteria2",
    label: "Hysteria2",
    description: "UDP 传输，证书只能引用控制面已登记的合成证书。",
  },
];

const HOSTNAME_PATTERN = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$/;

function isPortNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 1 && value <= 65535;
}

function isValidHostname(value: string): boolean {
  const normalizedValue = value.trim();
  return normalizedValue.length > 0 && normalizedValue.length <= 253 && HOSTNAME_PATTERN.test(normalizedValue);
}

function isValidIpv4(value: string): boolean {
  const octets = value.split(".");
  return octets.length === 4 && octets.every((octet) => {
    if (!/^\d{1,3}$/.test(octet)) return false;
    const numericOctet = Number(octet);
    return numericOctet >= 0 && numericOctet <= 255 && String(numericOctet) === octet.replace(/^0+(?=\d)/, "");
  });
}

function isValidIpv6(value: string): boolean {
  const normalizedValue = value.trim();
  if (!normalizedValue.includes(":")) return false;
  if (normalizedValue.includes("%") || normalizedValue.includes("/") || normalizedValue.includes(":::")) return false;

  const sections = normalizedValue.split("::");
  if (sections.length > 2) return false;

  const leftGroups = sections[0] ? sections[0].split(":") : [];
  const rightGroups = sections.length === 2 && sections[1] ? sections[1].split(":") : [];
  const groups = [...leftGroups, ...rightGroups];
  const hasEmbeddedIpv4 = groups.some((group) => group.includes("."));
  const normalizedGroups = hasEmbeddedIpv4 ? groups.slice(0, -1) : groups;
  const validHexGroups = normalizedGroups.every((group) => /^[0-9A-Fa-f]{1,4}$/.test(group));
  const validEmbeddedIpv4 = !hasEmbeddedIpv4 || isValidIpv4(groups[groups.length - 1] ?? "");
  if (!validHexGroups || !validEmbeddedIpv4) return false;

  if (sections.length === 2) return groups.length < 8;
  return groups.length === 8;
}

function isValidIpAddress(value: string): boolean {
  const normalizedValue = value.trim();
  return isValidIpv4(normalizedValue) || isValidIpv6(normalizedValue);
}

function isValidAdvertisedAddress(value: string): boolean {
  const normalizedValue = value.trim();
  const unwrappedValue = normalizedValue.startsWith("[") && normalizedValue.endsWith("]")
    ? normalizedValue.slice(1, -1)
    : normalizedValue;
  return isValidIpAddress(unwrappedValue) || isValidHostname(unwrappedValue);
}

function isValidTarget(value: string): boolean {
  const normalizedValue = value.trim();
  const bracketedIpv6 = /^\[([^\]]+)\]:(\d+)$/.exec(normalizedValue);
  if (bracketedIpv6) {
    return isValidIpAddress(bracketedIpv6[1]) && isPortNumber(Number(bracketedIpv6[2]));
  }

  const separatorIndex = normalizedValue.lastIndexOf(":");
  if (separatorIndex <= 0 || separatorIndex === normalizedValue.length - 1) return false;
  const targetHost = normalizedValue.slice(0, separatorIndex);
  const targetPort = normalizedValue.slice(separatorIndex + 1);
  return !targetHost.includes(":") && isValidAdvertisedAddress(targetHost) && isPortNumber(Number(targetPort));
}

function targetHost(value: string): string | undefined {
  const normalizedValue = value.trim();
  const bracketedIpv6 = /^\[([^\]]+)\]:\d+$/.exec(normalizedValue);
  if (bracketedIpv6) return bracketedIpv6[1];
  const separatorIndex = normalizedValue.lastIndexOf(":");
  if (separatorIndex <= 0) return undefined;
  return normalizedValue.slice(0, separatorIndex);
}

function createInitialConfig(proxy: ProxyDraft, proxies: ProxyDraft[]): ProxyConfig {
  const server = getServer(proxy.serverId);
  const initialConfig = { ...(proxy.config ?? defaultConfig(server, proxies)) };

  if (server.profile === "lite") initialConfig.exposure = "internal";
  if (initialConfig.protocol === "vless-reality") initialConfig.network = "tcp";
  if (initialConfig.protocol === "hysteria2") initialConfig.network = "tcp-udp";

  if (server.network === "nat") {
    const matchingMapping = server.portMappings.find(
      (mapping) => mapping.listenPort === initialConfig.listenPort,
    ) ?? server.portMappings[0];
    if (matchingMapping) {
      initialConfig.listenPort = matchingMapping.listenPort;
      initialConfig.advertisedAddress = server.address;
      initialConfig.advertisedPort = matchingMapping.publicPort;
    }
  }

  return initialConfig;
}

function normalizeConfig(config: ProxyConfig, server: ServerAsset): ProxyConfig {
  const normalizedConfig = {
    ...config,
    name: config.name.trim(),
    listenAddress: config.listenAddress.trim(),
    advertisedAddress: config.advertisedAddress.trim(),
    sni: config.sni.trim(),
    target: config.target.trim(),
    certificateRef: config.certificateRef.trim(),
  };

  if (server.profile === "lite") normalizedConfig.exposure = "internal";
  if (config.protocol === "vless-reality") normalizedConfig.network = "tcp";
  if (config.protocol === "hysteria2") normalizedConfig.network = "tcp-udp";

  if (server.network === "nat") {
    const matchingMapping = server.portMappings.find(
      (mapping) => mapping.listenPort === normalizedConfig.listenPort,
    );
    if (matchingMapping) {
      normalizedConfig.advertisedAddress = server.address;
      normalizedConfig.advertisedPort = matchingMapping.publicPort;
    }
  }

  return normalizedConfig;
}

function occupiedListenPorts(
  proxies: ProxyDraft[],
  serverId: string,
  currentProxyId: string,
): Set<number> {
  const occupiedPorts = new Set<number>();
  for (const candidateProxy of proxies) {
    if (candidateProxy.id === currentProxyId || candidateProxy.serverId !== serverId) continue;
    for (const candidateConfig of [candidateProxy.config, candidateProxy.published]) {
      if (candidateConfig && isPortNumber(candidateConfig.listenPort)) {
        occupiedPorts.add(candidateConfig.listenPort);
      }
    }
  }
  return occupiedPorts;
}

function hasListenPortConflict(
  proxies: ProxyDraft[],
  proxy: ProxyDraft,
  serverId: string,
  listenPort: number,
): boolean {
  return occupiedListenPorts(proxies, serverId, proxy.id).has(listenPort);
}

function portErrorMessage(value: unknown): string | undefined {
  if (value === undefined || value === null || value === "") return undefined;
  if (!isPortNumber(value)) return "端口必须是 1-65535 的整数";
  return undefined;
}

function fieldPath(value: unknown): FieldPath | null {
  if (typeof value === "string" || typeof value === "number") return [value];
  if (!Array.isArray(value)) return null;
  const normalizedPath = value.filter(
    (pathPart): pathPart is string | number => typeof pathPart === "string" || typeof pathPart === "number",
  );
  return normalizedPath.length > 0 ? normalizedPath : null;
}

function tabForPath(path: FieldPath): string {
  const rootPath = path[0];
  if (rootPath === "protocol" || rootPath === "sni" || rootPath === "target" || rootPath === "method" || rootPath === "network") {
    return "protocol";
  }
  if (rootPath === "materialReady" || rootPath === "shortIdReady" || rootPath === "certificateRef") {
    return "security";
  }
  if (rootPath === "listenAddress" || rootPath === "advertisedAddress" || rootPath === "advertisedPort") {
    return "advanced";
  }
  return "basic";
}

function applyBlockers(
  config: ProxyConfig,
  server: ServerAsset,
  proxy: ProxyDraft,
  proxies: ProxyDraft[],
): string[] {
  const blockers: string[] = [];
  const otherProxyCount = proxies.filter(
    (candidateProxy) => candidateProxy.id !== proxy.id && candidateProxy.serverId === server.id,
  ).length;

  if (!server.online) blockers.push("服务器离线，暂不能应用。");
  if (!server.capabilitiesKnown) blockers.push("服务器能力未知，暂不能应用或配置协议。");
  if (otherProxyCount >= server.maxProxies) blockers.push(`服务器容量已满（${otherProxyCount}/${server.maxProxies}）。`);
  if (!server.protocols.includes(config.protocol)) blockers.push(`服务器未声明支持${protocolLabels[config.protocol]}。`);
  if (server.profile === "lite" && config.exposure !== "internal") blockers.push("lite 服务器仅支持 internal 用途。");
  if (server.network === "nat" && !server.portMappings.some((mapping) => mapping.listenPort === config.listenPort)) {
    blockers.push("NAT 端口必须使用可用映射 10080 或 10081。");
  }
  if (isPortNumber(config.listenPort) && hasListenPortConflict(proxies, proxy, server.id, config.listenPort)) {
    blockers.push("同机端口已被草稿或已发布入口占用。");
  }

  if (config.protocol === "vless-reality") {
    if (!config.materialReady || !config.shortIdReady) blockers.push("Reality 演示密钥与 Short ID 尚未标记就绪。");
  }
  if (config.protocol === "shadowsocks") {
    if (!config.materialReady) blockers.push("Shadowsocks 演示材料尚未标记就绪。");
    if (!server.methods.includes(config.method)) blockers.push("Shadowsocks 方法不在服务器现有方法清单中。");
    if (config.network === "tcp-udp" && !server.udp) blockers.push("服务器未声明 UDP 能力。");
  }
  if (config.protocol === "hysteria2") {
    if (!server.udp) blockers.push("Hysteria2 需要服务器 UDP 能力。");
    if (!server.certificates.some((certificate) => certificate.id === config.certificateRef)) {
      blockers.push("Hysteria2 必须引用 HK 的合成证书。");
    }
  }

  return blockers;
}

function protocolDisabledReason(
  protocol: Protocol,
  server: ServerAsset,
  published: boolean,
): string | undefined {
  if (published) return "已发布入口锁定协议。";
  if (!server.capabilitiesKnown) return "服务器能力未知，不能配置协议。";
  if (!server.protocols.includes(protocol)) return "服务器能力清单未声明支持该协议。";
  return undefined;
}

export default function ProxyConfigForm({
  proxy,
  proxies,
  egressLabel,
  onSave,
  onClose,
}: ProxyConfigFormProps) {
  const server = getServer(proxy.serverId);
  const isPublished = proxy.published !== undefined;
  const isNewProxy = proxy.config === undefined;
  const initialConfig = useMemo(
    () => createInitialConfig(proxy, proxies),
    [proxy, proxies],
  );
  const [form] = Form.useForm<ProxyConfig>();
  const [activeTab, setActiveTab] = useState("basic");
  const [hasChanges, setHasChanges] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [submitIntent, setSubmitIntent] = useState<SubmitIntent | null>(null);
  const [syncNotice, setSyncNotice] = useState<string | null>(null);
  const [loadedProxyId, setLoadedProxyId] = useState(proxy.id);
  const [, setFormRevision] = useState(0);
  const submitIntentRef = useRef<SubmitIntent>("draft");
  const advertisedPortManualRef = useRef(
    initialConfig.advertisedPort !== initialConfig.listenPort,
  );
  const returnFocusRef = useRef<HTMLElement | null>(null);
  const [modal, modalContextHolder] = Modal.useModal();
  const { token } = theme.useToken();
  const screens = Grid.useBreakpoint();

  useEffect(() => {
    const activeElement = document.activeElement;
    if (activeElement instanceof HTMLElement && !activeElement.closest(".ant-drawer")) {
      returnFocusRef.current = activeElement;
    }
  }, []);

  useEffect(() => {
    if (loadedProxyId === proxy.id) return;
    form.setFieldsValue(initialConfig);
    advertisedPortManualRef.current = initialConfig.advertisedPort !== initialConfig.listenPort;
    setLoadedProxyId(proxy.id);
    setHasChanges(false);
    setActiveTab("basic");
    setSyncNotice(null);
    setFormRevision((revision) => revision + 1);
  }, [form, initialConfig, loadedProxyId, proxy.id]);

  const currentValues = form.getFieldsValue(true);
  const currentConfig = normalizeConfig(
    { ...initialConfig, ...currentValues },
    server,
  );
  const occupiedPorts = occupiedListenPorts(proxies, server.id, proxy.id);
  const capacityUsed = proxies.filter(
    (candidateProxy) => candidateProxy.id !== proxy.id && candidateProxy.serverId === server.id,
  ).length;
  const capacityReached = capacityUsed >= server.maxProxies;
  const blockers = applyBlockers(currentConfig, server, proxy, proxies);
  const canApply = blockers.length === 0;
  const isNat = server.network === "nat";
  const isLite = server.profile === "lite";
  const docked = Boolean(screens.xl);
  const mobileWidth = docked || screens.xs ? "100%" : 520;

  const updateForm = useCallback(
    (values: Partial<ProxyConfig>) => {
      form.setFieldsValue(values);
      setHasChanges(true);
      setFormRevision((revision) => revision + 1);
    },
    [form],
  );

  const handleValuesChange = useCallback(
    (changedValues: Partial<ProxyConfig>) => {
      setHasChanges(true);
      setFormRevision((revision) => revision + 1);
      if (Object.prototype.hasOwnProperty.call(changedValues, "target")) setSyncNotice(null);

      if (Object.prototype.hasOwnProperty.call(changedValues, "advertisedPort") && !isNat) {
        advertisedPortManualRef.current = true;
      }

      if (Object.prototype.hasOwnProperty.call(changedValues, "listenPort") && isNat) {
        const selectedPort = changedValues.listenPort;
        const matchingMapping = server.portMappings.find(
          (mapping) => mapping.listenPort === selectedPort,
        );
        if (matchingMapping) {
          form.setFieldsValue({
            advertisedAddress: server.address,
            advertisedPort: matchingMapping.publicPort,
          });
        }
      } else if (
        Object.prototype.hasOwnProperty.call(changedValues, "listenPort") &&
        !isNat &&
        isPortNumber(changedValues.listenPort) &&
        !advertisedPortManualRef.current
      ) {
        form.setFieldsValue({ advertisedPort: changedValues.listenPort });
      }
    },
    [form, isNat, server.address, server.portMappings],
  );

  const selectProtocol = useCallback(
    (nextProtocol: Protocol) => {
      const disabledReason = protocolDisabledReason(nextProtocol, server, isPublished);
      if (disabledReason) return;

      const nextValues: Partial<ProxyConfig> = { protocol: nextProtocol };
      const formValues = form.getFieldsValue(true);
      if (nextProtocol === "vless-reality") nextValues.network = "tcp";
      if (nextProtocol === "shadowsocks") {
        nextValues.method = server.methods.includes(formValues.method)
          ? formValues.method
          : server.methods[0] ?? "";
        nextValues.network = formValues.network === "tcp-udp" && server.udp ? "tcp-udp" : "tcp";
      }
      if (nextProtocol === "hysteria2") {
        nextValues.network = "tcp-udp";
        if (!server.certificates.some((certificate) => certificate.id === formValues.certificateRef)) {
          nextValues.certificateRef = server.certificates[0]?.id ?? "";
        }
      }
      updateForm(nextValues);
    },
    [form, isPublished, server, updateForm],
  );

  const syncSni = useCallback(() => {
    const host = targetHost(String(form.getFieldValue("target") ?? ""));
    if (!host || !isValidHostname(host)) {
      setSyncNotice("请先填写带域名主机的目标地址，再显式同步 SNI。");
      return;
    }
    updateForm({ sni: host });
    setSyncNotice(`已显式将 SNI 同步为 ${host}。`);
  }, [form, updateForm]);

  const restoreFocus = useCallback(() => {
    const returnElement = returnFocusRef.current;
    if (returnElement && document.contains(returnElement)) {
      returnElement.focus({ preventScroll: true });
    }
  }, []);

  const closeImmediately = useCallback(() => {
    onClose();
    globalThis.setTimeout(restoreFocus, 0);
  }, [onClose, restoreFocus]);

  const requestClose = useCallback(() => {
    if (!hasChanges) {
      closeImmediately();
      return;
    }
    modal.confirm({
      title: "放弃未保存更改？",
      content: isNewProxy ? "关闭会删除这个新占位入口。" : "关闭会丢弃当前表单中的未保存更改。",
      okText: "确认关闭",
      cancelText: "继续编辑",
      onOk: closeImmediately,
    });
  }, [closeImmediately, hasChanges, isNewProxy, modal]);

  const handleFinish = useCallback(
    (values: ProxyConfig) => {
      const apply = submitIntentRef.current === "apply";
      const config = normalizeConfig({ ...initialConfig, ...values }, server);
      if (apply && applyBlockers(config, server, proxy, proxies).length > 0) {
        setSubmitting(false);
        return;
      }
      try {
        onSave(config, apply);
        setHasChanges(false);
      } finally {
        setSubmitting(false);
        setSubmitIntent(null);
      }
    },
    [initialConfig, onSave, proxy, proxies, server],
  );

  const submit = useCallback(
    (intent: SubmitIntent) => {
      submitIntentRef.current = intent;
      setSubmitIntent(intent);
      setSubmitting(true);
      form.submit();
    },
    [form],
  );

  const nameRules = [
    { required: true, message: "请输入入口名称" },
    {
      validator: async (_rule: unknown, value: unknown) => {
        if (typeof value === "string" && value.trim().length === 0) {
          return Promise.reject(new Error("入口名称不能只有空格"));
        }
        return Promise.resolve();
      },
    },
  ];
  const listenPortRules = [
    { required: true, message: "请选择或填写监听端口" },
    {
      validator: async (_rule: unknown, value: unknown) => {
        const errorMessage = portErrorMessage(value);
        if (errorMessage) return Promise.reject(new Error(errorMessage));
        if (isPortNumber(value) && hasListenPortConflict(proxies, proxy, server.id, value)) {
          return Promise.reject(new Error("同机端口已被草稿或已发布入口占用"));
        }
        return Promise.resolve();
      },
    },
  ];
  const advertisedPortRules = [
    { required: true, message: "请输入公布端口" },
    {
      validator: async (_rule: unknown, value: unknown) => {
        const errorMessage = portErrorMessage(value);
        return errorMessage ? Promise.reject(new Error(errorMessage)) : Promise.resolve();
      },
    },
  ];
  const sniRules = [
    { required: true, message: "请输入 SNI" },
    {
      validator: async (_rule: unknown, value: unknown) => {
        if (typeof value !== "string" || value.trim().length === 0) return Promise.resolve();
        return isValidHostname(value)
          ? Promise.resolve()
          : Promise.reject(new Error("SNI 必须是合法域名，例如 example.invalid"));
      },
    },
  ];
  const targetRules = [
    { required: true, message: "请输入目标 dest" },
    {
      validator: async (_rule: unknown, value: unknown) => {
        if (typeof value !== "string" || value.trim().length === 0) return Promise.resolve();
        return isValidTarget(value)
          ? Promise.resolve()
          : Promise.reject(new Error("目标必须是 host:port，例如 example.invalid:443"));
      },
    },
  ];
  const listenAddressRules = [
    { required: true, message: "请输入监听 IP" },
    {
      validator: async (_rule: unknown, value: unknown) => {
        if (typeof value !== "string" || value.trim().length === 0) return Promise.resolve();
        return isValidIpAddress(value)
          ? Promise.resolve()
          : Promise.reject(new Error("监听地址必须是合法 IPv4 或 IPv6 地址"));
      },
    },
  ];
  const advertisedAddressRules = [
    { required: true, message: "请输入公布地址" },
    {
      validator: async (_rule: unknown, value: unknown) => {
        if (typeof value !== "string" || value.trim().length === 0) return Promise.resolve();
        return isValidAdvertisedAddress(value)
          ? Promise.resolve()
          : Promise.reject(new Error("公布地址必须是合法 IP 或域名"));
      },
    },
  ];

  const protocolTab = (
    <div className="su-proxy-config-section">
      <Form.Item label="协议" required>
        <Space wrap className="su-proxy-config-protocol-actions">
          {PROTOCOL_CHOICES.map((choice) => {
            const disabledReason = protocolDisabledReason(choice.value, server, isPublished);
            return (
              <Tooltip key={choice.value} title={disabledReason ?? choice.description}>
                <span className="su-proxy-config-protocol-button">
                  <Button
                    type={currentConfig.protocol === choice.value ? "primary" : "default"}
                    disabled={Boolean(disabledReason)}
                    aria-pressed={currentConfig.protocol === choice.value}
                    onClick={() => selectProtocol(choice.value)}
                  >
                    {choice.label}
                  </Button>
                </span>
              </Tooltip>
            );
          })}
        </Space>
      </Form.Item>
      <Form.Item name="protocol" hidden>
        <Input />
      </Form.Item>
      {!server.capabilitiesKnown && (
        <Alert
          className="su-proxy-config-capability-alert"
          type="warning"
          showIcon
          title="服务器能力未知"
          description="协议快捷按钮已禁用；当前只能保存草稿，不能配置协议或应用。"
        />
      )}
      {server.capabilitiesKnown && !server.protocols.includes(currentConfig.protocol) && (
        <Alert
          className="su-proxy-config-capability-alert"
          type="warning"
          showIcon
          title="当前协议未获服务器声明"
          description="可以保留为草稿，应用前需要选择服务器能力清单中的协议。"
        />
      )}
      {currentConfig.protocol === "vless-reality" && (
        <>
          <Form.Item name="sni" label="SNI" rules={sniRules}>
            <Input disabled={!server.capabilitiesKnown} placeholder="example.invalid" />
          </Form.Item>
          <Form.Item name="target" label="目标 dest" rules={targetRules}>
            <Input disabled={!server.capabilitiesKnown} placeholder="example.invalid:443" />
          </Form.Item>
          <Space wrap className="su-proxy-config-sync-actions">
            <Button disabled={!server.capabilitiesKnown} onClick={syncSni}>
              显式将 SNI 同步为目标主机
            </Button>
            {syncNotice && <Typography.Text type="secondary">{syncNotice}</Typography.Text>}
          </Space>
        </>
      )}
      {currentConfig.protocol === "shadowsocks" && (
        <>
          <Form.Item
            name="method"
            label="SS2022 方法"
            rules={[
              { required: true, message: "请选择服务器现有方法" },
              {
                validator: async (_rule: unknown, value: unknown) => {
                  if (typeof value !== "string" || value.length === 0 || server.methods.length === 0) {
                    return Promise.resolve();
                  }
                  return server.methods.includes(value)
                    ? Promise.resolve()
                    : Promise.reject(new Error("方法必须来自服务器现有方法清单"));
                },
              },
            ]}
          >
            <Select
              placeholder="选择服务器现有方法"
              options={server.methods.map((method) => ({ value: method, label: method }))}
              disabled={!server.capabilitiesKnown || server.methods.length === 0}
            />
          </Form.Item>
          <Form.Item
            name="network"
            label="网络"
            rules={[
              { required: true, message: "请选择网络能力" },
              {
                validator: async (_rule: unknown, value: unknown) => {
                  if (value === "tcp-udp" && server.capabilitiesKnown && !server.udp) {
                    return Promise.reject(new Error("该服务器未声明 UDP 能力"));
                  }
                  return Promise.resolve();
                },
              },
            ]}
          >
            <Select
              options={[
                { value: "tcp", label: "TCP" },
                { value: "tcp-udp", label: "TCP + UDP", disabled: server.capabilitiesKnown && !server.udp },
              ]}
              disabled={!server.capabilitiesKnown}
            />
          </Form.Item>
        </>
      )}
      {currentConfig.protocol === "hysteria2" && (
        <Alert
          className="su-proxy-config-hy2-alert"
          type={server.udp ? "info" : "warning"}
          showIcon
          title="Hysteria2 使用 UDP"
          description={server.udp ? "传输已固定为 UDP；证书引用在安全 Tab 中选择。" : "该服务器没有 UDP 能力，应用会被阻止；可以先保存草稿。"}
        />
      )}
    </div>
  );

  const securityTab = (
    <div className="su-proxy-config-section">
      {currentConfig.protocol === "vless-reality" && (
        <>
          <Alert
            type="info"
            showIcon
            title="Reality 演示材料"
            description="按钮只把 materialReady 和 shortIdReady 标记为 true，不生成、不保存真实秘密。"
          />
          <Space wrap className="su-proxy-config-material-actions">
            <Button onClick={() => updateForm({ materialReady: true, shortIdReady: true })}>
              生成演示密钥与 Short ID
            </Button>
            <Tag color={currentConfig.materialReady && currentConfig.shortIdReady ? "green" : "default"}>
              {currentConfig.materialReady && currentConfig.shortIdReady ? "material ready" : "待准备"}
            </Tag>
          </Space>
        </>
      )}
      {currentConfig.protocol === "shadowsocks" && (
        <>
          <Alert
            type="info"
            showIcon
            title="Shadowsocks 演示材料"
            description="按钮只标记演示材料已就绪，不生成或保存真实密码。"
          />
          <Space wrap className="su-proxy-config-material-actions">
            <Button onClick={() => updateForm({ materialReady: true })}>生成演示材料</Button>
            <Tag color={currentConfig.materialReady ? "green" : "default"}>
              {currentConfig.materialReady ? "material ready" : "待准备"}
            </Tag>
          </Space>
        </>
      )}
      {currentConfig.protocol === "hysteria2" && (
        <>
          <Form.Item
            name="certificateRef"
            label="证书引用"
            rules={[
              ...(server.certificates.length > 0
                ? [{ required: true, message: "请选择 HK 合成证书引用" }]
                : []),
              {
                validator: async (_rule: unknown, value: unknown) => {
                  if (typeof value !== "string" || value.length === 0 || server.certificates.length === 0) {
                    return Promise.resolve();
                  }
                  return server.certificates.some((certificate) => certificate.id === value)
                    ? Promise.resolve()
                    : Promise.reject(new Error("证书引用必须来自 HK 的合成证书清单"));
                },
              },
            ]}
          >
            <Select
              allowClear
              placeholder={server.certificates.length > 0 ? "选择合成证书引用" : "当前服务器没有证书引用"}
              options={server.certificates.map((certificate) => ({
                value: certificate.id,
                label: `${certificate.name} · ${certificate.domain}`,
              }))}
              disabled={!server.capabilitiesKnown || server.certificates.length === 0}
            />
          </Form.Item>
          <Typography.Text type="secondary" className="su-proxy-config-security-note">
            只接受控制面登记的 HK 合成证书引用，不接受文件路径。
          </Typography.Text>
          {server.certificates.length === 0 && (
            <Alert
              className="su-proxy-config-certificate-alert"
              type="warning"
              showIcon
              title="没有可用证书引用"
              description="可保存草稿；应用前需要 HK 的合成证书引用。"
            />
          )}
        </>
      )}
      <Form.Item name="materialReady" valuePropName="checked" hidden>
        <Switch />
      </Form.Item>
      <Form.Item name="shortIdReady" valuePropName="checked" hidden>
        <Switch />
      </Form.Item>
    </div>
  );

  const advancedTab = (
    <div className="su-proxy-config-section">
      <Form.Item name="listenAddress" label="监听地址" rules={listenAddressRules}>
        <Input placeholder=":: 或 0.0.0.0" />
      </Form.Item>
      <Form.Item name="advertisedAddress" label="公布地址" rules={advertisedAddressRules}>
        <Input disabled={isNat} placeholder="example.invalid" />
      </Form.Item>
      <Form.Item name="advertisedPort" label="公布端口" rules={advertisedPortRules}>
        <InputNumber min={1} max={65535} precision={0} disabled={isNat} style={{ width: "100%" }} />
      </Form.Item>
      {isNat && (
        <div
          className="su-proxy-config-nat-mapping"
          style={{
            background: token.colorFillAlter,
            border: `1px solid ${token.colorBorderSecondary}`,
            borderRadius: token.borderRadiusLG,
            padding: token.paddingSM,
          } satisfies CSSProperties}
        >
          <Typography.Text strong>NAT mapping</Typography.Text>
          <Space wrap className="su-proxy-config-nat-mapping-list">
            {server.portMappings.map((mapping) => (
              <Tag key={`${mapping.listenPort}-${mapping.publicPort}`}>
                {mapping.listenPort} → {mapping.publicPort}
              </Tag>
            ))}
          </Space>
          <Typography.Text type="secondary">
            NAT 监听端口只允许可用映射；切换端口会同步公布端口，公布地址锁定为 {server.address}。
          </Typography.Text>
        </div>
      )}
      {isLite && (
        <Alert
          className="su-proxy-config-lite-alert"
          type="info"
          showIcon
          title="lite · direct-only"
          description="lite 首版仅支持本机直出，不支持客户分配；用途已强制为 internal。"
        />
      )}
    </div>
  );

  const footer = (
    <div className="su-proxy-config-footer">
      <Button onClick={requestClose} disabled={submitting}>取消</Button>
      <Space wrap className="su-proxy-config-submit-actions">
        <Button loading={submitting && submitIntent === "draft"} onClick={() => submit("draft")}>
          保存草稿
        </Button>
        <Tooltip title={!canApply && blockers.length > 0 ? blockers.join(" ") : undefined}>
          <span className="su-proxy-config-apply-button">
            <Button
              type="primary"
              loading={submitting && submitIntent === "apply"}
              disabled={!canApply || submitting}
              onClick={() => submit("apply")}
            >
              {isNewProxy ? "创建并应用" : "应用更改"}
            </Button>
          </span>
        </Tooltip>
      </Space>
    </div>
  );

  const statusDescription = blockers.length > 0 ? (
    <Space orientation="vertical" size={2} className="su-proxy-config-blocker-list">
      {blockers.map((blocker) => <Typography.Text key={blocker}>{blocker}</Typography.Text>)}
      <Typography.Text type="secondary">可保存草稿，应用按钮会在条件满足后启用。</Typography.Text>
    </Space>
  ) : "能力/材料已就绪，提交时继续校验字段。";

  const basicTab = (
    <div className="su-proxy-config-section">
      <Form.Item name="name" label="名称" rules={nameRules}>
        <Input maxLength={128} placeholder="入口名称" />
      </Form.Item>
      <Form.Item name="exposure" label="用途" rules={[{ required: true, message: "请选择入口用途" }]}>
        <Radio.Group disabled={isLite}>
          <Radio value="subscription">subscription · 订阅</Radio>
          <Radio value="internal">internal · 内部</Radio>
        </Radio.Group>
      </Form.Item>
      <Form.Item label="服务器">
        <Input
          value={`${server.name} · ${server.region} · ${server.id}`}
          readOnly
          disabled
          suffix={isPublished ? <Tag>已发布锁定</Tag> : <Tag>拓扑分配</Tag>}
        />
      </Form.Item>
      <Form.Item label="出口">
        <Input value={egressLabel || "未指定"} readOnly disabled />
      </Form.Item>
      <Form.Item name="listenPort" label="监听端口" rules={listenPortRules}>
        {isNat ? (
          <Select
            options={server.portMappings.map((mapping) => ({
              value: mapping.listenPort,
              label: `${mapping.listenPort} → ${mapping.publicPort}${occupiedPorts.has(mapping.listenPort) ? "（已占用）" : ""}`,
              disabled: occupiedPorts.has(mapping.listenPort) && mapping.listenPort !== currentConfig.listenPort,
            }))}
          />
        ) : (
          <InputNumber min={1} max={65535} precision={0} style={{ width: "100%" }} />
        )}
      </Form.Item>
      <Alert
        className="su-proxy-config-simulation-alert"
        type="warning"
        showIcon
        title="真实应用为模拟流程"
        description="本原型不会发起网络请求、写入真实秘密或生成真实密钥；应用结果由主控模拟记录。"
      />
      <Alert
        className="su-proxy-config-readiness-alert"
        type={canApply ? "success" : "warning"}
        showIcon
        title={canApply ? "应用前检查通过" : "当前只能保存草稿"}
        description={statusDescription}
      />
      <Typography.Text type="secondary" className="su-proxy-config-capacity-text">
        同机容量：{capacityUsed}/{server.maxProxies} · {server.online ? "online" : "offline"} · {server.capabilitiesKnown ? "capabilities known" : "capabilities unknown"}
        {capacityReached ? " · capacity reached" : ""}
      </Typography.Text>
    </div>
  );

  return (
    <>
      {modalContextHolder}
      <Drawer
        className="su-proxy-config-drawer"
        rootClassName={docked ? "su-docked-drawer" : undefined}
        getContainer={docked ? false : undefined}
        rootStyle={docked ? { position: "absolute" } : undefined}
        push={false}
        open
        title={proxy.config ? "编辑入口" : "配置入口"}
        size={mobileWidth}
        keyboard
        mask={docked ? false : { closable: true }}
        onClose={requestClose}
        afterOpenChange={(open) => {
          if (!open) restoreFocus();
        }}
        footer={footer}
        styles={{
          body: { padding: token.paddingLG },
          footer: { padding: token.paddingSM, borderTop: `1px solid ${token.colorBorderSecondary}` },
        }}
      >
        <div className="su-proxy-config-form">
          <Form
            form={form}
            layout="vertical"
            initialValues={initialConfig}
            onValuesChange={handleValuesChange}
            onFinish={handleFinish}
            onFinishFailed={(errorInfo) => {
              setSubmitting(false);
              const firstErrorPath = fieldPath(errorInfo.errorFields[0]?.name);
              if (!firstErrorPath) return;
              setActiveTab(tabForPath(firstErrorPath));
              globalThis.setTimeout(() => form.scrollToField(firstErrorPath), 0);
            }}
          >
            <Tabs
              className="su-proxy-config-tabs"
              activeKey={activeTab}
              onChange={setActiveTab}
              items={[
                { key: "basic", label: "基础", children: basicTab, forceRender: true },
                { key: "protocol", label: "协议", children: protocolTab, forceRender: true },
                { key: "security", label: "安全", children: securityTab, forceRender: true },
                { key: "advanced", label: "高级", children: advancedTab, forceRender: true },
              ]}
            />
          </Form>
        </div>
      </Drawer>
    </>
  );
}
