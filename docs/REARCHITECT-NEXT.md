# sing-ui R0-NEXT：品牌、分层 Agent 与入口／连线修订方案

> 日期：2026-09-08。状态：**产品方案修订稿，未批准实施**。三项产品决定已纳入设计；本文不代表功能已经实现或资源指标已经达标。本轮只修改文档，不修改应用源码、SQL、依赖、Agent、部署或生产服务。
>
> 已完整阅读本轮任务书和 `docs/REARCHITECT.md`、`docs/REARCHITECT-EXT.md` 各 512 行原稿。当前工程静态基线 `ef2c8d4`，本地 3x-ui 基线 `2ec6c73`；保留原稿历史证据日期，不将旧轮次测试记成本轮通过。当前验证和交付结果见 `tmp/rearchitect-next-report.md`。

## 0. 本次决定及覆盖顺序

**产品叫 sing-ui；服务器有完整／轻量两种 Agent；入口通过创建表单生产，出口通过流程卡连线定义。** 两层节点模型、服务器拖入画布、同机多代理、模板、内部／外部分组、用户生命周期和客户十个覆写方案的既定体系不推翻。

冲突优先级：**NEXT → EXT → R0**；只覆盖下表明确变更，其他已确认约束继续有效。

| 原章节 | 本次替代／补充 | 不变的边界 |
| --- | --- | --- |
| R0／EXT 文档品牌、README 标题 | 产品与未来 UI／仓库目标名统一 sing-ui；第 1 节限定迁移边界 | 本轮不重命名远端仓库、运行路径、模块或环境变量 |
| R0 3.1 一机一个 sing-box 的绝对约定 | 一机一个受管 Agent 身份；full 管理 sing-box，lite 管理经过验证的单协议运行时 | 同一个 ServerNode，不新增第三层节点或轻量机器表 |
| R0 4.1–4.5、5.4 表单中的出站选择器 | 创建入口默认 direct；连线产生 chain；出站摘要只读 | 同一画布创建、编辑、保存草稿、明确确认发布 |
| R0 6–8 编译、能力和 Agent 协议 | 增加 profile-aware renderer、能力版本、轻量配置编码及受控注册 | 服务器级聚合、幂等、版本校验、匹配 ACK 后晋升 |
| EXT 1、2.2、3、4、6–11 | 增加轻量协议／计量能力门禁、NAT 地址和分期验收 | 内外组授权、用户独立凭据、模板隔离、覆写十个名额 |
| EXT 10.3 分期 | R1 交互确认；R2 轻量资源试验；R3 轻量闭环；DDNS 独立 R4+ | 不将 DDNS 或新运行时试验阻塞现有 full 纵切 |

## 1. 改名范围：文档现在改，部署另行执行

- **本轮落地**：本方案、R0、EXT 的产品称呼和文档标题统一 `sing-ui`，README 更名并提供方案入口；未来页面标题、导航品牌、安装包展示名和新文档使用同一拼写，不混用 SingUI／singui。
- **后续目标**：GitHub 仓库目标名 `sing-ui`，完整 Agent 展示名 `sing-ui Agent`，轻量版 `sing-ui Lite Agent`；拟议二进制名 `sing-ui-agent`／`sing-ui-agent-lite`。这些不是本轮已存在的安装命令。
- **本轮不执行**：GitHub 仓库 rename、origin 改址、Go module/import 改名、镜像／包名、Compose service、容器／卷、systemd unit、数据库名、环境变量前缀、域名和订阅路径变更。UI 品牌实际代码也不改。
- **兼容字面量保留**：现有命令中的 `COXPANEL_*`、旧数据库标识、历史任务书路径和当前远端 `github.com:huanxiaofu/coxpanel` 是原项目兼容标识，不机械替换成尚不能工作的命令。历史证据 ID 如 EXT 的 `[CP-G]` 继续有效，不是产品品牌。
- **实施改名检查表**：另行批准仓库 rename → 确认旧地址兼容与 CI／镜像引用 → 更新项目 origin 和文档示例 → 验证新 clone/build → 逐项灰度运行标识。保留用户、服务器、代理稳定 ID、现有订阅 token／路径和数据卷；不可通过改品牌重建数据库。失败回退须保留兼容入口，不能承诺上游永久重定向。

本次提交仍推送当前项目 `origin/main`，不提前更换远端。旧名称在兼容命令和来源路径中的出现须明确标注，不算漏改产品品牌。

