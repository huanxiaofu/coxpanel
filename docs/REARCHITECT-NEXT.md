# sing-ui R1-C：可复用入站资源与出站意图修订方案

> 日期：2026-09-09。状态：**R1 待用户确认**。本轮交付范围是前端原型与设计文档；交互仅作模拟，不触及后端真实 API、Agent、SQL、部署或生产服务。本文不代表真实监听已经支持多出站，或资源指标已经达标。
>
> 已完整阅读本轮任务书和既有方案；本轮以本地 `singbox_ui` 参考源码为事实依据，保留旧稿历史内容但不将其测试记成本轮通过。交付证据包括前端原型 diff、构建／回归日志和项目 `tmp/sing-ui-r1-status.md` 阶段报告。

> **最高优先级范围澄清：** R1-C 只覆盖 full Agent 的入站资源复用、ProxyDraft 出站意图、单服务器画布、端口冲突模型、能力分区和出站预览。NAT 与 lite Agent 在本轮明确**后续再议**；第 2、4、5 节及其他旧 lite/NAT 段落仅保留为历史／未来设计，不构成 R1 或 R2 的实施范围、退出门槛或验收前置条件。

## 0. 本次决定及覆盖顺序

**产品叫 sing-ui；R1-C 只实现 full Agent 的入口资源与节点意图模型。** 入站通过创建表单生产，出口意图由 ProxyDraft 的 `egress` 表达并可由画布连线投影；同一入口可被多个节点复用。服务器拖入画布、单服务器场景、模板、内部／外部分组、用户生命周期和客户十个覆写方案的既定体系不推翻。

冲突优先级：**NEXT → EXT → R0**；只覆盖下表明确变更，其他已确认约束继续有效。

| 原章节 | 本次替代／补充 | 不变的边界 |
| --- | --- | --- |
| R0／EXT 文档品牌、README 标题 | 产品与未来 UI／仓库目标名统一 sing-ui；第 1 节限定迁移边界 | 本轮不重命名远端仓库、运行路径、模块或环境变量 |
| R0 入口／节点模型 | `InboundResource` 独立持有监听配置；`ProxyDraft` 只引用 `inboundId` 并持有权威 `egress` | 不为每个节点创建重复监听；草稿与已发布快照隔离 |
| R0 4.1–4.5、5.4 表单中的出站选择器 | 入站可新建或选择已有资源；direct／next-hop／block 由 `ProxyDraft.egress` 表达，连线只是投影 | 同一画布创建、编辑、保存草稿、明确确认发布 |
| R0 6–8 编译、能力和 Agent 协议 | R1-C 只做 full Reality、SS2022、Hysteria2 的能力门禁与出站预览；真实聚合留 R2 | 不把 UI 意图声称为 runtime 已生效 |
| EXT 1、2.2、3、4、6–11 | 本轮只保留授权、模板、内外组边界的兼容要求 | NAT／lite 运行时、计量与地址自动化不进入 R1-C |
| EXT 10.3 分期 | R1-C 交互确认；R2 full 真实监听／路由验收；NAT／lite 独立后续评审 | 不将 lite、NAT 或 DDNS 绑定到 R1/R2 退出 |

## 1. 改名范围：文档现在改，部署另行执行

- **本轮落地**：本方案、R0、EXT 的产品称呼和文档标题统一 `sing-ui`，README 更名并提供方案入口；未来页面标题、导航品牌、安装包展示名和新文档使用同一拼写，不混用 SingUI／singui。
- **后续目标**：GitHub 仓库目标名 `sing-ui`，完整 Agent 展示名 `sing-ui Agent`，轻量版 `sing-ui Lite Agent`；拟议二进制名 `sing-ui-agent`／`sing-ui-agent-lite`。这些不是本轮已存在的安装命令。
- **本轮不执行**：GitHub 仓库 rename、origin 改址、Go module/import 改名、镜像／包名、Compose service、容器／卷、systemd unit、数据库名、环境变量前缀、域名和订阅路径变更。UI 品牌实际代码也不改。
- **兼容字面量保留**：现有命令中的 `COXPANEL_*`、旧数据库标识、历史任务书路径和当前远端 `github.com:huanxiaofu/coxpanel` 是原项目兼容标识，不机械替换成尚不能工作的命令。历史证据 ID 如 EXT 的 `[CP-G]` 继续有效，不是产品品牌。
- **实施改名检查表**：另行批准仓库 rename → 确认旧地址兼容与 CI／镜像引用 → 更新项目 origin 和文档示例 → 验证新 clone/build → 逐项灰度运行标识。保留用户、服务器、代理稳定 ID、现有订阅 token／路径和数据卷；不可通过改品牌重建数据库。失败回退须保留兼容入口，不能承诺上游永久重定向。

本轮不提交、不推送；旧名称在兼容命令和来源路径中的出现须明确标注，不算漏改产品品牌。

## 2. Agent 分层与进程架构（历史设计，R1-C 不验收）

> 本节的大量 full/lite、NAT、运行时和资源预算内容是旧稿的后续设计存档。R1-C 不实现、不验证、不依赖 lite 或 NAT；若与第 3、6、8、9 节的 R1-C 约束冲突，以 R1-C 约束为准。

### 2.1 两种形态，同一个管理面（后续再议）

