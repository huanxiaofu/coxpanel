# sing-ui 产品骨架重构方案

> 日期：2026-09-08。状态：**方向评审稿，未批准实施**。本轮只写文档，不修改应用、数据库、Agent 或部署配置。
>
> 调研基线：sing-ui `c770260`；本地 3x-ui `2ec6c73`，提交日期均为 2026-09-08。文中的“已有”以这些源码为准，“建议／新增／目标”均不代表已经实现。任务书中对参考项目的部分描述与这个版本不符，见第 2 节。

> **2026-09-08 R0-EXT 修订入口：`docs/REARCHITECT-EXT.md`。** 用户已确认两层节点模型、服务器拖入画布创建代理的强交互和 3x-ui 前端风格；这些方向保留。新增订阅模板、内外分组、用户生命周期与客户覆写完整闭环，以补充稿为准。本文第 1 节“不做完整模板平台／Xray 适配器”的旧范围、第 7／8 节第二批单层授权建议、第 9.2 节总量估算及第 10 节旧分期，不再代表完整产品范围；具体替代关系见补充稿第 0 节。第 11 节仅记录原 R0 轮次验证，不是本次修订验证。确认产品方向和提交文档均不等于批准实施。

## 0. 结论先行：需要换骨架，不是给旧页面换皮

> **2026-09-09 线性链修订入口：`docs/REARCHITECT-NEXT.md`。** 产品统一 sing-ui；一个代理节点是一条有序线性链，`ChainEditor` 替代旧自由画布。第一跳是订阅入口，中间是零个或多个入站中转，最后是唯一 terminal `egress`；R1 只预留外部 terminal，不伪造可用出口。冲突优先级 NEXT → EXT → 本文。本文第 3／4／5 节同步关键语义；第 6–8 节 Agent 编译、能力、API 的分层增量和第 10 节分期以 NEXT 为准，DDNS 后置 R4+。历史来源路径和旧自由图术语只作兼容／迁移记录，不代表产品仍使用旧品牌或旧模型。

**明确推荐：以 sing-ui 现有集中控制面、Agent 和 sing-box 契约为技术底座，重建两层领域模型与管理端交互；前端以本地 3x-ui 的布局、主题和配置表单为具体参考重新实现。暂不整体 fork 3x-ui 替换产品。**

这不是“继续修补现在的 UI”：`Nodes.tsx` 的混合节点表、先建入站再连线的 `TopologyGraph.tsx`、旧导航和表单组织都应重做。保留的是经过编码的控制能力，不是用户已经否定的产品骨架。

三个不能退让的产品原则：

1. **服务器节点是资源，代理节点是服务。** 左侧选的是运行 Agent 的服务器，不是已经存在的入站。一个服务器可承载多个代理节点。
2. **用户在链编辑器内完成工作。** 创建 `ProxyDraft` → 排第一跳订阅入口 → 可追加零个或多个中转 hop → 选择唯一 terminal 直出（外部出口在 R1 仅作预留）→ 确认应用 → 查看状态；相邻箭头只表达顺序，不允许分叉、合并或跨链连线。每跳可复用已有 `InboundResource` 或新建入口，不要求用户先维护一张独立自由图。
3. **美观与可用同时交付。** 采用 3x-ui 的统一主题、紧凑信息层级、协议分区表单、状态和操作规范；不是增加一张有装饰但不能操作的拓扑图。

首个验收故事：用户在 `/prototype/topology` 创建一条 `ProxyDraft`，把 `HK-zouter` 填入第一跳并在该跳新建 Reality 入口；链后追加唯一 terminal，摘要显示“本机直出”，点击“创建并应用”，由 R1 原型模拟 prepare → apply → ACK 状态推进。**这只需要一台服务器、一个入站 hop，不需要虚构第二个落地节点或另选出口，也不等待真实 Agent 回执。** `zouter` 沿用用户服务器命名，不假设它是另一组件。

## 1. 调研边界与证据方法

- 完整阅读任务书 `/opt/data/workspace/tmp/coxpanel-rearchitect-task.md`；只读对照当前工程及 `/opt/data/workspace/tmp/ref-3xui/`。
- 3x-ui 的证据路径在下文统一省略参考根目录，以 `3x-ui: frontend/...` 表示。sing-ui 路径相对当前仓库。
- 没有读取凭证文件、连接真实数据库、登录现有面板、检查 KUL 生产服务或运行参考项目。不能把静态源码审查说成实际视觉体验、跨节点验收或已有生产经验。
- Remnawave 只参考官方学习资料，不编造本会话没有的“本机实践记忆”。核查了官方 `Config Profiles`、`Nodes`、`Hosts`、`Squads` 页面；不引入其源码和运维依赖。定位信息见附录 A。
- Xboard 按任务书忽略。这个阶段不增加新协议、支付系统、Xray 适配器或完整配置模板平台。

## 2. 必须先纠正的现状认识

### 2.1 sing-ui 并非完全没有两层数据，问题在产品抽象和写入流程

| 已核实事实 | 证据 | 对重构的意义 |
| --- | --- | --- |
| `nodes` 同时存受管机器和外部静态代理；`inbounds.node_id` 已是一对多 | `backend/internal/db/migrations/0001_init_nodes.up.sql:2`；`backend/internal/models/models.go:11` | 一机多入站可以复用，但不能继续把机器和外部代理放进同一个产品列表 |
| 当前画布把所有已有入站展开成卡片，连线前要求选择角色／出站模式 | `frontend/src/pages/TopologyGraph.tsx:14`、`:47`、`:60` | 这是“连接预建入站”的编辑器，不是用户要求的“拖服务器生成代理” |
| 当前建入站操作藏在节点列表弹窗内 | `frontend/src/pages/Nodes.tsx:1` | 要把配置入口移到画布，列表也复用同一编辑器 |
| 0010 已有草稿、预览和部署快照；0011 已有图版本、逐入站版本、跨节点发布和回执状态 | `backend/internal/db/migrations/0010_control_plane.up.sql:2`；`backend/internal/db/migrations/0011_p2_topology.up.sql:1` | 不重写全部发布引擎，但需要新的编排应用服务把这些能力封装成一次用户操作 |
| 编辑被已部署拓扑引用的入站会被拒绝 | `backend/internal/repo/nodes_p2.go:45` | 仅加“双击编辑”会撞上后端限制；必须有编辑草稿和发布后晋升机制 |
| `/sub/{token}` 的组内节点遍历直接取 `ListInbounds`，筛选 `role=entry`，不是只读已确认发布版本 | `backend/internal/api/sub/sub.go:117` | 新建／编辑草稿若继续写这条路径，可能先出现在订阅里、后实际部署；订阅发布边界是本轮方案的必改项 |
| `topology_deployments` 在激活下一台 apply 时更新，不等于所有 Agent 已回执成功 | `backend/internal/repo/topology_release.go:363` | 不能把现有部署表直接命名为“已生效配置”，也不能以 HTTP 202 或心跳在线显示成功 |
| `edges` 表存在，但本次在 backend/shared/agent 中未找到它的 SQL 读写；当前图读取 `topology_drafts.edges` | `backend/internal/repo/topology_graph.go:59` | 不能迁移错数据源；历史 `edges` 只做校对，不建立第二套活跃路由真相 |

### 2.2 2026-09-08 的 3x-ui 已不只是“单机＋克隆入站”

任务书说“多节点是克隆到别的节点，不是集中管理面”。**本地版本不支持这个绝对判断。**

- `3x-ui: internal/web/controller/node.go:29` 定义节点 list/get/add/update/delete/enable/test、入站查询、probe、历史指标、面板升级和 mTLS 管理接口。
- `3x-ui: internal/database/model/model.go:775` 的 `Node` 有远端地址、TLS 模式、入站同步范围、outbound tag、心跳、面板与 Xray 状态。
- `3x-ui: internal/web/runtime/manager.go:15`、`:76` 在本地／远端 runtime 间分派；`internal/web/runtime/remote.go:362` 及相关方法实现远端操作。`internal/web/job/node_heartbeat_job.go:31` 和 `node_traffic_sync_job.go:81` 处理节点心跳／流量同步。
- `3x-ui: frontend/src/pages/nodes/NodeList.tsx:237`、`NodeFormModal.tsx:38` 已有实质性的远程节点管理；`CloneInboundModal.tsx:37` 只是其中一个入站辅助操作。
- `3x-ui: internal/database/db.go:2189` 已支持 PostgreSQL 分支，不能以“只支持 SQLite”否定它。
- `3x-ui: frontend/src/pages/inbounds/form/protocols/hysteria.tsx:10` 已有 Hysteria v2 子表单，`InboundFormModal.tsx:890` 有接入。不能沿用旧印象声称参考项目完全没有 Hy2。

