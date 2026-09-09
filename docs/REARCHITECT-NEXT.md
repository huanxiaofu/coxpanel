# sing-ui R1-C：可复用入站资源与线性链代理方案

> 日期：2026-09-09。状态：**R1 线性链原型，待用户确认**。本轮重构前端合成原型，同步本方案与 `docs/REARCHITECT.md`，完成限定测试、build/lint、提交推送及阶段报告。后端 API、Agent、SQL 和生产部署不在范围；本文不代表真实监听、共享监听分流、外部出口或资源指标已经达标。
>
> 已完整阅读线性链任务书和既有方案；本轮保留旧稿历史上下文，但明确旧自由画布方案已被线性链取代，不将历史测试或实现计划记成本轮通过。

> **最高优先级模型纠正：** 一个代理节点就是一条有序线性链，不是自由连线图。链固定为“第一跳订阅入口 → 中间零或多个入站中转 → 唯一 terminal 出站”；terminal 可以是本机 `direct`，也可以预留外部出口，但 R1 不伪造外部出口可用。禁止分叉、合并和任意多叉连线；跨链只复用 `InboundResource`，不把两条 `ProxyDraft` 链连接成图。NAT 与 lite Agent 在本轮明确**后续再议**；第 2、4、5 节及其他旧 lite/NAT 段落仅保留为历史／未来设计，不构成 R1 或 R2 的实施范围、退出门槛或验收前置条件。

## 0. 本次决定及覆盖顺序

**产品叫 sing-ui；R1-C 只实现 full Agent 的入站资源复用与线性链节点意图模型。** `ProxyDraft` 由一条有序 `chain` 和一个独立的 terminal `egress` 组成；第一跳是订阅入口，中间跳是可选入站中转，terminal 不是 inbound hop。服务器资产、模板、内部／外部分组、用户生命周期和客户十个覆写方案的既定体系不推翻，但旧自由画布不再是产品模型。

冲突优先级：**NEXT → EXT → R0**；只覆盖下表明确变更，其他已确认约束继续有效。

| 原章节 | 本次替代／补充 | 不变的边界 |
| --- | --- | --- |
| R0／EXT 文档品牌、README 标题 | 产品与未来 UI／仓库目标名统一 sing-ui；第 1 节限定迁移边界 | 本轮不重命名远端仓库、运行路径、模块或环境变量 |
| R0 入口／节点模型 | `InboundResource` 独立持有监听配置；`ProxyDraft` 持有有序 `chain` 和独立 terminal `egress` | 不为每个节点创建重复监听；草稿与已发布快照隔离 |
| R0 4.1–4.5、5.4 表单中的自由出站／画布连接 | 改为 `ChainEditor`：每跳选择服务器并复用已有入站或新建入口；末端单独选择 `direct` 或外部出口预留 | 一链一节点；不分叉、不合并、不把两条链连成图 |
| R0 6–8 编译、能力和 Agent 协议 | R1-C 只做 full Reality、SS2022、Hysteria2 的能力门禁与出站预览；真实聚合留 R2 | 不把 UI 意图声称为 runtime 已生效 |
| EXT 1、2.2、3、4、6–11 | 本轮只保留授权、模板、内外组边界的兼容要求 | NAT／lite 运行时、计量与地址自动化不进入 R1-C |
| EXT 10.3 分期 | R1-C 交互确认；R2 full 真实监听／路由验收；NAT／lite 独立后续评审 | 不将 lite、NAT 或 DDNS 绑定到 R1/R2 退出 |

## 1. 改名范围：文档现在改，部署另行执行

