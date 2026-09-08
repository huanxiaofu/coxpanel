# Coxpanel P2 设计增量文档

> 版本：v0.1 评审稿
> 日期：2026-09-08
> 配套：`PRD.md`（需求）、`TECH-DESIGN.md`（技术设计）、`SKELETON-DESIGN.md`（骨架）
> 基线：P1 工作树与迁移 `0001`～`0010`；本文仅定义 P2 增量，不包含实现或上线操作。
> 状态：待 Hermes 评审；第 10 节是后续实现的验收要求，不代表本次已经实现或通过真实业务验收。

---

## 1. 总体目标与范围边界

### 1.1 目标

让管理员能够编排并可靠发布多级中转链，让用户能够选择模板、编辑订阅覆写、查看可信用量并收到邮件提醒。继续遵守「面板生成配置、agent 原子应用」「订阅配置与节点运行配置分离」「节点组授权不被模板或覆写绕过」三条原则。

以 `PRD.md` 5.2 和本次任务书确定 P2 范围；`TECH-DESIGN.md` 8 节将告警列为 P3 的旧排期，以本文调整为 P2 邮件告警，Telegram Bot 仍属 P3。不重写三份基础文档，不把草案中尚未实现的能力当作可直接复用的 P1 能力。

### 1.2 模块边界

| 模块 | P1 复用 | P2 做 | P2 不做 |
|---|---|---|---|
| 多级中转 | React Flow、入站角色、单节点草稿/预览/显式部署、配置哈希与 agent 原子应用 | 入口→中转→落地及更多中转；跨节点一致性校验、依赖发布、失败恢复、应用回执 | 多落地负载均衡、自动故障切换、任意分流、跨机器瞬时原子切换 |
| 模板 | `templates`、`subscriptions.template_id` 已有表字段，mihomo/base64 生成器 | 模板定义、版本、发布、复制、切换；mihomo 自定义，补齐 sing-box 客户端格式；base64 保留有限定制 | 任意脚本/文件读写、模板执行网络请求、远程规则订阅、v2rayN 专属模板 |
| 告警通知 | 用户配额/到期字段、用户及节点组授权 | 配额阈值、到期提醒、个人邮件偏好、持久化去重和失败重试 | 支付/计费、自动月结、流量超额自动断网、Telegram/Webhook 实际投递 |
| 流量图表 | `traffic_records` 空表、上报契约与鉴权框架 | 真实采集、幂等入库、5m/1h/1d 聚合、用户/节点图表 | 用假数据补历史、外部节点精确流量、逐域名/逐连接审计、线速带宽计费 |
| 覆写 UI | 节点级与入站级覆写表、写接口、安全字段过滤 | 读取/编辑/预览/恢复继承、字段来源、按入站隔离、排序分组 | 修改运行配置、普通用户覆写身份凭据/私钥、外部节点协议覆写 |
| SMTP | 邀请制注册及邀请码消耗事务 | fuvia.net 邮局 587 STARTTLS、注册验证、存量邮箱验证、邀请邮件、告警/到期邮件 | 搭建邮局、修改 DNS/生产配置、密码找回、用户自定义任意收件人/邮件正文 |

**角色**：沿用实际 `owner/admin/user`。owner 独占模板发布、系统告警规则和 SMTP 系统设置；owner/admin 管理节点、拓扑、用户配额和邀请码；user 仅访问自己的订阅、用量、偏好和通知。不能只靠前端隐藏菜单做授权。

### 1.3 兼容策略

- 保留 P1 的 `/api/my/subscriptions`、`/api/invites`、嵌套入站路由和 `/sub/:token`；不另建含义相同但权限不同的 `/api/subscriptions`。
- 保留「保存草稿 → 预览 → 显式部署」；保存不得触发 agent 应用。新 agent 能力未就绪时只能保持旧的已部署配置，不能降级成直连。
- `subscriptions.template_id IS NULL` 继续走等价的内置模板；现有 token、用户凭据、节点组授权不变。
- 保留单机多入站；P2 的一条转发链不重复经过同一物理节点。同机入站互转仍不开放，明确延续 P1 的实际限制，而不是删除基础设计中的远期能力。
- 新邮箱验证仅对启用后的新注册强制；存量账号不因尚未验证邮箱而无法登录或拉取订阅，但未验证邮箱不接收告警与到期邮件。

---

## 2. 数据模型增量

### 2.1 通用约定与迁移顺序

沿用 PostgreSQL、`snake_case`、`BIGSERIAL/BIGINT` 主键、`TIMESTAMPTZ` UTC 时间、`JSONB` 定义。以下是表结构规格，不是已执行的迁移。除标记「可空」外字段均 `NOT NULL`；新表的 `created_at/updated_at` 默认 `now()`，更新时由 service 维护。外键默认 `ON DELETE RESTRICT`；历史记录使用快照和软删除，不让删除节点抹掉统计与部署证据。

现有迁移实际位于 `backend/internal/db/migrations/`，新增从 `0011` 起，实施时先核对当前最大序号：

| 建议迁移 | 内容 | 回填/兼容要求 |
|---|---|---|
| `0011_p2_topology` | 草稿布局、全图版本、发布批次与回执 | 不重放旧部署，不自动改变 relay 角色；无边旧 entry 标为 direct |
| `0012_p2_templates_overrides` | 模板版本、节点组默认值、订阅并发版本、覆写可继承字段 | NULL 模板映射内置；旧覆写保留现有值；无效 template_id 先列清单再由管理员映射，不能直接建 FK 失败 |
| `0013_p2_traffic` | 采集游标、幂等报告、聚合、用量账户 | 旧流量表若存在数据，先判明来源和粒度；无来源记录隔离为 legacy，不计入可信配额 |
| `0014_p2_notifications_mail` | 邮箱状态、验证挑战、规则、通知队列与投递记录 | 存量邮箱不伪造验证时间；无 SMTP 时保持旧注册策略，显式启用后才强制验证 |

执行顺序是 schema 扩展→兼容服务→回填验证→启用能力；不得修改 `0001`～`0010`。回退首先关闭新功能并恢复已发布快照；有 P2 数据后禁止直接删表式回滚。

### 2.2 拓扑与发布

仍以 `topology_drafts` 按物理节点保存的出边为草稿真相源，`topology_deployments` 是 agent 可取的当前显式发布快照。**不新增另一份可独立编辑的全局图**。旧 `edges` 表不作为 P2 的第二写入口；已有数据需核对用途，兼容只读，不能覆盖草稿或已部署快照。