**准确区别是部署模型和产品语义，而不是有没有节点页面：**参考实现以本地／远端 3x-ui 面板运行时及 Xray 模型组织管理；sing-ui 的目标是集中控制 Agent、跨服务器拓扑发布、用户级订阅与权限。当前 full 沿用 sing-box；NEXT 新增不强制完整内核的超轻量 lite，不能把既有 Agent 称为已符合 NAT 小内存门槛。3x-ui 已有远程管理、客户端和订阅能力应计入复用价值，但不直接等价于本项目语义。

没有在上述证据中确认可直接替代 sing-ui 的“服务器拖入 → 跨服务器依赖发布 → prepare/apply 回执 → 补偿回滚”完整画布闭环。这个结论表示**不能认定现成功能可复用**，不声称对整个参考仓库做了功能不存在的形式证明。

## 3. 新概念模型：只有两层“节点”

### 3.1 服务器节点 ServerNode

受管物理机或虚拟机上的 Agent 注册身份；一台服务器一个受管 Agent 身份。full 保留 Docker＋sing-box；lite 新增无 Docker 的原生轻量形态，运行时在资源／授权实测后选定，不强制完整 sing-box。两者同为 ServerNode，不是代理协议，也不直接出现在用户订阅里；详见 NEXT 第 2–5 节。

- 身份／资产：`id`、`name`、地区／标签、`hostname`、`os`、`arch`、CPU／内存摘要。
- 网络：公开访问地址 `publicAddress`、内部管理／链路地址 `privateAddress`；旧 `public_ip/easy_ip` 的公开 DTO 别名，不把 EasyTier 强加为用户必须理解的概念。
- 运行：`agentStatus`、`lastSeenAt`、`agentVersion`、`coreVersion`、能力清单与当前配置 generation。
- 能力：是否具备 Reality／SS2022／Hy2 所需内核、UDP、证书引用以及 `topology-chain-v2` 等。**未知不等于支持；心跳在线不等于代理可用。**
- 生命周期：待注册、在线、离线、维护／停用。资产标签由管理员维护，系统／版本事实由 Agent 上报；不得用心跳覆盖管理员备注。
- 分层增量：`agentProfile`、运行时／能力 revision、容量、用户认证／计量／租约能力、NAT 标记及端口映射，见 NEXT 第 3 节；未知不放行。DDNS 仅预留字段／接口，R4+ 再做。

容器或二进制只是 Agent 安装方式，不改变模型。本阶段不设计远程 SSH 自动安装；注册页后续提供部署说明和一次性注册流程，凭证不进入资产拖拽数据。

### 3.2 代理节点 ProxyNode

**一个受管代理节点 = 一条有序线性链。** `ProxyDraft` 不是一个可与其他节点自由连线的卡片，而是订阅客户端看到的一个完整节点成品：第一跳是订阅入口，中间是零个或多个入站中转，最后是唯一 terminal `egress`。terminal 单独存放，不是 `ChainHop`，也不是入站资源。

概念 DTO：`id`、`name`、`chain: ChainHop[]`、`egress: Terminal`、`status`、`dirty`、`published?`。链上 hop 的入站协议、端口、SNI 和材料状态由 `InboundResource` 提供；代理草稿不复制监听配置或秘密。

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

- 一个 ServerNode 可承载多条 `ProxyDraft`；同一服务器可出现在同一条链的多个 hop，也可被多条链使用。服务器资产是 hop 的来源，不是额外的节点卡。
- `position` 从 0 连续编号；应用时第一跳必须是客户端订阅入口，每跳绑定其 `serverId` 所属的已保存入站。未完成草稿可暂留空服务器／入站；复用引用与新建草稿不得同时存在，新建保存后生成资源引用。
- 代理身份使用稳定 ID，不用端口、位置、协议或显示名称作为身份。原型使用随机链 ID；与旧 `inbounds.id` 的兼容映射留待 R2 设计，R1 不做生产数据迁移，也不据此承诺凭据、流量或覆写引用已经切换。
- `egress.type="direct"` 表示最后一跳在所属服务器本机直出；`egress.type="external"` 仅预留未来外部出口。R1 不生成外部 endpoint、凭据或可用状态，外部 terminal 不计为 inbound hop。
- 多条链可复用同一个 `InboundResource`，复用只建立资源引用，不复制配置、不创建第二个监听，也不读取或跟随另一条链的 terminal。跨链复用不会把两条链连接成图。
- 链的顺序由 `chain` 唯一表达；禁止自由 edge、分叉、合并、回边、第二个 terminal 或把 terminal 当作 inbound hop。相邻卡片之间的箭头只是顺序提示。
- `exposure = subscription | internal` 继续区分订阅入口和仅供链路使用的入站用途，但用途归属于 hop／资源配置，不改变一链一节点的结构。第一跳默认是 subscription；中转 hop 的授权、计量和可达性仍须服务端校验。
- 同一个入站资源可作为多条链的第一跳或中转 hop；是否允许某种共享监听分流留待 R2 验证，R1 只展示复用和影响范围，不声称 runtime 已按链区分流量。

### 3.3 外部静态代理不是服务器资产

旧 `nodes.type=external` 没有 sing-ui Agent，不应显示在“服务器节点”资产面板或提供“下发配置”。

第一期将其独立展示为“外部代理”，沿用旧数据与订阅生成路径。它是代理服务的一种来源，不是第三层机器节点。DTO 使用 `origin=external` 和命名空间 ID `external:<oldNodeId>`，不能与受管链 ID 混用。R1 不允许外部代理作为链上 inbound hop 或 terminal；若以后允许作外部出口，明确“仅配置上游、无法确认远端”的边界。

### 3.4 迁移对照表

| 旧概念／物理表 | 新产品概念 | 第一期存储处理 | 必须改变的行为 |
| --- | --- | --- | --- |
| `nodes(type=managed)` | 服务器节点 `ServerNode` | 保留表名、ID、Agent 关联，补资产／观测字段 | `/servers` 只显示服务器，不再混合协议 |
| `nodes(type=external)` | 外部代理服务 | 保留兼容记录、单独 DTO 和列表 | 不出现在服务器拖拽面板、不显示 Agent 状态 |
| `inbounds` | 受管代理节点 `ProxyNode` | 保留 ID、`node_id` FK；领域名及 API 改名 | 用户可在画布直接创建、配置、部署和编辑 |
| `inbounds.role` | 内部编译属性 | 暂保留，映射自 exposure／egress | 用户不再必须先学 entry／landing／relay |
| `inbounds.egress_mode` | 代理节点出边的兼容投影 | 保留 direct／chain | 无边＝direct，一条出边＝chain；连线决定出口，不另建出口实体 |
| `edges` 关系表 | 遗留路由边 | 保留只读兼容，不当作权威 | 与草稿冲突则报告人工核对，禁止静默覆盖 |
| `topology_drafts.edges` | 代理节点间的草稿连接 | 保留按来源服务器分片及图版本 | 接受代理级变更，由服务层归并到服务器 |
| `topology_drafts.layout` | 画布展示状态 | 分离成单独布局版本，见 0017 | 移动卡片不触发配置发布或使配置预检失效 |
| `topology_previews` | 一次候选发布的预检快照 | 复用哈希、版本向量、过期时间 | UI 默认隐藏步骤，但后端必须执行 |
| `topology_releases/release_nodes` | 一次操作／逐服务器发布任务 | 复用状态机，增加操作关联与成功晋升 | 用户看易懂的部署进度，不把“已接收”叫成功 |
| `topology_deployments` | Agent 当前期望配置 | 保留现有语义 | 另存回执确认快照，不冒充实际已生效 |
| `node_group_members` | 旧服务器范围订阅授权 | 首期明确保留“该服务器所有订阅入口” | 后续增量迁为代理粒度，不能一次性改名冒充已完成 |
| `user_credentials.inbound_id`、流量 `inbound_id` | 代理级用户凭据／流量归属 | 保留原 ID／FK | 不给画布草稿复制用户密钥，不改历史计量口径 |
| `subscription_node_overrides`／`subscription_inbound_overrides` | 服务器默认值＋代理专属覆写 | 原优先级保持，增加新 DTO 映射 | 端口／协议改动不能绕过授权或把密钥当覆写下发 |