| 维度 | full：完整 Agent，保留 | lite：超轻量 Agent，新增设计 |
| --- | --- | --- |
| 使用对象 | 常规 VPS、足够内存的多协议机器 | 小内存 NAT VPS，优先验证 64 MiB 实机 |
| 进程／安装 | 保留 Docker＋Agent＋完整 sing-box | 无 Docker 前提；静态二进制＋受控 SS 子进程，或验证后的单进程集成 |
| 协议 | 当前 Reality、Hysteria2、SS2022 多入站能力 | Shadowsocks 先行，只公布实际通过的 method／TCP／UDP 能力 |
| 运行时 | 现有 sing-box 编译和发布契约 | 独立小型 SS 运行时优先评估；裁剪 sing-box 作为兼容对照，不强制采用 |
| 拓扑 | 多级 chain、内部 relay／landing | 首版 direct-only；不能作为 chain 源／中继；满足门禁后可作 full 链路末端 |
| 发布 | prepare → apply → ACK、补偿回滚 | 保留同样的确认安全语义，缩小数据和执行范围；不靠心跳冒充 apply |
| 授权／计量 | 延续 EXT 独立用户凭据、授权租约和流量设计 | 同样按能力验收；不支持用户隔离／撤权／计量的运行时不得加入客户产品 |
| 安装服务 | 现有容器方式不迁走 | 有 systemd 时极简 unit；无 systemd 时现有 supervisor 前台管理，不以 systemd 为必需 |
| 后置事项 | 原协议／授权演进继续 | DDNS、任意多跳、自动安装、通用插件、远程 shell 不进轻量首版 |

`agentProfile` 表示形态，不等于协议支持或资源保证。full 也须按能力检查；lite 不是“只换名称的完整 Docker 镜像”。一个服务器一个 Agent 身份管理一个原生运行时，运行时可承载能力上限内多个入站；首批容量从一个 SS 入站开始验证，不自动允许无限用户／端口。

### 2.2 轻量推荐结构与候选选择

```text
sing-ui 控制面：ServerNode／ProxyNode／授权／发布操作
  ← Agent 主动 HTTPS：注册、心跳、拉配置、ACK、流量批次
NAT 小机器
  sing-ui-agent-lite（控制循环、受控配置、版本状态、限额遥测）
    → SS runtime adapter（固定命令参数，不执行任意 shell）
      → 已验证 SS 运行时（TCP；UDP 按能力开启）→ 本机出网
```

**优先路线**：薄管理进程＋专用 SS 服务端。管理进程复用认证、发布 envelope、流量去重的契约定义，不把完整渲染器／所有协议及管理 UI 嵌进去。配置由面板按运行时适配器生成受限文档；协议加密由成熟实现处理，不自写密码学。

| 候选 | 本轮可确认／采用理由 | 选型前必须验证 |
| --- | --- | --- |
| 小型 SS 服务端，例如 shadowsocks-rust 的 ssserver | 官方 README 有独立服务端和 AEAD-2022 构建特性；只是候选，不是已选定依赖 [UP-SS] | 锁定版本、架构／静态链接、真实内存、独立用户标识与计量、重载／终止连接、受控启动 |
| 裁剪／单协议 sing-box | 官方构建文档列可选 build tags；当前契约复用潜力大 [UP-SB] | 关闭可选功能并不证明只剩 SS；比较依赖、二进制、RSS/PSS 与重载峰值，不能用文件变小证明内存达标 |
| 同进程嵌入 SS 库 | 可能减少第二个运行时及 IPC；仅备选设计 | 维护／许可证评审、库 API 稳定性、鉴权计量、故障隔离；不能以省内存为由自实现协议 |

R2 输出同一硬件、版本和负载下的对照记录再定运行时。任一候选过不了“资源＋授权＋发布”三类门槛，就保留实验状态、不售为轻量可用版；允许专用运行时胜出，不把品牌 sing-ui 解释为所有机器必须跑完整 sing-box。

### 2.3 Shadowsocks 首版协议与用户语义

- 对外协议值继续用现有 `shadowsocks`；UI 标签 `SS`。cipher 由 `methods[]` 区分，不另造 `ss` 与 `ss2022` 两种 ProxyNode 身份。既有 full renderer 只支持 SS2022，这一事实不因新方案改变。
- lite 首选验证现有 SS2022 客户端／独立用户契约，method 从真实交集选择，例如先验证 `2022-blake3-aes-128-gcm`；并不因名称属于 Shadowsocks 就自动宣称全部算法可用。
- 如资源对照需要传统 AEAD SS，单列运行时、客户端适配器、凭据与授权迁移试验。没有原生用户区分时，只能评估“一用户独立入站／端口”的明确模型并计入 NAT 端口预算；禁止让所有客户共享一个不可区分的密码充当多用户产品。该路线未通过即不开放；不开放旧 stream／none 算法。
- `trafficScope=proxy` 不能冒充用户精确计量。客户组准入要求独立可撤销认证、用户维度流量、授权到期本地约束；不满足时仅允许管理员实验／受限内部末端，UI 显示“不支持客户分配”，不能用模板隐藏不足。
- 能力矩阵同时覆盖 `profile × runtimeVersion × protocol/method × network × clientFormat/adapterVersion`；EXT 的 Mihomo、Sing-box、Base64、Xray-json 仅输出已验证组合。不兼容须阻止或经明确确认排除，不能生成坏订阅。

### 2.4 资源预算：目标，不是实测结论

计量范围必须包含 **Agent＋运行时＋子进程＋reload／rollback 峰值**，并记录整机内核／系统服务和页缓存；不只报 Go heap、二进制大小、空闲一刻的 RSS，或忽略 Docker daemon。以下是 R2 待确认工程门槛，不是现有性能承诺。

