# sing-ui

图形化代理节点编排面板（自托管）。替代 Remnawave 的自研方案。

> 产品现统一命名 **sing-ui**（原 Coxpanel）。架构修订阅读顺序：`docs/REARCHITECT.md` → `docs/REARCHITECT-EXT.md` → `docs/REARCHITECT-NEXT.md`，冲突以 NEXT 为准。完整／轻量 Agent 与“入口创建、出口连线”是待实施方案，DDNS 留在 R4+；本轮仅修订文档，不表示这些功能已上线或 UI 品牌代码已更新。
>
> 下方现有命令的 `COXPANEL_*`、数据库标识和当前仓库地址保留为原项目兼容标识；仓库目标名为 `sing-ui`，实际仓库、模块、镜像与部署改名另行批准执行，不把文档改名替换成无法运行的命令。

## 技术栈

- **后端**：Go + chi + PostgreSQL
- **Agent**：Go（节点端，管理 sing-box）
- **前端**：React + Vite + TypeScript + Ant Design
- **节点核心**：sing-box（Reality / SS / Hysteria2）
- **共享契约**：shared/ Go module（backend 与 agent 共用配置结构）

## 核心能力

- 一机多入站：一台服务器一个 sing-box 内核挂 N 个入站（入口/落地/中转）
- 拓扑编排：React Flow 画布拖拽 → 翻译 sing-box 配置（P2 完整化）
- 外部节点：静态参数接入，绑定节点组给指定用户
- 邀请制注册：管理员邀请码 + 邮箱
- 多订阅多模板：一用户多订阅链接，刷订阅即全设备同步
- 节点覆写：用户在服务端调节点参数（三层优先级），P1 已具备数据模型与合并逻辑

## 快速开始（开发）

```bash
# 依赖：Go 1.26+、Node 20+、Docker
# 部署环境必须从外部注入这些值，不要提交 .env 或密码。
export COXPANEL_DB_PASSWORD="$(openssl rand -hex 24)"
export COXPANEL_DB_URL="postgres://coxpanel:${COXPANEL_DB_PASSWORD}@postgres:5432/coxpanel?sslmode=disable"
export COXPANEL_JWT_SECRET="$(openssl rand -hex 32)"
docker compose up --build -d
```

环境变量见 backend/internal/config/config.go（`COXPANEL_*` 前缀）；容器化构建与隔离验收材料见 `deployments/README.md`。

## 当前状态：P1 开发中

- [x] 骨架（go.work + 3 module + 前端 Vite）
- [x] 认证闭环（邀请码注册 + 登录 + JWT）
- [x] 节点管理（受管 + 外部）
- [x] Agent（心跳 + 拉配置 + 原子应用）
- [x] 订阅生成（mihomo + base64）+ /sub/:token
- [x] 拓扑翻译引擎（inbounds + edges → sing-box JSON）
- [x] 前端页面（登录/注册/总览/节点/订阅）

容器构建、首次 owner 引导和隔离验收流程见 `deployments/README.md`；完整 P1 仍需控制面与真实多协议/多跳验收完成。

## 待办（P2+）

- [ ] React Flow 拓扑画布完整版 + 多级中转链
- [ ] 覆写编辑 UI
- [ ] 流量统计入库 + 图表
- [ ] SMTP 邮件验证
- [ ] 节点组管理 UI

## 安全

- 密钥只存部署机环境变量，仓库零密钥
- 密码 argon2id（当前 bcrypt，待迁移）
- 订阅 token 随机生成、可重置
