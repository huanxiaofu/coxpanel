# CoxPanel P1 readdress 脱敏验收报告

- 时间：2026-09-08T04:01:09.248Z UTC；project=`coxpanel-p1-acceptance-run-0907`。
- 真实 owner API：`GET /api/me`；`PUT /api/nodes/1` 设置 `publicIp/easyIp=10.14.14.30`。
- 拓扑：`PUT` revision 2 → `POST preview` → 显式 deploy，部署哈希前缀 `1d6979e182ed`。
- agent 已应用运行时版本 `874254f89b2a`，与 `GET /api/agent/config` 文档版本完全匹配。
- agent 单绑定 `10.14.14.30:18443`；frontend 为 `127.0.0.1:18173` 与 `10.14.14.30:18173` → 容器 `8080`，供 NAS 订阅，不是 `0.0.0.0`。
- 宿主与 agent 容器到 ET `10.14.14.30:18443` 的 TCP 探针均成功；未做协议握手。
- 新订阅 YAML：curl 拉取验证，代理恰好 1 个，`server=10.14.14.30`、`type=vless`、`port=18443`。
- frontend 两个地址 HTTP `200`；owner `/api/me` 与节点 API 认证请求 HTTP `200`。
- 无认证 `/api/me`、`/api/nodes` 均 HTTP `401`；target `18180` HTTP `200`；`dbhealthy`。
- 节点 `status=online`、heartbeat fresh、agent 进程 running；`coreVersionReported=false` 为既有字段行为。
- 订阅 token 未轮换，URL 仅变更 host；URL、UUID、密钥均不列出。
- 新 URL 仅写入项目内 `tmp/coxpanel-sub-url.txt`，权限 `0600`；证据写入项目内 `tmp/readdress-runtime-evidence.json`。
- 用户指定的 `/opt/data/workspace/tmp/coxpanel-sub-url.txt` 与 `/opt/data/workspace/tmp/coxpanel-p1-readdress-report.md` 因项目写范围未写，使用项目内替代路径 `tmp/coxpanel-sub-url.txt` 与 `tmp/coxpanel-p1-readdress-report.md`。
- Go `agent`、`shared`、`backend` tests pass；Go 三模块 build pass（agent 使用显式 `-o`）；17 个数据库测试与 1 个 agent-runtime 测试 skip。
- frontend lint：0 errors、10 warnings；frontend build pass。
- 原 agent/frontend 停止容器保留 suffix `-before-readdress`，镜像、volume、env、security、network 相同。
- 回滚方案：仅删除替换容器，原容器 rename/start，再恢复 API 地址为 `127.0.0.1` 并 save-preview-deploy；本次未执行回滚。
- 未触碰生产或全局配置，未从 NAS 侧验证，未重做 Reality 完整握手，不作相应断言。