## 2. Agent 分层与进程架构

### 2.1 两种形态，同一个管理面

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

## 3. 数据模型与能力增量

只追加逻辑字段设计，实际迁移号在实施时检查；不改已应用迁移，不建立 `lite_servers`、`egress_nodes` 或第二套授权表。

| 所属模型 | 拟新增字段 | 约束／权威来源 |
| --- | --- | --- |
| ServerNode（旧 managed nodes 投影） | `agentProfile=full/lite/unknown`、`agentProtocolVersion`、`runtimeKind/runtimeVersion` | 注册准入＋已认证握手确认；旧机器先 unknown，经匹配现有部署证据确认为 full，不能盲填 lite |
| ServerNode | `capabilitiesRevision`、`capabilitiesObservedAt`、结构化 `capabilityDetails` | 保留旧 `capabilities[]` 兼容；未知／过期不视为支持；管理员不能手勾协议冒充 Agent 事实 |
| capabilityDetails | `protocols[{name,methods,networks}]`、`egressModes`、`canBeChainTarget`、`deploymentModes`、`configFormats` | 面板取“上报∩适配器支持∩平台策略”；lite 首版 `egressModes=[direct]`，chainTarget 另行准入 |
| capabilityDetails | `userAuthMode`、`trafficScope`、`supportsAuthLease`、`supportsSessionRevocation`、`limits` | 用于外部组分配和产品准入；没有遥测能力返回 unknown，不报零；limits 是校验上界而非建议 |
| ServerNode | `networkKind=public/nat/unknown`、`memoryLimitBytes`、`resourceObservedAt` | NAT 标记可由管理员说明并保留来源；内存由 Agent 观测 cgroup／宿主限制，时间可见 |
| ServerNode | `natPortMappings[]`：transport、publicAddress/publicPort、listenPort、状态／来源 | 管理员登记已有映射，不自动创建云 NAT／UPnP；端口属于此服务器可用映射池 |
| ProxyNode | 复用 `advertisedAddress/Port`，补 `chainDialEndpointRef`（可空）及公开地址来源 | advertised 给客户端，chain dial 给上游；监听地址不等于两者。引用绑定目标代理＋版本，不接受任意出站 URL |
| ProxyNode／publication | `runtimeAdapterVersion`、发布时能力 revision、公布 endpoint 快照 | 草稿／期望／已确认三态仍独立；endpoint 更新走同一版本和影响预览 |
| Agent 发布／流量状态 | format/schema、generation/hash、ACK phase；流量 epoch/sequence、采样区间、gap | 复用已有发布表与去重逻辑，只增必要列；不可将内部代理转发量重复计入客户额度 |
| 地址预留（R4+） | `addressMode=static/domain`、`ddnsPolicyRef=NULL`、`observedPublicAddresses`、`addressObservedAt/addressRevision` | 本阶段仅字段／接口预留；Agent 观测无权直接改管理员域名或触发 DNS 写入 |

更换 profile 不是资产 PATCH 改一个枚举：先检查在用代理、方法、容量、计量、链路与凭据兼容，展示差异；停用／迁移不兼容代理，再在受控重新注册／配置确认后切换。能力减少使预检失效、阻止新发布并告警；既有合法服务不因一份短暂缺字段的心跳被自动删掉，亦不把未知状态显示成功。

## 4. Agent API：路径复用，轻量编码协商

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

lite 初版实现上述有界 prepare/apply/ack；未达到者**不允许连成受管链路目标**，也不得复用 full 的强确认标签。full → lite 末端的联合发布仍 prepare 所有参与服务器、下游先 apply；不把末端成功直接当整个链成功。流量客户计费只在授权入口计一次，内部末端流量是运维遥测。

## 5. NAT 与 DDNS：管理在线不等于入口可达

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

## 6. 核心交互：入口＝创建，出口＝连线

### 6.1 画布唯一可连接实体是入口卡

**入口在服务器上创建，是 ProxyNode 生产单元；出口不是另一种可创建资源，是该入口的流量去向。** “入口卡”包括 subscription 入口和 internal 入站；internal 不等于允许向客户公开。沿用 R0 的双用途限制，不能把用户入口静默变成共享内部目标。

```text
左侧服务器资产（full／lite、NAT、能力）
  拖服务器 → 创建入口表单 → 保存草稿／创建并应用
画布
  [A：Reality／服务器 full] ──下一跳──> [B：SS／服务器 lite／internal]
  A 有出边：chain                         B 无出边：本机直出
```