| 目标环境 | 设计预算 | 负载与退出条件 |
| --- | --- | --- |
| 64 MiB 实机、无 swap、系统基线 ≤20 MiB | 两进程合计稳态 RSS ≤16 MiB；测试负载 ≤24 MiB；启动／重载／回滚峰值 ≤32 MiB；整机至少 12 MiB 安全余量 | 1 个入站、2 个独立用户、16 个持续 TCP 会话，24h；若支持 UDP，加 16 个 UDP 会话；记录各阶段峰值，无 OOM／无持续增长 |
| 32 MiB 实机探索档 | 系统基线 ≤8 MiB；组件空闲 ≤8 MiB、负载 ≤12 MiB、峰值 ≤16 MiB，余量 ≥8 MiB | 1 入站、1 用户、4 TCP；同样 24h。未通过则明确最低验证容量为 64 MiB，禁止宣传“32 MiB 可用” |
| 64／128 MiB 容量扩展 | 逐级增加入站、用户、TCP／UDP、流量 backlog，维持整机余量 | 得出 `maxInbounds/maxUsers/maxConnections/maxUDPAssociations`；不得把单用户跑通外推为多人无限使用 |

统一记录架构、内核、虚拟化限制、vCPU、精确二进制版本／摘要、方法、吞吐负载、p50/p95 控制面延迟、RSS/PSS、cgroup memory.current/peak（环境支持时）和 OOM 事件。64 MiB 档以稳定 5 Mbit/s 聚合负载为初始测试点，同时报告机器带宽限制；达不到需记录原因并重新评审服务规格，不拿空闲状态过关。

限制并发、UDP 会话与缓存寿命、配置文档 128 KiB、单批遥测 32 KiB、本地未确认遥测 spool 1 MiB（均为待验证的默认上界，可协商取更小值）。超限显式拒绝或报告 gap，不无限堆内存；削减日志和可选指标，不削减认证／版本校验／回执。缺少可安全停止／回滚的内存余量时拒绝应用，而不是冒险启动第二份完整实例。

## 3. R1-C 数据模型：入站资源与代理草稿

> R1-C 只定义 full Agent 的前端原型模型；实际持久化、渲染和运行时行为留到 R2。`InboundResource` 与 `ProxyDraft` 是两个独立对象，不能用“节点已创建”代替“监听已创建”。

### 3.1 独立入站资源

```text
ProxyConfig {
  listenPort: number
  protocol: "vless-reality" | "shadowsocks" | "hysteria2" | ...规划协议
  materialReady: boolean
  shortIdReady: boolean
  obfsReady: boolean
  certificateRef: string
  ...能力允许的监听／协议参数
}

InboundResource {
  id: string
  serverId: string
  config: ProxyConfig
  published?: ProxyConfig
}
```

- `id` 是入口资源稳定身份；`serverId` 决定监听归属。`config` 只保存 `listenPort`、协议和材料状态等结构化值，材料正文／私钥不进入卡片、草稿导出或本文。R1 的材料字段仅表达认证材料、Short ID、obfs 就绪状态和 `certificateRef`，没有 `materialRefs`、真实 `realityKeyRef` 或 `authRef`；R2 的受控材料存储及发布元数据另行设计。
- `published` 直接保存最近一次确认成功的 `ProxyConfig` last-known-good 快照，不是“草稿已保存”的别名，也不附带 R1 未定义的 `revision`／`publishedAt` 包装。新建、编辑和端口迁移先改 draft config；失败时保留原 `published`，不能把失败候选晋升为已发布。
- 同一 `serverId` 下，创建表单必须先检查既有 `InboundResource` 并支持“新建入口”或“选择已有入口”。选择已有入口只建立引用，不复制配置、不创建第二个监听。
- 端口占用 R1 保守以 `serverId + listenPort` 为键，不区分 TCP／UDP 或监听地址；未来再细化传输与地址维度。R1 模型导出 `inboundPortConflict` 时，**占用集合必须包含当前 draft config 与所有非空 `published` 快照**。旧快照尚未成功释放前仍占用端口，不能因节点被删除或草稿失败而消失。

### 3.2 独立代理草稿与权威出站意图

```text
ProxyDraft {
  id: string
  name: string
  serverId: string
  inboundId: string
  position: ...
  status: ...
  published: ...
  egress: {
    type: "direct" | "next-hop" | "block"
    targetInboundId?: string
  }
  ...其他既有草稿／画布字段
}
```

- 本轮只规定 `ProxyDraft` 的独立 `name`、`inboundId` 和权威 `egress` 关系；`serverId`、布局、状态、发布标记等既有字段继续保留，不在此处固化完整接口。节点不拥有端口、协议或材料。多个 `ProxyDraft` 可以复用同一 `inboundId`，并分别拥有 `direct`、不同 `next-hop` 或其他未来意图。
- `next-hop.targetInboundId` 可引用**任意服务器、任意用途**的已有 `InboundResource`，不要求目标标为 `internal`，不限制为单一上游，也不创建新的监听。目标是入口资源，不是目标 `ProxyDraft`。
- `egress` 是唯一权威。画布连线只是把 `source.inboundId → targetInboundId` 投影回同一份 `egress`；连线复用目标入口，不读取、复制或跟随目标节点自己的 `egress`。
- `direct` 的逻辑 tag 为 `direct`；`next-hop` 的逻辑 tag 为 `proxy_out`。这是 R1 预览语义；后端聚合在未来必须按实际 endpoint／身份生成唯一稳定命名并去重，不能为每个节点重复发出相同 tag。`block` 只作为兼容预留，运行时预览应转成现代 `reject`，不生成废弃的 `block` outbound。
- `egress.type=block` 不接受 `targetInboundId`；`direct` 也不接受目标。`next-hop` 缺少目标、目标不存在或目标未纳入同一草稿快照时阻止确认。

### 3.3 资源级校验、共享影响与隔离