| 表 | 新增/修改字段 | 约束与用途 |
|---|---|---|
| `inbounds` | `egress_mode TEXT DEFAULT 'direct'`、`revision BIGINT DEFAULT 1` | entry 为 direct/chain；relay 必须 chain；landing 为 direct；角色更改必须重新校验所有引用 |
| `topology_drafts` | `layout JSONB DEFAULT '{}'`、`validation_status TEXT DEFAULT 'unknown'` | layout 仅存入站坐标/视口；edges 沿用原 JSON 字段；revision 每次有效编辑递增；校验结果不代替实时校验 |
| `topology_material_state` | `graph_revision BIGINT DEFAULT 1` | 保留已有 material revision；全图草稿事务加锁递增 graph_revision，防止跨画布并发丢失更新 |
| `topology_previews` | `preview_id UUID UNIQUE`、`graph_revision BIGINT DEFAULT 1`、`revision_vector JSONB DEFAULT '{}'`、`candidate_bundle JSONB DEFAULT '{}'`、`expires_at TIMESTAMPTZ` | 保留按 root node_id 存储的原预览；bundle 保存逐节点候选与脱敏展示所需材料，敏感值加密；新预览替代旧预览，发布只能消费当前 preview_id |
| `nodes` | `agent_capabilities JSONB DEFAULT '[]'`、`config_generation BIGINT DEFAULT 0` | 能力由已认证心跳上报；任何运行材料/凭据变化递增 generation；不放凭据 |
| `topology_releases`（新） | `id BIGSERIAL PK`、`root_node_id BIGINT FK nodes`、`graph_revision BIGINT`、`material_revision BIGINT`、`revision_vector JSONB`、`status TEXT`、`created_by BIGINT FK users`、`error_code TEXT 可空`、`created_at`、`finished_at TIMESTAMPTZ 可空` | 状态 preparing/ready/applying/succeeded/failed/rolling_back/rolled_back/manual_required；revision_vector 保存本批所有依赖的草稿版本和材料哈希 |
| `topology_release_nodes`（新） | `release_id BIGINT FK topology_releases`、`node_id BIGINT FK nodes`、`phase TEXT`、`candidate_topology JSONB`、`previous_topology JSONB 可空`、`routing_version TEXT`、`expected_runtime_version TEXT 可空`、`expected_generation BIGINT`、`previous_generation BIGINT`、`apply_order INT`、`status TEXT`、`attempts INT DEFAULT 0`、`last_ack_at TIMESTAMPTZ 可空`、`error_code TEXT 可空` | PK `(release_id,node_id)`；候选与旧快照含运行材料，按现有安全设计应用层加密敏感内容；客户端只取脱敏视图；错误只存枚举和可公开定位 |
| `topology_deployments` | `release_id BIGINT 可空 FK topology_releases`、`generation BIGINT DEFAULT 0` | 保留 version/schema_version/draft_revision/topology/rendered；只有轮到该节点 apply 时才切换当前快照 |

每个 node 同时最多属于一个非终态发布批次，通过事务锁定按 ID 排序的节点行实现；不只靠内存互斥。批次及快照默认保留最近 30 天且每节点至少 10 次，任何当前引用、待恢复引用不能清理。

### 2.3 模板、节点组默认值与覆写

| 表 | 新增/修改字段 | 约束与用途 |
|---|---|---|
| `templates` | `description TEXT DEFAULT ''`、`status TEXT DEFAULT 'draft'`、`is_builtin BOOLEAN DEFAULT FALSE`、`published_version INT 可空`、`revision BIGINT DEFAULT 1`、`created_by BIGINT 可空 FK users`、`updated_at`、`archived_at TIMESTAMPTZ 可空` | 沿用 name/format/definition；definition 为可编辑草稿；format 属于 mihomo/sing-box/base64；内置模板只读，可复制 |
| `template_versions`（新） | `template_id BIGINT FK templates`、`version INT`、`schema_version INT`、`definition JSONB`、`checksum TEXT`、`published_by BIGINT FK users`、`published_at TIMESTAMPTZ DEFAULT now()` | PK `(template_id,version)`；版本不可修改；templates 的已发布版本以复合 FK 引用同一模板的版本 |
| `subscriptions` | `template_id` 补 FK、`template_version INT 可空`、`revision BIGINT DEFAULT 1` | NULL version 跟随最新已发布版本；非空用复合 FK 固定版本；NULL template_id 仅允许 NULL version；format 必须与模板一致，service 事务校验 |
| `node_groups` | `subscription_defaults JSONB DEFAULT '{}'`、`revision BIGINT DEFAULT 1` | 仅允许第 6 节的客户端安全默认值；节点组默认值不赋予节点权限 |
| 两类覆写表 | `sort_order` 改为可空且无默认值、`revision BIGINT DEFAULT 1` | 原有 0 仍是显式 0；新记录 NULL 表示继承；沿用 display_name/icon/params/proxy_group；现有两个 UNIQUE 不变 |

受管入站级覆写的 `(node_id,inbound_id)` 必须匹配真实入站且 role=entry；外部节点不允许入站级覆写。可为 `inbounds(node_id,id)` 建 UNIQUE 并补复合 FK，跨表类型/权限仍在 service 事务内校验。任何模板引用中的分组 ID 不得被发布版本悄悄删除。

### 2.4 流量、聚合与账户

**计量分两条线**：用户入口有效负载用于用量/告警；节点所有入站有效负载用于运维。两者不能相加。所有 bytes 字段为非负 BIGINT，应用层检查溢出。