第一版 direct 不需要第二台机器或第二个落地入口。画布没有“创建出口”按钮、OutboundNode 表、直出服务卡或可持久化的 direct 节点。可在拖线菜单提供“本机直出”**动作／临时投放区**；选择它等价于清除出边，动作完成不留下卡片／边记录。

### 6.2 一个路由真相与编辑规则

| 操作／状态 | 草稿语义 | 应用与校验 |
| --- | --- | --- |
| 创建入口 A，尚无连线 | `egress_mode=direct`，无目标，无出边；卡内显示本机直出 | 创建表单不要求选择出口；单机确认即可发布 |
| 从 A 拖线到入口 B | 一个规范边 A→B；投影 `egress_mode=chain,targetProxyNodeId=B` | B 必须 internal、能力／地址就绪；改草稿不立即改 runtime |
| 从 A 改接 C | 原子替换 A 的出边，版本冲突返回 409 | 不产生两个下一跳；确认受影响服务器后发布 |
| 删除 A→B 或选择本机直出 | 同事务清边／目标并投影 direct | **显式警告流量改为本机出网**；不自动应用；已运行链路保持至确认成功 |
| B 离线／失败／被停用 | 连线仍在，标不可用或阻止新发布 | 不自动删边回落 direct，避免流量绕过既定出口 |
| 隐藏卡片／收起服务器分组 | 仅布局；隐藏相关连线可用摘要指示 | 不改 egress、不拆链、不触发配置发布 |
| 删除目标 B | 返回依赖清单；必须先明确重连／停用上游并确认发布 | 禁止级联删边造成隐式直出；历史引用保留 |
| 键盘／小屏连接 | 选源卡“连接到…”再选目标，与拖线提交同一 Connect 命令 | 是连线的无障碍替代，不回到创建表单另存出站对象 |

**边是唯一出站目标权威**，egress 是同一 graph revision 的派生投影／兼容字段；不在表单 payload 留第二份可冲突的 target。历史 API 同时提交 egress 与 edge 不一致时明确拒绝，不任选一个覆盖。`direct` 与零出边一一对应，`chain` 与恰好一条合法出边一一对应。

保留原图约束：无自环／有向环、同一链不重入服务器、最多 8 个入站、一个入口最多一个下一跳、既有 relay 复用限制不放开。同服务器两个入口可独立创建，但本轮不允许它们互连绕过服务器重入校验。lite 源端拖线按钮禁用并解释 direct-only；服务端也拒绝伪造请求。full → lite/internal/direct 仅在第 4.2 节联合发布、协议拨号及可达性验收后开放；lite → full 和 lite → lite 首版不支持。

### 6.3 创建与连线两条主路径

1. **单机直出**：拖 full 的 HK-zouter → 3x-ui 式 Reality 入口表单 → 协议／端口／SNI／密钥校验 → 摘要“尚未连线，本机直出” → 创建并应用 → 等匹配 ACK → 入口卡已生效。不要求访问另一页面或创建落地。
2. **NAT 单机 SS**：拖 lite 服务器 → 只列已通过的 SS 方法 → 选可映射端口、检查公布地址 → 创建并应用。UDP 不满足时只允许 TCP 并明确标记，不能先生成不可用订阅。
3. **两入口链式**：创建 A 和 B 的入口草稿，B 用 internal → 在画布连 A→B → 汇总两台完整配置和授权影响 → “应用 2 项变更”。临时 clientRef 在同一操作解析成稳定 ID，不强迫先把 A 直出发布到生产才能连线。
4. **既有卡改链**：双击编辑只改入口；源卡连接把手／菜单改关系 → 差异预览 → 明确应用。取消表单或撤销未应用连线不改变既有运行配置；布局 undo 不等于运行回滚。

### 6.4 3x-ui 式分区表单与具体吸收

参考本地 `InboundFormModal.tsx` 的基础／协议／传输／安全／高级分区、提交 loading、错误切换页签和 Modal 组织；sing-ui 画布用 Drawer、列表用 Modal，共享同一个入口表单。**原项目用协议 Select＋分区 Tabs，不误写成每个协议已经有顶层 tab；sing-ui 的协议快捷 tabs 是在其范式上新增的产品设计。**