- 循环检查建立在入站资源图上：对每条 `ProxyDraft` 暂拟边 `source inboundId → targetInboundId` 做保守 DFS／拓扑检查；自环直接拒绝，任何会形成资源级有向环的组合都拒绝。最多 8 个去重后的入站资源；同一服务器的不同入站可以互相被选择，不沿用旧的“服务器重入”硬限制。
- 删除 `ProxyDraft` 只删除节点及其出站意图／连线投影，**保留所引用的 `InboundResource` 及其 `published` 快照**。入口资源只有在单独确认且依赖清单为空时才可删除，不得因节点归零而级联删监听。
- 修改一个已被复用的 `InboundResource`，表单提示引用节点总数（包括下一跳及仍生效的旧引用）；保存后全部影响节点标记草稿。工作区应用确认列出节点、服务器、端口和出站摘要；共享资源尚有未包含的引用时，禁止只应用当前节点。
- 草稿图、资源 draft config 与已发布图／快照隔离。任何保存、校验、应用或聚合失败，都保留用户输入和原 `published` 快照，不部分晋升、不把部分成功的引用显示成已生效。
- R1 只模拟这些出站意图和资源关系。真实同一监听如何按身份、用户或路由把不同 `ProxyDraft` 分到不同出站，仍待 R2 设计、渲染和运行验收；不能声称两个真实的无条件 route 会同时生效。

### 3.4 能力边界

`ServerNode` 的能力结果只负责决定哪些表单可配置、哪些选项显示为规划项；未知能力不等于支持。R1-C 仅允许 full Agent 配置 Reality、SS2022、Hysteria2。旧稿中的 profile、lite、NAT、计量和地址字段继续作为后续设计存档，不是 R1-C 或 R2 的必需模型。

## 4. Agent API：路径复用，轻量编码协商（后续设计，R1-C 不验收）

> 本节保留旧稿的 Agent API、lite 编码和发布一致性设计，仅作为后续输入。R1-C 不实现或验证 lite/NAT；R2 先处理 full 的真实监听、配置聚合和路由验收，不以本节内容作为阶段退出条件。

### 4.1 当前事实与拟议端点

当前 router 已有 config、heartbeat、report-traffic、deployment-acks；没有把用户 `/api/auth/register` 当 Agent 注册。下表 `register` 与 schema 协商均为**新增设计**；原 R0 的 `/api/servers` 注册入口仍负责管理员创建资产／一次性引导。

| 方法／路径 | 增量请求／响应设计 | 安全与兼容 |
| --- | --- | --- |
| `POST /api/servers/{id}/enrollment`（拟新增） | 管理员选择允许 profile、有效期和绑定服务器，签发一次性引导引用 | 限时单次使用；不出现在服务器列表、拖拽 payload、日志或命令行历史 |
| `POST /api/agent/register`（拟新增） | 引导材料放受控认证头；body 为 profile、版本、runtime、能力、资源摘要；返回 nodeId、协议协商及受控 Agent 凭据 | 同一资产幂等注册，防重放；身份材料仅一次受控交付；丢响应通过受控重新引导恢复，不匿名领取既有身份 |
| `POST /api/agent/heartbeat`（复用扩展） | 现有 nodeId、generation、appliedRuntimeVersion；增 profile、能力 revision、runtime／资源摘要、可选地址观测 | 缺省省略昂贵 CPU／用户实时统计；不是把缺失填成 0。响应沿用 pendingDeployment，增加建议轮询间隔 |
| `GET /api/agent/config`（复用扩展） | 已认证协商 `configFormat=lite-ss-v1` 与支持 schema；响应保留 release、phase、generation、hash、过期／授权版本，payload 为受限 SS 配置 | 不兼容返回明确升级错误；旧 Agent 保持原格式。未变可协商 304／空响应，但有任务不可被旧缓存吞掉 |
| `POST /api/agent/deployment-acks`（复用） | prepare／apply／rollback 的结果，匹配 nodeId、releaseId、generation、runtimeVersion/hash、phase | 相同 ACK 幂等；旧／乱序／异节点回执拒绝；错误码脱敏；已拉取配置不是应用成功 |
| `POST /api/agent/report-traffic`（复用扩展） | 复用 traffic-v2 的 epoch/sequence、代理／用户归属和累计计数，少量批次；扩展采样区间／gap 按 EXT | ACK 游标后删除 spool；重传不重复收费；无用户维度不得伪造 UUID 或均分给客户 |
| `GET /api/servers/{id}/capabilities`（R0 拟议） | 两 profile 统一 DTO，返回协议、容量、NAT 状态、就绪／限制原因 | 面板和 Agent 双重校验；不能只禁用前端按钮 |
| `POST /api/agent/address-observations`（仅 R4+ 预留） | 地址／时间／来源和期望 revision；也可先保留为心跳可选字段 | 本阶段不开放处理器／后台任务；未来只收观测，不直接授权 DNS 更新 |

已认证 Agent 身份绑定 nodeId；沿用现有身份头契约的验证原则，不能只信 JSON 的 nodeId。控制面仅 HTTPS 出站可达即可管理 NAT 机器，不新开机器管理入站端口，不强制常驻 WebSocket／全时长长轮询。默认控制循环参考现有 30 秒，建议空闲心跳 60 秒、配置轻轮询 30 秒、任务期 2–5 秒短轮询，抖动退避上界由授权租约约束；这些是待测参数，不是本轮改生产配置。

轻量配置仍是**单台服务器完整受管入站快照**，可省掉完整拓扑、模板和公开管理信息，不能只下发当前卡片导致其他入站被清空。`lite-ss-v1` 指独立版本化编码，不是悄悄向旧 ConfigDocument 塞不同 JSON。hash 绑定规范化 payload、服务器、generation、profile／adapter 和授权材料版本；TLS 提供传输认证，hash 本身不是数字签名。

