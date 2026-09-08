# sing-ui R0-EXT：完整产品闭环修订方案

> 日期：2026-09-08。状态：**产品方案修订稿，未批准实施**。本轮只改文档；下列页面、API、表、迁移及验收均为设计目标，不能当作已经实现。
>
> 已完整阅读扩展任务书与 `docs/REARCHITECT.md` 全文。sing-ui 调研基线 `e1e4c6b`；SubBoost 本地 `4a69b49`（package 版本 `2.8.1`）；3x-ui 本地 `2ec6c73`。参考项目只读，不运行、不搬代码或资产。Remnawave 以 2026-09-08 实际取得的官方文档正文为依据，出处见第 12 节。

## 0. 修订决策与原方案的关系

> **2026-09-08 R0-NEXT：`docs/REARCHITECT-NEXT.md`。** 产品名统一 sing-ui；完整 Agent 保留并新增无 Docker 的 SS 先行轻量版；入口通过表单创建，出口只由画布连接关系产生，DDNS 后置 R4+。冲突按 NEXT → 本文 → R0；本文模板、内外组、用户生命周期和客户十个方案的完整闭环继续有效。NEXT 第 3／4／7／8 节补充数据、API、产品准入与分期；第 13 节仍仅记录旧轮次验证。

**保留两层节点与画布，补全“管理员配置资源和产品 → 分配给用户 → 基础订阅 → 客户定制 → 客户端使用”的业务链。覆写不是管理员另一个节点配置页，而是客户自己的订阅工作台。**

| 原 R0 内容 | 本次处理 |
| --- | --- |
| 第 3／4／5 节两层模型、画布拖拽、同机多代理、双击／右键／抽屉、3x-ui 视觉 | 全部保留；组、模板、客户方案不是第三种节点，不要求先建完这些对象才能画图 |
| 第 6 节发布编排及已确认快照，第 7 节 0015–0018 规划 | 保留；新订阅、客户覆写均从已确认发布读取，不绕过 Agent ACK 边界 |
| 第 1 节排除完整模板平台和 Xray 适配器 | 订阅模板平台进入完整产品范围；新增 **Xray 客户端输出适配器**，不新增 Xray 服务端／Agent 运行内核 |
| 第 7／8 节拟议 0019 单层代理授权、`/api/groups/{id}/proxies` | 由本文第 3／6／7 节内外组与统一授权服务替代；原 0019 尚未实施，不再并建另一套 `proxy_group_members` 权威表 |
| 第 5.4 节订阅／模板“业务逻辑留用、只统一外壳” | 改为复用 P2 校验／生成基础，实质补充模板导入、分组、用户管理及客户方案工作流 |
| 第 9.2 节 29–44 人日、第 10 节 R4／R5 | 仅是原骨架范围估算与旧分期，不是扩展产品总工期；由第 10 节依赖门槛重新分期，纵切后重估，不叠加一个无证据的总报价 |
| 第 11 节历史验证／报告 | 保留为原 R0 记录；本轮结果见第 13 节及 `tmp/rearchitect-ext-report.md` |

### 0.1 三个容易混淆的概念，先校正再借鉴

1. **Remnawave Config Profiles ≠ 订阅 Templates。** 前者是节点运行的完整 Xray 服务端配置及入站；后者决定客户端收到的 Mihomo、Sing-box、Xray-json 等内容。其官方 Base64 格式不提供同样的完整配置模板。sing-ui 把“服务器运行编译”和“客户端输出模板”分开，不把客户端 YAML 下发 Agent。[RW-P][RW-T]
2. **Remnawave Internal Squad 是入站访问授权，External Squad 是模板／订阅设置覆写。** 官方说明用户可加入多个内部组；不是“内部组就是机器文件夹、外部组原生就是节点套餐”。sing-ui 按用户要求扩展成“内部资源／可授权代理池 → 外部客户分组绑定代理和模板 → 用户可选多个外部组”，这是本项目设计，不宣称与上游数据模型一比一。[RW-S][RW-U]
3. **SubBoost 模板描述生成策略，节点来源独立。** 本地实现确有服务端保存模板／订阅及持久 URL，不只是浏览器下载 YAML；但未发现“每客户总共十个方案”的对应限制。十个名额是 sing-ui 自己的产品约束，不是照抄其导入源限额。[SB-T][SB-P]

### 0.2 用户七条想法逐条落位

| 用户要求 | 产品落点／本文章节 |
| --- | --- |
| 服务器安装 Agent | 保留 R0 服务器资产、一次性注册及能力状态；不增加 SSH 自动运维 |
| 拖服务器、创建入口、连线定出口 | R0 画布闭环保留；NEXT 澄清入口表单与画布连线职责，出口不单独创建 |
| 各客户端订阅模板 | 第 2 节模板版本、导入／导出、发布与格式能力 |
| 代理节点＋模板、内外分组 | 第 3 节资源池、显式代理绑定和外部组产品授权 |
| 用户分组、到期、流量、重置 | 第 4 节列表、CRUD、批量、重置调度与运行授权 |
| 客户导入模板／手动覆写、最多十个、自有新链接 | 第 5 节客户工作台与方案生命周期，第 6／7／9 节约束 |
| Mihomo／Xray 软件使用 | 第 2.2 节区分订阅格式与内核配置，第 11 节真实客户端验收 |

## 1. 完整产品故事与职责边界

```text
管理员
  服务器安装 full 或 lite Agent → 同一服务器资产卡（能力／NAT 标签）
  拖服务器进画布 → 创建入口（无边直出）→ 需要链式时连到另一入口卡 → 应用 → 匹配 ACK
  建内部资源组 → 选可分配的代理
  建/发布客户端订阅模板
  建外部分组（显式代理集合＋允许模板＋默认模板＋可覆写策略）
  建用户 → 分配一个或多个外部分组 → 到期/流量/重置策略 → 授权应用确认
  生成并交付基础订阅
客户
  登录客户区 → 我的基础订阅 → 自定义
  继承官方模板 或 导入自己的模板 → 节点/规则/代理组/DNS 手动覆写
  脱敏预览 → 保存方案（已用 N/10）→ 发布为自己的独立订阅链接
  在 Mihomo / Xray 系客户端导入、更新和实际连接
```

- 管理员例：同一台 `HK-zouter` 创建订阅入口 Reality 和 SS2022，以及单独内部落地代理。两个入口可以分配不同客户组；内部落地即使与入口在同一资源组，也不进入客户订阅。
- 产品例：内部组“香港资源”包含合格入口；外部组“标准”只选其中一个，“VIP”显式选两个并允许不同模板；客户可同时分配“标准”和“VIP”。重复代理按稳定 ID 去重，不按显示名或端口去重。
- 客户例：基础订阅已可用后创建“家用 Mihomo”和“移动 Xray”两个独立方案；前者调整 DNS／规则／节点名，后者使用经过验证的 Xray JSON 模板。两者引用同一用户已有授权，不产生第二份账号、流量额度或服务端代理。
- 内部资源组只表达管理／分配范围，**不会自动创建服务器、开端口、调度迁移或替代画布连线**。管理员新建代理时可选“暂不分组”；加入外部组及完成授权应用前不向客户发布。
- 所有可连接画布卡都是 ProxyNode 入站，包括内部末端；direct 不是第二卡。lite 首版 direct-only，只能在通过方法／可达性／联合发布验证后作 full 链路内部末端；客户分配还须通过独立凭据、用户计量与租约门禁，详见 NEXT。

## 2. 订阅模板：面向客户端的版本化生成策略

### 2.1 模板管理交互

页面 `订阅模板` 分客户端家族、内置／自建、草稿／已发布／已归档；展示模板名、描述、当前发布版本、适配器版本要求、引用外部组数和校验结果。

操作闭环：创建空模板／复制内置模板 → 表单与结构化编辑器 → 导入文件或粘贴文本 → 查看支持字段与拒绝字段 → 合成授权节点预览 → 保存草稿 → 发布前校验 → 发布不可变版本 → 外部组选用。导出包含格式、schema、版本及校验和的无凭据模板包；“回滚”通过新发布版本或显式重绑旧版本完成，不改写历史内容。

- **导入两类输入**：sing-ui 模板包 JSON；对应适配器支持的 Mihomo YAML、Sing-box JSON、Xray 客户端 JSON 的配置子集。YAML/JSON 原生字段由格式适配器解析为受控 AST，不做跨格式盲目转换。
- 完整配置中的静态节点、认证字段、外部 provider、执行脚本等不能作为模板里的隐蔽节点来源。导入器只提取支持的生成策略，给出路径级诊断和清理差异；必须确认清理才能保存。不能静默丢掉 DNS／路由后显示“原样导入成功”。禁止原始秘密落到日志、错误、草稿历史或未清理导出。
- 第一版支持本地文件和粘贴配置，**不承诺直接兼容 SubBoost 配置包、任意 INI/subconverter 模板或第三方订阅转换服务**。没有适配器的格式返回不支持；未来适配另行版本化。
- 服务端保留现有 `templates` 与 `template_versions`，领域/API 命名 `SubscriptionTemplate`；不另建内容重叠的 `subscription_templates` 实表。新增字段与版本存储见第 6 节。
- 全局模板写入先保留现有 owner 权限；admin 可选已发布模板和管理组，不默认获得 owner 权限。今后放开 `template:write/publish` 必须显式授权。客户导入走自己的方案，不进入全局模板表。