- **本轮落地**：本方案、R0、EXT 的产品称呼和文档标题统一 `sing-ui`，README 更名并提供方案入口；未来页面标题、导航品牌、安装包展示名和新文档使用同一拼写，不混用 SingUI／singui。
- **后续目标**：GitHub 仓库目标名 `sing-ui`，完整 Agent 展示名 `sing-ui Agent`，轻量版 `sing-ui Lite Agent`；拟议二进制名 `sing-ui-agent`／`sing-ui-agent-lite`。这些不是本轮已存在的安装命令。
- **本轮不执行**：GitHub 仓库 rename、origin 改址、Go module/import 改名、镜像／包名、Compose service、容器／卷、systemd unit、数据库名、环境变量前缀、域名和订阅路径变更。仅更新原型导航与链交互，不进行运行标识迁移。
- **兼容字面量保留**：现有命令中的 `COXPANEL_*`、旧数据库标识、历史任务书路径和当前远端 `github.com:huanxiaofu/coxpanel` 是原项目兼容标识，不机械替换成尚不能工作的命令。历史证据 ID 如 EXT 的 `[CP-G]` 继续有效，不是产品品牌。
- **实施改名检查表**：另行批准仓库 rename → 确认旧地址兼容与 CI／镜像引用 → 更新项目 origin 和文档示例 → 验证新 clone/build → 逐项灰度运行标识。保留用户、服务器、代理稳定 ID、现有订阅 token／路径和数据卷；不可通过改品牌重建数据库。失败回退须保留兼容入口，不能承诺上游永久重定向。

本轮验证通过后以 `feat(sing-ui): R1 linear chain proxy model` 提交并推送项目 `origin/main`，停在 R1。旧名称在兼容命令和来源路径中的出现不算漏改产品品牌；不重命名远端仓库。

## 2. Agent 分层与进程架构（历史设计，R1-C 不验收）

> 本节的大量 full/lite、NAT、运行时和资源预算内容是旧稿的后续设计存档。R1-C 不实现、不验证、不依赖 lite 或 NAT；若与第 3、6、8、9 节的 R1-C 约束冲突，以 R1-C 约束为准。

### 2.1 两种形态，同一个管理面（后续再议）

| 维度 | full：完整 Agent，保留 | lite：超轻量 Agent，新增设计 |
| --- | --- | --- |
| 使用对象 | 常规 VPS、足够内存的多协议机器 | 小内存 NAT VPS，优先验证 64 MiB 实机 |
| 进程／安装 | 保留 Docker＋Agent＋完整 sing-box | 无 Docker 前提；静态二进制＋受控 SS 子进程，或验证后的单进程集成 |
| 协议 | 当前 Reality、Hysteria2、SS2022 多入站能力 | Shadowsocks 先行，只公布实际通过的 method／TCP／UDP 能力 |
| 运行时 | 现有 sing-box 编译和发布契约 | 独立小型 SS 运行时优先评估；裁剪 sing-box 作为兼容对照，不强制采用 |
| 拓扑 | 有序线性 chain：订阅入口 → 入站中转* → terminal | 首版 direct-only；不能作为 chain 源／中继；满足门禁后可作 full 链路末端 |
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

## 3. R1-C 数据模型：独立资源与有序线性链

> R1-C 只定义 full Agent 的前端原型模型；实际持久化、渲染和运行时行为留到 R2。`InboundResource` 与 `ProxyDraft` 是两个独立对象，不能用“节点已创建”代替“监听已创建”。旧 `TopologyWorkspace` 的自由图只作为历史迁移输入，不再作为新模型的权威结构。

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

### 3.2 代理草稿、链跳与 terminal 出站

```text
ProxyDraft {
  id: string
  name: string
  chain: ChainHop[]
  egress: Terminal
  status: "draft" | "deploying" | "active" | "failed" | ...
  dirty: boolean
  published?: PublishedProxySnapshot
}

ChainHop {
  position: number
  serverId: string
  inboundId?: string
  newInboundDraft?: InboundDraft
}

Terminal {
  type: "direct" | "external"
  externalRef?: string
}
```