| 表 | 字段 | 主键、索引及说明 |
|---|---|---|
| `traffic_reports`（新） | `id BIGSERIAL PK`、`node_id BIGINT FK nodes`、`epoch_id UUID`、`sequence BIGINT`、`payload_hash TEXT`、`period_start TIMESTAMPTZ`、`period_end TIMESTAMPTZ`、`received_at TIMESTAMPTZ DEFAULT now()` | UNIQUE `(node_id,epoch_id,sequence)`；仅留幂等元数据，不存用户凭据或原始代理配置；received_at 索引 |
| `traffic_cursors`（新） | `node_id BIGINT FK nodes`、`epoch_id UUID`、`series_key TEXT`、`last_sequence BIGINT`、`last_up_bytes BIGINT`、`last_down_bytes BIGINT`、`last_sample_at TIMESTAMPTZ` | PK `(node_id,epoch_id,series_key)`；series_key 为入站统计标识或不含凭据的用户-入站标识；计数器差分基线 |
| `traffic_node_state`（新） | `node_id BIGINT PK FK nodes`、`active_epoch_id UUID`、`last_sequence BIGINT`、`last_received_at TIMESTAMPTZ 可空`、`last_complete_at TIMESTAMPTZ 可空`、`coverage_start TIMESTAMPTZ 可空`、`status TEXT` | status collecting/stale/unsupported/gap；串行摄取锁和数据新鲜度依据 |
| `traffic_records`（修改） | 保留 user_id/node_id/inbound_id/up_bytes/down_bytes/period_start；加 `estimated BOOLEAN DEFAULT FALSE`、`updated_at` | 固定为 UTC 5m 用户入口桶；保留原 UNIQUE；补非负检查；原 CASCADE FK 改 RESTRICT，清理后才能硬删除实体 |
| `traffic_node_records`（新） | `node_id BIGINT FK nodes`、`inbound_id BIGINT FK inbounds`、`period_start TIMESTAMPTZ`、`up_bytes BIGINT DEFAULT 0`、`down_bytes BIGINT DEFAULT 0`、`estimated BOOLEAN DEFAULT FALSE`、`updated_at` | PK `(node_id,inbound_id,period_start)`；5m 节点入站桶；不是用户桶相加后的副本 |
| `traffic_user_aggregates`（新） | `user_id BIGINT FK users`、`node_id BIGINT FK nodes`、`grain TEXT`、`period_start TIMESTAMPTZ`、`up_bytes BIGINT DEFAULT 0`、`down_bytes BIGINT DEFAULT 0`、`estimated BOOLEAN DEFAULT FALSE`、`updated_at` | PK `(user_id,node_id,grain,period_start)`；grain=1h/1d；索引 `(user_id,grain,period_start)` |
| `traffic_node_aggregates`（新） | `node_id BIGINT FK nodes`、`grain TEXT`、`period_start TIMESTAMPTZ`、`up_bytes BIGINT DEFAULT 0`、`down_bytes BIGINT DEFAULT 0`、`estimated BOOLEAN DEFAULT FALSE`、`updated_at` | PK `(node_id,grain,period_start)`；grain=1h/1d |
| `traffic_accounts`（新） | `user_id BIGINT PK FK users`、`quota_epoch BIGINT DEFAULT 1`、`period_start TIMESTAMPTZ`、`used_up_bytes BIGINT DEFAULT 0`、`used_down_bytes BIGINT DEFAULT 0`、`lifetime_up_bytes BIGINT DEFAULT 0`、`lifetime_down_bytes BIGINT DEFAULT 0`、`updated_at` | 只在去重摄取事务中增量累加；历史桶过期不扣减；当前期从启用计量/显式重置起，不等于自然月 |
| `traffic_account_events`（新） | `id BIGSERIAL PK`、`user_id BIGINT FK users`、`quota_epoch BIGINT`、`kind TEXT`、`old_limit_bytes BIGINT`、`new_limit_bytes BIGINT`、`snapshot_up_bytes BIGINT`、`snapshot_down_bytes BIGINT`、`actor_id BIGINT FK users`、`created_at` | 记录 limit_changed/reset；用于配额修改、重置及核对，不作商业账单 |
| `traffic_rollup_jobs`（新） | `grain TEXT`、`period_start TIMESTAMPTZ`、`revision BIGINT DEFAULT 1`、`updated_at` | PK `(grain,period_start)`；摄取事务将受影响小时/日标脏；worker 按 revision 重算覆盖，避免加法式重复聚合 |

上报元数据与游标保留 7 天，5m 桶 30 天、1h 桶 180 天、1d 桶 730 天；账户累计独立保留到账号依法/按管理员流程清理。活跃 epoch 的当前游标不可因 7 天清理被删除。采样缺口由节点状态和第 7 节质量字段表达，不伪造零值。

### 2.5 告警、邮件与邮箱验证

| 表 | 字段 | 约束与用途 |
|---|---|---|
| `users` | `email_verified_at TIMESTAMPTZ 可空`、`email_revision BIGINT DEFAULT 1`、`quota_revision BIGINT DEFAULT 1` | email_revision 绑定验证与投递；邮箱变更清空验证时间并递增；quota_revision 防并发覆盖现有 traffic_limit_bytes/expire_at |
| `alert_rules`（新） | `id BIGSERIAL PK`、`name TEXT`、`kind TEXT`、`enabled BOOLEAN DEFAULT TRUE`、`scope_type TEXT`、`scope_id BIGINT 可空`、`thresholds JSONB`、`channel TEXT DEFAULT 'email'`、`revision BIGINT DEFAULT 1`、`created_by BIGINT FK users`、`created_at`、`updated_at` | kind=traffic_limit/expiration；scope_type=all/group/user，all 的 scope_id=NULL；其他由事务验证 FK 目标；P2 channel 只接受 email |
| `notification_preferences`（新） | `user_id BIGINT PK FK users`、`traffic_enabled BOOLEAN DEFAULT TRUE`、`expiration_enabled BOOLEAN DEFAULT TRUE`、`revision BIGINT DEFAULT 1`、`updated_at` | 只控制本人可选通知；注册验证/本人请求的邮箱验证不受开关影响 |
| `notification_records`（新，兼 outbox） | `id BIGSERIAL PK`、`user_id BIGINT 可空 FK users`、`rule_id BIGINT 可空 FK alert_rules`、`kind TEXT`、`channel TEXT DEFAULT 'email'`、`dedupe_key TEXT UNIQUE`、`event_snapshot JSONB`、`recipient_ciphertext BYTEA`、`payload_ciphertext BYTEA`、`email_revision BIGINT 可空`、`state TEXT`、`attempts INT DEFAULT 0`、`next_attempt_at TIMESTAMPTZ`、`lease_until TIMESTAMPTZ 可空`、`message_id TEXT UNIQUE`、`provider_status TEXT 可空`、`last_error_code TEXT 可空`、`created_at`、`sent_at TIMESTAMPTZ 可空` | state=queued/sending/sent/retry/dead/suppressed/blocked_recipient；event_snapshot 仅含脱敏用量/阈值/时间；加 `(state,next_attempt_at)` 部分索引；发送内容中验证 token/邀请码必须加密 |
| `notification_attempts`（新） | `id BIGSERIAL PK`、`notification_id BIGINT FK notification_records`、`attempt_no INT`、`result TEXT`、`smtp_code INT 可空`、`duration_ms INT`、`error_code TEXT 可空`、`created_at` | UNIQUE `(notification_id,attempt_no)`；不存 SMTP 会话全文、密码、邮件正文或完整收件人 |
| `email_verifications`（新） | `id UUID PK`、`purpose TEXT`、`user_id BIGINT 可空 FK users`、`email_ciphertext BYTEA`、`email_lookup_hash BYTEA`、`invite_lookup_hash BYTEA 可空`、`token_hash BYTEA UNIQUE`、`ticket_hash BYTEA 可空 UNIQUE`、`email_revision BIGINT 可空`、`state TEXT`、`expires_at TIMESTAMPTZ`、`ticket_expires_at TIMESTAMPTZ 可空`、`verified_at TIMESTAMPTZ 可空`、`consumed_at TIMESTAMPTZ 可空`、`created_at` | purpose=register/verify_existing；state=pending/verified/consumed/expired；只存随机 token/ticket 的摘要；邮箱及邀请码索引用服务端 HMAC，不能用低熵明文哈希 |

mail 配置从环境注入，不新增保存 SMTP 明文密码的数据库表。敏感密文采用带 key ID 的 AEAD 信封，AAD 绑定表、记录 ID、用途；密钥缺失时禁止启动邮件 worker，不能退化为明文存储。P2 不顺手迁移整个 P1 的密码算法或所有历史密钥。

验证挑战到期后 7 天清理；通知及投递审计默认保留 90 天，成功或永久失败后 24 小时内清除密文正文/完整收件人，保留掩码及去重键。超过 90 天仍需防重的活动配额期/到期事件，其最小去重墓碑保留至事件关闭后 90 天，不能连同 UI 历史一起删除。

---

## 3. API 增量

### 3.1 共同契约