### 4.2 简化执行，不简化一致性

1. 保存入口／连线草稿，检查 capabilitiesRevision、图／代理／授权版本和容量；按 profile 选择编译器。未验证的轻量运行时只允许实验草稿，不进客户发布。
2. prepare 拉取受限候选，校验 schema、方法、端口、容量、材料和版本；持久化小型候选引用／状态，**此时不监听新端口、不替换在线认证**。
3. apply 仅应用已匹配 prepare 的候选；优先原生重载，若需停旧起新，明确短暂中断并保留一份 last-known-good。不为模拟热切换在 32／64 MiB 机器上强行双开运行时。
4. 原子替换受限配置文件、启动／存活和监听检查通过后持久化已应用状态，再发 ACK；崩溃重启核对实际进程和状态，不能凭磁盘 desired 指针发成功。
5. 面板仅在有效 ACK 及 operation 成功后晋升 publication／订阅。apply 失败回滚旧确认版；恢复不了报 manual_required、隔离受影响发布，不伪报成功。
6. 授权缩小、到期、额度耗尽仍按 EXT 先阻断订阅再确认 runtime 撤销；补偿回滚不得恢复已撤权材料。授权租约有到期上界；控制面离线时在本地停用过期授权，必要时停止入站，并实际测试已有连接终止语义。

上述 lite prepare/apply/ack、full → lite 联合发布和流量语义均为后续设计，**不属于 R1-C 或 R2 的验收范围**。在另行评审前不得在 UI 中把 lite/NAT 显示为本轮可配置能力，也不得以其占位字段推断真实链路已经可用。

## 5. NAT 与 DDNS：管理在线不等于入口可达（历史设计，后续再议）

> 本节旧稿内容仅作 NAT/DDNS 后续设计存档；R1-C 明确不处理 NAT、lite Agent、DDNS 或相关可达性验收。不得把这些字段、示例或未来接口写入 R1/R2 退出门槛。

### 5.1 NAT 首版必须说清的网络事实

- NAT 机器主动拉取能解决管理面通信，**不能自动解决公网入站可达性**。创建前区分本地监听端口、供应商映射的公网端口、advertised endpoint 和内部链路地址。
- 例（合成）：监听 `10080/TCP`，管理员登记公网 `edge.example.invalid:21080` 转到该端口；订阅只发布公布 endpoint，Agent 只绑定本机监听端口。示例域名不可真实使用，不含连接凭据。
- 端口字段只列登记的可用映射；TCP／UDP 映射分别检查，只有 TCP 不开放 UDP。Agent bind 成功不能证明公网映射通，心跳源 IP 也不能证明是入口 IP。
- 没有公网映射但有经验证的私网／现有覆盖网络地址，可作为 full 可拨入的内部末端；公网和私网都无可达路径时禁止发布可连接入口／连线目标，保留草稿和原因。首版不增加反向隧道、打洞、UPnP、自动端口转发或强制 EasyTier 安装。
- 全部客户端／链路拨号使用目标 ProxyNode 的受控 endpoint；不得将 `listenAddress=0.0.0.0/::` 当目标地址。端点变更影响上游时需列入发布范围，禁止偷偷改 A 的实际出口。

### 5.2 DDNS 明确放到 R4+

**本轮及轻量首版不实现 DDNS。** 可以手工填写管理员已有域名作为 advertisedAddress，不代表 sing-ui 已能更新 DNS；用户自行维护外部域名不改变此状态。

仅预留第 3／4 节字段与接口草案；不新增 DNS provider 凭据、provider SDK、定时器、更新 job、IP 探测服务、自动公布地址改写或 DDNS 开关假 UI。lite 可以为将来的心跳协议留地址观测结构，实施前不得声称已上报或已生效。

R4+ 独立评审路径：Agent 报 IP 变化观测 → 服务端认证、去抖与地址策略核查 → 管理员批准的 DNS 记录／provider 引用 → 最小权限更新 → 查询确认／TTL 与失败回退 → 按 endpoint 版本触发必要发布及缓存失效。共享 NAT 的出口 IP 变化不等于可入站映射改变；IPv4/IPv6 分开确认。provider 凭据将来只进受控秘密存储，不进 Agent 普通配置、浏览器草稿或文档。DDNS 不解决没有映射的问题。

## 6. R1-C 核心交互：资源复用与出站意图

### 6.1 画布对象和创建路径

**服务器拖入画布后先选择入站资源，再创建 `ProxyDraft`；节点不是监听器。** 新建入口产生一个 `InboundResource`，选择已有入口只建立 `inboundId` 引用。一个入口可以被多个节点复用：建好 HK 的 54321 Reality 直出节点后，该入口仍可被第二个节点复用，或作为其他入口的下一跳目标。

```text
左侧 full ServerNode
  拖 HK → 新建入口或选择已有入口 → 创建 ProxyDraft
画布
  InboundResource HK:54321 Reality
    ├─ ProxyDraft「HK direct」       egress: direct
    └─ ProxyDraft「HK via SG」       egress: next-hop → InboundResource SG:443 SS2022
  ProxyDraft「另一上游 via SG」       egress: next-hop ────────┘
```

R1-C 允许只有一台服务器；一个入口配 `direct` 就是完整的单机闭环，不要求第二台机器或额外落地卡。创建节点不创建重复监听，也不把 direct 伪装成独立出口资源。

### 6.2 `ProxyDraft` 出站意图和连线投影