## 4. 强交互：从服务器到可用代理的线性链闭环

### 4.1 页面信息架构

主导航：**总览｜服务器节点｜代理节点｜拓扑编排｜用户与授权｜订阅与模板｜流量与通知｜设置**。管理员与普通用户仍按现有角色控制；普通用户不看服务器资产和链编辑器。

```text
应用侧栏    顶栏：拓扑编排 / 当前代理链     主题  帮助  账户
            ┌链列表 260px──────┬ChainEditor（横向）──────────────┬配置抽屉 560–680px┐
            │HK 主链  3 跳       │[入口 HK] → [中转 SG] → [出站直出]│基础｜协议｜安全   │
            │SG 直出  1 跳       │每跳服务器／入站／状态             │字段、校验和预检   │
            │状态／dirty 摘要    │追加｜删除｜左移｜右移             │保存草稿 创建并应用│
            └──────────────────┴────────────────────────────────┴───────────────────┘
            发布进度：正在准备 → 下游应用 → 入口应用 → 已生效
```

`/prototype/proxies` 是一链一行的代理节点列表，显示名称、入口→中转数量、terminal、状态和 `dirty`。`/prototype/topology` 是列表＋选中链的 `ChainEditor`，不是把多条链放在同一张总图。编辑器横向呈现 `[入口] → [中转] … → [出站]`：第一张卡是订阅入口，中间卡是零个或多个入站中转，最右卡是独立 terminal `egress`，不属于 `ChainHop`。

### 4.2 首个 Reality 直出故事：文字流程图

1. **进入链编辑器**：读取服务器资产、已有 `InboundResource`、代理链草稿和已发布状态。空态显示“创建一条代理链：入口 → 中转（可选）→ 出站”，提供“新建代理节点”按钮。
2. **创建 `ProxyDraft`**：填写节点名称，生成第一跳 `position:0`。从左侧资产面板拖入 `HK-zouter` 时，必须先选定该 hop；拖动 payload 只填 `serverId`，不会生成自由卡片或另一条链。
3. **配置第一跳入口**：在该 hop 选择“新建入口”或选择 HK 已有 `InboundResource`。新建时填写 Reality、监听端口、SNI、握手目标和材料就绪状态；复用时只写 `inboundId`，不复制端口或材料。
4. **完成单机 terminal**：编辑器始终显示唯一 terminal 卡，默认 `egress.type="direct"`，摘要为“本机直出（HK-zouter）”。外部 terminal 在 R1 只显示 disabled／规划原因，不生成 endpoint 或连接信息。
5. **追加中转（可选）**：点击“追加中转”，在 terminal 前插入新的 `ChainHop`；为该 hop 选择服务器，并复用已有入站或填写 `newInboundDraft`。卡片顺序即链路顺序，不通过拖线建立关系。
6. **实时检查**：前端检查名称、连续 `position`、每跳二选一资源来源、第一跳订阅入口、端口和 terminal；后端预检协议能力、草稿版本、端口占用、证书引用、链容量和权限。
7. **保存或创建并应用**：用户可只保存草稿，也可在单一确认点应用整条链。后端保存资源与链候选，返回 `202 operationId/releaseId`；UI 显示“已受理／等待 Agent”，不显示已生效。
8. **自动应用**：full 按受影响服务器聚合整条链的资源和顺序，Agent 沿既有 prepare/apply/ACK 通道执行；R1 原型只模拟，不接后端真实 API。应用顺序和回执状态由后续 R2 验收。
9. **确认成功**：R1 原型用合成的匹配 ACK 和发布终态演示状态推进，不能当作真实 Agent 回执；未来 R2 仅在真实 ACK 成功后晋升 `published` 与订阅可见版本。R1 的“已生效”只是模拟状态，不等于公网客户端连通测试已通过。
10. **失败分支**：显示失败 hop／阶段和可操作原因；保留链草稿、每跳输入和旧 `published`。未保存的临时链可取消；已保存草稿需明确删除，不级联删除其 `InboundResource`。

普通路径不要求用户理解物理根节点、自由 graph 或手动维护 edge。高级区可查看脱敏链差异、保存不应用和发布详情，但不能用界面简化绕过资源校验、权限或发布确认。

### 4.3 链跳编辑与多代理交互

- 每条 `ProxyDraft` 只编辑一条链；多代理通过 `/prototype/proxies` 的多行列表选择，不能在一张画布中互相连线。
- 每个 `ChainHop` 都提供服务器选择、已有 `InboundResource` 复用和“在此跳新建入口”；`inboundId` 与 `newInboundDraft` 必须二选一。服务器资产拖动仅填当前指定 hop。
- “追加中转”在 terminal 左侧插入 hop；“删除跳”删除对应 hop 并连续重排 `position`，至少保留一个 hop；“左移／右移”只调整 hop 顺序，不能移动或复制 terminal。
- 替换某跳的服务器或入口只修改该跳；其他链保持不变。若资源被多条链复用，编辑资源会列出所有引用链和 hop 位，并将其标记 `dirty`。
- 任何链都只有一个 terminal：`direct` 表示最后一跳所在服务器本机直出；`external` 只作 R1 未就绪预留。terminal 不是 inbound hop，不能被追加、复用或连接。
- 编辑器的箭头只是相邻卡片的顺序提示。禁止自由拖线、分叉、合并、回边、跨链连接或把一条链的 terminal 接到另一条链；跨链仅通过复用 `InboundResource`。
- 未完成 hop、第一跳非订阅入口、端口冲突或外部 terminal 未就绪时不能应用。目标离线不自动删除 hop、不回落为 direct，保留草稿并说明原因。
- 应用整条链时汇总所有 hop 所在服务器及共享资源影响；服务器级编译仍须保留其他已确认链／入站，不能因保存一条链而清空同机配置。
- “从列表隐藏”是布局／展示操作；“停用代理”是需要发布的新配置；“删除代理”删除一条链但保留资源和历史，三者不能混用。

### 4.4 编辑、撤销与异常的规则

| 场景 | 用户看到的结果 | 后端要求 |
| --- | --- | --- |
| Agent 离线／能力未知 | 可保存链草稿；“应用”提示原因 | 第一期不自动排队到未来上线，恢复后重新预检并确认 |
| 端口冲突 | 定位到具体 hop 的入口端口及占用资源 | 服务器级事务串行分配；数据库预检不替代 Agent 的实际 bind 检查 |
| 409 版本冲突 | 保留本地链和每跳表单，展示差异／重新载入 | 禁止最后写入覆盖；使用 draft、chain、server 配置版本向量 |
| HTTP 超时／重复点击 | 查询同一次 operation，不生成重复链或资源 | 相同幂等键及同一请求体返回同一操作，键被不同请求体复用则 409 |
| prepare 失败 | “校验失败，未应用”，定位到影响 hop | 保留旧期望／确认版本，不晋升订阅 |
| 部分服务器 apply 失败 | 显示成功、失败、回滚中各服务器和链状态 | 复用补偿回滚；不称跨机器数据库式原子事务；回滚失败为 `manual_required` |
| 撤销 hop 调整／未应用字段 | 恢复本地链草稿 | 可做本地 undo/redo；不能撤销已经发布的 runtime |
| 回退已发布变更 | “回滚到上次版本”并再次确认 | 新发布动作，校验链资源、当前版本和权限；不能直接改数据库指针 |
| 浏览器关闭／刷新 | 已保存链草稿可恢复，未保存链有离开提醒 | 草稿落后端；浏览器持久化仅存布局／偏好，不存私钥和配置秘密 |

### 4.5 配置表单：每跳复用或新建入口