参考：Remnawave Templates 的客户端家族与外部组选择；SubBoost 模板与节点分离、应用策略的交互；3x-ui 的表单分区和主题壳。[RW-T][RW-S][SB-T][UX-3]

### 2.2 客户端格式与兼容性契约

| 客户端选项 | 输出契约 | 模板能力／限制 |
| --- | --- | --- |
| Mihomo（Clash 兼容家族） | `format=mihomo`，完整客户端 YAML，`application/yaml` | 节点、代理组、规则、DNS；只承诺验收过的 Mihomo 版本，不泛称所有旧 Clash 都兼容 |
| Sing-box 客户端 | `format=sing-box`，完整客户端 JSON，`application/json` | 对应版本的 outbound／route／DNS；不能把 Mihomo 字段直接复制过去 |
| v2rayN 等链接订阅客户端 | `format=base64`，UTF-8 节点 URI 列表按行 Base64，`text/plain` | 命名、排序、过滤和支持的 URI 参数；**不能携带完整 DNS、路由或代理组**。`v2rayn` 仅作为 UI 客户端标签／迁移别名，不当作第三种 JSON schema |
| Xray core／Xray JSON 客户端 | `format=xray-json`，**一份完整可运行 JSON 对象**，`application/json` | 必含适配的客户端入站（默认仅 loopback）、出站、路由和 DNS；不是服务端 Profile，不是假定所有 GUI 都接受对象数组 |

当前 sing-ui `GenerateClient` 与 `SupportedFormats` 实际只列 Mihomo、Sing-box、Base64；迁移注释中出现 `v2rayn` 不构成支持证据。Xray-json 是本次新增设计，不能在实施前标为已支持。[CP-G]

API 返回 `format × protocol × adapterVersion` 能力矩阵。不同客户端对 Reality／SS2022／Hy2 的支持按锁定版本实际验证；未知按不支持处理，不能因 Xray 家族名相同就承诺可用。选择格式时先显示不兼容节点清单；默认发布失败，可由用户明确确认 `exclude_unsupported`，返回被排除的稳定 ID／原因并阻止空结果。后续新节点也使用方案保存的兼容策略。

NEXT 增加 profile/runtime/method/network 维度：lite 只公布验证过的 Shadowsocks 组合，不能把 SS2022 适配直接扩张成全部 SS 算法；模板、客户端覆写不更改 Agent 能力或新增链路。

Base64 模板在 sing-ui 中是“节点列表生成策略”，不是 Remnawave 那种完整配置模板；UI 隐藏 DNS／规则编辑，API 提交这些字段返回 `422 format_capability_mismatch`，不静默忽略。[RW-T][CP-G]

纯 Xray core 的验收是获取新 JSON 链接内容后用 `xray run -test -config <file>` 校验、启动并代理访问；订阅轮询、GUI 导入和更新由**明确命名且锁定版本的客户端／更新器**负责。v2rayN 使用 Base64 URL 的导入测试与 Xray JSON 完整配置测试分开记录，不把 core 会读配置等同于具备 GUI 订阅管理。[CL-X]

### 2.3 发布与引用策略

- 发布版本不可变，记录 `schema_version/adapter_version/checksum/published_by/published_at`。只有支持且校验通过的版本可绑定组。
- 外部组对每个格式绑定一个或多个允许的**精确模板版本**，每格式恰有一个默认。默认变更须显示受影响基础订阅／客户方案及兼容性差异。
- 基础订阅默认跟随其组当前批准的默认版本；可显式选择仍被所有选中组允许的版本。客户方案默认固定创建时解析出的模板版本，可开启“跟随组默认”，升级时重新校验并展示差异。
- 归档阻止新绑定，不立即破坏已有合法引用；安全撤回通过 `withdrawn_at` 硬失效，所有读取包括客户固定版本都重新检查。不得回退到已撤回版本或用缓存继续服务。
- 客户导入的私有模板版本存入自己的方案修订，不计入全局模板版本。禁止客户读取未发布全局草稿或其他客户模板。

## 3. 内外分组：资源分配、客户产品与有效授权

### 3.1 数据关系与操作

```text
InternalSquad --服务器成员--> ServerNode
              --显式代理成员--> ProxyNode（服务器必须属于该资源组）
ExternalSquad --关联资源组--> InternalSquad
              --显式公开代理--> 上述资源组中的 subscription 代理
              --允许/默认模板版本--> SubscriptionTemplateVersion
User --多对多--> ExternalSquad
```

- **内部组页**：资源名称／地区标签、服务器选择、从所属服务器挑选代理、查看容量与发布状态摘要、哪些外部组在使用。内部代理可留在资源池管理视图，但不能选入客户公开清单。服务器加入资源池不自动授权它未来的所有代理。
- **外部组页**：基本信息（如标准／VIP）、“代理节点”“订阅模板”“客户策略”三个页签；先选内部资源组，再从池内显式选可订阅代理；每客户端绑定模板和默认项；设置是否允许自定义模板、可覆写字段与资源上限。客户预览显示最终节点／模板，不显示管理地址或内部组运维细节。
- 加新服务器／新代理不会自动扩大客户授权。成员移除先显示所有影响的外部组、用户和 runtime；经发布确认或明确“立即撤权”后，同一事务移除或裁剪相关有效授权并失效缓存，再跟踪 Agent 凭据撤销。仅从编辑草稿中移除成员不构成运行撤权；无影响确认不允许破坏性改组。
- 删除被引用的模板／内部组返回 `409 dependency_in_use`；外部组“停用”先撤权、异步确认运行撤销，再可归档。不是数据库级联删除用户和历史流量。

这结合 Remnawave 入站授权与客户端模板覆写的职责，但**用户多外部组、外部组显式节点清单、内部组服务器池**是为本产品新增的映射。[RW-S][RW-U]

**组也必须区分草稿与已发布授权。** `PUT/PATCH` 的成员、模板和策略只写 `squad_drafts`，带 `expectedDraftRevision/basePublishedRevision`；第6节成员绑定表是已发布快照的关系投影，不是编辑表。`publish` 校验所有关联组的已发布版本并创建授权 operation；新授权先以本次候选准备/应用用户凭据，匹配 ACK 后才在数据库事务内生成不可变 `squad_versions`、切换 `published_revision` 并更新有效关系投影。订阅、覆写、常规 runtime 装配和缓存只读已发布关系；候选发布器仅为指定 operation 读取草稿，不能供普通读取。

撤权采用保守例外：用户明确确认发布里的移除项、停用或调用“立即撤权”时，先同步发布一个**仅缩小权限**的版本，递增受影响用户 `authz_revision`、失效缓存，再等待 Agent 撤销确认；新增部分单独等待应用成功后晋升。混合变更可能“撤权已生效、新授权失败”，必须逐阶段展示，不能承诺跨机器原子切换或失败后自动恢复撤销的权限。立即撤权同时作废关联旧候选；之后旧草稿发布须重基并重新确认，补偿回滚也不得重加已撤权凭据。修改内部组须在同一有效投影事务中裁剪依赖外部组并推进其发布版本；模板/策略收紧使现有方案不合法时立即阻止其输出，不等待客户升级模板。

### 3.2 授权计算：同一份规则贯穿配置、订阅和覆写

令 `A(user)` 为当前启用且分配给该用户的外部组，`R(group)` 为该外部组关联内部资源池中的有效代理，`E(group)` 为外部组显式公开代理，`P` 为已确认发布、可订阅且未隔离的代理集合；全部组关系及策略只取 `published_revision` 对应投影，明确排除 `squad_drafts`：

```text
Grant(user) = union over group in A(user) of (E(group) intersect R(group) intersect P)
BaseNodes(subscription) = Grant(user) intersect NodesOfCurrentlyAssignedSelectedGroups
FinalNodes(preset) = BaseNodes(baseSubscription) intersect CustomerSelection
```

用户停用／删除、到期、额度耗尽或基础订阅撤销时，结果为空并拒绝交付可连接配置；**不是改个显示状态后继续输出旧配置**。授权服务以同样的代理 ID 集合生成用户 runtime 凭据和客户端输出，不能订阅仅允许代理 A、Agent 却接受同机代理 B 的用户认证。

模板、手动参数、客户过滤只能缩小 `BaseNodes` 或改变允许的客户端字段，不能增添服务器／代理／其他用户凭据。外部静态代理沿 R0 保持 `external:<id>` 命名空间；迁移期保持旧路径，进入新组前必须有明确资源绑定与无法管理远端授权／计量的限制，不能冒充已确认受管代理。

### 3.3 用户属于多个外部组时的确定性规则

