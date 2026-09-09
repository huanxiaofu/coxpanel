import {
  Alert,
  Button,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
} from "antd";
import type { ReactElement } from "react";
import type { Protocol, ProxyConfig, ServerAsset } from "./model";
import { protocolCatalog, protocolLabels } from "./model";

export type InboundFormSection = "protocol" | "security" | "tls" | "advanced";

export interface InboundProtocolFieldsProps {
  section: InboundFormSection;
  protocol: Protocol;
  config: ProxyConfig;
  server: ServerAsset;
  onUpdate: (values: Partial<ProxyConfig>) => void;
  onSyncSni: () => void;
  syncNotice?: string;
}

const IMPLEMENTED_PROTOCOLS: Protocol[] = ["vless-reality", "shadowsocks", "hysteria2"];

const SS_METHODS = [
  "2022-blake3-aes-128-gcm",
  "2022-blake3-aes-256-gcm",
  "2022-blake3-chacha20-poly1305",
  "chacha20-ietf-poly1305",
];

const FINGERPRINTS = ["chrome", "firefox", "safari", "edge", "random"];

function protocolSupportReason(protocol: Protocol, server: ServerAsset): string | undefined {
  if (!IMPLEMENTED_PROTOCOLS.includes(protocol)) {
    return "该协议为规划字段；当前原型只支持 Reality、Shadowsocks、Hysteria2 保存，不能伪造已支持能力。";
  }
  if (!server.capabilitiesKnown) return "服务器能力未知；当前只展示字段，应用需要能力清单。";
  if (!server.protocols.includes(protocol)) return `服务器未声明支持${protocolLabels[protocol]}。`;
  return undefined;
}

function plannedView(protocol: Protocol): ReactElement {
  const catalogEntry = protocolCatalog.find((entry) => entry.value === protocol);
  const fields = catalogEntry?.fields ?? [];
  return (
    <Alert
      type="info"
      showIcon
      title={`${protocolLabels[protocol]} · 规划字段`}
      description={
        <Space orientation="vertical" size={4}>
          <Typography.Text type="secondary">
            这些字段仅用于浏览设计范围，当前不能保存或应用该协议。
          </Typography.Text>
          <Space wrap>
            {fields.length > 0 ? fields.map((field) => <Tag key={field}>{field}</Tag>) : <Tag>字段目录待补充</Tag>}
          </Space>
        </Space>
      }
    />
  );
}

function readinessTag(ready: boolean, readyText: string, pendingText: string): ReactElement {
  return <Tag color={ready ? "green" : "default"}>{ready ? readyText : pendingText}</Tag>;
}

function realityFields(
  section: InboundFormSection,
  config: ProxyConfig,
  onUpdate: (values: Partial<ProxyConfig>) => void,
  onSyncSni: () => void,
  syncNotice?: string,
): ReactElement | null {
  if (section === "protocol") {
    return (
      <>
        <Form.Item
          name="sni"
          label="SNI"
          rules={[{ required: true, message: "请输入 Reality SNI" }]}
        >
          <Input placeholder="example.invalid" />
        </Form.Item>
        <Form.Item
          name="target"
          label="目标 dest"
          rules={[{ required: true, message: "请输入 Reality 目标，例如 example.invalid:443" }]}
        >
          <Input placeholder="example.invalid:443" />
        </Form.Item>
        <Space wrap className="su-proxy-config-sync-actions">
          <Button onClick={onSyncSni}>从 dest 提取 SNI</Button>
          {syncNotice && <Typography.Text type="secondary">{syncNotice}</Typography.Text>}
        </Space>
      </>
    );
  }

  if (section === "security") {
    return (
      <>
        <Alert
          type="info"
          showIcon
          title="Reality 材料引用"
          description="私钥、公钥与 UUID 不在原型中回显；按钮只记录合成材料引用已就绪。"
        />
        <Space wrap className="su-proxy-config-material-actions">
          <Button onClick={() => onUpdate({ materialReady: true })}>生成合成密钥与 UUID</Button>
          {readinessTag(config.materialReady, "密钥 / UUID 已就绪", "密钥 / UUID 待准备")}
        </Space>
        <Space wrap className="su-proxy-config-material-actions">
          <Typography.Text type="secondary">引用状态：</Typography.Text>
          {readinessTag(config.materialReady, "私钥就绪", "私钥待准备")}
          {readinessTag(config.materialReady, "公钥就绪", "公钥待准备")}
          {readinessTag(config.materialReady, "UUID 就绪", "UUID 待准备")}
        </Space>
        <Alert
          type="info"
          showIcon
          title="Short ID 引用"
          description="Short ID 只保存就绪状态，不回显合成值；它与密钥材料独立校验。"
        />
        <Space wrap className="su-proxy-config-material-actions">
          <Button onClick={() => onUpdate({ shortIdReady: true })}>生成合成 Short ID</Button>
          {readinessTag(config.shortIdReady, "Short ID 已就绪", "Short ID 待准备")}
        </Space>
      </>
    );
  }

  if (section === "tls") {
    return (
      <>
        <Form.Item
          name="fingerprint"
          label="指纹"
          rules={[{ required: true, message: "请选择 Reality 指纹" }]}
        >
          <Select options={FINGERPRINTS.map((fingerprint) => ({ value: fingerprint, label: fingerprint }))} />
        </Form.Item>
        <Form.Item name="utls" label="uTLS" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="关闭" />
        </Form.Item>
        <Form.Item name="alpn" label="ALPN">
          <Input placeholder="h2,http/1.1（可选）" />
        </Form.Item>
        <Typography.Text type="secondary" className="su-proxy-config-security-note">
          TLS 只引用合成配置；不会在界面中显示 Reality 私钥或公钥内容。
        </Typography.Text>
      </>
    );
  }

  if (section === "advanced") {
    return (
      <>
        <Form.Item name="tcpFastOpen" label="TCP Fast Open" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="关闭" />
        </Form.Item>
        <Form.Item name="multiplex" label="Multiplex" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="关闭" />
        </Form.Item>
      </>
    );
  }

  return null;
}