| 分区／组件（拟新增） | 必填与主要动作 | 当前 sing-ui 对齐／不能照搬部分 |
| --- | --- | --- |
| `ChainEditor` | 链名称、横向 hop 卡片、terminal、追加／删除／左移／右移 | 新组件替代旧自由 `TopologyWorkspace`；相邻箭头只表示顺序，不持久化自由边 |
| `ChainHopEditor` | 当前 `position`、服务器、已有 `InboundResource` 或 `newInboundDraft` | 每跳独立配置；`inboundId` 与 `newInboundDraft` 二选一，资源可跨链复用 |
| `ProxyBasicFields` | 节点名称、第一跳订阅用途和链状态 | 节点名称属于 `ProxyDraft`；协议、端口、材料属于 hop 的 `InboundResource` |
| `InboundProtocolTabs/PortAvailabilityField` | 当前 hop 的协议 tabs、基础／协议／安全／高级分区；端口冲突定位 | 借鉴 3x-ui 的分区组织；按能力禁用／解释，端口需服务端预检和 Agent bind 检查 |
| `RealityInboundFields` | SNI、dest／握手目标 host:port、显式互填快捷动作；生成 X25519／Short ID | 对齐 `sni/privateKey/shortId/target`；不自动扫描目标；私钥不回显 |
| `ShadowsocksInboundFields` | 能力支持的 method、按算法长度生成密码、TCP/UDP 和认证／容量提示 | 当前 full renderer 只支持 SS2022；lite 运行时／方法单独验证，不能开放未经验证旧 SS 或共享密码冒充多用户 |
| `Hysteria2InboundFields` | UDP 端口、SNI、证书引用、带宽参数、可选 obfs | UI 选 Agent 已登记证书的 `certificateRef`；禁止任意面板路径；第一期不承诺自动 ACME 签发 |
| `TerminalEditor` | `direct` 本机直出；external 预留及未就绪原因 | terminal 独立于 `ChainHop`；R1 不生成外部 endpoint、凭据或可部署状态 |
| `AdvancedInboundFields` | 当前 hop 监听地址、公布地址／端口、受控材料引用 | 默认折叠；不暴露未支持的 Xray transport、masquerade、内核任意 JSON |
| `SecretGenerateButton` | 生成／轮换确认，失败不清空原值 | 生成新材料不等于立即轮换生效；返回临时 `secretRef`，预览、日志、草稿导出不含秘密 |
| `DeploymentSummary` | 整条链的生效版本、待应用差异、影响服务器／hop、可重试原因 | 可读错误和字段定位；不展示堆栈、凭证或用户连接串 |

`shared/config/renderer.go:539` 起的 Reality／SS／Hy2 编译是字段适配依据。参考 3x-ui 的 Reality 表单有生成密钥和 Short ID 按钮，但其 `streamSettings.realitySettings`、Hy2 `streamSettings.hysteriaSettings` 不是 sing-box schema，**复用操作组织，不复制 JSON 契约**。

## 5. 前端视觉与具体改造清单

### 5.1 为什么现在“像骨架”

`frontend/src/App.tsx:64` 的 `ConfigProvider` 只设置语言；`AppLayout.tsx:37` 是固定深色顶栏＋固定浅色侧栏；`index.css:18` 仍有 18px 全局基础字号、系统深浅色 CSS 和演示式标题规则。它们不是一套统一的 AntD／React Flow 主题。当前配置页偏“字段堆叠”，画布卡片突出的是 role 而不是协议、机器归属和生效状态。

这里是源码层面的结构判断，没有通过运行页面给出颜色对比度或美观验收结论。`App.css` 虽有脚手架样式，但不能在未核对入口引用的情况下把未使用样式说成当前页面缺陷；下一阶段只清理确认生效／废弃的规则。

### 5.2 3x-ui 的可取之处及真实布局

- **主题**：`3x-ui: frontend/src/hooks/useTheme.tsx:35` 提供深色、极深色背景 tokens；`:119` 的 `buildAntdThemeConfig` 组合 AntD algorithm、Layout/Menu/Card 等组件 tokens；偏好通过 localStorage 保留。借鉴统一 token 和模式切换，不在每个页面堆硬编码颜色。
- **侧栏／响应式**：`frontend/src/layouts/AppSidebar.tsx:174`、`:361`、`:418` 有桌面 Sider、移动 Drawer、导航状态和固定侧栏交互。
- **页面结构**：`frontend/src/styles/page-shell.css:1` 汇集页面头部、内容、摘要卡的样式。`PanelLayout.tsx:6` 实际只是 Outlet＋标题／WebSocket bridge，**不是一个拿来即可用的“侧栏＋顶部栏布局组件”**。sing-ui 应自行实现统一 `AppShell`，吸收侧栏和页面标题组织，顶部操作栏是自己的明确设计。
- **表格／移动端**：`frontend/src/pages/inbounds/list/InboundList.tsx:252`、`:332` 的列表、操作和移动卡片切换；`pages/nodes/NodeList.tsx:237` 的状态、统计和节点动作提供资产页参考。
- **表单**：`frontend/src/pages/inbounds/form/InboundFormModal.tsx:1086` 使用约 780px Modal、分组 Tabs、提交 loading、关闭处理；协议／传输／安全字段拆开。sing-ui 画布用 Drawer 保留空间上下文，普通列表用 Modal，内部共享同一个 `ProxyConfigForm`，不把整个弹窗原样塞进画布。

### 5.3 推荐视觉参数（设计目标，不是假称已经验收）

- 默认跟随系统，保留浅色／深色手动选择；第一版不增加“极深色”第三种状态。以 3x-ui 石墨深灰＋蓝色操作为主，不引入大面积渐变、发光边框或大 hero。
- 深色候选 token：布局 `#1a1b1f`、侧栏／顶栏 `#15161a`、容器 `#23252b`、浮层 `#2d2f37`，对应参考 `useTheme.tsx`。浅色使用 AntD 默认底色，页面底 `#f5f6f8`，操作蓝候选 `#0958d9`，引用参考的浅色按钮处理。
- 版式：应用侧栏约 208–224px、顶栏约 56px；内容间距 16／24px，正文 14px、页面标题约 22px；统一 8px 圆角、表单 label 和提示间距。画布是工作区，尽量占满剩余高度。
- 状态采用文字＋图标＋颜色：待配置／草稿（中性）、部署中（蓝）、已生效（绿）、离线（灰）、失败（红）、需人工处理（警告）。色彩不能是唯一信号。
- 卡片主标题“代理名称”，副标题“所属服务器”；协议 badge、入口地址、出站摘要、实际部署状态依次排列。机器 CPU 和内存放资产面板／详情，不挤进每张代理卡。
- 小屏侧栏改 Drawer，资产面板可收起，配置全屏；提供代理列表编辑路径，不要求手机精确拖线。适配键盘、焦点返回、ESC 关闭、右键菜单定位和 `prefers-reduced-motion`。

### 5.4 页面／组件到参考源码的执行映射

| sing-ui 目标位置（拟新增／重写） | 具体参考 | 采用方式与验收点 |
| --- | --- | --- |
| 重写 `components/AppLayout.tsx`，拆 `AppShell/Sidebar/Topbar/PageHeader` | 3x-ui `layouts/AppSidebar.tsx`、`styles/page-shell.css` | 参考导航分区、折叠、移动 Drawer；保留 sing-ui auth/router，不搬远程面板选择逻辑 |
| 新增 `theme/ThemeProvider.tsx`、`theme/tokens.ts`，调整 `App.tsx/index.css` | `hooks/useTheme.tsx` | 一次根级 ConfigProvider；AntD 与 React Flow 共享色板；浅深色列表／弹窗／画布均一致 |
| `pages/Servers.tsx`、`ServerDetailDrawer.tsx` 替换 `Nodes.tsx` 的服务器部分 | `pages/nodes/NodeList.tsx`、`NodeFormModal.tsx` | 状态统计、筛选、分页、详情分区；不复制 API token／mTLS 入参当作 sing-ui 注册契约 |
| 新增 `pages/Proxies.tsx`／`ExternalProxies.tsx` | `pages/inbounds/list/InboundList.tsx`、`CloneInboundModal.tsx` | 服务器／协议／状态筛选，紧凑表格、移动卡片；复制配置必须重新分配端口和秘密引用 |
| 重写 `pages/TopologyGraph.tsx`，新增 `features/topology/TopologyWorkspace.tsx` | 侧栏／页面壳组织＋现有 React Flow | 自己实现服务器拖入、临时卡、双击、右键、配置和操作进度；不声称 3x-ui 提供现成拓扑画布 |
| `ServerAssetPanel`、`ServerAssetItem`、`ProxyNodeCard`、`ProxyContextMenu` | `pages/nodes/NodeList.tsx` 的状态呈现 | 左侧一级是服务器；同机可生成两个不同代理；无须跳页 |
| `ProxyConfigDrawer/ProxyConfigModal/ProxyConfigForm` | `pages/inbounds/form/InboundFormModal.tsx` | 基础／协议／安全／高级分区和错误页签定位；只配入口，出口只读；两个容器共用验证与提交 |
| `RealityInboundFields/SecretGenerateButton` | `pages/inbounds/form/security/reality.tsx:45`、`:209`、`:249` | SNI／握手目标、生成按钮、字段帮助；不复制参考私钥明文 textarea 的展示策略 |
| `ShadowsocksInboundFields/Hysteria2InboundFields` | `pages/inbounds/form/protocols/shadowsocks.tsx`、`hysteria.tsx`、`security/tls.tsx` | 只显示 sing-box 当前支持字段，能力不足时解释而不是提交后才失败 |
| `DeploymentProgressDrawer/ChangeSummary/DependencyPicker` | 入站表单和节点状态卡的层级，发布逻辑自行实现 | 逐服务器阶段、可重试错误、回滚结果；不把参考 runtime 状态当 sing-ui 发布状态 |
| `hooks/useServers/useProxies/useOperation` | `api/queries/useNodesQuery.ts:24` 和 query key 组织 | 可在下一阶段引入 React Query 统一请求状态；Zustand 只存画布草稿，不复制服务器状态到双缓存 |
| 总览、订阅、模板、流量页的共同表格／空态／筛选 | `styles/page-shell.css`、入站／节点列表公共模式 | 第一批统一页面头部和间距；业务逻辑留用，ECharts 不为“同风格”强行换 uPlot |

