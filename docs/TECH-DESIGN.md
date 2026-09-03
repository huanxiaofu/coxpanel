# 自研代理面板 — 技术设计文档

> 版本：v0.1 草案
> 日期：2026-09-03
> 配套：`PRD.md`

---

## 1. 技术选型

| 层 | 选型 | 理由 |
|---|---|---|
| 后端 | **Go** | 与 agent 同语言；内嵌 sing-box `option` 包生成/校验配置，无需造轮子 |
| Web 框架 | **chi**（轻量路由） | 接近标准库、可组合、中间件生态好 |
| 数据库 | **PostgreSQL** | 多租户 + 并发流量写入 |
| 查询 | **sqlc**（编译期生成类型安全 SQL） | SQL 写文件、生成 Go 代码，避免拼错 |
| 迁移 | **golang-migrate** | |
| 前端框架 | **React + Vite + TypeScript** | |
| UI 组件 | **Ant Design** | 后台管理表单/表格最全，中文文档好 |
| 拓扑画布 | **React Flow** | 节点管线拖拽，MIT 协议 |
| 图表 | **ECharts** | 流量曲线 |
| 节点核心 | **sing-box** | 单二进制全协议、纯 JSON 配置、`check` 原子校验、原生多入站 |
| 节点 Agent | **Go** | 与 sing-box 同源、内嵌配置结构体、单静态二进制、镜像小 |
| 配置契约 | 后端 + agent **共享 Go module** 的配置结构体 | 生成/应用/校验天然对齐 |
| 部署 | Docker Compose（面板）+ 容器化 agent | |

---

## 2. 总体架构

```
┌─────────────────────────────────────────────────────────┐
│  浏览器 (React + React Flow + AntD)                      │
└──────────────────────┬──────────────────────────────────┘
                       │ HTTPS (REST + WebSocket)
┌──────────────────────▼──────────────────────────────────┐
│  Panel 控制面 (Go/chi + Postgres)                        │
│  ├─ 管理员 API：节点/入站/拓扑/用户/邀请码/模板           │
│  ├─ 用户 API：注册/登录/订阅管理                          │
│  ├─ 订阅端点：/sub/<token>  →  生成客户端配置             │
│  ├─ 拓扑引擎：画布图 → sing-box config                   │
│  ├─ 配置生成器：sing-box JSON / mihomo YAML / base64     │
│  └─ agent 通信：配置下发 + 心跳 + 流量上报                 │
└──────────────────────┬──────────────────────────────────┘
                       │ HTTPS / WS（优先，轮询兜底）
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
   ┌─ Agent1 ─┐   ┌─ Agent2 ─┐   ┌─ AgentN ─┐
   │ sing-box │   │ sing-box │   │ sing-box │
   └──────────┘   └──────────┘   └──────────┘
     (服务器A)      (服务器B)      (服务器N)
```

### 2.1 关键设计决策

1. **面板生成配置、agent 负责应用**：面板是配置的唯一真相源，agent 是无状态的执行者。区别于 Xboard（agent 自己拼配置）、Marzban（面板直管 gRPC）。
2. **agent 断连不中断服务**：配置已生效则继续跑，恢复连接后重新同步。
3. **配置下发原子性**：agent 收到新配置 → `sing-box check` → 写入临时文件 → 原子 rename → 热载/重启。失败保留旧配置。
4. **拓扑是核心抽象**：画布图是唯一编辑入口，数据库存图结构，不存手写 JSON（避免 Remnawave 的 JSON 黑盒）。

---

## 3. 数据模型（核心表）

### 3.1 实体关系

```
nodes (服务器) 1 ──── N inbounds (入站)
     │                        │
     │                        └── 有 role: entry / landing / relay
     │
     └── 1:N edges (拓扑连线：from_inbound → to_outbound)

users 1 ──── N subscriptions
  │                    │
  └── 邀请码绑定 ── invite_codes
                       │
subscriptions 绑定 node_groups（节点组）+ templates
```

### 3.2 表结构（草案）