function shadowsocksFields(
  section: InboundFormSection,
  config: ProxyConfig,
  server: ServerAsset,
  onUpdate: (values: Partial<ProxyConfig>) => void,
): ReactElement | null {
  if (section === "protocol") {
    const methodOptions = Array.from(new Set([...server.methods, ...SS_METHODS])).map((method) => ({
      value: method,
      label: method,
      disabled: server.methods.length > 0 && !server.methods.includes(method),
    }));
    return (
      <>
        <Form.Item
          name="method"
          label="加密方法"
          rules={[{ required: true, message: "请选择 Shadowsocks 方法" }]}
        >
          <Select
            placeholder="选择服务器能力清单中的方法"
            options={methodOptions}
            onChange={(method) => {
              if (method !== config.method) onUpdate({ method, materialReady: false });
            }}
          />
        </Form.Item>
        <Form.Item
          name="network"
          label="网络"
          rules={[{ required: true, message: "请选择网络" }]}
        >
          <Select
            options={[
              { value: "tcp", label: "TCP" },
              { value: "tcp-udp", label: "TCP / UDP", disabled: !server.udp },
            ]}
          />
        </Form.Item>
        {!server.udp && config.network === "tcp-udp" && (
          <Alert type="warning" showIcon title="服务器未声明 UDP 能力" description="可以保存草稿，应用前需要改为 TCP。" />
        )}
      </>
    );
  }

  if (section === "security") {
    return (
      <>
        <Alert
          type="info"
          showIcon
          title="Shadowsocks 密码引用"
          description="密码只以合成材料就绪状态存在，不回显或复制任何密码内容。"
        />
        <Space wrap className="su-proxy-config-material-actions">
          <Button onClick={() => onUpdate({ materialReady: true })}>生成合成密码</Button>
          {readinessTag(config.materialReady, "密码已就绪", "密码待准备")}
        </Space>
      </>
    );
  }

  if (section === "tls") {
    return <Alert type="info" showIcon title="Shadowsocks 不使用 TLS 入站字段" description="协议认证由所选 method 与合成密码材料负责。" />;
  }

  if (section === "advanced") {
    return (
      <>
        <Form.Item name="tcpFastOpen" label="TCP Fast Open" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="关闭" />
        </Form.Item>
        <Form.Item name="multiplex" label="Multiplex" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="关闭" />
        </Form.Item>
      </>
    );
  }

  return null;
}