- 默认提供按外部组拆分的基础订阅；每个基础订阅可提供其允许的客户端格式。这样不同客户模板不会不明原因互相覆盖。
- 用户可创建“合并分组”基础订阅，显式选一组或多组当前已分配的外部组；集合并集按稳定代理 ID 去重。同名节点生成稳定唯一展示名，规则引用仍用 ID。
- 合并输出对选中且当前有效的组计算每格式**允许模板版本的交集**；只能选择共同允许的模板。默认模板一致则自动选，否则要求选一个共同版本；交集为空返回 `409 template_conflict` 并引导拆分链接，不按数据库顺序随便覆盖模板。
- 多组客户覆写策略按更严格的约束合并：允许字段取交集、大小等上限取最小、禁止项取并集；官方默认客户端参数冲突时要求明确选择可用订阅分组，不做隐式深度合并。
- 以后被取消某组，节点立即从新请求结果裁剪；如剩余组仍允许所选模板与策略则继续，否则返回 `subscription_policy_changed` 要求重选。绝不能用旧缓存保留已撤权组。新增用户分组不自动加入既有显式范围链接。
- 无分组／空节点的客户区显示“等待管理员分配”，不提供一个伪装成功的空订阅；管理员可以保存尚未就绪的组草稿，但不能交付“可用”基础订阅。

## 4. 用户管理：授权、到期、额度与重置

### 4.1 页面与管理工作流

`用户管理` 从现有 `Administration` 拆出独立页面：用户名、邮箱、账号状态、服务状态、外部分组、到期时间、本周期已用／限额、下一次重置、最近授权应用状态。支持搜索、字段筛选、分页、列选择；流量进度同时显示数字和计量新鲜度，不把没有遥测解释为零流量。

创建／编辑抽屉分“身份”“访问权限”“流量与有效期”“订阅”页签：输入身份、分配多个外部组、到期（或不限）、流量额度（`0` 延续不限）、重置策略，确认影响代理后保存。账号创建和授权任务幂等；返回 `202` 时显示等待应用，不先承诺订阅可连接。凭据准备与发布采用 R0 的候选材料契约。

- CRUD：创建、读取、改邮箱／备注／访问策略、禁用／启用、删除。删除默认逻辑归档并撤销会话、全部订阅及方案链接，历史流量／审计保留；不级联丢历史。凭据恢复不能“重新注册同名用户就继承旧链接”。
- admin 可管普通客户；角色提升、owner 删除／降级不走批量客户 API；不可修改自己的权限扩大管理范围。密码只走既有认证／一次性邀请重置流程，不在用户表格或报告回显。
- 批量动作：分配／移除／替换外部组、启停、到期设置／延期、限额及重置策略、立即重置、归档。先冻结选中 ID＋revision 或筛选快照，预览数量／影响，再提交有幂等键的 job；逐用户成功、冲突、失败分别报告，不能宣称跨所有用户原子成功。
- 每用户可查看并复制基础订阅及授权诊断；管理员默认不读客户私有模板正文、不给自己签发客户编辑会话。客户链接显式复制，普通列表和审计不携带 token。

信息架构参考 Remnawave Users 的 Traffic & Limits、Access Settings、列选择、批量、订阅链接入口；仍使用 sing-ui 角色与 3x-ui 视觉，不复制上游含完整 UUID 的详情展示。[RW-U][UX-3]

### 4.2 状态与撤销语义

账号状态与派生服务状态分开：`users.is_active/deleted_at` 是管理状态；`active/disabled/expired/exhausted/no_access/provisioning` 由当前时间、额度、分组、授权应用计算，优先级为归档/停用 → 到期 → 额度耗尽 → 无授权 → 应用中 → 可用。页面可同时展示多个原因。

- 到期边界 `now >= expire_at`，`expire_at=NULL` 表示不限；保存 UTC 时间，展示用户时区和绝对日期。续费／改到期不重置流量，重置流量不延期。
- 每个订阅请求和凭据发放都重新检查账号、到期、额度；自动到期／额度事件还要触发 runtime 授权更新。停用数据库成功不等于离线 Agent 已撤销凭据，UI 展示 desired/confirmed 授权状态与最后回执。
- 已下载的配置不会凭删除 URL 自动消失。到期与撤权需要 Agent/runtime 停止接受旧凭据；离线或控制面断连的最大撤权窗口要通过**有期限授权租约、Agent 本地失效规则**补齐并测试。租约时长作为显式产品策略，重连不能重新激活已过期用户；实现前不承诺即时踢线或绝对实时配额。
- 流量上报周期带来额度超用窗口；计量 stale／gap 要可见，不能“隐藏节点就等于计费强制生效”。这些运行边界是用户管理验收门槛，不增加支付／账单系统。

### 4.3 流量重置契约

第一版明确支持：`none`、`monthly`、`interval`（每 N 日）、`one_time`（自定义日期一次）；UI 明确“每月固定日时”与“指定日期仅重置一次”，不接受任意 cron 或含糊的“按月就是 30 天”。

- `monthly`：保存 IANA 时区、日号 1–31 和本地时分；当月无该日取月末。夏令时不存在的时刻顺延到首个有效时刻，重复时刻执行一次；数据库保存解析后的 `next_reset_at` UTC 和规则 revision。
- `interval`：保存正整数间隔日及锚点 UTC，定义为 N×24h；`one_time` 执行后进入 `none`，保留事件记录；禁用用户仍按计划推进额度周期，但不因此激活服务。
- 重置只推进 quota epoch、本期计数和周期边界；不清空累计流量／原始记录，不重复改用户限额。计划变更不立即赠送一次重置；预览下一执行时间，立即重置另用确认动作。
- 多实例调度使用数据库到期任务锁；唯一 `(user_id, policy_revision, scheduled_at)` 防重复。同一事务锁用户额度行、完成重置事件并推进 `next_reset_at`，重试不会连续送多个周期。
- 停机错过多个周期，按规则恢复当前周期及审计缺口，不将多次额度累加。手动重置与计划任务串行化、带 `expectedQuotaEpoch`；不默认改变下一计划时间。
- **迟到流量必须按采样时间归属。** 当前实现把报告增量加到当前 `traffic_accounts`，不能只增加一个定时器就宣称月周期正确。[CP-Q] 新账本按实际采样区间切分周期，迟到报告只修正对应历史周期；跨边界累计样本按时间分摊并标 `estimated`，保留未能精确分摊的事实。去重继续用 Agent epoch／sequence，生命周期累计只计一次。
- 系统重置事件使用 `actor_kind=system, actor_id=NULL`，人工事件保留操作者；需兼容扩展现有 `traffic_account_events.actor_id NOT NULL`，不能伪造 owner 身份让调度通过。

## 5. 客户覆写工作流：自己的模板、十个方案、新链接

### 5.1 工作台不是任意第三方订阅转换器

客户从 `我的订阅` 已就绪的基础订阅点击“自定义”，工作台固定展示来源、所属分组、到期、限额和授权节点数。也允许粘贴**本站自己的基础订阅 URL**作为选择快捷入口：只解析受控本站路径、在数据库绑定到当前用户所属基础订阅，绝不服务器回抓任意 URL。知道他人的 URL 或 token 不等于拥有它。

基础链接是动态产品视图，不把下载得到的节点明文再当成独立数据库事实。每个客户方案绑定 `base_subscription_id`；每次请求重新解析基础订阅的当前授权与已确认节点。禁止“覆写订阅套覆写订阅”形成递归，禁止第三方节点源／provider 使客户绕开自己的分组范围。

### 5.2 客户操作路径

1. **选基础订阅与客户端**：只列本人的已就绪基础订阅和支持格式；显示基础状态异常／不兼容节点，不允许伪造 `user_id`。
2. **选模板来源**：继承官方模板、从我的已存方案复制模板策略、上传 `.yaml/.yml/.json` 或粘贴配置。客户私有模板只能改变生成策略；字段导入与清理遵循第 2.1／9.2 节。
3. **手动覆写**：节点选择／排除、重命名、排序、客户端连接选项；增加规则、客户端代理组和 DNS。简单表单与高级结构化编辑器操作同一个 AST，明确“客户端代理组”不等于管理端内外 squad。
4. **预览差异**：左侧原授权节点，中间修改项，右侧脱敏结果、校验告警与字段来源。预览不改基础订阅、不创建服务端代理、不扣保存名额、不暴露凭据。
5. **保存为方案**：输入名称，显示已用 `N/10`；保存草稿保留选择的稳定节点 ID、模板快照／引用、覆写 AST，不存一份永久连接凭据副本。
6. **发布并生成新订阅链接**：校验成功后生成独立随机 token；客户复制 URL／二维码，或下载正式完整配置。只有正式订阅响应含当前用户运行所必需的客户端认证材料。
7. **我的自定义订阅**：显示方案卡片／列表，支持继续编辑、复制成新方案、导出无凭据方案、暂停／恢复、删除、轮换链接；普通编辑保存后再发布，已用链接保持不变。

### 5.3 十个保存名额的精确定义

**每客户最多保存 10 个“模板＋覆写＋节点选择”的自定义订阅方案。不是十个节点，也不是模板十个、链接再十个。**