- `chain` 至少有一个 `ChainHop`，`position` 从 0 连续递增。未完成草稿允许空服务器／入站；应用前每跳必须绑定一个服务器及其入站，第一跳必须是订阅入口。`inboundId` 与 `newInboundDraft` 不能同时存在；新建表单保存后产生资源引用，未保存的新建草稿不可应用。
- `egress` 是链的唯一末端，**不是一个 inbound hop**。`direct` 表示最后一跳在所属服务器本机直出；`external` 只表示未来外部出口的预留意图，R1 不提供可用选择、不生成伪造 endpoint、不宣称可部署。R1 不再使用 `next-hop` 或 `block` 作为链节点／自由边模型。
- `ProxyDraft` 不拥有端口、协议或材料正文；这些仍由每个 hop 引用或新建的 `InboundResource` 提供。节点名称、顺序、链状态、dirty 标记和已发布快照与资源配置分开保存。
- 多个 `ProxyDraft` 可以在任意 hop 复用同一 `InboundResource`，但 `hop.serverId` 必须等于该资源的 `serverId`，不同服务器各取自己的入站。复用只增加引用，不复制端口、协议、材料或快照，也不把其他链的结构带入当前链。
- 链的顺序本身就是链路顺序；不能添加第二个 terminal，不能在 hop 之间建立额外边，不能从一条链分叉、合并或连接到另一条链。`ChainEditor` 的连接线只是相邻卡片之间的方向提示，不是可持久化的自由图边。
- 当 `egress.type=external` 时必须保留未就绪／规划态原因；不得使用 `externalRef` 伪造真实可用外部出口。`egress.type=direct` 不需要目标资源，且是 R1 唯一可完成的 terminal。

### 3.3 资源级校验、共享影响与隔离

- 线性链不生成 `ProxyDraft` 之间的拓扑图，也不做旧自由图的分叉／合并／图环推导；校验每条链的顺序、首跳订阅入口、每跳资源完整性和唯一 terminal。同一条链不可重复同一入站，跨链复用资源不改变链的独立性；不沿用旧 DFS 的八跳限制，真实容量门槛留待后续验证。
- 删除 `ProxyDraft` 只删除节点及其 `chain`／terminal 草稿，**保留所引用的 `InboundResource` 及其 `published` 快照**。入口资源只有在单独确认且依赖清单为空时才可删除，不得因节点归零而级联删监听。
- 修改一个已被复用的 `InboundResource`，表单提示引用节点和链跳总数（包括首跳、中转跳及仍生效的旧引用）；保存后所有受影响的 `ProxyDraft` 标记 dirty。工作区应用确认列出链、跳位、服务器、端口、协议和 terminal 摘要；共享资源尚有未包含的引用时，禁止只应用当前节点。
- 草稿链、资源 draft config 与已发布链／快照隔离。任何保存、校验、应用或聚合失败，都保留用户输入和原 `published` 快照，不部分晋升、不把部分成功的引用显示成已生效。
- R1 只模拟线性链意图和资源关系。真实同一监听如何按身份、用户或路由把复用该资源的不同 `ProxyDraft` 分到各自 terminal，仍待 R2 设计、渲染和运行验收；不能声称共享监听分流或真实部署已经可用。

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

1. 保存入站资源／线性链草稿，检查 capabilitiesRevision、链／代理／授权版本和容量；按 profile 选择编译器。未验证的轻量运行时只允许实验草稿，不进客户发布。
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
- 全部客户端／链路拨号使用链上 hop 的受控 endpoint；不得将 `listenAddress=0.0.0.0/::` 当目标地址。端点变更影响包含该 hop 的链时需列入发布范围，禁止偷偷改链的实际 terminal。

### 5.2 DDNS 明确放到 R4+

**本轮及轻量首版不实现 DDNS。** 可以手工填写管理员已有域名作为 advertisedAddress，不代表 sing-ui 已能更新 DNS；用户自行维护外部域名不改变此状态。