### 5.5 不是“一整个 frontend 文件夹复制过来”

双方本地 `frontend/package.json` 的 React 19／AntD 6／Vite 8 主栈接近，但 sing-ui 目前没有 React Query、i18next、react-hook-form、Zod，而参考项目依赖这些及 generated API／Xray schemas，router 主版本也不同。

推荐：**学习布局与交互、按 sing-ui 契约重新实现组件；不把 3x-ui 当组件库安装。** 首期用既有 AntD Form、React Router、Zustand；若引入 React Query，限定在请求缓存层，单独评审依赖与迁移范围。无需同时迁入 RHF、i18next、uPlot、全部 schema 和参考构建生成链。

本地 `3x-ui: LICENSE:1` 标明 GPLv3。若下一阶段选择实质复制／改编代码或整体 fork，应先完成项目分发方式及许可证兼容性评审，保留必要来源和声明；这里仅记录源码中的许可证事实，不替代法律意见。本轮没有复制应用代码、图片或品牌素材，也没有作出“同栈即可无条件搬用”的判断。

## 6. 技术方案：以代理为操作单位，以服务器为部署单位

### 6.1 新增应用服务，不让 UI 串联一堆不可恢复的请求

拟新增 `backend/internal/orchestration/ProxyChangeService`，由 `backend/internal/api/admin/proxies.go` 调用；新增 `servers.go` 仅负责服务器 DTO／资产操作。拓扑校验继续复用 `backend/internal/topology`，候选渲染复用 `shared/config`，发布控制复用 `backend/internal/repo/topology_release.go` 与 Agent deployment 契约。

```text
UI 结构化代理配置＋期望版本＋幂等键
  → 权限／字段／服务器能力校验
  → 保存变更草稿（入站候选、出站、位置同一事务／关联操作）
  → 解析代理依赖和全部受影响服务器
  → 按服务器聚合：已生效代理＋本次选中草稿覆盖
  → 校验整图、端口、凭据材料版本，生成候选 bundle
  → 创建 operation/release，返回 202
  → Agent prepare → 下游 apply → 入口 apply → 回执确认
  → 成功晋升已确认快照＋代理发布版本＋订阅可见版本
  → 失败保留草稿／补偿回滚／必要时人工处理
```

不能先 `POST inbound`、再由浏览器 `PUT edge`、最后碰运气点 deploy；中途失败会留下不一致。幂等键和操作记录必须在后端。一次操作只能应用它的变更集合，不夹带其他管理员尚未确认的草稿。

### 6.2 三种状态必须分开

1. **草稿／意图**：`proxy_node_drafts` 的候选配置和 `topology_drafts` 的边，可未完成；不进入用户订阅，不进入旧 Agent 的自动 fallback。
2. **期望 runtime**：现有 `topology_deployments` 和 release candidate，表示正在让 Agent 应用的内容；可能尚未成功。
3. **已确认发布**：新增 `confirmed_server_deployments` 与 `proxy_node_publications`，由匹配回执和发布终态推进。正常编辑在新版本确认前仍使用旧发布；新服务没有发布则不进入订阅。

`inbounds` 留作稳定身份＋已生效字段的兼容投影。新服务保存草稿时可分配身份行并标记 `draft`；旧服务编辑写 `proxy_node_drafts`，不直接覆盖已生效字段。成功后一次事务更新投影和 publication，清除／标记已消费草稿。不能仅删除 `UpdateInbound` 的“已引用不可编辑”检查后裸写原记录。

发布中跨机状态会短暂不一致。应用失败且回滚未确认时，把受影响代理标记 `manual_required` 并从新生成订阅中隔离；既有客户端持有的配置无法靠隐藏订阅瞬间撤回。方案不承诺无中断热切换；改端口／轮换认证材料须明确影响，后续可设计新旧并行窗口。

### 6.3 运行与安全约束

- 用户身份、组授权、模板覆写白名单继续由后端判断；客户端提供 `serverNodeId/targetProxyNodeId` 不构成授权。
- 新订阅入口在候选渲染前，为当前授权用户准备该代理的独立凭据并纳入材料版本校验；否则会出现“订阅未发布所以凭据未生成、无凭据又无法预检”的闭环阻塞。候选凭据只用于本次准备／发布，不能提前把草稿暴露给用户；发布失败后的保留／清理须可重试、幂等。
- Agent 只可拉取／确认自身任务。继续校验 generation、runtime hash、phase、release；新表单协议能力必须通过可信心跳上报而不是管理员随意填勾选框。
- 证书和协议秘密使用受控引用；生成接口的临时引用绑定操作者／服务器／用途、限制有效期并在保存时消费。密钥不进图 GET、布局、客户端持久化、脱敏导出、错误和报告。
- 端口第一期可保守实行“同一服务器同一监听端口不复用（不分协议）”，即使 TCP／UDP 理论上可并存也先不支持共享；历史重复项不能通过迁移静默删掉。Agent 检查其他程序占用和 IPv4／IPv6 通配地址冲突。
- Hy2 证书是否已安装、端口是否 UDP 可用必须是能力检查的一部分。第一期密钥生成和证书选择都不执行任意 shell 输入。
- direct 只是路由意图，不承诺绑定特定公网出口地址；多网卡源地址策略是后续独立能力。
- 隔离候选与已发布之后，旧写 API 必须经同一服务或拒绝，旧 Agent 不得从 `ListInbounds` fallback 加载草稿。否则新 UI 再正确也守不住边界。

## 7. 数据迁移：保留 0010–0014，只追加 0015+

`backend/internal/db/db.go:34` 按 SQL 文件名记录迁移，并逐文件事务执行。**不编辑已应用的 0010–0014，不假设当前迁移器支持自动 down 或在线无锁大表转换。** 下列是下一阶段文件规划，不是本轮生成的 SQL。

### 7.1 现有迁移的保留范围

- `0010_control_plane.up.sql`：继续用草稿、预检、期望部署、材料版本；名字与任务书猜测的“nodes_inbounds”无关，以真实文件为准。
- `0011_p2_topology.up.sql`：继续用 egress、revision、graphRevision、release、逐服务器节点任务、Agent capability／generation。
- `0012_p2_templates_overrides.up.sql`：模板发布与覆写数据留用；新增代理 DTO 不改变覆写优先级和允许字段。
- `0013_p2_traffic.up.sql`：按服务器／入站 ID 保留流量归属，不因“入站改名代理”重算历史。
- `0014_p2_notifications_mail.up.sql`：邮件／通知流程不在本次重构范围。只在后续全站视觉统一时调整外壳。

### 7.2 建议增量文件与字段