- 草稿、已发布、暂停的未删除方案全部计入；空白浏览器未保存编辑不计入。一个方案内部的模板版本／历史修订、同一方案的链接轮换不额外计数。
- 基础订阅、管理员全局模板不占名额；从旧方案“另存为”或复制占一个新名额。单个客户导入的模板若要独立保存，也保存为尚未发布的方案，不能另开无限私有模板库绕过十个上限。
- 已满时允许原位编辑／发布现有方案；第 11 个创建返回 `409 preset_limit_reached` 和 `used=10,limit=10`，不默默覆盖最旧方案。
- 数据库为每用户提供 `slot=1..10`，未删除记录的 `(user_id,slot)` 唯一，加 `CHECK`；创建事务锁用户名额范围。并发创建只会成功至十个，不能用前端计数或无锁 `COUNT(*)` 保障。
- 删除立即释放 slot 并吊销方案 token，审计仅留元数据；恢复删除的方案需重新占空位且生成新 token，已删除 URL 不复活。暂停不释放名额。
- 每个方案第一版锁定一种输出格式，保证导入模板与覆写的解释唯一；换客户端家族用“另存为”，占一个名额，避免同一 URL 突然由 YAML 变 JSON。

### 5.4 链接、版本与实时继承

```text
基础订阅：GET /sub/{baseToken}?format=mihomo
基础订阅：GET /sub/{baseToken}?format=xray-json
覆写订阅：GET /sub/custom/{presetToken}
客户管理：登录后的 /api/my/override-presets/...（不是拿 token 就能编辑）
```

上面只有路径占位符，不是实际订阅地址。基础 token 与客户 token 独立随机生成；客户 URL 不携带原始基础链接、用户 ID、模板文本或节点凭据。公开读取不能创建／改写方案；客户登录权限与 bearer 订阅访问权严格分开。

- 方案编辑新增 draft revision；发布事务切换 `published_revision`，失败保留上一个可用版本，但它仍必须通过当前授权／模板安全状态检查。旧版本不得作为越权恢复通道。
- 默认 `selection_mode=all_authorized`，动态跟随基础范围内新确认代理；`explicit` 只保留客户选定的稳定 ID。授权消失的节点总是裁剪；关联手动覆写标记失效引用。引用失效导致规则／组不可用时阻止输出并提示修复，不回退到更多权限。
- 固定模板快照**不固定授权、到期、额度、公布地址、用户凭据或代理发布版本**。这些事实每次生成重取；基础配置编辑成功后同步生效，失败草稿永不被客户看见。
- 基础 token 仅轮换时，客户方案仍引用同一基础订阅 ID；若选择“撤销基础订阅及所有衍生”，基础与所有客户 token 立即失效。UI 明确这两种动作，不能让用户误以为泄漏响应只换基础 token 就吊销所有副本。
- 基础订阅撤销／删除、用户停用／到期／超额、方案暂停／删除或 token 轮换：下次公开请求拒绝；账号禁用还同步撤销 runtime 凭据。再次启用需重新确认授权，不能复活已明确吊销的 token。

参考 SubBoost 的快捷／高级生成器、策略与源分离、模板卡片和保存后固定 URL；sing-ui 不采用其可导入任意订阅来源作为授权事实，也不把其 YAML-only 生成证据扩张成 Xray 支持。[SB-U][SB-T][SB-P]

## 6. 数据模型增量：逻辑齐全，物理复用

所有新表带必要 `created_at/updated_at`；可编辑聚合有 `revision`，FK 指向现有稳定用户／服务器／入站 ID。以下是设计清单，**不新增 SQL 文件、不预占已应用迁移号**。

NEXT 第 3 节补充同一个 ServerNode 的 agentProfile、能力版本／容量、用户计量／租约、NAT 映射和地址预留；不另建轻量服务器、出口节点或授权表。DDNS 引用仅预留 NULL，R4+ 再评审实施。

| 聚合／表 | 字段与约束（拟新增或扩展） | 目的／复用 |
| --- | --- | --- |
| `templates`、`template_versions` | 复用名称、format、definition、status、published_version、revision；版本增 `adapter_version`, `min_client_version`, `source_kind`, `withdrawn_at`；definition 使用 schema v2 AST | 对外称 subscription templates；保留 v1 parser 和既有不可变版本，不能重写旧 definition 冒充迁移 |
| `squads` | `id,kind(internal/external),name,status,description,revision,draft_revision,published_revision NULL,legacy_group_id NULL`；`(id,kind)` 唯一；原组映射唯一 | 一个公共实体类型＋两个职责明确的子类型；发布指针通过复合FK绑定本组版本 |
| `squad_drafts`、`squad_versions` | 草稿 `(squad_id PK,revision,base_published_revision,definition JSONB,updated_by)`；版本 `(squad_id,version)` PK，`definition JSONB,operation_id,published_by,published_at` | 无凭据成员/模板/策略快照；草稿用关联版本向量校验，版本不可变；以下关系表仅在发布/显式撤权时事务更新为有效投影 |
| `internal_squads`、`external_squads` | 各以 squad_id 为 PK，固定 kind 用复合 FK/check 对齐父表；external 增 `override_policy JSONB,settings JSONB` | 强制绑定类型；设置只允许经过 schema 校验的产品元数据，不能任意响应头 |
| `internal_squad_servers` | `(internal_squad_id,server_node_id)` PK/FK | 内部资源分配，不等于客户端授权 |
| `internal_squad_proxies` | `(internal_squad_id,inbound_id)` PK，含 `server_node_id`；复合 FK 指向同组服务器成员及 `(inbounds.node_id,id)` | 代理确实属于该资源组中的服务器，不能仅信前端传的 serverId |
| `external_squad_internal_squads` | `(external_squad_id,internal_squad_id)` PK/FK | 外部产品可用的资源池 |
| `external_squad_proxies` | `(external_squad_id,inbound_id)` PK；`source_internal_squad_id`；复合 FK 约束两类关联 | 显式客户可见代理；服务事务再校验 exposure/publication，不动态授权整台机器 |
| `external_squad_templates` | `(external_squad_id,format,template_id,template_version)` PK；`is_default`；模板版本 FK | 每组每格式一个默认的部分唯一索引；发布校验至少一个可用默认；format 必须匹配模板 |
| `user_external_squads` | `(user_id,external_squad_id)` PK/FK，`assigned_by,assigned_at` | 用户多外部组；runtime 内部权限从此派生，不再让用户另一份直接内部组授权绕过外部产品 |
| `users` 扩展 | 保留 `username,email,is_active,expire_at,traffic_limit_bytes,quota_revision`；增 `revision,authz_revision,deleted_at,notes` | 不重复建存储状态或 used_bytes 真相；角色与现有 auth 共用 |
| `user_traffic_reset_policies` | `user_id PK,mode,timezone,day_of_month,local_time,interval_days,anchor_at,next_reset_at,revision`，互斥字段 check | 每用户一个可明确解释的计划；日期/interval 校验见第 4.3 节 |
| `traffic_reset_runs` | `id,user_id,policy_revision,scheduled_at,status,quota_epoch_before,quota_epoch_after`；唯一用户＋策略版本＋计划时间 | 多调度器幂等恢复；状态与重置提交同事务，不靠内存锁 |
| `traffic_accounts`／`traffic_account_events` | 复用当前额度聚合、累计流量和 epoch；events 扩展 `actor_kind,actor_id NULL,reason,scheduled_at,policy_revision`，kind 与 actor 一致性 check | 人工／系统重置审计；不重新计算历史账单或删除历史 |
| `traffic_quota_periods` | `(user_id,quota_epoch)` PK，`start_at,end_at,up_bytes,down_bytes,estimated,closed_at`；时间不重叠、计数非负 | 接纳迟到流量并支持周期查询；账户当前计数是当前周期聚合，更新与报告游标同事务 |
| `subscriptions` 扩展 | 保留 ID/user_id/token 兼容；新增 `kind(base/legacy),status,revoked_at,default_format,authz_revision`；新链接只存 token hash＋受控加密可取回值 | 基础订阅是授权视图；不把 legacy 单组字段继续当新模型权威 |
| `subscription_squads`、`subscription_template_bindings` | 前者 `(subscription_id,external_squad_id)`；后者 `(subscription_id,format)`, `mode(group_default/pinned),template_id/version NULL` | 显式基础范围、多格式模板选择；每次验证当前用户组和允许模板集合 |
| `client_override_presets` | `id UUID,user_id,base_subscription_id,name,slot,format,status,draft_revision,published_revision NULL,revision,deleted_at` | `CHECK slot BETWEEN 1 AND 10`，未删除的用户 slot 部分唯一；`(base_subscription_id,user_id)` 复合 FK 防跨用户来源；基础类型在服务端强校验 |
| `client_override_preset_revisions` | `(preset_id,revision)` PK，`schema_version,template_source,template_ref/version NULL,template_ast NULL,selection_mode,selected_proxy_ids,override_ast,compatibility_policy,checksum,created_at` | 互斥模板引用／私有 AST；正文仅受控无凭据策略；发布指针复合 FK；历史保留最近 20 个非当前版本，当前发布和草稿引用不可清掉 |
| `subscription_access_tokens` | `id,base_subscription_id NULL,preset_id NULL,token_hash UNIQUE,encrypted_token,token_version,revoked_at`；owner 目标二选一 | 新基础与客户 token 分开解析；随机至少 256 bit，散列查找，加密值仅显式复制可用；加密存储未准备好不得开放可恢复复制 |
| `subscription_delivery_state` | `subscription_id/preset_id,authz_revision,render_revision,last_validation_code,last_success_at`，不存带凭据结果 | 展示兼容性和“待修复”状态，不作为授权来源 |
| `user_access_operations`、`admin_bulk_jobs/items` | `actor_id,idempotency_key,request_hash,status,expected_revision,affected_server_ids,release_id/result_code`；幂等唯一约束 | 复用 R0 operation/release 模式跟踪凭据应用；批量逐用户结果，不记录 token/配置正文 |

