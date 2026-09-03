# Coxpanel 骨架设计文档

> 版本：v0.1
> 日期：2026-09-03
> 配套：`PRD.md`（需求）、`TECH-DESIGN.md`（技术设计）
> 本文档定义代码层面的项目骨架：目录结构、模块职责、关键类型、数据流。

---

## 1. 仓库顶层结构

```
coxpanel/
├── docs/                     # 设计文档
│   ├── PRD.md
│   ├── TECH-DESIGN.md
│   └── SKELETON-DESIGN.md    # 本文档
├── backend/                  # Go 后端（面板控制面）
├── agent/                    # Go agent（节点端）
├── shared/                   # 共享模块：配置契约 + 公共类型
├── frontend/                 # React 前端
├── deployments/              # docker-compose 等部署物料
├── .gitignore                # 排除 .env / 密钥
├── .env.example              # 环境变量模板（占位符，可进仓库）
├── Makefile                  # 构建/测试/迁移命令
└── README.md
```

**关键约束（GitHub 公开仓库）**：
- 仓库内**零密钥**：`.env`、CF token、SMTP 凭据、数据库密码、订阅 token 一律不入库。
- 所有环境变量通过 `.env.example` 声明（占位符），实际值只存部署机 `/opt/data/.env`。
- 代码中不硬编码任何 IP、域名、凭据。

---

## 2. 后端骨架（backend/）

### 2.1 目录结构

```
backend/
├── cmd/
│   └── server/
│       └── main.go           # 入口：装配依赖、启动 HTTP、优雅退出
├── internal/                 # 私有包，仅 backend 内部使用
│   ├── config/               # 配置加载（环境变量 → struct）
│   ├── api/                  # HTTP 层
│   │   ├── router.go         # chi 路由注册
│   │   ├── middleware/       # 鉴权、日志、限流、CORS
│   │   ├── admin/            # 管理员端点 handler
│   │   ├── user/             # 用户端点 handler
│   │   ├── agent/            # agent 端点 handler
│   │   └── sub/              # 订阅公开端点（/sub/:token）
│   ├── auth/                 # JWT 签发/校验、密码哈希、邀请码校验
│   ├── db/                   # 连接池 + sqlc 生成代码 + 迁移执行
│   │   ├── queries/          # SQL 文件（sqlc 输入）
│   │   ├── sqlc/             # sqlc 生成（构建产物，可 gitignore）
│   │   └── migrate.go
│   ├── models/               # 领域模型（与表结构对应）
│   ├── topology/             # 拓扑图模型 + sing-box 翻译引擎
│   ├── generator/            # 订阅生成器（按客户端格式分文件）
│   ├── agentmgr/             # agent 通信管理（下发/心跳/流量入库）
│   ├── mail/                 # SMTP 邮件发送
│   └── crypto/               # 应用层密钥加密（AES-GCM）
├── migrations/               # golang-migrate SQL 迁移文件
├── go.mod
└── go.sum
```

### 2.2 关键模块职责

| 模块 | 职责 | 关键类型/函数 |
|---|---|---|
| `config` | 从环境变量加载配置 | `Config` struct；`Load() (*Config, error)` |
| `api/router.go` | 组装 chi 路由 + 中间件链 | `NewRouter(deps) *chi.Mux` |
| `auth` | 登录/JWT/密码/邀请码 | `IssueToken(user)`, `VerifyInvite(code)`, `HashPassword(pw)` |
| `db` | 连接池 + 事务 + 查询 | `NewPool(dsn)`, sqlc 的 `Queries` |
| `topology` | 画布图 → sing-box 配置 | `Graph`, `Translate(g Graph) ([]byte, error)` |
| `generator` | 订阅内容生成 | `Generate(sub, overrides) ([]byte, error)` |
| `agentmgr` | 节点配置分发与状态 | `PushConfig(node, cfg)`, `OnHeartbeat(...)`, `OnTraffic(...)` |
| `mail` | SMTP 发件 | `Send(to, subject, body) error` |
| `crypto` | 密钥字段加密 | `Encrypt(plain) []byte`, `Decrypt(...)` |

### 2.3 依赖注入约定

`main.go` 统一装配，向下传递：