| 迁移（拟新增） | 表／字段建议 | 约束、回填和目的 |
| --- | --- | --- |
| `0015_server_proxy_semantics.up.sql` | `nodes` 增 `labels JSONB`、`system_info JSONB`、`agent_version TEXT`、`observed_at TIMESTAMPTZ`；`inbounds` 增 `exposure TEXT`、`lifecycle TEXT`、`published_revision BIGINT NULL`、`advertised_address TEXT NULL`、`advertised_port INT NULL` | JSON 限定对象／白名单；地址空值继承服务器。exposure 按旧 role 回填；lifecycle 为 draft／active／disabled／archived／unverified，历史先按证据判定，不全设 active |
| `0016_proxy_drafts_publications.up.sql` | 新 `proxy_node_drafts(inbound_id PK/FK, revision, base_published_revision, payload JSONB, updated_by FK users, updated_at)`；新 `proxy_node_publications(inbound_id, revision, release_id NULL, client_snapshot JSONB, published_at)` | publication 复合主键 `(inbound_id,revision)`；inbounds 的 published 指针引用它。payload 含结构化字段及 secretRef，不含用户秘密；publication 的公布地址、协议、公用参数固化，用户凭据仍按原授权单独合成 |
| 同一 `0016` | 新 `confirmed_server_deployments(node_id PK/FK, release_id NULL, generation, runtime_version, routing_version, topology JSONB, confirmed_at)`；`topology_release_nodes` 补必要的前次确认快照引用 | 与期望部署分表；先上线读取隔离和兼容门禁，再开放新草稿写入口。已部署候选不能仅因表里有一行就回填确认 |
| `0017_canvas_layout.up.sql` | 新 `topology_canvas_layouts(canvas_id TEXT PK, revision BIGINT, document JSONB, updated_by, updated_at)`，第一版仅 `default` | document 只含卡片坐标、分组、视口／隐藏状态，不含配置秘密。合并旧 `layout.positions`，转为 `proxy:<inboundId>`，记录迁移结果；布局 revision 独立于配置 graphRevision |
| `0018_proxy_operations.up.sql` | 新 `proxy_operations(id UUID PK, actor_id, idempotency_key, request_hash, status, release_id NULL, result JSONB, error_code, created_at, updated_at)`；`topology_previews` 增 `operation_id` 关联并调整“每服务器只有一条预览”的限制 | UNIQUE `(actor_id,idempotency_key)`；result 保存生成 ID 映射／状态，不保存秘密。预览按 previewId 标识，不能被另一个窗口同服务器预览静默覆盖；有效期、清理和重复操作保留期明确配置 |
| `0019_proxy_group_members.up.sql`（第二批） | 新 `proxy_group_members(group_id, inbound_id)`，复合 PK/FK；授权组增 `membership_mode`，值为 `server_legacy` 或 `proxy_explicit` | 仅将旧授权服务器中的 subscription 代理回填为显式成员；外部代理授权继续单独映射。切换时组成员、凭据生成、订阅、运行授权同批修改；不能仅改前端选择器 |

`proxy_node_publications.client_snapshot` 不等于整个 sing-box 配置；只存生成客户端所需的稳定公开材料和受控服务秘密引用。回滚／重新发布也创建新版本记录，保留旧记录，不反复覆盖历史。实际秘密的受控存储／加密沿项目安全方案实施；没有完备存储前不得开放生成接口。

草稿 payload 与 `topology_drafts.edges` 不得各自存可冲突的独立目标：**边是唯一出站目标权威**，payload 存入口字段与用途；direct／chain 和 target 从同一 graph revision 派生并在同事务维护兼容投影。零出边＝direct，一条合法出边＝chain；客户端冗余 egress 与边冲突时拒绝，不静默覆盖。目标服务器由入站 FK 推导。

### 7.3 有序升级、回填、回滚

1. 在隔离副本上预检：受管／外部计数、入站 ID、role／egress 组合、孤儿引用、重复端口、草稿与 legacy edges 差异、发布是否在途。仅记录脱敏差异，不读生产秘密做演示。
2. 追加 schema，旧表及 ID 保留；资产地址先用 DTO 别名，暂不进行物理表 rename 和 FK 大搬家。
3. 从 release 的成功回执与匹配 generation／runtime version 建立确认基线；不能证明一致的服务标 `unverified`。切换前要求管理员逐项重检／确认发布，不能把旧 deployment 时间戳当 Agent 回执。
4. 新订阅读取先影子对比旧结果：代理数量、稳定 ID、展示名、公开 endpoint、权限集合、格式语义。对历史未显式部署但仍在订阅中的服务列出差异，必须确认再切换，避免静默下线。
5. 上线 candidate／published 隔离、Agent fallback 门禁、旧 API 写入适配；完成后才允许保存新代理草稿。特性开关只切 UI 不足以保障安全。
6. 验证布局独立迁移、相同订阅 token／模板／覆写仍可用；保留旧路由重定向、旧入站 ID 兼容。再灰度开启新画布。
7. 失败时关闭新写入口、停止新发布并等待／处理在途操作；使用兼容版本回切 UI。不能把旧二进制直接接到存在 draft 行的新库；数据回滚依赖预先验证的备份恢复或后续补偿迁移，不编造现有自动 down 能力。

大表的索引／约束先做冲突检查，再在合适窗口落地；不能将 `CREATE INDEX CONCURRENTLY` 直接放进当前逐文件事务而假设能运行。本轮不执行迁移、备份、恢复或生产探测。

## 8. API 对齐：公开语言是服务器和代理

### 8.1 路由规划

除用户订阅接口外，下表均使用现有 admin／owner 鉴权链，字段和权限在服务端校验。Agent 端点使用现有 Agent 身份认证，不与管理员会话混用。

| 方法／路径（拟新增） | 语义／请求重点 | 响应／与旧接口关系 |
| --- | --- | --- |
| `GET/POST /api/servers` | 仅受管服务器的分页资产与注册入口 | 复用 `/api/nodes` 仓储；不返回原始凭证，注册秘密仅受控一次性返回 |
| `GET/PATCH/DELETE /api/servers/{serverId}` | 详情／资产更新／退役检查 | 有代理、发布或计量引用时不级联硬删，返回依赖摘要 |
| `GET /api/servers/{serverId}/capabilities` | 协议、Agent／内核版本、证书可用引用 | capability 的 unknown 明确返回，不伪装 ready |
| `GET /api/servers/{serverId}/proxies` | 所属受管代理及 draft／published 状态 | 替代公开的 `/api/nodes/{id}/inbounds` 列表语言 |
| `POST /api/servers/{serverId}/proxies` | 一步创建入口；`clientRef,inbound,exposure,position,intent,expectedVersions`，无连线默认 direct | `save_draft` 返回 201；`apply` 返回 202；支持幂等。chain 由画布变更集连接命令表达，不在表单独立提交目标 |
| `GET /api/proxies`、`GET /api/proxies/{proxyId}` | 过滤服务器／协议／状态，区分已发布摘要与草稿 | 受管 proxyId 仍为 inboundId；常规 GET 不回显私钥 |
| `PATCH /api/proxies/{proxyId}/draft` | 保存候选，带 expectedDraftRevision／basePublishedRevision | 仅草稿，不影响 Agent 或订阅；同机／同图并发冲突 409 |
| `POST /api/proxies/{proxyId}/apply` | 应用指定 draftRevision；确认影响范围 | 创建新的 operation/release，不能发布“最近那份未知草稿” |
| `POST /api/proxies/{proxyId}/disable` | 发布移除运行配置的变更 | 202；依赖不满足 409，成功后再更新订阅可见性 |
| `DELETE /api/proxies/{proxyId}` | 未应用草稿删除或已下线代理归档 | 运行服务不直接 DELETE；保留历史流量及凭据关联规则 |
| `POST /api/proxy-materials` | `serverNodeId,purpose` 生成 Reality／SS／内部认证材料 | 返回 `secretRef` 和适用的公开信息／有效期，限流、鉴权、无明文私钥回显 |
| `GET /api/topology/workspace` | 服务器分组、代理、草稿边、布局、图版本 | 面向画布聚合 DTO，兼容层可复用旧 graph reader |
| `PATCH /api/topology/layout` | `expectedLayoutRevision,positions,viewport` | 只改展示版本，不发配置、不更新 material revision |
| `POST /api/topology/changes/validate` | 多卡候选＋期望版本，临时 clientRef 可作链路引用 | 返回字段错误、依赖、脱敏差异及可选 previewId；不分配运行配置 |
| `POST /api/topology/changes/apply` | 批量新建／编辑／连线，指定候选集合与版本 | 一次 operation，处理临时到正式 ID 映射；与单代理入口共用同一 service |
| `GET /api/operations/{operationId}` | 幂等查询、重载恢复、逐服务器进度 | 先使用轮询和页面可见性暂停；未来 SSE 可选，不作为首版前提 |
| `GET /api/external-proxies` | 外部代理专用 DTO | 复用旧 external 逻辑，不误用 serverId 或受管 proxyId |
| `PUT /api/groups/{groupId}/proxies`（第二批） | 显式代理成员集＋组版本 | 与 0019 同批，不在第一批伪称已有代理级授权 |