- 路由表沿用 `/api`，下表 `:id` 表示 chi 路径参数；浏览器 JSON 使用现有 camelCase，数据库仍 snake_case。
- 错误沿用 `{"error":{"code":"...","message":"..."}}`，可增加 `details:[{field,nodeId,inboundId,edgeId,reason}]` 和 requestId。不得把配置原文、订阅 token、SMTP 原始响应作为错误输出。
- bytes 一律以十进制字符串返回/接收，前端用 BigInt 或安全转换后绘图；时间为 RFC3339 UTC。列表用 `cursor/limit`（默认 50、最大 200），响应 `{items,nextCursor}`；既有列表响应不强制改包装，新客户端兼容原格式。
- 新编辑接口以 `expectedRevision` 或 `expectedGraphRevision` 做乐观锁，冲突返回 409 `revision_conflict`；不存在/非本人订阅统一 404，已知管理权限不足 403，语义无效 422，限流 429 并带 Retry-After。
- 本人接口的 userId 由认证上下文取得，不能信任 body；模板/覆写/统计查询重新检查归属和节点组授权。读取历史本人用量不随节点组撤销而消失，但不得再返回该节点当前连接参数。

### 3.2 拓扑与 agent

| 方法 | 路径 | 权限 | 请求/响应要点 |
|---|---|---|---|
| GET | `/api/topology/graph` | owner/admin | 新增；返回全图草稿、graphRevision、逐节点 revisions、脱敏入站元数据、layout、校验问题、当前部署摘要；不返回完整密钥配置 |
| PUT | `/api/topology/graph` | owner/admin | 新增；`expectedGraphRevision,changes:[{nodeId,expectedRevision,edges,layout,inboundModes}]`；仅修改列出的节点，单事务全图校验并保存；返回新 revisions 和 draftValid |
| GET/PUT | `/api/topology/:nodeId` | owner/admin | 保留；增加 revision/layout/egressMode 校验；PUT 仅改本节点，必须调用同一个全图校验 service，不能绕过环检测 |
| POST | `/api/topology/:nodeId/preview` | owner/admin | 扩展；已保存草稿为输入，返回 `previewId,graphRevision,dependencies,versions,configs,warnings,expiresAt`；configs 脱敏，明确仅预检不是应用成功 |
| POST | `/api/topology/:nodeId/deploy` | owner/admin | 扩展；`previewId,expectedGraphRevision`；202 返回 `releaseId,status`；旧单节点 version 请求只允许无跨节点依赖的兼容发布，否则 409 `upgrade_required` |
| GET | `/api/topology/releases/:id` | owner/admin | 新增；逐节点准备/应用/回滚状态，routingVersion/runtimeVersion、脱敏错误与时间 |
| POST | `/api/topology/releases/:id/rollback` | owner/admin | 新增；仅 failed/manual_required 等允许状态；幂等返回当前恢复批次，不能随意回退无关成功批次 |
| PUT | `/api/nodes/:id/inbounds/:inboundId` | owner/admin | 新增；更新名称/角色/监听/参数/egressMode，带 expectedRevision；有活跃链引用时返回影响列表，需重新预览后显式发布 |
| POST | `/api/agent/heartbeat` | 本节点 agent | 扩展 `capabilities,configGeneration,appliedRuntimeVersion`；保留旧字段；心跳在线不等于配置已应用 |
| GET | `/api/agent/config` | 本节点 agent | 保留当前已激活快照；新增 `phase=prepare&releaseId=` 获取仅本节点候选；响应增加 releaseId/generation/routingVersion，原 runtime version/SHA256 语义不变 |
| POST | `/api/agent/deployment-acks` | 本节点 agent | 新增；`releaseId,phase,generation,runtimeVersion,status,errorCode`；重复回执幂等；过期 generation 409，不允许替别的节点确认 |
| POST | `/api/agent/report-traffic` | 本节点 agent | 从存根改为真实事务入库；v2 请求见第 7 节；200 返回 `accepted,duplicate,acceptedSequence,serverTime`；旧 v1 明确 `persisted:false`，不能伪称已计量 |

### 3.3 模板与订阅覆写

| 方法 | 路径 | 权限 | 请求/响应要点 |
|---|---|---|---|
| GET | `/api/templates` | 登录用户 | 可选择的已发布模板；owner 可用 `includeDrafts=true`；返回 format、能力、publishedVersion，不含节点/用户材料 |
| POST | `/api/templates` | owner | `name,format,definition,description`；201 草稿；复制通过 `sourceTemplateId,sourceVersion`，不复制绑定订阅 |
| GET/PUT/DELETE | `/api/templates/:id` | GET 登录；写 owner | PUT 带 expectedRevision 保存草稿，不影响已发布订阅；DELETE 实为归档，仍被订阅引用时 409 `template_in_use` |
| GET | `/api/templates/:id/versions` | 登录用户 | 不可变已发布版本列表；用户只能读取可见模板版本 |
| POST | `/api/templates/:id/validate` | owner | 校验草稿 schema 和各格式合成样例；返回 fieldErrors/warnings；不访问真实用户订阅 |
| POST | `/api/templates/:id/publish` | owner | `expectedRevision`；原子创建版本、校验现有绑定的分组引用、更新 publishedVersion；不兼容则 409，返回脱敏引用计数 |
| PUT | `/api/groups/:id/subscription-defaults` | owner/admin | 新增；`expectedRevision,defaults`；受安全字段白名单约束；保留现有节点组管理 API |
| GET/POST | `/api/my/subscriptions` | 本人 | 保留；POST 新增 templateId/templateVersion；已支持格式校验，不把模板格式与订阅格式混用 |
| GET/PUT | `/api/my/subscriptions/:id` | 本人 | 新增；PUT `expectedRevision,name,nodeGroupId,templateId,templateVersion,format`；切换前检查所有覆写分组引用，成功原子递增 revision，不更换 token |
| GET | `/api/my/subscriptions/:id/overrides` | 本人 | 新增；按 `nodeId,inboundId` 返回 raw/effective/sources/allowedFields/revision；敏感字段只返回 configured，不返回值 |
| PUT/DELETE | `/api/my/subscriptions/:id/overrides/:nodeId` | 本人 | PUT 保留、DELETE 新增；外部节点仅显示属性；受管节点此路由保留旧回退语义，新 UI 优先入站级编辑 |
| PUT/DELETE | `/api/my/subscriptions/:id/overrides/:nodeId/inbounds/:inboundId` | 本人 | PUT 保留、DELETE 新增；带 expectedRevision；PUT 全量替换该行可编辑字段，省略/NULL 表示继承，不接受静默丢弃的非法参数 |
| POST | `/api/my/subscriptions/:id/preview` | 本人 | 新增；可携带未保存的 template/overrides；返回脱敏配置、差异、来源和错误，不落库、不下发节点、不返回订阅 token |
| GET | `/sub/:token` | 持有 token 的客户端 | 路径不变；按已发布/固定模板实时合并，返回真实客户端配置；权限先校验，再决定缓存；禁止共享代理缓存 |