实现时 `selected_proxy_ids` 可规范为成员表以获得 FK；即使采用 JSON AST，也必须逐 ID 验证当前授权，不以数组里“曾经合法”作为永久授权。所有跨用户对象读取、更新、删除使用 `(id,user_id)` 条件，不能仅在前端隐藏。

## 7. API 增量与现有兼容入口

### 7.1 公共契约

Agent 侧沿 NEXT 第 4 节复用 config／heartbeat／report-traffic／deployment-acks，新增受控注册与 lite 配置编码协商；以下模板／组／客户端点不另分 full/lite 两套。客户端公开配置不得泄露内部链路／Agent 材料。

- 复用当前 `/api/templates`、`/api/users`、`/api/my/...` 路径风格，不平行新建第二套 `/api/admin/*`。表中标为新增的端点均尚未实现；旧端点见 [CP-A]。
- 登录接口按现有角色链鉴权；写入要求 CSRF／同源保护与严格 JSON schema。更新使用 `expectedRevision`，创建／发布／批量要求 `Idempotency-Key`，同 key 不同 body 返回 409。
- 分页 `cursor,limit`（最大 100），统一 `fieldErrors[{path,code}]`；401 未登录，403 无角色权限，404 他人对象／不可知对象，409 版本／名额／依赖冲突，413 导入超限，422 schema／能力不符，429 限流。响应、错误与审计不带秘密值。

### 7.2 管理端

| 方法／路径 | 权限 | 请求／响应及页面 |
| --- | --- | --- |
| `GET /api/templates`、`GET /api/templates/{id}`、`GET /api/templates/{id}/versions` | 已登录；客户仅可见其组允许的已发布版本 | 复用并收紧可见性；全局模板管理／客户模板选择 |
| `POST /api/templates`、`PUT /api/templates/{id}`、`DELETE /api/templates/{id}` | owner | 复用 CRUD，增加源/schema；有引用归档而非破坏性删除 |
| `POST /api/templates/import-preview`、`POST /api/templates/import` | owner | 新增文件／文本提取与确认入库；不得在 preview 日志存原文 |
| `POST /api/templates/{id}/validate`、`POST /api/templates/{id}/publish` | owner | 复用入口，扩展适配器检查与版本锁定 |
| `GET /api/templates/{id}/versions/{version}/export`、`POST /api/templates/{id}/versions/{version}/withdraw` | owner | 新增无凭据导出／安全撤回，先显示引用影响 |
| `GET/POST /api/squads`、`GET/PATCH/DELETE /api/squads/{id}` | admin/owner | 新增内外组列表、草稿、状态与依赖；列表用 kind 筛选，kind 创建后不可改 |
| `PUT /api/squads/{id}/servers`、`PUT /api/squads/{id}/proxies` | admin/owner | 只改草稿；servers限内部组，proxies按kind分别编辑内部/外部成员，带草稿/发布版本 |
| `PUT /api/squads/{id}/internal-squads`、`PUT /api/squads/{id}/templates` | admin/owner，且 external | 草稿中原子替换外部资源关联／格式模板版本＋default，不提前改变有效关系 |
| `POST /api/squads/{id}/preview`、`POST /api/squads/{id}/publish` | admin/owner | 预览指定draftRevision与影响；发布按第3.1节先撤权后确认新增，涉及运行授权返回202 operation |
| `POST /api/squads/{id}/revoke` | admin/owner | 显式成员/整组立即撤权，期望publishedRevision、原因和影响确认；同步缩小有效投影、异步确认runtime撤销 |
| `GET/POST /api/users`、`GET/PATCH/DELETE /api/users/{id}` | admin/owner，只管理可管角色 | 扩展现有列表为完整 CRUD、字段筛选；软删除流程 |
| `PUT /api/users/{id}/external-squads` | admin/owner | `{squadIds,expectedRevision}`；返回授权变更 operation，不谎报已应用 |
| `PUT /api/users/{id}/quota`、`POST /api/users/{id}/traffic-reset` | admin/owner | 复用限额／到期及立即重置；后者带 epoch/reason，不能由客户调用 |
| `PUT /api/users/{id}/traffic-reset-policy` | admin/owner | 新增结构化调度规则，返回解析后的 nextResetAt |
| `POST /api/users/{id}/base-subscriptions` | admin/owner | 新增交付基础订阅；groupIds＋格式选择，不接受任意节点明文 |
| `POST /api/users/bulk-preview`、`POST /api/users/bulk-jobs`、`GET /api/users/bulk-jobs/{id}` | admin/owner | 新增快照选择／动作预览／逐用户结果；作业创建者与权限再次检查 |
| `GET /api/operations/{id}` | 有权限的操作者 | 延续 R0 状态查询，用于组／用户凭据部署进度 |

### 7.3 客户端管理与公开订阅

| 方法／路径 | 核心语义 |
| --- | --- |
| `GET /api/my/groups`、`GET /api/my/subscriptions`、`GET /api/my/subscriptions/{id}` | 适配现有接口为本人外部组和基础订阅，含到期/额度/就绪状态但常规列表不回 token |
| `POST /api/my/subscriptions` | 兼容本人从已分配组创建基础视图；服务端只允许 kind=base，不能利用旧创建路径绕过方案十个上限 |
| `GET /api/my/subscription-capabilities` | 支持格式、协议、允许字段、模板导入限制和 used/limit；不携带内部管理资源 |
| `GET /api/my/subscriptions/{id}/proxies`、`GET /api/my/subscriptions/{id}/templates` | 本人当前授权的脱敏节点与可用模板版本；404 避免泄露他人资源 |
| `POST /api/my/override-imports/preview` | `{baseSubscriptionId,format,sourceKind,content}` 或受限上传；返回已清理 AST、诊断，不保存方案 |
| `POST /api/my/override-presets/preview` | 无副作用临时编辑预览，来源＋模板＋选择＋覆写；全部从服务端重验权限 |
| `GET/POST /api/my/override-presets` | 本人列表／保存草稿；POST 同事务占用一个 slot，返回 used/limit |
| `GET/PATCH/DELETE /api/my/override-presets/{id}` | 本人读／更新草稿／删除并撤销；更新不能改 owner、来源归属或既有格式 |
| `POST /api/my/override-presets/{id}/publish` | `{expectedRevision,draftRevision}`；校验后切发布指针，首次创建独立 token，失败保留旧合法发布 |
| `POST /api/my/override-presets/{id}/duplicate` | 复制无凭据策略，重新验证基础范围并占新 slot，不复制旧 token |
| `POST /api/my/override-presets/{id}/pause`、`.../resume` | 暂停立即拒绝公开请求；恢复重验策略，不免计名额 |
| `POST /api/my/override-presets/{id}/link`、`.../rotate-token` | 本人显式复制／轮换；响应不缓存、不记正文日志，旧 token 即失效 |
| `GET /api/my/override-presets/{id}/export` | 无节点认证材料的方案包，可用于后续导入；不是可连接客户端成品 |
| `POST /api/my/subscriptions/{id}/link`、`.../rotate-token`、`.../revoke` | 基础链接交付／仅轮换／撤销基础与衍生；明确影响范围 |
| `GET /sub/{token}` | 保留旧 URL；新增显式 format 支持及默认配置；只读授权和发布状态，不以 UA 认证 |
| `GET /sub/custom/{token}` | 新增公开客户成品，只用方案固定格式；绕过格式参数更改被拒绝；不返回管理信息 |

浏览器客户管理从登录页进入；第一版公开 `/sub` 始终按格式返回配置，不引入复杂 UA 嗅探。Remnawave 同 URL 自动返回网页或客户端格式仅作为交互参考，未来做自动检测必须保留显式格式优先级。[RW-T][RW-U]

旧 `/api/my/subscriptions/{id}/overrides*` 不能继续作为不限数量的自定义方案旁路：迁移期仅允许读取旧覆写，写入由兼容适配器进入已占 slot 的新方案或返回明确迁移错误；旧预览复用新授权 pipeline。旧 `/api/groups/{id}/nodes`、`/api/users/{id}/groups` 切换后也必须拒绝不明确的 legacy 写入，不让旧路由恢复整机授权。

## 8. 前端信息架构与组件增量

管理导航：**总览｜服务器节点｜代理节点｜拓扑编排｜分组管理（内部／外部）｜订阅模板｜用户管理｜流量与通知｜设置**。

品牌显示 sing-ui；服务器资产与入口表单显示 full/lite、NAT、方法／端口／用户计量能力。画布只连接入口卡，创建抽屉的出站编辑器由只读摘要替代；客户无权改 profile、拓扑或 DDNS。实际 UI 代码留待实施。