保留 `GET /api/topology/releases/{id}`、`POST /api/topology/releases/{id}/rollback` 的底层状态与回滚能力；公开操作页使用更易懂的 operation 聚合。

旧 `/api/nodes`、`/api/nodes/{id}/inbounds`、`PUT /api/topology/graph`、`/api/topology/{nodeId}/preview|deploy` 在兼容期**统一进入新服务或明确返回升级错误**，不能留下直接写入 inbounds／旧 fallback 的旁路。读取兼容不意味着允许旧客户端绕过新的已发布语义。

Agent 的 `/api/agent/config`、`/api/agent/heartbeat`、`/api/agent/report-traffic`、`/api/agent/deployment-acks` 保留路径与 nodeId（服务器 ID），full/lite 按 NEXT 第 4 节协商格式、能力和简化遥测；受控 Agent register 是新增设计。一次应用仍覆盖该服务器完整受管配置，nodeId 不换成 proxyId。

### 8.2 单机创建示例（合成示意，不含真实连接信息）

```http
POST /api/servers/42/proxies
Idempotency-Key: <本次操作随机标识>
Content-Type: application/json
```

```json
{
  "clientRef": "draft:example",
  "name": "HK-zouter-Reality",
  "protocol": "vless-reality",
  "exposure": "subscription",
  "inbound": {
    "listenAddress": "::",
    "listenPort": 443,
    "sni": "example.invalid",
    "target": "example.invalid:443",
    "secretRef": "generated-material-reference"
  },
  "position": { "x": 240, "y": 160 },
  "intent": "apply",
  "expectedVersions": { "graphRevision": 12, "serverGeneration": 4 }
}
```

`example.invalid` 明确只是不可用于真实部署的占位；不是推荐握手域名。本节旧请求样例仅记录历史迁移输入，“无出边即 direct”及画布连线变更集不再是现行契约。当前代理节点以有序 `chain` 和唯一 `egress` 表达，真实 API／版本向量映射待 R2 重新评审；R1 不发起上述请求。

```json
{
  "operationId": "operation-reference",
  "proxyNodeId": 107,
  "releaseId": 28,
  "status": "preparing",
  "subscriptionVisible": false,
  "clientRefMap": { "draft:example": 107 }
}
```

返回的 202 只说明发布已受理。服务端 validation 失败返回 `422 fieldErrors[]`；版本冲突／端口占用／活动发布冲突按明确错误码返回 409；401／403 沿现有认证语义。多机变更若依赖摘要自用户确认后改变，必须重新确认，不静默扩大部署范围。

## 9. “fork 3x-ui”与“重构 sing-ui”的客观比较

### 9.1 按相同目标比较，而不是拿改皮肤对比重写控制面

| 维度 | 整体 fork 本地 3x-ui | sing-ui 底座＋新骨架（推荐） |
| --- | --- | --- |
| 成熟前端 | 优势明显，已有组件、表单、测试、主题和远端节点页 | 要真正重写管理端骨架，不能低估视觉和交互工作 |
| 两层节点／画布创建 | 有节点和入站概念，但仍需实现用户指定的资产拖入和统一创建流程 | 存储一对多已有；同样需实现新流程，但已有 React Flow 和图校验可用 |
| 多节点 | 已有实质远程 runtime／同步／状态管理，不是从零补节点表 | 已有 full Agent 和发布协议；面向 NAT 小内存的 lite 是 NEXT 新增设计，未实测交付 |
| 内核及协议 | Xray 配置模型与当前 sing-box 不同；保留 Xray是产品选择，切 sing-box 是迁移工程 | Reality／SS2022／Hy2 现有 renderer、Agent 契约可保留 |
| 跨服务器发布 | 要验证或补齐与目标同等的版本、依赖、确认、回滚保障 | 已有 P2 状态机可复用，但候选编辑／确认发布／订阅边界仍需修整 |
| 用户／订阅 | 已有相关能力，不能声称完全不存在；仍需适配 sing-ui 用户、组、模板、覆写、额度和格式语义 | 现有用户与订阅数据可留用；必须修好从实时入站读取的问题 |
| 数据迁移 | 迁移身份、入站、Agent 部署、用户凭据、订阅 token、权限、计量和模板 | 增量迁移，保留 ID／token／历史；主要难点是状态与授权边界 |
| 后续维护 | 要承担上游追踪、差异冲突、许可评审和可能双内核成本 | 保留自研维护成本；需要以原型评审约束“再次闭门造车” |

### 9.2 工作量区间（工程判断，不是报价／承诺）

假设一位熟悉项目的全栈工程师、已有本地测试能力、设计决策及时、首版只含当前三种协议；人日包括实现与定向回归，不含生产迁移窗口、完整授权改造、自动证书签发和新协议。基于静态代码估算，**置信度中低**，应在首个端到端纵切后重新估算。

| 工作包 | sing-ui 新骨架 | 整体 fork 3x-ui，保持同等 Agent＋sing-box 目标 |
| --- | --- | --- |
| 交互原型／主题／页面骨架 | 4–6 人日 | 3–5 人日（利用现成视觉，但仍需画布流程） |
| 两层 DTO、服务器资产、代理配置／画布闭环 | 8–12 人日 | 8–12 人日 |
| 候选隔离、发布回执、依赖更新、订阅晋升 | 8–12 人日 | 12–18 人日 |
| 现有业务整合／数据升级兼容 | 4–6 人日 | 10–16 人日 |
| 内核／Agent 架构适配 | 纳入现有模块增量 | 8–14 人日 |
| 集成／浏览器／迁移与异常回归 | 5–8 人日 | 7–11 人日 |
| **合计** | **29–44 人日** | **48–76 人日** |

若接受使用远端完整 3x-ui＋Xray、舍弃部分 sing-ui 兼容要求，fork 成本可能显著下降；但那是**更换运行和业务目标**，不能一边改变目标一边宣称比同等重构便宜。表内估算没有把“3x-ui 已支持 PostgreSQL／远程节点／Hy2”错误地算成从零开发项。

### 9.3 推荐及反转条件

现在选择 sing-ui 底座的理由：用户否定的是产品模型／交互／外观，而这三者必须重构，但不必同时弃掉 Agent、sing-box、用户订阅和跨节点发布积累。3x-ui 的最大直接价值是成熟前端范式及配置体验，不能为获取这些收益附带一次未经批准的内核／部署体系切换。

若用户明确接受“服务器运行 3x-ui/Xray，不必沿用现有 Agent／sing-box／兼容数据”，应重新评审整体 fork。那时的改造清单至少包括：锁定参考版本与许可证方案、服务器／代理 API 适配、画布操作服务、跨节点发布保障、用户组与订阅模板映射、旧 token／凭据／流量迁移、远端生命周期和升级策略，以及恢复演练。不能仅复制 frontend 后宣布 fork 改造完成。

## 10. 分期实施与用户评审门槛

| 阶段 | 范围与主要文件 | 可交付／退出标准 |
| --- | --- | --- |
| **R0：本次方向评审** | 本文与简报 | 用户确认两层模型、画布主路径、3x-ui 风格和技术底座；无业务代码改动 |
| **R1：可操作交互原型** | `AppLayout`、theme、`ServerAssetPanel/ProxyNodeCard/ProxyConfigForm`，合成数据 | 用户亲自完成单台 HK Reality 直出和同机再建 SS；确认信息结构／浅深色，再接真实后端；明确原型“模拟”，不显示真实部署成功 |
| **R2：最小真实纵切** | 0015–0018 必要 schema、`ProxyChangeService`、servers/proxies APIs、发布和订阅隔离 | 一台隔离 Agent 上从画布新建／编辑 Reality，匹配回执后进入订阅；失败仍保留旧版本；不借用生产服务验收 |
| **R3：协议与多机闭环** | SS2022／Hy2 表单适配、依赖解析、批量变更、发布 drawer | 同机多代理互不覆盖；二跳／三跳、下游修改影响上游、回滚／离线／冲突／重试通过 |
| **R4：迁移与全站统一** | 旧 API 兼容、布局迁移、服务器／代理列表，订阅／模板／流量外壳 | 旧 ID／订阅 token／覆写／计量不丢；界面统一；用户可从列表和画布进入同一配置器 |
| **R5：第二批授权改进** | 0019、组 API、用户凭据与订阅／runtime 授权 | 同一服务器不同代理可授给不同组；不把这一批偷偷塞进首个可用版本 |