`TECH-DESIGN.md` 中的 `/api/subscriptions`、`/api/invite-codes`、平铺 `/api/inbounds` 是早期草案路径。P2 使用上述实际路径，不新增重名镜像 API；需要外部兼容层时另立任务，不能扩大本次范围。

### 3.4 流量、规则与通知

| 方法 | 路径 | 权限 | 请求/响应要点 |
|---|---|---|---|
| GET | `/api/my/traffic` | 本人 | `from,to,grain=auto|5m|1h|1d,nodeId?,groupBy=none|node`；范围 `[from,to)`；返回 summary/series/quality/quota |
| GET | `/api/traffic/:userId` | owner/admin 或该本人 | 落实技术草案已有规划；同上；普通用户他人 ID 返回 404 |
| GET | `/api/nodes/:id/traffic` | owner/admin | 节点所有入站吞吐；可 `inboundId` 过滤，groupBy=none|inbound；入站维度只在 5m 的 30 天保留范围内提供 |
| GET | `/api/nodes/:id/stats` | owner/admin | 落实既有规划；心跳状态与 trafficFreshness 分列，不能用心跳 CPU 数据冒充流量 |
| PUT | `/api/users/:id/quota` | owner/admin | `expectedRevision,trafficLimitBytes,expireAt`；0 不限，expireAt=NULL 不到期；不隐式清零当前使用量 |
| POST | `/api/users/:id/traffic-reset` | owner/admin | `expectedQuotaEpoch,reason`；显式重置当前期、保存快照、递增 epoch；重复旧 epoch 请求 409；lifetime 不清零 |
| GET/POST | `/api/alert-rules` | owner | 列表/创建；kind/scope/thresholds/channel/enabled；预置规则 traffic `[80,90,100]`、expiration `[7,3,1,0]` |
| GET/PUT/DELETE | `/api/alert-rules/:id` | owner | PUT expectedRevision；DELETE 禁用并保留事件历史；非法 scope/阈值 422 |
| GET/PUT | `/api/my/notification-preferences` | 本人 | trafficEnabled/expirationEnabled/expectedRevision；不接收自填收件人 |
| GET | `/api/my/notifications` | 本人 | 本人通知的掩码摘要、状态和错误类别；不返回原始收件地址、token、邮件正文 |
| GET | `/api/notifications` | owner | 系统队列过滤 state/kind/from/to；只返回脱敏诊断 |
| POST | `/api/notifications/:id/retry` | owner | 仅可重试非过期、非撤销 dead 记录；复用 dedupeKey/Message-ID；不创建新事件 |

流量响应示意（合成数据，单位是 bytes，不是 MiB）：

```json
{
  "from": "2026-09-08T00:00:00Z",
  "to": "2026-09-09T00:00:00Z",
  "grain": "1h",
  "timezone": "UTC",
  "summary": {"upBytes": "1024", "downBytes": "4096", "totalBytes": "5120"},
  "series": [{"key": "total", "points": [{"at": "2026-09-08T00:00:00Z", "upBytes": "1024", "downBytes": "4096", "complete": true}]}],
  "quality": {"status": "partial", "availableFrom": "2026-09-08T00:00:00Z", "lastReceivedAt": "2026-09-08T01:01:00Z", "estimated": false},
  "quota": {"epoch": 1, "limitBytes": "0", "usedBytes": "5120", "unlimited": true}
}
```

### 3.5 SMTP 与邮箱验证

| 方法 | 路径 | 权限 | 请求/响应要点 |
|---|---|---|---|
| GET | `/api/settings/smtp` | owner | 只读环境配置摘要：host/port/security/fromMasked/configured/passwordConfigured/lastTest；绝不回传密码 |
| POST | `/api/settings/smtp/test` | owner | 无任意 to/body 参数；只发往当前 owner 的账号邮箱，202 返回 notificationId；限 3 次/小时 |
| POST | `/api/auth/email-verifications` | 公开且限流 | `email,inviteCode`；对无效邀请码、已注册邮箱等统一 202 与等形 challengeId；仅有效请求入队，不消耗邀请码 |
| POST | `/api/auth/email-verifications/confirm` | 持有验证 token | `challengeId,token`；成功返回短期 registrationTicket 或 existing_email_verified；token 不能作为登录 JWT |
| POST | `/api/my/email-verifications` | 本人 | 发往本人当前邮箱；绑定 userId/emailRevision；202；不能任意替别人发邮件 |
| POST | `/api/auth/register` | 公开且限流 | 启用强制验证后，在原 username/password/email/inviteCode 上加 registrationTicket；事务消费 ticket 和邀请码后创建用户并签 JWT |
| POST | `/api/invites/:id/send-email` | owner/admin | `email,requestId`；只发送自己有权分发的有效邀请码；同 requestId 幂等；有明确确认步骤，默认 20 封/小时/管理员 |

P2 不提供从浏览器修改 SMTP 凭据的 PUT 端点；环境变更属于经批准的部署操作。接口测试发信只证明邮件工作链路，不能替代注册验证或实际收件验收。

---

## 4. 前端页面与关键交互

### 4.1 页面与组件增量

保持现有 `frontend/src/pages/*.tsx` 风格，不为匹配早期骨架而强制重排目录。继续 React + AntD + React Flow，流量页新增按需加载的 ECharts；复用现有 AuthContext、API 封装和布局。

| 路由 | 页面/组件 | 交互与权限 |
|---|---|---|
| `/topology`、`/topology/:nodeId` | 改造 `Topology.tsx`；新增 `TopologyCanvas`、`InboundCard`、`TopologyValidationPanel`、`DeploymentDrawer` | 管理员；保留原入口，带 nodeId 时聚焦该物理节点而不是截断依赖图 |
| `/templates`、`/templates/:id` | 新增 `Templates.tsx`、`TemplateEditor`、`TemplatePreview`、`TemplateVersionSelect` | owner 编辑；其他登录用户只看已发布模板摘要，实际选模板在订阅页 |
| `/subscriptions` | 改造 `Subscriptions.tsx`、`SubscriptionTemplateSelect` | 本人；新增模板/版本/跟随最新选择，显示不兼容原因，切换前预览 |
| `/subscriptions/:id/overrides` | 新增 `OverrideEditor.tsx` 页面壳和同名组件、`OverrideField`、`OverrideDiff` | 本人；按节点→入站组织，而非一台机器只有一条覆写 |
| `/traffic` | 新增 `Traffic.tsx`、`TrafficFilters`、`TrafficChart`、`TrafficSummary`、`TrafficQualityBanner` | 本人；管理员可切用户/节点视图，普通用户不能出现全局用户选择器 |
| `/alerts` | 新增 `Alerts.tsx`、`AlertRuleForm`、`NotificationPreferenceForm`、`NotificationHistory` | 普通用户只有本人偏好/历史；owner 额外有规则与投递队列 |
| `/settings/mail` | 新增 `MailSettings.tsx` | owner；只读 SMTP 配置状态、测试发送和脱敏诊断，不能把密码放浏览器状态 |
| `/register`、`/verify-email` | 改造 `Register.tsx`；新增 `VerifyEmail.tsx` | 邮箱验证→注册；验证码链接只显示确认动作，不在 GET 导航时消费 token |
| `/administration` | 改造现有 `Administration.tsx` | 复用用户/邀请/节点组页，增加配额到期编辑、明确重置确认、邀请邮件、节点组客户端默认值 |