客户导航：**我的订阅｜覆写工作台｜我的自定义订阅｜流量／账户**；默认不显示服务器、内部组、Agent、拓扑或管理员发布菜单。前端隐藏不是权限实现，API 同样隔离。

| 页面／组件（拟新增或重构） | 主要交互与状态 | 参考／现有落点 |
| --- | --- | --- |
| `SubscriptionTemplatesPage/TemplateEditor/VersionDiff` | 格式 tabs、导入校验、版本比较、发布、引用与归档；客户只读允许版本 | Remnawave Templates＋SubBoost 策略导入；重构 `frontend/src/pages/Templates.tsx` |
| `SquadsPage/InternalSquadEditor/ExternalSquadEditor` | 内部资源选择；外部代理、模板与客户策略 tabs；影响预览和授权应用进度 | Remnawave squad 职责，本项目外部产品绑定；拆出现有 Administration 组表单 |
| `UsersPage/UserEditor/UserBulkDrawer` | 可选列、筛选、到期／配额、外部多选、重置预览、逐用户批量结果 | Remnawave Users；替代单一 Administration 页面中用户功能 |
| `MySubscriptionsPage/BaseSubscriptionCard` | 基础订阅状态、各客户端入口、复制／二维码、自定义按钮 | 原 Subscriptions 数据＋Remnawave 订阅交付；不混放官方基础与客户草稿 |
| `OverrideWorkbench/TemplateImport/ClientPatchEditor/RedactedPreview` | 来源只读、模板/节点/代理组/规则/DNS tabs，简单/高级模式、差异和字段来源、N/10 | SubBoost 生成器；重构 `frontend/src/pages/OverrideEditor.tsx`，不是在管理员代理抽屉加功能 |
| `MyOverrideSubscriptionsPage/PresetCard/PresetPublishDialog` | 保存十个槽位、草稿/已发布/暂停/待修复、编辑/复制/导出/删除/轮换 | SubBoost 保存及固定 URL 体验；本项目加入授权与数量边界 |

工作台桌面三栏：来源／方案约 260px，中央编辑自适应，右侧预览 400px 可折叠；窄屏改分步骤 tabs，不要求并排阅读 YAML。统一继承 R0 的 3x-ui 深浅色 tokens、紧凑表格、侧栏／移动 Drawer、按钮／加载／空错态，而不是给三个参考项目各做一套互不相干的视觉。[UX-3]

交互底线：编辑错误不丢输入；未保存离开提醒；删除／撤销明确显示链接影响；import 的字段诊断能定位编辑器；键盘、焦点返回、屏幕阅读器标签、非颜色状态提示齐全。只持久化页面偏好和无秘密草稿引用；token、不清理的上传正文和连接材料不进 localStorage。

## 9. 统一生成 pipeline 与客户输入安全边界

### 9.1 生成顺序与覆写优先级

```text
token 或登录用户 → 找到本人基础订阅/方案（custom 只允许一层引用 base）
  → 检查账号/到期/额度/撤销/方案状态
  → 当前外部组授权 ∩ 内部资源 ∩ 已确认代理发布 ∩ 基础显式范围
  → 检查客户端格式能力、允许模板版本、当前最严格覆写策略
  → 读取无秘密 ClientProxyIR + 官方模板/组默认，构成基础 ClientConfigIR
  → 私有模板替换被允许的客户端配置区段；绝不替换授权节点源
  → 客户节点筛选/排序/命名 + 客户节点参数 + 手动代理组/规则/DNS AST patch
  → 结构、引用、循环、格式能力、字段策略二次校验
  → 正式响应最后注入该用户当前可用认证材料；预览只注入类型正确的合成材料
  → 格式适配器序列化 → 输出校验/资源限制 → 响应
```

允许字段的普通默认优先级：代理已发布公开值 → 官方模板 defaults → 外部组默认 → 迁移兼容基础覆写（服务器默认后代理专属）→ 客户私有模板 defaults → 客户手动全局覆写 → 客户单代理覆写。**授权、服务器真实连接端点／认证、强制策略不参与“最后写入赢”**，始终由服务端事实覆盖或拒绝修改。

客户选择私有模板时，可替换管理员允许变更的客户端规则／代理组／DNS 区段；管理员锁定项单独标注 `locked` 并参与最终校验，不依靠排在 merge 的前面抵抗覆写。数组不做不透明递归 merge：代理按 ID，组按稳定组 ID，规则按有序 `add/remove/replace/move` 操作；`null` 明确表示继承而非空字符串。预览显示每个最终字段来自哪一层。

发布授权新授予先完成必要凭据 prepare/apply，才交付可用节点；撤销则先在请求路径禁止，再等待 runtime 删除确认。生成器读取一致的授权／发布版本快照，发送前校验版本未变；若变则重新生成或拒绝，缓存命中也不能跳过该校验。

### 9.2 客户可改与绝不可改

| 类别 | 允许范围 | 不允许／限制 |
| --- | --- | --- |
| 节点展示与选择 | 自己节点的显示名、顺序、过滤、客户端分组 | 新增任意 endpoint、跨组 proxy ID、通过同名替换身份 |
| 节点客户端参数 | 格式/协议支持的 fingerprint、协商选项及管理员批准的 SNI/endpoint 别名 | 默认锁定 server/port/protocol/UUID/password/Reality 密钥；别名只能选管理员批准值，不是任意字符串跳转 |
| 规则、代理组 | 引用本人动态节点集合或现有客户端组，受支持类型、明确顺序、DIRECT/REJECT 等适配器语义 | 组循环、悬空引用、跨用户引用、把客户代理组当 runtime 拓扑下发 |
| DNS | 受支持 schema、批准的解析器配置或用户填写的有效客户端解析器地址，UI 提示流量影响 | 后端不会替客户访问该地址；禁止使用 DNS 字段偷偷加入节点源或 shell 参数 |
| 测试／规则资源 | 平台批准的测试目标和规则资源 ID；导出为客户端需要的声明 | 任意后端网络拉取、provider URL、文件路径、插件、脚本、环境变量展开 |
| 客户端本地监听 | 模板支持的 loopback 监听与受控端口 | 默认拒绝 `0.0.0.0/::`、外部控制器、TUN/系统路由／任意文件输出；开放这类能力需单独安全交互评审 |

**私钥与客户端认证材料要说准确。** Reality 服务端私钥、TLS 私钥、Agent 注册凭据和内部链路秘密永不进入客户模板、预览或正式订阅；Reality 公钥、当前用户的 UUID/密码以及 SS2022 等协议客户端连接必需的材料会出现在其正式配置中，否则无法连接。不能承诺“成品订阅不含任何秘密”；保护的是服务端私钥及他人凭据，并防止模板可自定义认证字段。[CP-G]

当前旧覆写能力允许部分 server/port/insecure/obfsPassword 字段，本设计对客户新流程收紧为显式策略，不能原封不动继承宽白名单。旧数据先生成脱敏冲突清单并由管理员决定批准别名、清理或迁移，不能直接删值导致静默断线，也不能以兼容为由允许凭据发送到任意端点。

### 9.3 导入、token、缓存和资源治理

- 输入首版限制作为服务端可配置上界：单份上传 256 KiB、UTF-8、禁止压缩包、YAML 自定义 tag／别名扩张／重复键、JSON 重复键、深度 32；规则最多 2,000 条、客户端组 100 个、节点选择 1,000 个。超限明确失败，前端与后端使用同能力返回值。
- 客户自带配置是不可信数据：仅受控 AST 解析，不执行 JavaScript/Lua、模板函数、shell、动态 include、不加载本地路径。服务端模板验证不出网；原生内核静态检查只能在无网络、无秘密、有限 CPU/内存/时间的隔离验收环境用合成材料执行。
- 一开始就不提供远程模板 URL 抓取，避免把“导入”变成 SSRF 代理；将来若增加，需独立设计目标 allowlist、DNS/重定向重检、私网／metadata 拒绝、大小时限及无凭据请求。客户端 DNS 地址不是服务器抓取授权。
- 新 token 随机生成，不是方案 ID 编码，也不以基础 token 派生。日志、中间件、反向代理访问日志、追踪 span 对 `/sub/*` 路径参数统一脱敏；正式响应和含 token 的复制响应使用 `Cache-Control: private, no-store`，禁止第三方统计、Referrer 泄漏及公共 CDN 缓存。
- 只缓存无秘密模板解析／配置 IR；key 至少含用户 authz_revision、基础/方案 revision、组和模板版本、代理 publication、适配器版本。每次访问仍实时判断到期、额度、token／模板撤销；不缓存含认证成品跨用户共享。
- 未知 token／他人对象返回统一不可知响应；已撤销／停用公开订阅不返回可用空配置或历史成功体。内部审计保留原因码，客户登录页可解释修复操作。
- 渲染上限参考现有 5 MiB／2 秒限制并升级为**执行中可取消**的预算检查，而非全部渲染结束后才检查。预览按用户、公开请求按 token/IP 限流；限流状态不泄露其他客户存在性。[CP-G]

## 10. 增量迁移与实施门槛（替代原 R4／R5）

### 10.1 现有能力不是从零创建