```sql
-- 服务器节点（受管 + 外部）
CREATE TABLE nodes (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    type          TEXT NOT NULL DEFAULT 'managed',  -- managed / external
    public_ip     TEXT,             -- external 可空
    easy_ip       TEXT,             -- EasyTier 内网 IP（受管）
    ssh_host      TEXT,             -- 首次部署用（受管）
    ssh_user      TEXT,
    ssh_port      INT DEFAULT 22,
    core_version  TEXT,             -- agent 上报的 sing-box 版本（受管）
    status        TEXT DEFAULT 'offline',  -- 受管：online/offline/maintenance；external：无状态
    last_seen_at  TIMESTAMPTZ,
    -- external 节点：静态协议参数（一条订阅条目）
    ext_protocol  TEXT,             -- vless-reality / shadowsocks / hysteria2 / trojan ...
    ext_params    JSONB,            -- 完整静态参数（server/port/uuid/sni/publicKey/flow/obfs...）
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now()
);

-- 入站
CREATE TABLE inbounds (
    id            BIGSERIAL PRIMARY KEY,
    node_id       BIGINT REFERENCES nodes(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    protocol      TEXT NOT NULL,      -- vless-reality / shadowsocks / hysteria2
    role          TEXT NOT NULL DEFAULT 'entry',  -- entry / landing / relay
    listen_addr   TEXT DEFAULT '::',
    listen_port   INT NOT NULL,
    config        JSONB NOT NULL,      -- 协议特定参数（Go struct JSON 序列化）
    min_client_ver TEXT DEFAULT '1.8.2',  -- Reality minClientVer 统一管理
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now()
);

-- 拓扑连线（中转关系）
CREATE TABLE edges (
    id             BIGSERIAL PRIMARY KEY,
    from_inbound_id BIGINT REFERENCES inbounds(id) ON DELETE CASCADE,
    to_node_id     BIGINT REFERENCES nodes(id) ON DELETE CASCADE,
    to_inbound_id  BIGINT REFERENCES inbounds(id) ON DELETE CASCADE,  -- 落地入站
    weight         INT DEFAULT 1,      -- 负载均衡权重
    created_at     TIMESTAMPTZ DEFAULT now()
);

-- 用户
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    email         TEXT UNIQUE,
    role          TEXT DEFAULT 'user',  -- owner / admin / user
    traffic_limit_bytes BIGINT DEFAULT 0,  -- 0 = 不限
    expire_at     TIMESTAMPTZ,
    created_at    TIMESTAMPTZ DEFAULT now()
);

-- 邀请码
CREATE TABLE invite_codes (
    id            BIGSERIAL PRIMARY KEY,
    code          TEXT UNIQUE NOT NULL,
    max_uses      INT DEFAULT 1,
    used_count    INT DEFAULT 0,
    expires_at    TIMESTAMPTZ,
    node_group_id BIGINT,             -- 绑定的节点组权限
    created_by    BIGINT REFERENCES users(id),
    created_at    TIMESTAMPTZ DEFAULT now()
);

-- 节点组
CREATE TABLE node_groups (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);
CREATE TABLE node_group_members (
    group_id BIGINT REFERENCES node_groups(id),
    node_id  BIGINT REFERENCES nodes(id),
    PRIMARY KEY (group_id, node_id)
);

-- 订阅
CREATE TABLE subscriptions (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    token       TEXT UNIQUE NOT NULL,   -- URL /sub/<token>
    format      TEXT DEFAULT 'mihomo',  -- mihomo / sing-box / base64 / v2rayn
    node_group_id BIGINT REFERENCES node_groups(id),
    template_id BIGINT,                -- 订阅模板
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);

-- 订阅-节点覆写（用户对节点的参数调整，服务端持久化，随订阅下发）
CREATE TABLE subscription_node_overrides (
    id              BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT REFERENCES subscriptions(id) ON DELETE CASCADE,
    node_id         BIGINT REFERENCES nodes(id) ON DELETE CASCADE,
    display_name    TEXT,               -- 覆写显示名/排序/图标
    sort_order      INT,
    icon            TEXT,
    params          JSONB NOT NULL,     -- 覆写的协议参数（SNI/port/flow/Reality/Hy2/SS...）
    proxy_group     TEXT,               -- 客户端分组归属
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE (subscription_id, node_id)
);

-- 订阅模板
CREATE TABLE templates (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    format     TEXT NOT NULL,          -- 对应订阅 format
    definition JSONB NOT NULL,          -- 模板定义（分组/规则/占位符）
    created_at TIMESTAMPTZ DEFAULT now()
);

-- 流量统计（用户级，按节点）
CREATE TABLE traffic_records (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT REFERENCES users(id),
    node_id     BIGINT REFERENCES nodes(id),
    inbound_id  BIGINT REFERENCES inbounds(id),
    up_bytes    BIGINT DEFAULT 0,
    down_bytes  BIGINT DEFAULT 0,
    period_start TIMESTAMPTZ NOT NULL,  -- 聚合周期（如每小时）
    UNIQUE (user_id, node_id, inbound_id, period_start)
);
```

