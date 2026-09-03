# Coxpanel

图形化代理节点编排面板（自托管）。替代 Remnawave 的自研方案。

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
docker compose up -d postgres        # 起数据库
cd backend && go run ./cmd/server    # 后端 :18080
cd frontend && npm install && npm run dev  # 前端 :5173
```

环境变量见 backend/internal/config/config.go（`COXPANEL_*` 前缀）。

## 当前状态：P1 完成

- [x] 骨架（go.work + 3 module + 前端 Vite）
- [x] 认证闭环（邀请码注册 + 登录 + JWT）
- [x] 节点管理（受管 + 外部）
- [x] Agent（心跳 + 拉配置 + 原子应用）
- [x] 订阅生成（mihomo + base64）+ /sub/:token
- [x] 拓扑翻译引擎（inbounds + edges → sing-box JSON）
- [x] 前端页面（登录/注册/总览/节点/订阅）

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