| 操作 | 草稿语义 | 应用与校验 |
| --- | --- | --- |
| 新建入口并创建节点 | `ProxyDraft.inboundId = 新资源 id`，默认 `egress.type=direct` | 单服务器即可保存；不要求选择出口，不创建第二个监听 |
| 选择服务器已有入口 | `ProxyDraft.inboundId = 已有资源 id` | 只增加引用；不复制端口、协议、材料或 `published` 快照 |
| 节点 A 连接入口 B | `A.egress={type:"next-hop",targetInboundId:B.id}`；边是该字段投影 | B 可属于任意服务器、任意用途；目标是入口资源，不读取 B 节点的出站 |
| 多个节点连接同一入口 B | 多个 `ProxyDraft` 保存相同 `targetInboundId` | 共享一个监听；每个源节点仍可有不同出站意图 |
| A 改接 C | 原子替换 `targetInboundId`，不新增监听 | 预览全部影响范围后保存；不得同时保留两个下一跳 |
| 删除连线／选择本机直出 | 清除目标并设 `egress.type=direct` | 显式提示流量意图变为本机出网；未确认前不改已发布快照 |
| 目标离线或能力不足 | 目标选择禁用或标记不可用，保留用户草稿 | 不自动回落 direct；失败保留旧 `published` 快照并可重试 |
| 删除 `ProxyDraft` | 只删除节点、意图和连线投影 | 保留 `InboundResource`、其余引用和 `published` 快照 |
| 删除入口资源（R2 设计） | R1 不提供入口资源删除操作；移除节点仍可重新选择已有资源 | 未来依赖清单清空并单独确认后才可删；禁止级联删入口 |
| 编辑共享入口资源 | 修改 draft config，列出所有引用节点及发布项 | 所有引用标记 dirty；一次确认覆盖完整影响范围 |
| 键盘／小屏连接 | 选源卡“连接到…”再选目标，提交同一 Connect 命令 | 是拖线的无障碍替代，不另存出站对象 |

**`egress` 是唯一出站目标权威**，边只是同一 graph revision 的可视投影；不在 payload 留第二份可冲突的 target。`direct` 与零目标一一对应，`next-hop` 与恰好一个 `targetInboundId` 一一对应，`block` 不允许目标。

校验建立在**入站资源图**而不是服务器图：拒绝自环和任何资源级有向环，最多 8 个去重后的入站资源；同一服务器的不同入站可以相互选择，不沿用旧的服务器重入硬限制，也不要求目标标记为 `internal` 或只有一个上游。R1-C 只模拟意图；真实同一监听如何按身份／用户／路由区分不同出站，留待 R2 设计、聚合和运行验收，不能声称两个真实无条件 route 会同时生效。

### 6.3 入站表单、能力门禁和端口占用

参考本地 `frontend/components/inbound/*.tsx` 的基础监听、协议字段、TLS／安全和高级字段组织，以及提交 loading、错误定位和协议快捷切换。12 个规划协议目录为 WireGuard、Mixed、VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、Naive、ShadowTLS、AnyTLS、HTTP；R1-C 只让 full Agent 的 Reality（VLESS+Reality）、SS2022、Hysteria2 可配置，其余按能力显示 disabled／规划原因，不冒充已支持。

| 组件／分区 | R1-C 字段与动作 | 门禁／失败语义 |
| --- | --- | --- |
| `InboundResourcePicker` | 新建入口或选择当前服务器已有入口；显示端口、协议和引用计数 | 选择已有只建立 `inboundId`，不复制配置或创建监听 |
| `ProxyBasicFields` | 服务器只读、入口名称、用途、监听端口；节点名称单独属于 `ProxyDraft` | 入口和节点分开保存；单机可完成闭环 |
| `RealityInboundFields` | SNI、dest／目标 host:port、材料就绪状态、`certificateRef`（如适用）、ShortID、指纹、uTLS；“从 dest 提取 SNI” | 快捷动作只做本地字符串解析；不把 key/auth 秘密建模为字段；材料失败保留输入和旧快照 |
| `ShadowsocksInboundFields` | SS2022 method 下拉、密码生成、网络／用户认证摘要 | 仅显示 full 能力交集，例如 `2022-blake3-aes-128-gcm`；失效 method 阻止应用并保留表单 |
| `Hysteria2InboundFields` | UDP 监听端口、SNI、证书引用、obfs、上／下行带宽 | 缺证书或 UDP 能力时阻止应用并给原因 |
| `TLSCertificateFields` | `certificateRef` 与合成证书名称／域名提示；R1 不生成或导入证书 | 不展示私钥正文；缺少或清空证书引用阻止应用，不导致页面异常 |
| `PortAvailabilityField` | 同机端口检查、冲突资源／快照列表和端口自动同步提示 | R1 按 `serverId + listenPort` 保守冲突，不区分 TCP／UDP／监听地址；`inboundPortConflict` 占用集合包含当前 draft 与所有非空 `published`；阻止新资源，不因删节点释放端口 |
| `AdvancedProxyFields` | 监听地址、公布 endpoint／用途和受控材料引用 | 不提交任意文件路径或核心 JSON；NAT/DDNS 不在本轮 |

编辑共享资源时，表单先提示引用计数，工作区确认覆盖全部影响节点的名称、服务器、端口和出站；真实 publication 差异预览由 R2 实现。草稿、资源 draft config 与模拟发布快照隔离；模拟失败保留用户输入和原 `published`，提供重试，不部分晋升。

### 6.4 出站视图、路由和 DNS 预览

参考本地 `frontend/components/outbound/*.tsx` 的 direct、block、订阅节点和协议出站表单，以及 `route/routing-config.tsx`、`dns/dns-config.tsx` 的字段与合成方式。R1-C 将出站配置呈现为节点级意图和预览，不把每个 `ProxyDraft` 渲染成真实监听。