现有 `0002/0008` 已有用户、邮箱、到期、限额、启停与用户组；`0003` 是服务器粒度节点组；`0004/0012` 已有订阅、模板、不可变发布版本、组 defaults、两层覆写；`0013` 已有 quota epoch、周期起点和人工重置审计。当前模板是有限 schema，不是任意客户端配置导入；当前用户路由主要为列表／赋组，而非完整新工作流。[CP-D][CP-A][CP-Q]

### 10.2 迁移顺序

1. 保持 R0 的 0015–0018 候选／发布隔离前提；扩展迁移实际编号在实施时核对最新清单。本轮只描述“内外组与绑定 → 模板 schema v2 → 用户计划/周期 → 基础与客户方案/token”，不生成或修改任何既有 SQL。
2. 为每旧 `node_group` 创建内部资源组和外部产品组的可追溯映射；旧服务器关系进入内部服务器成员，当前实际允许的订阅入口进入外部**显式代理集合**；旧 `user_node_groups` 转用户外部组。新代理不继续自动继承旧整机授权，管理员确认此行为差异。
3. 外部静态记录沿原兼容路径，单列迁移差异；不强制伪造 managed publication。没有用户级可撤销凭据与可靠计量的静态服务不能默认售为严格额度产品。
4. 模板 v1 版本与引用保持；schema v2 新版本经验证后逐组绑定，不用 JSON 字段重解释污染历史。group defaults 归位外部组策略；`v2rayn` 旧 format 如存在，先核查真实输出，再显式迁移为 Base64 标签。
5. 旧订阅保留 ID 和 URL，通过路由兼容层解析；生成新基础授权视图并映射旧用户覆写为客户方案。旧 node/inbound override 的继承优先级保持，endpoint 安全冲突须先处理。
6. **历史自定义超过十个不能静默删掉。** 切换前显示超额清单并让客户选择保留十个，其余导出无凭据策略并确认撤销；完成前该用户冻结 legacy 覆写新增/编辑，仅保留既有链接受新授权检查的只读兼容。全量验收必须清零未迁移超额账户，不能带“十个限制只对新用户”的永久例外上线。
7. token 迁移在受控应用层生成 hash／加密新表示，保留旧明文列仅到兼容结束；不打印、导出真实 token。旧 URL 映射到升级后的同一发布策略，不能让旧链接永远保留旧节点或绕过新授权。
8. 流量保留 lifetime 和原始历史；以确认的周期边界建立新周期账本。历史无法精确分摊标记 estimated，而不是重置全部用户为零。调度与计量同期开启，先用合成跨时区／迟到样本验证。
9. 在隔离副本做影子对比：有效代理 ID、模板版本、用户凭据归属、公开字段、额度周期、旧新链接权限。切换后关闭 legacy 写旁路；回退只能回到理解新授权／草稿边界的兼容版本，不能让旧二进制重新读取草稿或整机放权。

### 10.3 分期与退出标准

以下保留 EXT 产品主线；NEXT 第 8 节叠加 R1 入口／连线原型、R2 轻量资源试验、R3 SS／授权闭环及 **DDNS 独立 R4+**，不以轻量试验或 DDNS 阻塞原 full 纵切，也不把 R0-NEXT 文档提交当完成实现。

| 阶段 | 范围 | 退出门槛 |
| --- | --- | --- |
| R0-EXT（本轮） | 完整模块方案、参考映射、数据/API/UI、验收与报告 | 用户可审阅闭环与取舍；无应用改动 |
| R1 | 保留画布原型＋新增模板/组/用户/客户工作台合成原型 | 用户完成七条故事的点击路径，确认两种角色不混淆、十个方案语义 |
| R2 | 原最小真实 Reality 画布纵切、已确认发布隔离 | 单机 ACK 前不进订阅，编辑失败保留旧版；不挪到所有订阅模块之后 |
| R3 | 原协议/链路能力；并完成内外组代理级授权、基础模板和用户凭据纵切 | 同机不同组隔离、模板绑定、建用户并交付基础订阅能实际连接 |
| R4 | 客户模板导入/手动覆写/十个槽位/稳定独立链接；完整用户生命周期/重置 | Mihomo 与 Xray JSON 基础＋覆写真实加载，撤权/超额/迟到/并发验收 |
| R5 | Sing-box/v2rayN 适配矩阵、旧数据迁移、全站视觉和异常回归 | 第 11 节覆盖完成、无 legacy 越权旁路、用户最终 UI 评审通过 |

完整产品验收不能只交付 R2 画布或 R3 基础订阅就声称满足任务书。总工期需根据原型与两个关键纵切（授权更新、模板安全导入）重新估算；不拿原 R0 总量包含本次大量新增范围。

## 11. 验收矩阵：文档覆盖与未来真实实现分开

本轮文档验收：任务书 A 对应第 0／1／8 节及 R0 保留章节；B 对应第 2–5／11 节；C 对应第 6–8／10 节；D 对应第 0.1／12 节。下面所有运行验收均是**实施完成后待执行**，本轮不打通过勾。

| 编号 | 合成场景与步骤 | 通过标准／证据 |
| --- | --- | --- |
| A1 两层强交互 | 同一服务器拖两次创建 Reality 和 SS2022 入口，无边直出；链式用 A→内部入口 B | 不跳页预建，不创建出口实体；稳定 ID；键盘连接和拖线同命令；表单仅配入口；ACK 前不进入基础及覆写订阅 |
| B1 模板生命周期 | 各格式创建/导入/导出、草稿发布、版本回退、归档与安全撤回 | AST/schema 校验、无凭据导出、版本不可变；撤回版本不从缓存输出；Base64 拒绝 DNS/规则 |
| B2 组授权 | 同机代理 A 给标准组、B 给 VIP，另有 internal 落地；用户单/多组切换 | A/B 按代理粒度隔离、内部落地不出现；runtime 拒绝未授权代理；重复节点去重、模板冲突可解释 |
| B3 用户管理 | CRUD、多外部组、限额/延期、禁用/恢复、选中及筛选批量 | 权限不提升、批量部分失败不伪报成功；到期/额度状态与基础/覆写/runtime 同步，离线撤权窗口有实测 |
| B4 计划与计量 | 月末31日、闰年、DST缺/重复小时、单次/间隔、两调度器、停机补跑、迟到跨周期上报 | 同计划只重置一次；不改到期/lifetime；历史修正不侵占新周期；计数无负数/溢出/重复收费，误差显式标记 |
| B5 客户导入/覆写 | 本人基础→导入配置→节点名/规则/客户端组/DNS修改→预览→保存→发布 | 预览脱敏；修改只影响该方案；基础不变，独立 URL 刷新后保持；来源不是粘贴任意节点即可授权 |
| B6 十个名额 | 十个保存成功，第11个拒绝；两个并发请求争最后slot；原位编辑/复制/暂停/删除 | 数据库约束有效；暂停仍占位，编辑不占位，复制占位；删除释放且旧token失效 |
| B7 链接生命周期 | 发布草稿、失败回退、固定模板、基础改节点/撤组/撤销、轮换、用户到期 | 链接稳定且内容按合法发布更新；固定模板不冻结权限；撤销衍生全部拒绝，不输出缓存成功体 |
| C1 越权与恶意输入 | 合成两客户互换对象ID/模板引用、导入 provider/脚本/别名炸弹/超限、修改 endpoint/认证字段 | 401/403/404/413/422 按契约；无任意服务器请求/文件访问/代码执行；无服务端私钥或他人凭据 |
| C2 一致性与可恢复性 | 乐观锁冲突、重复publish、组草稿增删、授权更新与渲染并发、下游部署失败 | 组草稿不进runtime/订阅/缓存；混合发布新增等ACK、撤权先阻断；旧草稿/回执/回滚不恢复撤权；错误定位且可重试 |
| D1 UI与可达性 | 桌面浅/深、窄屏、键盘、读屏、加载/空/错、10/10满额 | 3x-ui风格统一，客户与管理员导航隔离，真实截图和交互记录；不把 build 成功当视觉验收 |

### 11.1 客户端真实加载与连接硬门槛

在隔离测试资源中创建合成用户、代理和临时凭据，不使用生产账户／真实订阅作为样例。验收记录客户端 **名称、精确版本、binary/container digest、模板版本、格式/协议矩阵、脱敏结果**；实施时锁定可用版本，本轮不虚构“最新版本”。

| 客户端验收 | 基础订阅＋覆写订阅必须分别执行 |
| --- | --- |
| Mihomo | 获取 YAML，`mihomo -t -f <synthetic-config.yaml>` 成功；启动本地实例，经其 SOCKS/HTTP 代理访问隔离目标，验证实际出站路径；覆写命名/DNS/规则/组行为按设计改变 [CL-M] |
| Xray core | 获取**完整 JSON 对象**，`xray run -test -config <synthetic-config.json>` 成功；以 loopback inbound 启动，逐一验证其声明支持的代理协议与路由/DNS；不是把 JSON parse 成功算内核通过 [CL-X] |
| v2rayN＋明确的核心版本 | 导入 Base64 URL，核对实际节点数量/名称/可连接协议；更新同一 URL 后收到合法变更；DNS/规则只能验证客户端自己的设置，不声称 Base64 传了完整配置 |
| Sing-box | `sing-box check -c <synthetic-config.json>` 后启动并经本地代理访问；记录精确 schema/核心版本，不依赖未知版本自动兼容 [CL-S] |