| 组件／分区 | 第一版字段与动作 | 能力／安全／对齐 |
| --- | --- | --- |
| `InboundProtocolTabs` | Reality／Shadowsocks／Hysteria2 快捷选择；内部表单为基础、协议、安全、高级 | 按 ServerNode 能力显示可用项；未知／不支持给原因；切协议确认清理不兼容字段，不迁移秘密 |
| `ProxyBasicFields` | 服务器只读、入口名、用途、监听端口；NAT 显示映射公网端口 | 已发布入口不能直接换机器／协议；复制先新 ID、新端口、新材料，不隐式迁移 |
| `PortAvailabilityField` | 端口范围、同机冲突定位、可选空闲端口、NAT TCP/UDP 映射提示 | 面板事务预检＋Agent bind 检查；不照抄随机端口就当可用；冲突在字段和页签显示 |
| `RealityInboundFields` | SNI、dest／握手目标 host:port；生成 X25519、Short ID；公钥复制 | 对齐 sing-box 的 sni/target；“将 SNI 填为目标 host”“从目标提取 SNI”是显式快捷动作，保留独立编辑，不自动扫描公网候选 |
| `ShadowsocksInboundFields` | method、生成适当长度密码、网络、用户认证模式／容量提示 | full 维持 SS2022；lite 只显示已验证交集；不要求 SNI／证书；独立用户秘密继续由授权流程管理 |
| `TLSCertificateFields` | 证书引用／有效期／域名覆盖提示；生成或导入入口按能力展示 | 首版沿用已登记 certificateRef；“生成自签测试证书”仅后续受控能力，明确不受客户端默认信任；ACME／自动签发未实现不显示可用按钮 |
| `AdvancedProxyFields` | 监听地址、advertisedAddress/Port、NAT 提示、受控链路地址引用 | 域名可手填，DDNS 显示后续而非已启用；不得提交任意文件路径／核心 JSON |
| `SecretGenerateButton` | 生成／轮换、加载、失败重试；取消不毁旧材料 | 临时 secretRef 绑定服务器／操作者／用途，过期可清理；私钥不进卡片、GET、localStorage、日志 |
| `EgressSummary`（替代 EgressEditor） | 只读本机直出／下一跳＋“在画布连接”定位动作 | **没有 direct/chain 编辑器或目标选择字段**；键盘连接仍由画布命令统一维护边 |
| `DeploymentSummary` | 能力、端口、地址、待应用差异、影响范围、逐机进度 | 隐藏页签错误自动定位，失败保留输入；202 只显示已受理，不等于已生效／公网可连接 |

吸收“生成密钥／证书”的操作分区，不照搬参考工程的 Xray schema、明文私钥展示、任意目标扫描或证书路径。SNI/dest 快捷填写不发网络请求，不将 example.invalid 当推荐目标；若以后加入握手探测／签发，单独批准权限、出网与秘密处理设计。

## 7. 前端与 EXT 产品体系的影响

- **服务器资产／详情**：full／lite、NAT、内存容量与观测时间、支持方法／TCP/UDP、可创建入口数、认证／计量等级。安装说明分 Docker 与原生两档，但本轮只规划，不写可执行远程安装脚本。
- **代理卡**：卡标题始终是入口名，副标题是服务器；协议、公布 endpoint、只读出口摘要和 ACK 状态。lite direct-only 源把手不可用；可接入目标把手是否可用另按门禁判断，不能“lite 都能连”或“lite 永远不能作为末端”一刀切。
- **内外分组**：内部资源组可纳入任意 profile；外部组只能选可订阅且发布／用户隔离／计量／租约合格的代理。full/lite 标签不能变成自动授权规则；新代理不继承整机公开权限。
- **用户与流量**：保留 EXT 到期、额度、重置周期、延迟报告、撤权流程。轻量断网／缓冲满显示 stale/gap；旧流量不会因重连或重置重复收费。链路末端内部计数不作为第二份用户消费。
- **模板／客户覆写**：模板不是 Agent 配置；客户无法选运行时、启用 DDNS、改拓扑／私钥或向未授权入口连线。稳定代理 ID、十个保存名额、基础与自定义链接和实时授权约束全保留。
- **原型验收**：浅深色、小屏、键盘“连接到…”路径、错误页签定位、NAT 映射不足、能力未知／过期、负载上限、未保存离开和焦点返回都要演示；不能以 lint/build 通过替代交互认可。

## 8. 分期、依赖与回退