仅预留第 3／4 节字段与接口草案；不新增 DNS provider 凭据、provider SDK、定时器、更新 job、IP 探测服务、自动公布地址改写或 DDNS 开关假 UI。lite 可以为将来的心跳协议留地址观测结构，实施前不得声称已上报或已生效。

R4+ 独立评审路径：Agent 报 IP 变化观测 → 服务端认证、去抖与地址策略核查 → 管理员批准的 DNS 记录／provider 引用 → 最小权限更新 → 查询确认／TTL 与失败回退 → 按 endpoint 版本触发必要发布及缓存失效。共享 NAT 的出口 IP 变化不等于可入站映射改变；IPv4/IPv6 分开确认。provider 凭据将来只进受控秘密存储，不进 Agent 普通配置、浏览器草稿或文档。DDNS 不解决没有映射的问题。

## 6. R1-C 核心交互：ChainEditor 与资源复用

### 6.1 页面职责和创建路径

**一个 `ProxyDraft` 就是一条有序线性链，不是自由画布上的一组可互连卡片。** `/prototype/proxies` 一链一行，展示节点名称、链路摘要、terminal 和草稿／发布状态；不会把多条链拼在一张总图里。`/prototype/topology` 路径保留，页面改为“代理节点链编辑器”：资产面板 + 链列表 + 选中单链编辑。

```text
/prototype/topology
  代理节点列表       ChainEditor（仅编辑当前一条链）
  HK 主链             [入口 HK] ──→ [中转 SG] ──→ [出站：本机直出]
  SG 直出             [入口 SG] ──→ [出站：本机直出]
```

`ChainEditor` 横向展示“入口 → 中转（可选）→ 出站”。第一张卡是客户端订阅入口；中间零或多张卡是入站中转；最右侧 terminal 卡是独立的 `egress`，**不是 `ChainHop`，也不是 inbound**。卡片顺序就是链路顺序，没有分叉、合并、回边或跨链连接。

创建链时先生成 `ProxyDraft` 的名称和第一跳位置。每个 hop 位置都选择“服务器 + 已有 `InboundResource`”，或在该位置新建入口；新建表单在保存前保留本地草稿。服务器资产面板保留，拖动只填充接收卡片对应的 hop；资产按钮填入当前选中跳，空态可创建首条链。新建入口产生独立资源，复用已有入口只建立引用，不生成自由卡片或连线。

R1-C 允许只有一台服务器：一条链含一个入口 hop，terminal 设为 `direct`，就是完整单机闭环。不同 `ProxyDraft` 可以复用同一个 `InboundResource`，但只共享资源，不把两条链连接成一张图，也不复制监听。

旧 `TopologyWorkspace` 的多卡片自由连线是被本模型取代的历史方案。旧坐标、边和“连接到……”交互只用于迁移／兼容说明；新建和编辑统一使用链列表与 `ChainEditor`，不再把自由连线作为产品能力。

### 6.2 `ProxyDraft`、ChainHop 与 terminal 操作

