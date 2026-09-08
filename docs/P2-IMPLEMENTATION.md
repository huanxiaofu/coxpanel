# P2 实现与隔离验收

## 启用边界

迁移 `0011`～`0014` 为增量迁移。NULL 模板仍使用内置客户端生成器，不重置订阅 token 或用户凭据。模板发布、订阅切换和覆写写入使用事务锁；订阅元数据的 `expectedRevision` 对应订阅版本，覆写编辑的 `expectedRevision` 对应覆写行版本（新行传 0）。覆写写入同时递增订阅版本。

拓扑 P2 需要配置应用层加密密钥并使用支持 `topology-chain-v2` 的新 agent。保存图只写草稿，10 分钟预览必须由管理员显式发布。发布先 prepare/check，再从落地向入口 apply/ack；失败恢复仍需真实回执。心跳在线不是部署成功。配置响应保留原始 sing-box 字节，避免 JSON 压缩改变 runtimeVersion/SHA256。

旧单节点保存入口也参与同一全图事务校验与 graphRevision；跨节点依赖拒绝旧 version 部署并返回 `upgrade_required`。旧无 revision 请求仅作兼容入口，新编辑器必须传 expectedRevision。发布历史每小时清理：保留最近 30 天和每节点至少十次；当前部署引用及任何活跃/待恢复节点的依赖历史不会被清理。

授权组/配额到期修改与发布回执串行化：授权变更保守地递增全部受管节点 generation 并中止活跃发布。每次新回执还会重新渲染当前有效用户集合，防止自然到期发生在 prepare 取配置之后仍接受旧材料。此策略优先撤权安全，代价是无关节点的并行批次也可能需要重新预览；恢复旧路由始终使用当前授权集合。

模板、版本、告警规则和通知列表返回 `{items,nextCursor}`，cursor 为十进制稳定 ID，limit 默认 50、最大 200。通知数据库查询使用键集分页；模板和规则当前在读取集合后截页。前端 API 同时兼容旧数组并跟随分页，避免第一页以外的模板不可选。

## 流量采集

真实采集使用 sing-box 本机 V2Ray stats gRPC API。标准发行包可能未包含 `with_v2ray_api`；本地验收另使用同版本源码、带 `with_v2ray_api,with_utls,with_quic` 的测试内核。不要仅因心跳在线就声称流量采集已启用。

- 面板环境 `COXPANEL_STATS_LISTEN` 指定下发到各节点的 loopback 地址，例如 `127.0.0.1:10085`。
- agent 环境 `COXPANEL_STATS_ADDR` 必须与该节点收到的 stats 地址相同，只接受 loopback IP。
- 未配置时不生成 stats listener、不采样、不填充历史假数据。首次采样和内核 epoch 变化只建立基线。
- agent 持久保存 epoch/sequence/pending report；服务端幂等摄取、入口用户计量与全部入站节点吞吐分开。5m/1h/1d 聚合分别保留 30/180/730 天。
- 若一台测试机器启动多实例，每个实例必须使用独立业务端口、stats 端口及状态文件。

## 邮件

SMTP 仅由 `COXPANEL_SMTP_HOST/PORT/USER/PASSWORD/FROM` 等环境配置，端口 587，强制 STARTTLS。浏览器只读摘要；测试邮件只能发往当前 owner 邮箱。通知正文与收件人使用 AEAD 加密并绑定记录与用途，无加密配置不启动投递。设置 `COXPANEL_REQUIRE_EMAIL_VERIFICATION=true` 前必须准备 SMTP 和加密配置。

通知事件逻辑去重不等于 SMTP exactly-once；sent 仅代表邮局接收。本任务不配置生产邮件凭据、不修改 DNS、不声称真实收件箱投递验收。

## 验证方式

工具链使用 `/opt/data/go/bin/go`。分别在 `shared`、`agent`、`backend` 执行 `go build ./...`、`go vet ./...`、`go test ./... -count=1`；前端执行 `npm run build`。

数据库测试沿用 opt-in `P1_TEST_DB_URL`、独立 schema 及清理模式。本次仅使用批准的 `127.0.0.1:55439/p2_test` 隔离实例，不使用默认或生产 DSN。

真实内核验收入口：

- `COXPANEL_REAL_SINGBOX`：锁定 sing-box 1.13.21 的测试二进制。
- `P2_TEST_MIHOMO`：本地 mihomo 测试二进制。
- `P1_TEST_AGENT_BINARY`：本项目构建出的 agent；测试自动启动并清理自己创建的子进程。
- `P2_TEST_STATS_SINGBOX`：带 V2Ray stats 功能的本地测试内核，仅用于 agent 的真实统计测试。

关键测试：`TestP2ThreeHopRealAgentsPrepareApplyAndHandshake`、`TestP2ThreeHopPrepareApplyAndRollbackIntegration`、`TestPanelDatabaseAPIAndProtocolAcceptance`、`TestP2TemplateOverrideHTTPAuthorizationAndPreview`、`TestP2RealTrafficCollectorReadsCore`、`TestP2TrafficIdempotencyRollupAndReset`、`TestP2VerificationTicketInviteAtomicity`、`TestP2AlertSuppressionAndTransactionalMail`。

边界回归：`TestP2LegacyTopologyCannotBypassGraphOrDeployChain`、`TestP2TopologyRetentionProtectsRecentReferencedAndRecovery`、`TestP2PaginationLimitsAndContinuation`。两跳协议回归改用 P2 发布和合成回执，再启动真实内核执行断路验收；三跳测试使用真正三个 agent 的回执，二者不能混称。

## 设计补充解释

仓库中的 `P2-DESIGN.md` 共 409 行，实际止于 6.4，没有任务书所述第 7～10 节正文。缺失部分按任务书验收条目、迁移字段及现有安全约定实施。归档仍被订阅引用的模板返回 409，不物理删除；已发布格式不可改变。图标使用本地标识符，不加载外部 URL；base64 不接受代理组、路由、DNS 或协议覆写。模板/节点组层禁止保存混淆密码，用户 Hy2 混淆密码用 keep/replace/clear 独立操作并仅回显 configured。