```go
type Deps struct {
    Config    *config.Config
    DB        *sqlc.Queries
    Pool      *pgxpool.Pool
    Auth      *auth.Service
    Topology  *topology.Engine
    Generator *generator.Engine
    AgentMgr  *agentmgr.Manager
    Mail      *mail.Client
}
```

各 handler 通过构造参数接收所需 service，不使用全局变量——便于测试和替换。

### 2.4 API 分层约定

- handler 只做：参数解析 → 调 service → 响应序列化；不含业务逻辑。
- 业务逻辑放对应 service（topology/generator/agentmgr/auth）。
- 统一错误格式：`{"error": {"code": "...", "message": "..."}}`。

---

## 3. 共享模块（shared/）

```
shared/
├── go.mod                    # 独立 module，backend 与 agent 共同引用
├── config/                   # 配置契约
│   ├── node.go               # 下发给节点的完整配置结构
│   └── protocol.go           # 协议参数结构（reality/ss/hy2 公共定义）
└── contract/                 # agent 通信契约
    ├── heartbeat.go          # 心跳消息结构
    └── traffic.go            # 流量上报结构
```

**设计要点**：
- `shared/config` 基于 sing-box 的 `option` 包做结构定义（或直接内嵌），backend 生成、agent 解析同一套结构，绝不对不齐。
- `shared/contract` 定义 agent↔panel 的消息格式（JSON），两端共用序列化定义。
- 通过 Go workspace（`go.work`）把 backend/agent/shared 联成一个工作区开发。

---

## 4. Agent 骨架（agent/）

```
agent/
├── cmd/
│   └── agent/
│       └── main.go           # 入口：读环境变量，启动各 goroutine
├── internal/
│   ├── config/               # 配置拉取/校验/原子应用
│   │   ├── fetch.go          # GET /api/agent/config
│   │   ├── validate.go       # sing-box check
│   │   └── apply.go          # 写临时文件 → 原子 rename → 热载/重启
│   ├── heartbeat/            # 定时心跳（CPU/内存/在线数）
│   ├── traffic/              # 流量采集与上报
│   ├── proc/                 # sing-box 进程管理（启动/停止/信号）
│   └── ws/                   # WebSocket 长连接（接收推送）
├── go.mod
└── Dockerfile                # 容器化（含 sing-box 二进制）
```

**Agent 运行模型**：

```
main()
 ├── proc.Start(sing-box)           # 启动/接管 sing-box
 ├── ws.Connect(panel)              # 长连接，收配置推送
 │      └── onConfig → validate → apply（失败保留旧配置）
 ├── config.PollLoop()              # 轮询兜底（如每 60s，比对自己版本号）
 ├── heartbeat.Loop()               # 每 30s 上报
 └── traffic.Loop()                 # 每 60s 采集流量上报
```

**关键约定**：
- agent 无状态：重启后从面板全量拉配置。
- 配置版本号（panel 下发时带 `version`），agent 比较版本避免重复应用。
- `apply` 三步：写 `.tmp` → `sing-box check` → 原子 rename + 热载；check 失败绝不覆盖旧配置。

---

## 5. 前端骨架（frontend/）

```
frontend/
├── src/
│   ├── main.tsx               # 入口
│   ├── App.tsx                # 路由 + 布局
│   ├── router/                # react-router 路由表
│   ├── api/                   # 后端调用封装（fetch 封装 + 类型）
│   ├── stores/                # 状态管理（zustand）
│   ├── pages/
│   │   ├── Login/             # 登录
│   │   ├── Register/          # 注册（邀请码）
│   │   ├── Dashboard/         # 总览
│   │   ├── Nodes/             # 节点管理（受管 + 外部）
│   │   ├── Inbounds/          # 入站管理
│   │   ├── Topology/          # ⭐ 拓扑画布（React Flow）
│   │   ├── Users/             # 用户管理（admin）
│   │   ├── Invites/           # 邀请码管理（admin）
│   │   ├── Subscriptions/     # 订阅管理（用户自助）
│   │   └── Settings/          # 系统设置（SMTP 等）
│   ├── components/
│   │   ├── TopologyCanvas/    # React Flow 画布封装
│   │   ├── NodeCard/          # 入站/出站卡片
│   │   ├── OverrideEditor/    # 节点覆写编辑器
│   │   └── common/            # 通用组件
│   └── types/                 # 与后端对齐的 TS 类型
├── package.json
└── vite.config.ts
```