| 操作 | 草稿语义 | 应用与校验 |
| --- | --- | --- |
| 新建单机链 | 创建 `chain:[{position:0,serverId,inboundId?或newInboundDraft}]`，默认 `egress:{type:"direct"}` | 第一跳必须是订阅入口；单机可完成，不创建额外出口资源 |
| 选择已有入口 | 指定 hop 写入 `inboundId`，保留资源原配置 | 只增加引用；不复制端口、协议、材料或 `published` 快照 |
| 在指定 hop 新建入口 | 指定 hop 写入 `newInboundDraft`，保存后产生 `InboundResource` | `inboundId` 与 `newInboundDraft` 不可同时存在；端口冲突阻止保存／应用 |
| 追加中转 | 在 terminal 前插入一个新的 `ChainHop`，后续 `position` 顺延 | 追加后必须选择服务器及已有／新建入口；terminal 永远保持唯一且在最右侧 |
| 删除 hop | 删除一个 `ChainHop` 并重新编号；至少保留一个 hop | 允许调整草稿；新首跳若非订阅入口则明确提示并阻止应用，不删除被引用的资源 |
| 左右调整 | 仅在 `ChainHop[]` 内移动并重排 `position` | 不移动 terminal；实时检查第一跳订阅入口和每跳资源完整性 |
| 替换 hop | 更换该位置的服务器，再选择已有入口或新建入口草稿 | 只影响当前 hop；共享资源的其他链保持不变，并纳入影响提示 |
| terminal 设为直出 | `egress={type:"direct"}` | 以最后一个 hop 所在服务器本机出网；R1 唯一可完成的 terminal |
| terminal 选择外部出口 | `egress={type:"external",externalRef?}`，保持未就绪 | R1 只预留选项和原因，不伪造 endpoint、连接信息或可部署状态 |
| 删除 `ProxyDraft` | 只删除该链草稿、状态和引用关系 | 保留 `InboundResource`、其他链引用和 `published` 快照 |
| 编辑共享入口资源 | 修改资源 draft config，列出所有引用链和 hop 位 | 所有受影响链标记 `dirty`；应用须确认完整影响范围 |
| 资产拖入指定 hop | 目标 hop 接收 `serverId`，然后显示该服务器可复用入口／新建入口 | 不创建节点卡、不建立链间边、不改变其他 hop |

链的唯一结构权威是 `chain` 的有序 hop 列和单独的 `egress` terminal；不保存可与之冲突的自由 edge／target。`position` 从 0 连续编号。草稿可保留未完成跳，应用前必须绑定服务器和已保存入站；terminal 永远只有一个。

### 6.3 线性校验、共享影响与隔离

- 校验每条链的第一跳、连续顺序、每跳资源完整性和唯一 terminal；线性序列天然没有分叉、合并和图环，不再运行旧自由图的 DFS／拓扑连线检查。跨链复用 `InboundResource` 不产生跨链边，也不改变链的独立性。
- 链可经过同机或不同服务器，但每跳只能引用所选服务器的入站；跨链复用只增加引用，不复制监听。同链重复入站被拒绝；同机端口冲突规则覆盖每个 hop 的 draft 与 `published` 快照，不沿用旧自由图八跳上限。
- 删除 `ProxyDraft` 只删除链，不删除所引用资源。入口资源只有在单独确认且依赖清单为空时才可删除，不得因链归零而级联删监听。
- 修改被复用的 `InboundResource` 时，表单显示引用链、hop 位和仍生效的旧引用；保存后全部影响链标记 `dirty`。工作区应用确认列出链、跳位、服务器、端口、协议和 terminal 摘要。
- 草稿、资源 draft config 与已发布快照隔离。保存、校验、模拟部署或未来聚合失败，均保留用户输入和原 `published`，不部分晋升、不把失败候选显示成已生效。
- R1 只模拟线性链意图和资源关系。真实同一监听如何按身份／用户／路由把复用资源的不同链分到各自 terminal，以及跨服务器真实发布，均留待 R2 设计、实现和运行验收；不得以 UI 预览声称共享监听分流或真实部署可用。

### 6.4 入站表单、能力门禁和端口占用

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

编辑共享资源时，表单先提示引用计数，工作区确认覆盖全部影响链的名称、跳位、服务器、端口和 terminal；真实 publication 差异预览由 R2 实现。草稿、资源 draft config 与模拟发布快照隔离；模拟失败保留用户输入和原 `published`，提供重试，不部分晋升。

### 6.5 terminal、路由和 DNS 预览

参考本地 `frontend/components/outbound/*.tsx` 的 direct、订阅节点和协议出站表单，以及 `route/routing-config.tsx`、`dns/dns-config.tsx` 的字段与合成方式。R1-C 将 terminal 和策略呈现为链级意图与预览，不把 `ProxyDraft` 或任何 hop 渲染成已经真实部署的监听。