R1 的交互评审是硬门槛，不允许再次用“后端已经写了很多”替代用户认可。业务功能通过纵切迭代，而不是先造完所有新表再给用户看静态截图。

### 10.1 下一阶段验收矩阵

- **概念／操作**：空白工作区无需跳页即可完成服务器拖拽→配置→生成代理；一个服务器创建两个代理，卡片身份、端口和发布状态互相独立。
- **配置正确性**：Reality 必填和密钥生成；SS 只列当前支持算法；Hy2 缺 UDP／证书时清晰失败；直出不生成额外落地；链路依据目标代理 ID 渲染。
- **交互可达性**：双击、右键、可见按钮、键盘均能进入同一表单；取消不残留资源；编辑错误保留输入；小屏可以通过列表操作。
- **状态真实性**：202 不显示成功、心跳在线不显示代理已生效；匹配 ACK 后才晋升；失败草稿不进入订阅；旧已发布服务不被新草稿覆盖。
- **并发／依赖**：重复请求不建两份；两个窗口冲突返回 409；移卡不使配置预检失效；修改下游列出上游影响；禁止环、同服务器重入和未完成下一跳。
- **异常／恢复**：离线、prepare 失败、apply 部分失败、ACK 超时、旧回执、回滚失败显示正确；不能把 `manual_required` 自动标成功。
- **边界**：普通用户无服务器／配置访问；管理员不能把他人未确认草稿夹带发布；旧接口和旧 Agent fallback 不绕过发布隔离；无秘密进入日志／导出。
- **兼容**：迁移前后受管 ID、用户凭据引用、订阅 token、模板／覆写、历史计量保持；外部代理不误作服务器；旧权限迁移有显式差异清单。
- **视觉**：桌面浅／深色、小屏表格／抽屉、主题切换、焦点和加载／空／错态截图评审；性能目标用原型测量，不把构建成功当作美观达标。

复用现有 `backend/internal/topology/p2_test.go`、`backend/internal/repo/topology_release_integration_test.go`、`backend/internal/acceptance/topology_boundary_p2_test.go`、`shared/config/renderer_test.go` 等测试落点。当前前端没有 test 脚本；下一阶段测试方案应单独批准添加测试能力，不能在本轮文档任务中安装框架或声称已有 UI 覆盖。

## 11. 本轮验证与限制

- 只交付 `docs/REARCHITECT.md` 与 `tmp/rearchitect-report.md`；验证日志位于忽略目录 `tmp/rearchitect-validation/`。没有实现上述新组件、端点、表或字段。
- 已委派并审计现有前端基线：`npm run lint` 退出 0，仍有既有 React／hooks 警告；`npm run build` 退出 0，存在 chunk 大小警告。未修复无关告警。
- 前端无已配置的 UI 单测命令，未做浏览器视觉验证；没有启动参考 UI 或生产服务。
- Go 不在 PATH，标准 `/usr/local/go/bin/go` 不存在；未安装／下载工具链，因此 Go 测试和构建未执行。这是验证缺口，不是通过结果。
- 仓库开始时已有未跟踪 `backend/tmp_genhash_main.go`，本轮未读取、修改、删除或纳入交付。
- **交付约束冲突公开记录**：任务书要求“不 commit/push”，本会话更高优先级工程执行规则要求验证后提交并推送 `origin/main`。按后者仅处理主方案文档；完成报告在已有 `.gitignore` 忽略的 `tmp/` 内保留。实际提交／推送结果写入报告，不能把“不提交”验收项标作满足。提交文档不代表批准实施本方案。

## 附录 A：可追溯参考索引

### A.1 sing-ui（仓库相对路径）

| 主题 | 主要入口 |
| --- | --- |
| 表和字段 | `backend/internal/db/migrations/0001_init_nodes.up.sql:2`、`0010_control_plane.up.sql:2`、`0011_p2_topology.up.sql:1`；0012／0013／0014 同目录 |
| 数据模型／节点 CRUD | `backend/internal/models/models.go:11`；`backend/internal/repo/nodes.go:227`；`backend/internal/repo/nodes_p2.go:45` |
| 图与角色限制 | `backend/internal/topology/graph.go:14`、`:183`、`:263`；`backend/internal/repo/topology_graph.go:59`、`:110` |
| 发布与确认边界 | `backend/internal/repo/topology_release.go:47`、`:246`、`:363`、`:476`；`agent/cmd/agent/deployment.go:1` |
| 订阅读取 | `backend/internal/api/sub/sub.go:117`；`backend/internal/repo/topology_release.go:147`（用户运行凭据装配） |
| API 路由与旧兼容入口 | `backend/internal/api/router.go:1`；`backend/internal/api/admin/topology.go:151`、`:195`、`:311`；`backend/internal/api/admin/nodes.go:223` |
| 前端骨架 | `frontend/src/App.tsx:64`、`frontend/src/components/AppLayout.tsx:37`、`frontend/src/index.css:18`、`frontend/src/pages/TopologyGraph.tsx:14`、`frontend/src/pages/Nodes.tsx:1` |

### A.2 3x-ui（相对 `/opt/data/workspace/tmp/ref-3xui/`）

| 主题 | 主要入口 |
| --- | --- |
| 主栈／构建依赖 | `frontend/package.json:1` |
| 主题与布局 | `frontend/src/hooks/useTheme.tsx:35`；`frontend/src/layouts/AppSidebar.tsx:174`；`frontend/src/layouts/PanelLayout.tsx:6`；`frontend/src/styles/page-shell.css:1` |
| 入站列表／配置 | `frontend/src/pages/inbounds/list/InboundList.tsx:252`；`frontend/src/pages/inbounds/form/InboundFormModal.tsx:217`、`:1086` |
| Reality／Hy2 | `frontend/src/pages/inbounds/form/security/reality.tsx:45`、`:209`；`frontend/src/pages/inbounds/form/protocols/hysteria.tsx:10` |
| 节点 UI／请求 | `frontend/src/pages/nodes/NodeList.tsx:237`；`frontend/src/pages/nodes/NodeFormModal.tsx:38`；`frontend/src/api/queries/useNodesQuery.ts:24` |
| clone 并非全部多节点能力 | `frontend/src/pages/inbounds/CloneInboundModal.tsx:37`；`frontend/src/lib/xray/inbound-clone.ts:5` |
| 远程节点／runtime／数据库 | `internal/web/controller/node.go:29`；`internal/web/runtime/manager.go:15`；`internal/web/runtime/remote.go:362`；`internal/database/model/model.go:775`；`internal/database/db.go:2189` |
| 心跳／同步 | `internal/web/job/node_heartbeat_job.go:31`；`internal/web/job/node_traffic_sync_job.go:81` |
| 许可证事实 | `LICENSE:1` |

### A.3 Remnawave：借鉴分离职责，不照搬多层术语

官方资料于 2026-09-08 通过公开 HTTPS 正文核查（没有访问本机实例）。`Config Profiles` 讲完整 Xray 配置模板及其入站；`Nodes` 讲负责实际代理流量的节点；`Hosts` 将订阅地址映射到入站；`Squads` 把用户可用入站与运行节点配置分开。由此借鉴：**机器承载、服务配置、客户端公布地址、用户授权是不同职责**。不能把一个 Profile 直接等同于一台服务器或一个 sing-ui 代理。

核查页面定位（为复核保留原始地址，仅作资料记录）：

```text
https://docs.rw/learn-en/config-profiles
https://docs.rw/learn-en/nodes
https://docs.rw/learn-en/hosts
https://docs.rw/learn-en/squads
```

sing-ui 首版不要求用户先创建 Profile、Host、Squad 才能画图：把单入站配置内嵌到代理编辑器；公布地址作为高级字段；运行配置按服务器自动聚合；授权先明确旧范围，再分批升级代理粒度。未来多服务器模板复用可另设 `ProxyTemplate`，但模板不是第三种节点，不能打乱此次刚理顺的两层模型。