| 阶段 | 本次增量 | 退出门槛 |
| --- | --- | --- |
| R0-NEXT：本轮方案 | 品牌与三项新决定；修订 R0／EXT；本地报告 | 文档范围完整、无应用源码改动；不是功能已交付 |
| R1：交互评审 | sing-ui 品牌原型、full/lite/NAT 资产、入口表单、连线定义出口 | 用户完成单机直出、两卡链式和键盘替代操作；没有创建出口页 |
| R2：full 真实纵切＋lite 独立试验 | 原 Reality／ACK 隔离继续；轻量候选资源／方法／授权对照 | 确定运行时、最低真实内存和不支持项；不为了等 DDNS 延迟 full 纵切 |
| R3：轻量闭环＋原协议／授权 | 同 ServerNode 注册、能力门禁、SS direct、原生部署、流量／租约；合格 full→lite 末端 | 与 EXT 内外组／基础订阅纵切一起验证；不合格运行时保持实验禁售 |
| R4：EXT 完整客户／用户体系 | 模板导入、手动覆写、十个方案、生命周期／重置继续 | 保留 EXT 原退出标准；DDNS 不自动成为本阶段必交项 |
| R4+：单独批准 DDNS | 地址观测、provider 策略、更新／确认／回退 | 独立设计和真实 DNS 验收后才开放，不混入 SS 首版 |
| R5：兼容迁移与全站收口 | profile 回填、旧接口门禁、客户端矩阵、文档／品牌迁移检查 | 旧 ID／链接／授权／计量不丢，旧入口无越权旁路；运行重命名单独窗口执行 |

增量实施顺序：能力／profile 可读兼容 → 新 renderer/configFormat 与候选隔离 → 注册和 lite 原生运行试验 → 发布／授权 ACK → 前端能力门禁 → 客户产品准入；从未实现新字段的旧 Agent 不自动推断支持。新能力下线先冻结新写入并处理在途发布，再回到理解 publication／授权边界的兼容版本；不能将旧 full 二进制直接用于 lite 配置，也不能把轻量服务无提示迁回更重运行时。

资源评估、原生发布、方法适配和 NAT 可达性是新增工作包；原 R0 的 29–44 人日及 EXT 估算边界不包含这些任务。R2 后按实测和确定运行时重新估算，不编造本轮总工期或稳定吞吐保证。

## 9. 验收清单：方案覆盖与未来运行证据分开

### 9.1 本轮文档验收

- [x] sing-ui 产品命名、README／方案入口一致，旧运行兼容字面量有说明。
- [x] 完整 Agent 保留；轻量架构、内存目标、协议方法、数据、API、前端、授权与 DDNS 预留齐全。
- [x] 入口＝创建、出口＝连线；direct 不建第二卡；egress 与边唯一权威一致。
- [x] EXT 模板、内外组、用户、客户十个覆写方案不被新形态绕过。
- [x] 所有功能／真实小内存及 NAT 验收列为待实施，不以文档测试假称运行通过。

“未改源码”、检查结果与 commit/push 以最终 diff 和报告证据为准，不在写作中预先宣告通过。

### 9.2 实施后必须执行，当前均待验证