- `egress.type="direct"` 显示最后一跳所在服务器本机直出，预览可使用逻辑 tag `direct`。它不创建额外 hop、不创建出口资源，也不要求第二台服务器。
- `egress.type="external"` 仅为后续外部出口预留；R1 显示 disabled／未就绪原因，不生成地址、凭据、连接参数或可用状态。外部出口不是 inbound hop，也不加入链的 hop 数量。
- 旧 `next-hop`、`block` 和“边决定出口”的写法属于历史兼容模型。若需显示旧数据，先转换为链的 hop／terminal 或报告人工核对；新 `ProxyDraft` 不保存自由 target。策略层的 `reject` 可以作为预览 action，但不是 terminal 类型。
- 出站策略可配置 `domainStrategy`、`bindInterface`、sniff、DNS `udp`／`tcp`／`https`／`tls`／`hosts` 五类和 `rule_set` 合成引用；`rule_set` 只记录引用，不下载数据。
- 路由预览展示 `route`、`sniff`、`hijack-dns`、`reject`、`resolve` action 及其 outbound／resolver 目标；预览不是 runtime 已按规则分流的证据。
- **R1 仅模拟意图。** 同一真实监听若要让复用该资源的不同链按身份／用户／路由得到不同 terminal，R2 必须先验收匹配、路由生成、tag 聚合、共享监听分流和实际客户端路径；真实部署也必须单独验收，不能用两个无条件 route 同时生效替代。

吸收本地参考工程的分区和生成动作，不照搬 Xray schema、明文私钥展示、任意目标扫描或证书路径；SNI/dest 快捷填写不发网络请求。

## 7. 前端与用户产品体系的影响

- **服务器资产／详情**：R1-C 的 `ChainEditor` 只接入 full ServerNode，并展示能力状态、可用协议、已有入站资源和发布状态。NAT、lite、运行时容量与地址观测仍是后续设计存档，不在本轮资产门禁或 R1/R2 退出条件中。
- **入站资源／代理行**：`/prototype/proxies` 每条链一行，显示节点名、入口与中转 hop 摘要、terminal、引用计数和草稿／已发布状态；材料就绪状态只在每跳配置中显示。删除链不删除资源；共享资源编辑提示引用链及 hop 位。
- **链编辑器与权限**：`/prototype/topology` 是列表＋当前链编辑，不是总图。每个 hop 可新建入口或复用已有 `InboundResource`；资产拖动只填指定 hop。不能借 hop 选择绕过授权、发布或可订阅条件；外部 terminal 仍是 R1 disabled 预留。
- **用户与订阅**：只有明确确认并成功发布的链进入用户可见订阅；草稿、失败候选和旧 `published` 快照隔离。保留 EXT 到期、额度、重置、撤权和延迟报告语义；R1 不把模拟 terminal 或资源复用计作真实客户端链路。
- **原型验收**：浅深色、375px 手机、桌面布局、八个导航项、键盘追加／删除／左右调整、每跳配置、错误定位、离线禁用、未保存离开和焦点返回都要演示；DOM 断言与截图证据分别记录，不能以 build／lint 代替用户交互验收。

## 8. 分期、依赖与回退