### 4.2 多级画布交互

1. 左侧资产栏列出受管节点及入站；服务器作为视觉分组，真正连线端点是入站 ID。外部节点仅出现在只读说明区，不能拖入转发图。
2. entry 卡片只有输出 handle；relay 有输入和输出 handle；landing 只有输入 handle。entry 显示「直连」或「中转链」模式；direct 卡片不要求连线。候选目标按类型、当前占用、同机限制禁用并解释。
3. 建三级链：选择 entry 的 chain 模式→拖至 relay 输入→relay 输出拖至 landing。快捷「添加下一跳」与拖拽共用校验逻辑，并提供键盘可操作的源/目标下拉框。
4. 连线时本地检查端点/重复出边/自环/回到祖先；不合法不落边，并展示具体原因。未接完的 relay 或 chain entry 标橙色「未完成」，允许保存草稿，不允许预览/部署。
5. 点击校验面板中的问题，定位并高亮相关卡片和边；环显示完整路径，缺落地显示中断点，孤立未使用落地显示警告而不是阻断整个资产池。依赖后端最终校验，不能信任仅有前端检查的导入图。
6. 删除节点/边前列出受影响入口和已部署版本；删除画布卡片不删除真实入站。资产删除使用节点管理专门确认流程，仍被已部署链引用时 409。
7. 保存只提交变更节点和 expected revisions；成功显示草稿版本。409 保留本地未保存副本，提供重新载入与人工重做，不静默覆盖他人编辑。
8. 预览以物理节点分 tab 展示脱敏配置差异、发布次序、协议能力、链深；发布按钮需要当前预览未过期且所有阻断问题清零。部署抽屉显示逐节点准备/应用/恢复状态，不能用一个「保存成功」提示代替整个发布成功。

### 4.3 模板、覆写、告警交互

- 模板编辑默认结构化表单，JSON 高级编辑是同一 schema 的另一视图；校验错误定位字段路径。发布前展示哪些订阅跟随最新、哪些固定旧版本；未发布草稿不影响正在使用的订阅。
- 覆写列表以稳定键 `nodeId:inboundId` 标识受管条目，外部条目用 `nodeId:external`；显示「当前值 / 生效来源 / 是否继承」。每个字段有「恢复继承」，整行有「删除覆写」；删除入站级覆写后若存在旧节点级覆写，明确提示将回退到旧值。
- 表单切换模板/分组时先预览引用是否仍有效；遇到孤立分组引用不自动丢弃用户设置，要求映射或恢复继承后再保存。失败时保留编辑内容。
- 流量卡片区分「当前配额期 / 今日 UTC / 近 7 天 / 可用历史累计」。无限配额显示「不限」而非 0%；过期账号仍允许登录看本人告警历史，运行凭据权限沿用现有过期逻辑。
- 告警规则编辑用百分比多选/数字输入和到期天数 chips，展示示例触发时刻及去重说明。普通用户只开关自己邮件通知；未验证邮箱显示验证入口，不能选任意收件人。
- SMTP 测试按 queued/sent/dead 反馈；sent 文案为「邮局已接收，待确认收件」，不写成「已进入收件箱」。

### 4.4 公共 UI 约束

所有新增页面覆盖 loading/empty/error/403/过期数据状态；表单有明确标签、键盘焦点和错误摘要。颜色不是唯一状态标识。图表下提供同口径表格，窄屏减少系列但不丢摘要；图表挂载后初始化、容器尺寸变化 resize、卸载 dispose。单页自动刷新 60 秒，切出页面暂停，返回立即补取；避免重建图表实例和重复请求。

---

## 5. 多级中转拓扑规格

### 5.1 图约束

定义有向图 `G=(V,E)`：V 是受管节点的入站，不是服务器；E 是 `fromInboundId → toInboundId`，toNodeId 必须匹配目标入站所属节点。每条边权重固定 1；非 1 拒绝，不提前实现 P3 权重。

| 对象 | 入度 | 出度 | 发布约束 |
|---|---|---|---|
| entry/direct | 0 | 0 | 合法 P1 直连入口；不要求额外 landing |
| entry/chain | 0 | 1 | 必须沿唯一下一跳最终到 landing |
| relay/chain | 1 | 1 | 至少一个祖先 entry；P2 不允许多个上游共用同一 relay 入站，需建立独立 relay 入站 |
| landing/direct | ≥1 | 0 | 可供多个上游复用；未被使用的资产入度 0 只警告，不发布成不完整链 |

- 链形态为 `entry → relay* → landing`；保留 entry→landing 两级。P2 允许 3～8 个入站的多级链，即最多 6 个 relay；超过 8 返回 `chain_too_deep`。
- 全图不允许自环或任何有向环；同一链不允许重复物理 node，避免跨不同入站重入同机的隐性回路。同一物理机仍可承载不同链或不同角色的独立入站。
- source 必须是 entry/chain 或 relay/chain，target 必须 relay 或 landing；外部节点、已删除入站、目标所属节点不匹配、重复出边均为硬错误。协议材料从后端读取，浏览器不能在边中注入 server/port/凭据。
- 保存草稿允许「缺下一跳/缺上游」这类未完成状态；环、非法类型、自连、重复边等结构错误保存即拒绝。预览/部署要求所有参与的 chain entry 和 relay 完整可达 landing。
- 草稿中的未使用资产不参与部署；运行快照仍被引用的入站不能删除、改为不兼容协议或改身份参数后不部署。role 从 relay 改 landing 属于显式修复，不根据旧存量数据擅自推断。
- 全图节点数上限 2,000 入站、边数 2,000，超限 422；本地交互 O(V+E) 检查，后端 DFS 三色标记/拓扑排序作权威校验，不用无限递归。

### 5.2 从图到每台 sing-box 配置

沿用项目锁定的 `sing-box/v1.13.21` 契约；P2 本身不升级内核。所谓「递归多层 outbound」是**遍历跨机依赖并分别生成各机下一跳**，不是把全部链条堆到入口机器，更不能误用一个指向 inbound 标签的 outbound。

```text
客户端 → A:entry(101) → B:relay(201) → C:landing(301) → 目标

A：inbound in-101 → route action=route → outbound to-201（连接 B 的受管协议入站）
B：inbound in-201 → route action=route → outbound to-301（连接 C 的受管协议入站）
C：inbound in-301 → route action=route → outbound direct
```

每个 source inbound 生成一条匹配该 inbound tag 的 route rule；每个不同目标入站生成稳定 `to-<inboundId>` outbound，参数来自该目标协议的服务端材料对应的**客户端连接参数**，不传 privateKey、证书私钥路径等服务端字段。Reality/SS2022/Hy2 沿用现有 renderer 的协议映射和字段命名。