---

## 4. 拓扑 → 配置翻译逻辑

### 4.1 核心映射

```
React Flow 画布                          sing-box 配置
─────────────────────                    ─────────────────────────
[入站卡片 reality :5443]       ───►       inbounds[0] (vless+reality)
[入站卡片 ss :8388]           ───►       inbounds[1] (shadowsocks)
[连线: reality → ss@另一台]   ───►       outbounds[] + route.rules[]
```

### 4.2 翻译规则

1. **每个入站卡片** → 一个 `inbounds[]` 元素。
2. **同机器内的连线**（entry → landing 同 node）→ 走本地 `route.rules`（inbound 标签 → outbound）。
3. **跨机器连线**（entry → 另一台的 landing）→ 生成 `outbounds[]`（ss/hy2 出站）+ `route.rules`。
4. **中转链**（A 入口 → B 中转 → C 落地）→ 递归翻译成多层 outbound。
5. **出站默认** → `direct`（直连）或指定落地机。

### 4.3 订阅生成的覆写层（优先级）

订阅内容生成时，按以下顺序合并参数（后者覆盖前者）：

```
管理员模板默认值  →  节点组默认配置  →  用户节点覆写（subscription_node_overrides）
```

- 用户刷订阅（GET /sub/:token）时实时合并，任何设备刷新即拿到最新覆写。
- 覆写只影响**客户端订阅内容**，不改动节点实际运行配置（节点运行配置由拓扑决定）。
- **外部节点覆写边界**：外部节点的协议参数由管理员填死，用户仅可改显示名/排序/分组，**不可覆写协议参数**（否则连不上）。受管节点用户可完整覆写。

### 4.4 校验

- 生成后调 `sing-box check -c <config>`（agent 侧或面板侧预检）。
- 校验失败：返回具体错误，画布标红对应卡片/连线。

---

## 5. API 设计（草案）

### 5.1 认证

- `POST /api/auth/login` → JWT
- `POST /api/auth/register`（需邀请码）
- 管理员接口要求 role=owner/admin