- `direct` 显示本机直出，逻辑 tag 为 `direct`；`next-hop` 显示目标 `InboundResource` 推导出的服务端地址、端口和协议，逻辑 tag 为 `proxy_out`。目标入口的 draft／published endpoint 变化进入影响范围。
- `next-hop` 只复用目标入口，不跟随或复制目标节点的 `egress`；多个源节点可以共享同一入口。后端未来聚合必须按实际 endpoint／身份生成唯一稳定命名并去重，不能为每个节点重复生成相同 `proxy_out` tag。
- `block` 仅预留兼容意图；预览使用现代 `reject` action，不生成废弃的 `block` outbound，也不接受 `targetInboundId`。
- 出站策略可配置 `domainStrategy`、`bindInterface`、sniff、DNS `udp`／`tcp`／`https`／`tls`／`hosts` 五类和 `rule_set` 合成引用；`rule_set` 只记录引用，不下载数据。
- 路由预览展示 `route`、`sniff`、`hijack-dns`、`reject`、`resolve` action 及其 outbound／resolver 目标；预览不是 runtime 已按规则分流的证据。
- **R1 仅模拟意图。** 同一真实监听若要让不同身份／用户／路由得到不同出站，R2 必须先验收身份匹配、路由生成、tag 聚合和实际客户端路径；不能把两个无条件 route 同时生效写成当前能力。

吸收本地参考工程的分区和生成动作，不照搬 Xray schema、明文私钥展示、任意目标扫描或证书路径；SNI/dest 快捷填写不发网络请求。

## 7. 前端与用户产品体系的影响

- **服务器资产／详情**：R1-C 画布只接入 full ServerNode，并展示能力状态、可用协议、已有入站资源和发布状态。NAT、lite、运行时容量与地址观测仍是后续设计存档，不在本轮资产门禁或 R1/R2 退出条件中。
- **入站资源／代理卡**：节点卡显示节点名、入口资源名、服务器、协议／端口、出站意图、引用计数和草稿／已发布状态；材料就绪状态只在表单中显示。移除未发布节点不删除资源；共享资源编辑提示节点及下一跳引用计数，工作区确认完整影响范围。
- **画布与权限**：新节点可新建入口或选择所属服务器已有入口；`next-hop` 则可引用任意服务器已有入口，目标是入口资源而非目标节点。不能借连线绕过授权、发布或可订阅条件。内外分组、稳定代理 ID、模板隔离和客户十个覆写名额沿用 EXT，不因资源复用扩大公开权限。
- **用户与订阅**：只有明确确认并成功发布的节点进入用户可见订阅；草稿、失败候选和旧 `published` 快照隔离。保留 EXT 到期、额度、重置、撤权和延迟报告语义；R1 不把模拟的 `proxy_out` 或资源复用计作真实客户端流量链路。
- **原型验收**：浅深色、375px 手机、桌面布局、八个导航项、键盘“连接到…”、错误定位、离线禁用、未保存离开和焦点返回都要演示；DOM 断言与截图证据分别记录，不能以 build／lint 代替用户交互验收。

## 8. 分期、依赖与回退

| 阶段 | 本次增量 | 退出门槛 |
| --- | --- | --- |
| R0-NEXT：方案基线 | 品牌与本轮模型决定；保留用户／模板／授权边界 | 设计范围可追溯；不把文档当作功能已交付 |
| R1-C：前端原型＋交互确认 | full 服务器画布、`InboundResource` 复用、`ProxyDraft.egress`、Reality／SS2022／Hysteria2 表单、出站／路由／DNS 预览 | 用户完成单机 direct、共享入口、任意下一跳、端口／环／能力门禁和失败重试；只证明模拟意图，不触后端真实 API |
| R2：full 真实纵切 | 持久化资源与草稿／发布隔离、配置聚合、唯一 `proxy_out` 命名、发布 ACK／回滚、身份／用户／路由分流 | 先设计并验收同一真实监听区分不同出站的匹配与路由；真实客户端路径、端口释放和旧快照回退均有证据；不得以两个无条件 route 代替该验收 |
| R3：协议能力扩展 | R1 规划协议按 Agent 能力逐项解锁，补齐用户订阅、内外组和授权接入 | 每个协议有能力交集、材料保护、客户端矩阵和回退证据；未验收项保持规划态 |
| R4：EXT 用户体系收口 | 模板导入、手动覆写、十个方案、生命周期／重置、撤权与计量继续 | 保留 EXT 原退出标准；资源复用不扩大用户权限、不重复计量 |
| R5：兼容迁移与全站收口 | 旧入口／稳定 ID／订阅链接兼容、客户端矩阵、文档与品牌迁移检查 | 旧数据与授权不丢，旧入口无越权旁路；运行时重命名单独窗口执行 |

R1-C → R2 的顺序是：先稳定资源／节点模型和前端能力门禁，再实现 full 聚合与真实发布，最后验收身份／用户／路由分流。NAT、lite Agent、DDNS 和相关内存／可达性试验不绑定 R1 或 R2，须另行提出范围、模型和验收门槛；旧章节中的相关内容只作历史设计存档。

回退规则贯穿两阶段：保存、校验、应用或聚合失败均保留用户草稿与旧 `published` 快照；重试成功前不部分晋升、不把失败候选显示为已生效。新能力下线先冻结新写入并处理在途发布，再回到兼容版本；不凭未知能力推断支持。

## 9. 验收清单：原型证据与未来运行证据分开

### 9.1 本轮文档验收