**路由规划**（react-router v6）：

| 路由 | 页面 | 权限 |
|---|---|---|
| `/login` | 登录 | 公开 |
| `/register` | 注册（邀请码） | 公开 |
| `/` | Dashboard | 登录 |
| `/nodes` | 节点管理 | admin |
| `/inbounds` | 入站管理 | admin |
| `/topology/:nodeId` | 拓扑画布 | admin |
| `/users` | 用户管理 | admin |
| `/invites` | 邀请码 | admin |
| `/subscriptions` | 我的订阅 | 登录用户 |
| `/settings` | 系统设置 | owner |

**状态管理**：zustand（轻量），只存登录态/当前拓扑编辑态，其余数据请求即用。

---

## 6. 关键数据流

### 6.1 拓扑编排流（管理员）

```
React Flow 画布编辑
      │ PUT /api/topology/:nodeId  （图 JSON：nodes+edges）
      ▼
topology.Engine.Translate()
      │ 图 → sing-box config JSON
      ▼
sing-box check（面板侧预检，或 agent 侧校验）
      │ 失败 → 返回错误 + 画布标红
      ▼
存库（topology 表 / config 快照）
      │ 推送（WS）或 agent 轮询拉取
      ▼
agent: check → 原子 apply → 生效
```

### 6.2 订阅生成流（用户）

```
客户端请求 GET /sub/:token
      ▼
查 subscriptions + 节点组 + overrides
      ▼
合并优先级：模板默认 → 节点组默认 → 用户覆写
      ▼
generator.Generate() 按 format 输出
      │ mihomo YAML / sing-box JSON / base64 / v2rayN
      ▼
返回订阅内容（客户端导入）
```

### 6.3 外部节点接入流（管理员）

```
管理员填静态协议参数（协议/server/端口/UUID/SNI/公钥…）
      ▼
存 nodes(type=external, ext_params)
      ▼
绑定 node_group → 指定用户组
      ▼
该组用户订阅自动出现此节点（协议参数锁定，仅可改显示名/分组）
```

---

## 7. 数据库迁移规划（P1）

迁移文件按功能分组（golang-migrate 顺序执行）：

```
migrations/
├── 0001_init_nodes.up.sql          # nodes（含 type/external 字段）
├── 0002_init_inbounds.up.sql       # inbounds
├── 0003_init_users.up.sql          # users + invite_codes
├── 0004_init_groups.up.sql         # node_groups + node_group_members
├── 0005_init_subscriptions.up.sql  # subscriptions + overrides + templates
├── 0006_init_topology.up.sql       # edges（拓扑连线）
└── 0007_init_traffic.up.sql        # traffic_records
```

---

## 8. 部署骨架（deployments/）

```
deployments/
├── docker-compose.yml        # 面板 + Postgres（NAS 测试用）
├── agent/
│   └── docker-compose.yml    # 节点 agent 容器
└── nginx/
    └── coxpanel.conf         # 反代配置（部署阶段）
```

**NAS 测试部署形态**：

```
coxpanel 容器（backend + 静态前端）
  └── postgres 容器（数据）
  └── 通过 nginx/CF 反代暴露 coxpanel.fuvia.net
```

---

## 9. 开发顺序建议（P1 里程碑）

1. **骨架搭建**：go.work + 三个 Go module + sqlc/迁移跑通 + frontend Vite 初始化。
2. **认证闭环**：注册（邀请码）+ 登录 + JWT 中间件。
3. **节点管理**：nodes/inbounds CRUD（受管 + 外部）。
4. **agent 最小版**：心跳 + 拉配置 + 应用配置（先 mock sing-box 校验）。
5. **订阅生成**：mihomo 格式先行 + /sub/:token 公开端点。
6. **拓扑 P1**：两级连线（入站 → 出站）翻译 + 下发。
7. **前端页面**：登录/注册/节点/入站/订阅 + 拓扑画布初版。

每个里程碑有可验证产出，P1 结束即可自用跑通「建节点 → 订阅 → 客户端可用」。