| 阶段 | 本次增量 | 退出门槛 |
| --- | --- | --- |
| R0-NEXT：方案基线 | 品牌与本轮模型决定；保留用户／模板／授权边界 | 设计范围可追溯；不把文档当作功能已交付 |
| R1-C：前端原型＋交互确认 | `ChainEditor`、一链一行列表、`InboundResource` 复用、`ProxyDraft.chain`＋terminal `egress`、Reality／SS2022／Hysteria2 表单、出站／路由／DNS 预览 | 用户完成单机直出、两跳／三跳排链、共享入口、追加／删除／左右调整、端口／能力门禁和失败重试；R1 只用合成 ACK 演示状态，不触后端真实 API |
| R2：full 真实纵切 | 持久化资源与链草稿／发布隔离、配置聚合、发布 ACK／回滚、共享监听分流和真实客户端路径 | 共享同一监听的不同链必须先完成身份／用户／路由匹配与分流验收，再确认真实部署、端口释放和旧快照回退；本轮不预先记为通过 |
| R3：协议能力扩展 | R1 规划协议按 Agent 能力逐项解锁，补齐用户订阅、内外组和授权接入 | 每个协议有能力交集、材料保护、客户端矩阵和回退证据；未验收项保持规划态 |
| R4：EXT 用户体系收口 | 模板导入、手动覆写、十个方案、生命周期／重置、撤权与计量继续 | 保留 EXT 原退出标准；资源复用不扩大用户权限、不重复计量 |
| R5：兼容迁移与全站收口 | 旧入口／稳定 ID／订阅链接兼容、客户端矩阵、文档与品牌迁移检查 | 旧数据与授权不丢，旧入口无越权旁路；运行时重命名单独窗口执行 |

R1-C → R2 的顺序是：先稳定资源／节点模型和前端能力门禁，再实现 full 聚合与真实发布，最后验收身份／用户／路由分流。NAT、lite Agent、DDNS 和相关内存／可达性试验不绑定 R1 或 R2，须另行提出范围、模型和验收门槛；旧章节中的相关内容只作历史设计存档。

回退规则贯穿两阶段：保存、校验、应用或聚合失败均保留用户草稿与旧 `published` 快照；重试成功前不部分晋升、不把失败候选显示为已生效。新能力下线先冻结新写入并处理在途发布，再回到兼容版本；不凭未知能力推断支持。

## 9. 验收清单：原型证据与未来运行证据分开

### 9.1 本轮文档验收

- [x] sing-ui 产品命名、README／方案入口一致，旧运行兼容字面量有说明。
- [x] `InboundResource`／`ProxyDraft` 独立、共享入口、资源级环检、`published` 快照和 `inboundPortConflict` 规则已写明。
- [x] `ProxyDraft` 的有序 `chain` 与唯一 terminal `egress`、每跳复用／新建、无分叉合并自由连线和 R1 外部出口预留边界已写明。
- [x] 十二协议目录中 full Reality、SS2022、Hysteria2 可配置；其余九种显示能力规划门禁，不冒充已支持。
- [x] EXT 模板、内外组、用户、客户覆写、授权和订阅边界不被资源复用绕过。
- [ ] R1-C 浏览器 UI 验收待独立 loopback 实测；本清单不把文档、DOM 或截图预先记为通过。

检查结果与 commit/push 以最终 diff 和阶段报告证据为准；本轮修改前端原型，不修改后端源码。

### 9.2 R1-C 浏览器验收（当前待验证）

| 编号 | 用户操作／场景 | 证据要求 |
| --- | --- | --- |
| U1 | 创建 HK Reality `54321` 入口 hop，设置 terminal 为本机 `direct`，确认单机闭环；外部 terminal 保持 disabled | 链 hop、terminal、保存后的 DOM 状态和截图；不泄露秘密 |
| U2 | 在 `ChainEditor` 排出 HK 入口 → SG 中转 → `direct`，再追加第二个中转完成三跳链；每跳分别选择已有入口或新建入口 | `position` 连续、每跳服务器／资源引用、唯一 terminal 和链摘要；截图不能代替模型语义 |
| U3 | 在指定 hop 拖入服务器，尝试占用已有端口；尝试分叉、合并、跨链连接或把 terminal 当 inbound hop | `inboundPortConflict` 显示当前 draft＋非空 `published` 占用；非法结构被拒绝，资产只填目标 hop |
| U4 | 两条链复用同一 `InboundResource`；编辑共享入口，确认完整影响范围；删除一条链；制造失败并重试 | 引用链／hop 计数、全部引用 dirty；资源及旧快照保留；失败后用户输入和旧 `published` 保留，重试可继续 |
| U5 | 保存并重开 direct 高级 `domainStrategy`、`bindInterface`、sniff、DNS 五类型和 `rule_set` 引用；检查 route 预览 action | `route`／`sniff`／`hijack-dns`／`reject`／`resolve` 仅为预览；`rule_set` 不下载 |
| U6 | 配置 Hy2 UDP、证书、obfs、上／下行带宽；查看十二协议目录中其余九种规划门禁 | 表单字段、材料就绪和能力原因真实显示；规划项不可伪装为可发布协议 |
| U7 | 离线时尝试编辑／保存；检查 375px、桌面浅／深色和八导航布局 | 操作禁用或给出原因；截图证明布局；`pageerror` 为 0，DOM 与视觉结论分开 |