- [x] sing-ui 产品命名、README／方案入口一致，旧运行兼容字面量有说明。
- [x] `InboundResource`／`ProxyDraft` 独立、共享入口、资源级环检、`published` 快照和 `inboundPortConflict` 规则已写明。
- [x] `egress` 为唯一权威；边只投影；direct／next-hop／block 的 tag、预览 action 和 R1 模拟边界已写明。
- [x] 十二协议目录中 full Reality、SS2022、Hysteria2 可配置；其余九种显示能力规划门禁，不冒充已支持。
- [x] EXT 模板、内外组、用户、客户覆写、授权和订阅边界不被资源复用绕过。
- [ ] R1-C 浏览器 UI 验收待独立 loopback 实测；本清单不把文档、DOM 或截图预先记为通过。

检查结果与 commit/push 以最终 diff 和阶段报告证据为准；本轮修改前端原型，不修改后端源码。

### 9.2 R1-C 浏览器验收（当前待验证）

| 编号 | 用户操作／场景 | 证据要求 |
| --- | --- | --- |
| U1 | 拖 HK，创建 Reality `54321`，填写合成材料就绪状态与 ShortID，应用后显示单机 active；原节点默认 `direct` | 用户流程、保存后的 DOM 状态和截图；不泄露秘密 |
| U2 | 再拖 HK，选择已有 `54321` 入口保存第二节点；创建 SG 的订阅用途 SS 并作为第二个 HK 节点的 next-hop；原 HK direct 不变；多个节点复用 SG | 入口引用计数、各节点 `egress` 摘要和画布投影；截图不能代替模型语义 |
| U3 | 新建资源使用已占用 `54321`，并分别尝试 self-loop 与资源级环 | `inboundPortConflict` 显示当前 draft＋非空 `published` 占用；自环／环拒绝；同机不同入口可引用 |
| U4 | 编辑共享入口，确认完整影响范围；删除节点；制造失败并重试 | 引用节点／下一跳依赖计数、全部引用 dirty；资源及旧快照保留；失败后用户输入和旧 `published` 保留，重试可继续 |
| U5 | 保存并重开 direct 高级 `domainStrategy`、`bindInterface`、sniff、DNS 五类型和 `rule_set` 引用；检查 route 预览 action | `route`／`sniff`／`hijack-dns`／`reject`／`resolve` 仅为预览；`rule_set` 不下载 |
| U6 | 配置 Hy2 UDP、证书、obfs、上／下行带宽；查看十二协议目录中其余九种规划门禁 | 表单字段、材料就绪和能力原因真实显示；规划项不可伪装为可发布协议 |
| U7 | 离线时尝试编辑／保存；检查 375px、桌面浅／深色和八导航布局 | 操作禁用或给出原因；截图证明布局；`pageerror` 为 0，DOM 与视觉结论分开 |

### 9.3 R2 运行验收（不提前在 R1 通过）

| 编号 | 必须真实验收 | 证据 |
| --- | --- | --- |
| R2-1 | 同一监听按身份／用户／路由把不同 `ProxyDraft` 分到不同出站 | 配置聚合、唯一稳定 tag、实际客户端路径和规则匹配；不能用两个无条件 route 同时生效替代 |
| R2-2 | full Agent 真实发布、ACK、回滚、端口释放和旧快照恢复 | 成功／失败／重试的版本状态与运行配置一致；失败不部分晋升 |
| R2-3 | 用户授权、订阅、撤权、计量和模板边界 | EXT 原有门槛继续；资源复用不扩大权限或重复计量 |

所有验证只可在隔离 loopback／合成数据上执行；报告不包含私钥、真实订阅、UUID、password 或完整可连接配置。NAT、lite、DDNS 和历史 lite 资源预算另行评审，不是 R1/R2 验收。

## 10. 来源、证据与本轮限制

| 标识 | 本次读取依据 | 使用边界 |
| --- | --- | --- |
| TASK | `/opt/data/workspace/tmp/sing-ui-r1c-task.md`（项目外只读） | 用户本轮产品决定与交付要求；只在 panel-design 写文件 |
| R0／EXT | `docs/REARCHITECT.md`、`docs/REARCHITECT-EXT.md`，原稿全文各 512 行 | 保留原确认体系，本稿明确替代点；历史验证不是本轮结果 |
| LOCAL-UI | `/opt/data/workspace/tmp/ref-singbox-ui/frontend/components/inbound/*.tsx`、`outbound/*.tsx`、`route/routing-config.tsx`、`dns/dns-config.tsx`、`CLAUDE.md` | 只借鉴本地字段分区、协议表单、路由／DNS 合成和交互组织；不把参考 UI 当作本项目已实现 |
| LOCAL-AGENT | `/opt/data/workspace/tmp/ref-singbox-ui/server/handlers/singbox.go`、`server/services/singbox.go`（仅相关代码） | 只核对本地 handler／service 的协议与配置事实；不读取凭据文件，不据此声称 R1 已有后端聚合 |
| LOCAL-PROJECT | `frontend/src/prototype/` 当前原型与 `frontend/package.json` | 只用于本轮前端原型／脚本边界；不把 build／lint 当作用户验收 |

为可追溯保留任务书和本地参考定位：

```text
任务书：/opt/data/workspace/tmp/sing-ui-r1c-task.md
singbox_ui 参考：/opt/data/workspace/tmp/ref-singbox-ui/
```

本轮不使用外链作最新性断言，也不读取凭据、连接数据库、运行参考项目或接触生产。设计文档修改限本文件，阶段记录更新在项目 `tmp/`；浏览器验收的实际命令、端口、exit code、DOM／截图和 `pageerror` 结果以阶段报告为准，不预先宣称通过。