| 编号 | 实机场景／操作 | 验收证据 |
| --- | --- | --- |
| L1 原生形态 | 64 MiB NAT 实机无 Docker 安装、重启恢复、只开 SS | 完整进程树／版本、无容器依赖、ServerNode 同管理面注册；不使用生产机器 |
| L2 资源 | 按 2.4 节双候选同负载对照，24h、100 次应用／回滚、控制面断连再恢复 | RSS/PSS、整机／cgroup 峰值、吞吐／时延、无 OOM／持续增长；32 MiB 未达则不得标通过 |
| L3 能力门禁 | lite 请求 Reality/Hy2、未支持 method/UDP、超入站／用户数、伪造 chain | UI 原因明确，API schema／能力拒绝；旧 full 路径无回归 |
| L4 NAT 可达 | TCP 映射正确／错误、仅 TCP 无 UDP、无映射、仅私网末端 | 从授权隔离客户端／上游实际拨号；管理在线不冒充可连接；无路可达阻止发布 |
| L5 发布恢复 | prepare／apply 失败、断电、旧 ACK、重复请求、回滚失败、容量不足 | 旧版本保留，匹配 ACK 才晋升；manual_required 不标成功；订阅无失败草稿 |
| L6 用户边界 | 两客户不同授权、到期／禁用／超额、断网超过租约、旧连接持续 | 未授权认证失败，本地失效及连接终止窗口实测；不共享密码模拟隔离 |
| L7 计量 | 重发批次、Agent 重启、spool 满、跨额度周期迟到、入口＋末端同时上报 | epoch/sequence 幂等、gap 可见、历史按采样修正、只计授权入口一次 |
| U1 单机直出 | 拖 full 建 Reality；拖 lite 建合格 SS | 创建表单不选出口，只有入口卡；无边＝direct；ACK 前不向客户发布 |
| U2 画布关系 | A→B、改接、删线、B 离线、隐藏卡、删除 B、撤销未应用连接 | 无双目标／环／同机重入；删线直出有确认；离线不隐式 direct；布局不改运行 |
| U3 混合链路 | full A→合格 lite B/internal/direct；伪造 lite 作 relay | 下游先 apply、匹配全链 ACK；末端能力不足拒绝；客户端实际验证出网路径 |
| U4 表单与辅助输入 | 协议切换、密钥失败、证书缺失、SNI/dest 快捷、端口冲突、键盘／手机 | 分区定位、输入保留、能力解释、无秘密外泄；出口由同一 Connect 命令维护 |
| B1 完整产品回归 | 内外组、基础／客户订阅、四格式矩阵、十个名额、撤权和模板撤回 | EXT 全部原门槛继续；lite 不扩大授权、不多发无效格式／用户秘密 |
| D1 后置边界 | 检查 lite 首版无 DDNS worker/provider 写请求 | 只手工域名／字段预留；R4+ 独立批准和 DNS 实测前不显示 DDNS 已可用 |

所有运行试验只在另行授权的隔离资源上使用合成用户／凭据；报告不包含私钥、真实订阅、UUID/password 或完整可连接配置。本轮不启动这些实机／客户端／数据库验收。

## 10. 来源、证据与本轮限制

| 标识 | 本次读取依据 | 使用边界 |
| --- | --- | --- |
| TASK | 项目外只读任务书，原兼容文件名见下方代码块 | 用户三项产品决定与交付要求；只在 panel-design 写文件 |
| R0／EXT | `docs/REARCHITECT.md`、`docs/REARCHITECT-EXT.md`，原稿全文各 512 行 | 保留原确认体系，本稿明确替代点；历史验证不是本轮结果 |
| LOCAL-AGENT | `agent/Dockerfile:1`、`agent/cmd/agent/main.go:821`、`shared/contract/messages.go:18`、`backend/internal/api/router.go:70` | 当前容器／sing-box、心跳能力和四个真实 Agent 路径；profile/注册/轻量 schema 是新增设计 |
| LOCAL-SS | `shared/config/renderer.go:583`、`shared/config/renderer.go:729` | 当前 SS2022 方法和用户凭据适配边界，不推断其他 SS 实现也满足 |
| UX-FORM | 本地 3x-ui `frontend/src/pages/inbounds/form/InboundFormModal.tsx:551`、`:595`、`:635`、`:1086` | 保存、校验跳页签、协议 Select 和分区 Modal；没有运行参考 UI |
| UX-SECURITY | 3x-ui `frontend/src/pages/inbounds/form/security/reality.tsx:102`、`:171`、`:209`、`:249`，`useSecurityActions.ts:29` | 目标／SNI／Short ID／生成动作；不复制原实现和品牌资产 |
| UP-SB | sing-box 官方 build-from-source 文档 | 可选构建特性，不提供本文内存门槛的通过证据 |
| UP-SS | shadowsocks-rust 官方 README 的 ssserver／构建特性章节 | 候选实现资料；不称已完成版本锁定、多用户验收或内存实测 |

为可追溯保留原始定位（旧文件名／仓库标识不是新品牌）：

```text
任务书：/opt/data/workspace/tmp/coxpanel-rearchitect-next-task.md
3x-ui：/opt/data/workspace/tmp/ref-3xui/
UP-SB：https://sing-box.sagernet.org/installation/build-from-source/
UP-SS：https://raw.githubusercontent.com/shadowsocks/shadowsocks-rust/master/README.md
```

外部资料于 2026-09-08 只读核对；web 检索调用未返回可引用正文，改以公开 HTTPS 获取官方构建／README 相关段落。没有据此声称候选是最新版本或引用任何第三方内存数字；未来 R2 要锁定版本与摘要。本轮不重新认证 EXT 的旧外部资料，也未读取凭证、连接数据库、运行参考项目或接触生产。实际测试／构建限制、文档提交和推送状态记录在本轮报告。