发布快照中的同节点全部入站一起生成，避免只部署本条链时覆盖该机其他入站。entry 使用已有按用户-入站分配的身份；relay/landing 使用受管的链间身份，不能把多用户账号伪装成同一用户计费。下游统计只作节点吞吐，用户只在入口记一次。

转发地址沿用 P1 的 public_ip 优先、easy_ip 回退；预览明确展示选择的地址（按管理员权限脱敏）。不静默切换地址优先级；内网优先策略另行显式配置和验收。P2 不引入基于延迟的自动选路。

未匹配业务入站不能因链缺边而退回 direct；relay 缺下一跳必须阻断发布。已有 direct entry 和 landing 才显式生成 direct 规则。配置生成排序、tag 和规范化序列化必须确定性，同一材料生成相同哈希。

### 5.3 全图预览与发布状态机

1. **确定影响集合**：从入口沿边找下游，按物理节点合并配置；同时反向检查这些节点的所有已部署引用和草稿依赖。未修改的同机入站保留原快照；若要改变共享 landing 或影响其他入口，预览必须列出这些入口并纳入一致性检查，不能在局部画布偷偷替换公共材料。
2. **冻结预览**：读取 graphRevision、每节点 draftRevision、materialRevision、凭据 generation 和渲染材料哈希，形成 revision_vector。预览有效期 10 分钟；任何相关变化都使预览失效，发布返回 409 `preview_stale`。不以 UI 已刷新替代事务检查。
3. **prepare**：事务建立 release 并锁住影响节点，候选通过私有 agent 接口下发；agent 仅写临时候选、运行锁定版本 `sing-box check`，不切当前配置。所有节点返回 prepared 且 generation/version 匹配才进入 ready。新能力标识为 `topology-chain-v2`；缺能力返回 `agent_upgrade_required`。
4. **apply**：从最下游到入口依次激活当前快照，沿用 agent 临时文件、校验、原子替换、ready 检查和 last-good。每次通过原 `/api/agent/config` 提供的只有已激活版本；仅 prepare 的候选永远不算当前版本。每节点实际应用并回执后才推进上游。
5. **完成**：最后一个入口回执成功后为 succeeded；全程 UI 可轮询 release。心跳 appliedRuntimeVersion 是辅助观测，不能代替携带 releaseId/generation 的明确回执。
6. **失败/恢复**：prepare 失败不激活任何候选；apply 失败先停止剩余节点，已切换部分按**上游先撤回、再下游**恢复旧路由快照，仍需实际回执。节点离线或恢复失败标 manual_required，保留故障集合，不谎报全图原子回滚。无旧快照的首次部署只撤销候选并进入安全无监听状态，不自动生成直连。

每节点 prepare/apply 超时 120 秒，每次回执允许幂等重试，整个批次 15 分钟超时；后端重启从数据库非终态 release 恢复，agent 重启仍使用 last-good 并报告真实版本。节点恢复操作有相同锁和 generation 防护，不能执行旧批次迟到命令。

**版本语义**：routingVersion 是不含动态用户凭据的路由材料版本；runtimeVersion 是真实下发字节 SHA256，继续沿用 P1 `version`。prepare/apply 按 expected generation 固定材料；期间正常管理变更返回 409 或使预览失效。用户撤权/过期优先，必须递增 generation、中止冲突发布并重新生成当前路由的用户集合；回滚旧路由时使用**当前有效用户凭据**，不能恢复已撤销权限。

**可用性边界**：分布式节点发布不是分布式事务。下游切换可能短暂影响正在使用共享入站的旧链；P2 默认不允许在线批次直接变更共享入站身份/端口，要求先增加新入站、迁移入口、最后移除旧入站，或显式维护窗口。配置 check/ready 也不证明公网端到端通；第 10 节必须做真实握手。

---

## 6. 模板、覆写与告警通知规格

### 6.1 模板 schema 与版本

模板只描述客户端订阅，不描述 agent/sing-box 服务端运行配置。按格式提供 renderer 适配器，不能将 mihomo YAML 直接替换扩展名当 sing-box JSON。

| 格式 | P2 能力 | 不支持时处理 |
|---|---|---|
| mihomo | 完整模板：显示默认值、代理组、内联规则、受限 DNS、命名变量 | 非白名单键 422，不透传到生成器 |
| sing-box | 新增真正客户端生成器；代理 outbound、selector/urltest、路由、受限 DNS，覆盖受管三协议 | 先完成锁定内核与客户端 check/握手再出现在创建下拉框 |
| base64 | 复用 URI 列表编码；允许显示名/排序；不含代理组/规则/DNS | 有不兼容字段即拒绝，不假装模板已生效 |
| v2rayN | PRD 远期格式，P2 不交付专属模板 | 返回 format_unsupported，UI 不列为可用 |

模板定义使用版本化 JSON object，不做文本 eval 或 Go 模板任意函数执行。示意：

```json
{
  "schemaVersion": 1,
  "defaults": {"displayNamePattern": "${node.name}-${inbound.name}", "params": {"sni": ""}},
  "variables": {"regionLabel": {"type": "string", "default": "默认"}},
  "groups": [{"id": "main", "name": "节点选择", "type": "select", "members": ["$authorizedProxies", "DIRECT"]}],
  "rules": [{"type": "MATCH", "target": "group:main"}],
  "dns": {"enabled": false}
}
```

此例中的空 sni 表示模板未设置该默认值，不覆写实际节点 SNI。schema 规范：

- 顶层仅 `schemaVersion/defaults/variables/groups/rules/dns`；definition ≤256 KiB、JSON 嵌套≤16、groups≤32、rules≤1,000；生成后≤5 MiB，渲染预算 2 秒，超限返回明确错误。
- 变量仅 string/bool/int 的固定定义，名字 `[A-Za-z][A-Za-z0-9_]{0,31}`；`${node.name}`、`${inbound.name}`、`${subscription.name}` 为内置显示上下文，`${var.regionLabel}` 为自定义常量。未知变量错误，不允许环境变量、文件路径、函数、用户密码或订阅 token 变量。base64 外部条目无 inbound.name，取空串后规范化分隔符。
- `$authorizedProxies` 是**结构化展开标识**而非字符串拼接；只展开该用户该订阅已授权条目，永远不使 relay/landing 出现在订阅里。分组引用使用稳定 id，不依赖可能重名的显示名称；检测分组循环和无效引用。
- group type 为 select/urltest，urltest 的检测 URL 只能选择内置探测配置 ID，模板不接受任意 URL。只允许客户端探测，不由面板发出网络请求。
- P2 rules 支持 DOMAIN/DOMAIN-SUFFIX/IP-CIDR/MATCH；目标为 group:id、DIRECT 或 REJECT；MATCH 只能有一条且最后；不支持远程 rule-provider、脚本规则、任意文件路径。sing-box renderer 按自身 schema 映射，不能照抄 mihomo 键名。
- DNS 限 enabled 与服务端预定义 resolver ID 列表，禁任意代理控制面地址、外部控制器、tun、监听权限提升和下载外部 UI 等系统运行项。模板不能扩大代理条目的权限。
- 名称显示允许中文，长度≤128，禁 CR/LF/NUL；输出由 YAML/JSON/URI 标准序列化器转义；若多条渲染为同名，按稳定条目键追加短后缀并在预览标出，排序相同时以 nodeId/inboundId 稳定排序。

