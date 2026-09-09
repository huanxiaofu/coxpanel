# sing-ui

图形化代理节点编排面板（自托管）。替代 Remnawave 的自研方案。

> 产品现统一命名 **sing-ui**（原 Coxpanel）。架构修订阅读顺序：`docs/REARCHITECT.md` → `docs/REARCHITECT-EXT.md` → `docs/REARCHITECT-NEXT.md`，冲突以 NEXT 为准。R1 提供“入口创建、出口连线”的独立合成交互原型；真实后端纵切、轻量 Agent、授权与迁移按后续阶段实施，DDNS 留在单独批准的 R4+。
>
> 下方现有命令的 `COXPANEL_*`、数据库标识和当前仓库地址保留为原项目兼容标识；仓库目标名为 `sing-ui`，实际仓库、模块、镜像与部署改名另行批准执行，不把文档改名替换成无法运行的命令。

## 技术栈

- **后端**：Go + chi + PostgreSQL
- **Agent**：Go（节点端，管理 sing-box）
- **前端**：React + Vite + TypeScript + Ant Design
- **节点核心**：sing-box（Reality / SS / Hysteria2）
- **共享契约**：shared/ Go module（backend 与 agent 共用配置结构）

## R1 交互原型（独立、无后端）

```sh
cd frontend
npm ci
npm run prototype
```

在本机浏览器打开 `http://127.0.0.1:4175/prototype/topology`。启动命令仅绑定 loopback，不替换现有面板或生产服务；远程评审请使用已有的安全端口转发方式。也可以在已有前端开发服务访问 `/prototype/`。

- 拖 `HK-zouter` 到画布 → 双击占位卡或点击「配置入口」→ Reality 的协议区检查 SNI/dest、安全区生成演示材料 →「创建并应用」。
- 同机再拖一次，选择 Shadowsocks、独立端口和演示材料；无连线即本机直出。
- 创建 `SG-edge` 的内部入口，拖把手连接或使用「连接到…」，确认「应用更改」；连线仅改草稿，清线需确认直出影响。
- 可体验 full/lite/NAT 能力提示、端口冲突、证书引用、模拟失败/重试、八项主导航和浅深色/小屏布局。

所有服务器、地址、能力、材料和部署状态均为合成演示。**无真实密钥生成、业务 API 请求、Agent 部署或订阅交付**。只持久化主题偏好；刷新清空本次工作区。原型不挂载旧认证提供器，也不读取现有登录凭据；原有登录及业务路由保留。

桌面宽度 ≥1200px 时，配置入口会停靠在右侧，与服务器资产、画布组成三栏；小屏改为覆盖式抽屉。窗口缩小时，可点击画布左下角「适配全部入口」。确认生产布局请运行 `npm run build`，再运行 `npm run preview -- --host 127.0.0.1 --port 4176 --strictPort`，打开 `/prototype/topology`。仅看开发服务不足以验证懒加载 CSS：旧的单个 `lazy` 内条件 import 曾使生产页面漏载原型样式，入口现已分别声明。

R1 完成工程验证后仍需用户确认视觉与手感，确认前不得进入 R2。当前阶段报告与总进度在项目本地 `tmp/sing-ui-r1-status.md`、`tmp/sing-ui-roadmap.md`（按现有规则忽略，不提交测试产物）。前端现无 unit-test 脚本，不新增测试框架；使用已有 lint/build、隔离浏览器验收与三个 Go 模块的测试、构建和 vet。

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