硬性追加：基础与覆写输出都不得包含他人代理；自动更新后收到新确认节点及允许修改；更换基础授权／用户到期／超额／方案删除后再次拉取拒绝，runtime 在测得并批准的窗口内拒绝旧凭据。无法支持的协议要明确报告 excluded／不支持，不能输出无法加载的节点然后勾选“全客户端兼容”。

凭据在受限临时文件中仅用于测试，结果报告不附完整配置、token、UUID/password；环境清理临时客户端、测试监听和文件。不在本次方案任务中启动这些验收环境。

### 11.2 可复用的项目测试落点（未来）

后端沿现有 `backend/internal/generator/p2_regression_test.go`、`backend/internal/repo/subscriptions_p2_integration_test.go`、`backend/internal/repo/users_integration_test.go`、`backend/internal/traffic/traffic_test.go` 增加格式、授权、名额并发与周期边界用例；拓扑发布沿 R0 原回归。前端目前没有 test 脚本，下一阶段需单独批准测试能力建设，不在文档任务中新增框架。[CP-A][CP-G]

## 12. 参考映射与可追溯证据

下列为实际查阅的来源；本地路径＋行号定位静态实现，官方地址只指向公开资料。只总结产品职责／交互，不搬用实现、页面文案、样式、图标、截图或品牌资产。

### 12.1 Remnawave 官方资料

| 标识 | 页面及已确认内容 | sing-ui 采用／差异 |
| --- | --- | --- |
| [RW-P] | `Config Profiles`：完整服务端 Xray 配置、入站、节点选择 Profile | 对应运行配置聚合职责；不取代画布单代理编辑，更不作为客户端模板 |
| [RW-T] | `Templates`：客户端四家族、按客户端输出、多个模板；Base64 不提供完整模板 | 客户端适配器、版本模板选择；sing-ui Base64 只有列表策略；版本/导入安全契约由本项目设计 |
| [RW-S] | `Squads`：内部组选入站、用户多内部组；外部组 Templates/Settings 覆写 | 内部授权与外部体验分离；本项目显式资源池、多外部组、模板冲突规则是扩展，不假称上游原样支持 |
| [RW-U] | `Users`：Traffic & Limits/Access Settings、到期、重置、组、列选择/筛选/批量、订阅URL | 用户页信息布局与交付按钮；本项目现有 roles、quota epoch、调度一致性不从上游拷贝 |

资料定位（2026-09-08 通过公开 HTTPS 取得 article 正文；web 检索工具未返回可引用结果，改为直接核对官方正文，未登录实例）：

```text
[RW-P] https://docs.rw/learn-en/config-profiles
[RW-T] https://docs.rw/learn-en/templates
[RW-S] https://docs.rw/learn-en/squads
[RW-U] https://docs.rw/learn-en/users
[CL-M] https://raw.githubusercontent.com/MetaCubeX/mihomo/Meta/main.go
[CL-X] https://xtls.github.io/document/command.html
[CL-S] https://sing-box.sagernet.org/configuration/
```

Remnawave URL 参考的是“用户创建后交付稳定订阅链接、浏览器和客户端可有不同响应”的产品模式；没有将其未核实的内部 API/token schema 写成 sing-ui 依赖。

### 12.2 SubBoost 静态源码（相对 `/opt/data/workspace/tmp/ref-subboost/`）

| 标识 | 具体证据 | 可借鉴／不继承的边界 |
| --- | --- | --- |
| [SB-U] | `packages/ui/src/product/converter/source-editor-dialog.tsx:88`；`packages/ui/src/store/config-store/definitions.ts:183`；`packages/ui/src/product/converter/quick-mode/constants.ts:13` | 来源状态、快捷/高级、命名/过滤、代理组/规则/DNS策略；不照搬任意节点输入成为sing-ui权限 |
| [SB-T] | `packages/ui/src/templates/template-upload-dialog.tsx:78`；`packages/ui/src/store/config-store/actions/template-actions.ts:139` | 模板仅描述策略、应用不改nodes/sources；本项目客户模板与基础授权同样分离 |
| [SB-PIPE] | `packages/core/src/parser/content-parsers.ts:153`；`packages/core/src/parser/platform/parse-platform-config.ts:43`；`packages/ui/src/store/config-store/generated-yaml.ts:53`；`packages/core/src/generator/index.ts:564` | 多来源解析→规范化策略→重新生成YAML；它的链接/Surge/Loon/QX支持不等于本项目原生JSON模板导入已经实现，也不等于采用subconverter服务 |
| [SB-P] | `local/prisma/schema.prisma:23`；`local/src/lib/subscription-service.ts:121`、`:433`；`packages/ui/src/product/home/subscription-link-dialog.tsx:78` | 服务端模板/订阅加密字段、按token重新生成、编辑保持URL；不复制节点明文作为授权事实；十个名额是本项目新约束 |
| [SB-LOCAL] | `packages/ui/src/store/config-store/persistence.ts:69`；`packages/ui/src/store/config-store/definitions.ts:60`；`packages/ui/src/product/converter/use-subscription-sources-controller.ts:102` | localStorage与服务端保存分开；源类型配额不是总方案配额；不能误写SubBoost“最多10模板” |
| [SB-PROVIDER] | `packages/ui/src/store/config-store/source-actions.ts:214`；`packages/core/src/subscription/proxy-providers.ts:12` | provider模式仅写客户端provider声明，不等于服务端已验证其节点；sing-ui首版禁止客户外部provider |

SubBoost `package.json:4` 标识 `AGPL-3.0-only`，`LICENSE:1` 为 AGPL v3；3x-ui `LICENSE:1` 为 GPL v3。本轮仅借鉴产品交互和职责，不复制代码／资产，不提供或宣称已完成许可证兼容性意见；未来如决定引入或改编源码，须单独评审许可证义务，不能因依赖使用 MIT 就视参考项目自身为 MIT。

### 12.3 3x-ui 与本仓库

| 标识 | 文件证据 | 本轮用途 |
| --- | --- | --- |
| [UX-3] | 3x-ui `frontend/src/hooks/useTheme.tsx:35`；`frontend/src/layouts/AppSidebar.tsx:174`；`frontend/src/styles/page-shell.css:1`；`frontend/src/pages/inbounds/form/InboundFormModal.tsx:1086` | 沿 R0 的主题、布局、表格/表单设计证据；不声称本轮运行UI或复制组件 |
| [CP-D] | `backend/internal/db/migrations/0002_init_users.up.sql:2`、`0003_init_groups.up.sql:2`、`0004_init_subscriptions.up.sql:2`、`0008_auth_authorization.up.sql:1`、`0012_p2_templates_overrides.up.sql:1`、`0013_p2_traffic.up.sql:1` | 确认字段/表已存在后增量扩展，避免把已有P2版本发布说成从零开发 |
| [CP-A] | `backend/internal/api/router.go:93`；`backend/internal/api/admin/access.go:85`；`frontend/src/App.tsx:79`；`frontend/package.json:6` | 现有API/页面/权限、缺少前端test脚本；新客户工作台不虚报已有 |
| [CP-G] | `backend/internal/generator/client.go:10`；`backend/internal/generator/p2.go:31`、`:96`、`:109`；`backend/internal/generator/overrides.go:17` | 当前三格式、有限模板AST、宽旧覆写与优先级、5MiB/2秒生成后检查 |
| [CP-Q] | `backend/internal/traffic/quota.go:11`、`:46`；`backend/internal/traffic/traffic.go:217` | 现有人工额度/重置与按当前账户入账；计划重置及迟到周期归属需新增 |

## 13. 本轮交付、验证与限制

- 交付 `docs/REARCHITECT-EXT.md`；原 `docs/REARCHITECT.md` 只新增修订入口及冲突覆盖说明。没有改动应用源码、SQL迁移、Agent、依赖或部署配置。
- 已运行既有前端 `npm run lint`（exit 0，16条既有warnings）和 `npm run build`（exit 0，1条chunk大小warning）；未修无关告警。前端无现成test脚本，未添加框架。
- Go 在 PATH 和标准安装路径不可用，Go tests/build 未执行；没有安装／下载工具链或依赖、连接数据库、运行参考项目或生产服务。客户端运行验收全部是第11节未来门槛，不是本轮通过结果。
- 文档结构、覆盖、路径引用及最终 diff 由主线程检查，验证证据在 `tmp/rearchitect-ext-validation/`；保留且未读取、修改、暂存既有 `backend/tmp_genhash_main.go`。
- **报告路径冲突**：用户消息要求 `/opt/data/workspace/tmp/rearchitect-ext-report.md`，任务书验收条目要求 `/opt/data/workspace/panel-design/tmp/rearchitect-ext-report.md`；本会话工程规则限定只写选定项目。故报告写项目内 `tmp/rearchitect-ext-report.md`，不越界写外部路径，不声称外部路径已交付。
- 按工程规则验证后仅提交修订文档并推送项目 `origin/main`；实际commit/push结果、未运行验证和交付路径写入本地报告。报告及验证日志在已忽略的 `tmp/`，不强制加入Git。提交修订稿不是批准实施。