发布产生不可变 version，保存草稿不产生版本。跟随最新的订阅刷新后生效，固定版本的保持不变；回退通过把旧定义发布为新版本，不覆盖历史。发布事务校验受影响订阅仍引用的 group id，不能删除仍被覆写引用的组；归档不删除仍被固定版本引用的模板。

### 6.2 覆写合并与安全字段

先完成用户/节点组授权，再构造实际节点客户端基础参数。可编辑字段遵守：

```text
实际节点连接材料（基础值）
  → 管理员模板默认值
  → 当前订阅节点组默认值
  → 用户覆写（存在入站级行时选入站级，否则回退旧节点级行）
  → 强制注入受管条目的真实用户身份及不可编辑安全材料
```

保持 P1 的「入站级行优先、旧节点级行回退」而不是悄悄改成两行逐字段叠加；一旦存在入站级行，其中未设置字段直接继承模板/节点组层。UI 对该区别必须有提示。普通字段 null/缺省=继承；params 是按键合并的白名单对象，不允许 null 覆写成非法协议值；数组整体替换而非拼接。

| 字段组 | 受管 entry | 外部节点 |
|---|---|---|
| 显示名、排序、图标、proxyGroup | 可改；图标仅内置 ID，不加载任意 URL | 可改 |
| server、port | 高级设置可改并提示「仅改客户端，可能无法连接」；port 1～65535，server 是合法主机名/IP，无 scheme/path/control char | 不可改 |
| sni/serverName | Reality/Hy2 可改；统一存 sni，避免两个同义键冲突 | 不可改 |
| fingerprint、flow | Reality 客户端能力白名单；flow 仅空值/xtls-rprx-vision，并校验入站可支持；不接受任意字符串 | 不可改 |
| Hy2 obfs/obfsPassword、upMbps/downMbps | 协议专用；带宽 0～1,000,000 Mbps；混淆值写入后只回显 configured，保留/清除使用独立动作 | 不可改 |
| insecure | 仅 Hy2 高级设置，默认 false，启用要显式风险确认；模板/组不能默认开启 | 不可改 |
| UUID、SS password/method、Hy2 身份 password、Reality publicKey/shortId/privateKey | P2 普通用户均不可改；只由授权的节点管理/凭据服务提供，防止跨用户身份替换 | 不可改 |

PRD 3.7 的「可完整覆写协议参数」是较宽产品草案；P2 明确采用 P1 已实施的身份隔离下限，并只扩充经生成器支持的非身份字段。Reality spiderX 等当前 renderer 未支持的字段不显示为可用；将来支持需先补协议映射及测试，不能只加输入框。

所有写入路径，包括模板、节点组默认值、两类覆写 API，使用同一协议/格式能力注册表。非法字段返回 422 `override_field_forbidden`，不沿用 P1 的静默过滤结果来误导用户。脱敏预览只显示认证字段占位符；只有合法 `/sub/:token` 的真实下载包含客户端必需凭据。

订阅切换模板、格式、节点组和覆写保存都递增 subscription revision；P2 初期不引入 Redis。若增加生成缓存，其键至少含用户授权 revision、订阅 revision、模板 version、组 revision、节点材料/凭据 revision，且先鉴权再命中缓存。默认 `Cache-Control: private, no-store`，不把 token 放日志或前端分析事件。

### 6.3 告警规则与触发

默认每 5 分钟运行 `*/5 * * * *`（UTC）。数据库 advisory lock 保证同一检查轮次只有一个执行者；按 userId 分页，不把全部用户和记录载入内存。邮件 worker 每 10 秒领取队列，和告警扫描分开，SMTP 慢不能拖住流量摄取事务。

**流量阈值**：`used = used_up_bytes + used_down_bytes`，limit 为 users.traffic_limit_bytes。limit=0 不触发；以整数交叉相乘比较 `used*100 >= limit*threshold`，使用防溢出整数运算，不用浮点百分比作为判定。默认阈值 `[80,90,100]`，可设 1～100 的不重复递增整数，最多 5 个。

**到期提醒**：expire_at 非空才评估；天数是距绝对到期时刻的 24 小时倍数，不按用户浏览器日历日期推算。默认 `[7,3,1,0]`，允许 0～90 的不重复整数，最多 8 个。0 表示已到期通知；不到期账号无提醒。未启用/被删除用户不发常规提醒；已过期但仍启用的用户可收到一次到期通知。

相同 kind 的规则只选择一条：user 精确规则 > 当前用户授权 group 中优先级最高的规则 > all 默认规则；同层多组冲突按较小 rule id 确定，并在规则预览中显示。不能把多个组规则叠加导致同一人重复收到多封。业务默认可通过 UI 复制出用户专用规则，但不做任意表达式引擎。

### 6.4 去重、补偿与渠道

- 流量事件键为 `traffic:userId:quotaEpoch:limitBytes:threshold:email`；到期事件键为 `expiration:userId:expireAtUTC:days:email`。规则 revision 不进事件键，改规则名称/重启服务不能重复发；修改限额或延长到期日形成新事件上下文。
- 同轮跨过多个阈值，只发**最紧急的一条**，例如从 70% 跳至 101% 只发 100%；其他已越过档位写 suppressed 墓碑。同样，到期扫描中断后恢复，只发当前最紧急提醒，已过期不补发 7/3/1 天过时邮件。
- 事务中计算事件、插入 UNIQUE dedupe_key 的通知/outbox；冲突视为已处理，不重复排队。多实例并发、崩溃重启、SMTP 重试均使用同一记录；不能以「查过不存在再 insert」代替唯一约束。
- 用户关闭对应偏好时不入发送队列；已排队发送前重查偏好、emailRevision、账号状态、规则 enabled 和事件是否仍成立。续期/重置/改邮箱后旧事件 suppressed，不能重试旧有效期告警。
- 未验证邮箱为 blocked_recipient：界面说明原因，不走 SMTP，不当作成功发送；验证完成后仅重新评估当前事件，不补发已经过时的每个阈值。用户关闭通知后再开启，同事件已 sent/suppressed 的不重发。
- 流量采集不全时只能发送「已知用量达到阈值」并带截至时间/不完整标记；不能把无数据当未用量，也不能用估计数据自动封禁。到期通知不依赖流量采集是否在线。
- 渠道接口分离事件构建与发送，预留 email/telegram/webhook 的枚举扩展；P2 API 只接受 email，非 email 422 `channel_unsupported`。站内历史是投递记录，不扩建即时消息系统。

**投递语义**：数据库事件去重保证逻辑事件一次入队，SMTP 网络投递只能做到至少一次。邮局已接收但连接中断/进程在落库前崩溃，可能重发；固定 Message-ID 降低重复风险但不保证收件服务器去重。UI 和验收不得声称 SMTP exactly-once。