### 9.3 R2 运行验收（不提前在 R1 通过）

| 编号 | 必须真实验收 | 证据 |
| --- | --- | --- |
| R2-1 | 同一监听被不同链复用时，按身份／用户／路由把各链分到各自 terminal | 配置聚合、共享监听分流、唯一稳定 tag、实际客户端路径和规则匹配；不能用两个无条件 route 同时生效替代 |
| R2-2 | full Agent 真实发布、ACK、回滚、端口释放和旧快照恢复 | 成功／失败／重试的版本状态与运行配置一致；失败不部分晋升 |
| R2-3 | 用户授权、订阅、撤权、计量和模板边界 | EXT 原有门槛继续；资源复用不扩大权限或重复计量 |

所有验证只可在隔离 loopback／合成数据上执行；报告不包含私钥、真实订阅、UUID、password 或完整可连接配置。NAT、lite、DDNS 和历史 lite 资源预算另行评审，不是 R1/R2 验收。

## 10. 来源、证据与本轮限制

| 标识 | 本次读取依据 | 使用边界 |
| --- | --- | --- |
| TASK | `/opt/data/workspace/tmp/sing-ui-chain-task.md`（项目外只读） | 用户本轮线性链产品决定与交付要求；仅在 panel-design 修改前端原型、目标文档和阶段报告 |
| R0／EXT | `docs/REARCHITECT.md`、`docs/REARCHITECT-EXT.md`，原稿全文各 512 行 | 保留原确认体系，本稿明确替代点；历史验证不是本轮结果 |
| LOCAL-UI（历史来源） | `/opt/data/workspace/tmp/ref-singbox-ui/frontend/components/inbound/*.tsx`、`outbound/*.tsx`、`route/routing-config.tsx`、`dns/dns-config.tsx`、`CLAUDE.md` | 保留旧稿的字段与交互组织来源；本轮未重新读取参考工程，不作为当前实现或复验结果 |
| LOCAL-AGENT（历史来源） | `/opt/data/workspace/tmp/ref-singbox-ui/server/handlers/singbox.go`、`server/services/singbox.go` | 保留旧稿来源；本轮未复验，不据此声称 R1 已有真实聚合 |
| LOCAL-PROJECT | `frontend/src/prototype/` 当前原型与 `frontend/package.json` | 只用于本轮前端原型／脚本边界；不把 build／lint 当作用户验收 |

为可追溯保留任务书和本地参考定位：

```text
任务书：/opt/data/workspace/tmp/sing-ui-chain-task.md
singbox_ui 参考：/opt/data/workspace/tmp/ref-singbox-ui/
```

本轮不使用外链作最新性断言，也不读取凭据、连接数据库、运行参考项目或接触生产。设计文档修改限本文件与 `docs/REARCHITECT.md`；前端原型完成限定测试及 build/lint 后与文档一同提交推送。浏览器实际命令、回环端口、exit code、DOM／截图和 `pageerror` 结果记录在 `tmp/sing-ui-r1-status.md`，不得提前宣称通过；交付后停在 R1 等待用户确认。