function hysteria2Fields(
  section: InboundFormSection,
  config: ProxyConfig,
  server: ServerAsset,
  onUpdate: (values: Partial<ProxyConfig>) => void,
): ReactElement | null {
  if (section === "protocol") {
    return (
      <>
        <Alert
          type={server.udp ? "info" : "warning"}
          showIcon
          title="Hysteria2 使用 UDP"
          description={server.udp ? "监听端口将在监听分区按 UDP 校验。" : "该服务器没有 UDP 能力，只能保存草稿。"}
        />
        <Form.Item
          name="sni"
          label="SNI"
          rules={[{ required: true, message: "请输入 Hysteria2 SNI" }]}
        >
          <Input placeholder="example.invalid" />
        </Form.Item>
        <Typography.Text type="secondary" className="su-proxy-config-security-note">
          认证密码、混淆与证书引用分别在安全和 TLS 分区管理。
        </Typography.Text>
      </>
    );
  }

  if (section === "security") {
    return (
      <>
        <Alert
          type="info"
          showIcon
          title="Hysteria2 认证"
          description="认证密码只记录合成材料就绪状态，不在原型中回显。"
        />
        <Space wrap className="su-proxy-config-material-actions">
          <Button onClick={() => onUpdate({ materialReady: true })}>生成合成认证密码</Button>
          {readinessTag(config.materialReady, "认证已就绪", "认证待准备")}
        </Space>
        <Form.Item name="obfs" label="Obfs">
          <Select
            options={[
              { value: "none", label: "无" },
              { value: "salamander", label: "salamander" },
            ]}
            onChange={(value: ProxyConfig["obfs"]) => {
              if (value === "none") onUpdate({ obfsReady: false });
            }}
          />
        </Form.Item>
        {config.obfs === "salamander" && (
          <Space wrap className="su-proxy-config-material-actions">
            <Button onClick={() => onUpdate({ obfsReady: true })}>生成合成 Obfs 密码</Button>
            {readinessTag(config.obfsReady, "Obfs 密码已就绪", "Obfs 密码待准备")}
          </Space>
        )}
      </>
    );
  }

  if (section === "tls") {
    return (
      <>
        <Form.Item
          name="certificateRef"
          label="证书引用"
          rules={server.certificates.length > 0 ? [{ required: true, message: "请选择 Hysteria2 证书引用" }] : []}
        >
          <Select
            allowClear
            placeholder={server.certificates.length > 0 ? "选择控制面登记的合成证书" : "当前服务器暂无证书引用"}
            options={server.certificates.map((certificate) => ({
              value: certificate.id,
              label: `${certificate.name} · ${certificate.domain}`,
            }))}
          />
        </Form.Item>
        {server.certificates.length === 0 && (
          <Alert type="warning" showIcon title="没有可用证书引用" description="可以浏览并保存草稿；应用前需要登记的合成证书。" />
        )}
        <Typography.Text type="secondary" className="su-proxy-config-security-note">
          仅接受服务器证书清单中的引用，不读取或显示证书私密内容。
        </Typography.Text>
      </>
    );
  }

  if (section === "advanced") {
    return (
      <>
        <Form.Item name="ignoreClientBandwidth" label="忽略客户端带宽" valuePropName="checked">
          <Switch checkedChildren="忽略" unCheckedChildren="遵循" />
        </Form.Item>
        <Space.Compact block>
          <Form.Item name="upMbps" label="上行 Mbps" style={{ flex: 1 }} rules={[{ type: "number", min: 0, message: "请输入非负带宽" }]}>
            <InputNumber min={0} precision={0} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item name="downMbps" label="下行 Mbps" style={{ flex: 1 }} rules={[{ type: "number", min: 0, message: "请输入非负带宽" }]}>
            <InputNumber min={0} precision={0} style={{ width: "100%" }} />
          </Form.Item>
        </Space.Compact>
        <Alert type="info" showIcon title="TCP Fast Open / Multiplex 不适用于 UDP" description="Hysteria2 使用 UDP 传输，这两项不会写入该入站配置。" />
      </>
    );
  }

  return null;
}

export default function InboundProtocolFields({
  section,
  protocol,
  config,
  server,
  onUpdate,
  onSyncSni,
  syncNotice,
}: InboundProtocolFieldsProps): ReactElement {
  const supportReason = protocolSupportReason(protocol, server);

  if (!IMPLEMENTED_PROTOCOLS.includes(protocol)) {
    return plannedView(protocol);
  }

  const fields = protocol === "vless-reality"
    ? realityFields(section, config, onUpdate, onSyncSni, syncNotice)
    : protocol === "shadowsocks"
      ? shadowsocksFields(section, config, server, onUpdate)
      : hysteria2Fields(section, config, server, onUpdate);

  return (
    <>
      {section === "protocol" && supportReason && (
        <Alert type="warning" showIcon title="当前协议能力门禁" description={supportReason} />
      )}
      {fields}
    </>
  );
}