### 5.2 核心端点

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/nodes` | 节点列表/创建 |
| GET/PUT/DELETE | `/api/nodes/:id` | 节点详情/更新/删除 |
| GET/POST | `/api/inbounds` | 入站列表/创建（含 role） |
| GET/PUT/DELETE | `/api/inbounds/:id` | 入站详情/更新/删除 |
| GET/PUT | `/api/topology/:node_id` | 获取/保存某节点的拓扑图 |
| POST | `/api/topology/:node_id/deploy` | 生成配置并下发 |
| GET/POST | `/api/invite-codes` | 邀请码列表/生成 |
| GET/POST | `/api/subscriptions` | 订阅列表/创建 |
| GET/DELETE | `/api/subscriptions/:id` | 订阅详情/删除 |
| GET/PUT | `/api/subscriptions/:id/overrides` | 获取/更新该订阅的节点覆写 |
| GET | `/sub/:token` | 订阅内容（公开，客户端拉取） |
| GET | `/api/traffic/:user_id` | 用户流量统计 |
| GET | `/api/nodes/:id/stats` | 节点实时状态 |

### 5.3 Agent 通信

| 方向 | 端点/方式 | 说明 |
|---|---|---|
| agent → panel | `POST /api/agent/heartbeat` | 心跳 + 系统状态 |
| agent → panel | `POST /api/agent/report-traffic` | 流量上报 |
| agent → panel | `GET /api/agent/config` | 拉取最新配置（轮询兜底） |
| panel → agent | WebSocket 推送 | 配置变更即时下发 |

---

## 6. 安全设计

- 密码：argon2id 哈希。
- 密钥（Reality privateKey、ss password、订阅 token）：数据库加密存储（应用层 AES-GCM，密钥来自环境变量）。
- JWT 短期 + refresh token。
- 邀请码：防爆破（速率限制 + 失败计数）。
- agent 认证：每节点生成独立 API key。
- 面板不记录/不回显用户明文密码与长期凭据。

---

## 7. 项目结构（草案）

```
coxpanel/
├── docs/
│   ├── PRD.md              # 需求分析
│   └── TECH-DESIGN.md      # 本文档
├── backend/                # Go + chi
│   ├── cmd/server/         # 入口
│   ├── internal/
│   │   ├── api/            # 路由 handler
│   │   ├── db/             # sqlc 生成的查询 + 迁移
│   │   ├── models/         # 数据模型
│   │   ├── topology/       # 图模型 + 翻译引擎
│   │   ├── generator/      # 订阅生成器
│   │   ├── agent/          # agent 通信
│   │   └── auth/           # 鉴权
│   ├── migrations/
│   └── go.mod
├── agent/                  # Go agent（节点端）
│   ├── cmd/agent/
│   ├── internal/
│   │   ├── config/         # 拉取/校验/应用配置（内嵌 sing-box option）
│   │   ├── heartbeat/
│   │   └── traffic/
│   └── go.mod
├── shared/                 # 后端+agent 共享：sing-box 配置契约
│   └── config/             # 配置结构体（基于 sing-box option）
├── frontend/               # React + Vite + TS
│   ├── src/
│   │   ├── pages/          # 节点/入站/拓扑/用户/订阅
│   │   ├── components/     # React Flow 画布、表单
│   │   └── api/            # 后端调用封装
│   └── package.json
└── docker-compose.yml
```

---

## 8. 分期落地计划

- **P1（MVP）**：backend 骨架 + nodes/inbounds CRUD + agent 心跳 + 订阅生成（mihomo/sing-box）+ 邀请码注册 + 基础拓扑（两级连线）。
- **P2**：React Flow 完整画布 + 多级中转链 + 流量图表 + 模板自定义。
- **P3**：负载均衡、告警、Bot 通知、商业化能力。

---

## 9. 开放决策点（已定/待定）

**已定：**
1. 后端 + Agent 均 **Go**（全 Go 栈，内嵌 sing-box `option` 配置契约）。
2. 项目名 **Coxpanel**。
3. 部署：面板 + agent 均容器化，先在 **NAS 容器**测试；agent 后续可裸机部署（首期不做）。
4. 用户认证：邮箱 + 邀请码，用户填「用户名 + 密码 + 邮箱」，管理员配 **SMTP** 发件。
5. **不迁移** Remnawave 存量数据。
6. 公网域名：**fuvia.net** 二级域名（CF zone 已确认可管理；具体子域名部署阶段定，建议 `coxpanel.fuvia.net`）。
7. SMTP：使用 fuvia.net 自建邮局（部署阶段取连接参数）。
8. **项目将上传 GitHub**：代码仓库需 `.gitignore` 排除一切密钥（.env、CF token、SMTP 凭据、订阅 token 等），密钥只存 NAS `/opt/data/.env`。
9. Web 框架：**chi**（已定）。

**待定：**
1. Cloudflare DNS API token 的 dns_records 列表权限异常（Authentication error），部署阶段需排查（可用 CF 面板手动建记录兜底）。
