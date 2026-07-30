# 生产环境一键部署指南

## 项目概述

本方案将两个生产环境项目统一部署到一台 Linux 服务器上：

| 项目 | 功能 | 端口 | 说明 |
|------|------|------|------|
| **new-api** | AI API 网关/代理 | 3000 | 聚合 40+ AI 提供商的统一 API |
| **CLIProxyAPI** | CLI 代理 API | 8317 / 8085 | 为 CLI 提供 OpenAI/Gemini/Claude 兼容 API |

## 架构

```
┌─────────────────────────────────────────────┐
│              Linux 服务器                     │
│                                              │
│  ┌──────────┐  ┌──────────────┐              │
│  │ new-api  │  │ CLIProxyAPI  │              │
│  │  :3000   │  │  :8317/:8085 │              │
│  └────┬─────┘  └──────┬───────┘              │
│       │               │                      │
│       ▼               ▼                      │
│  ┌─────────────────────────┐  ┌───────────┐  │
│  │   PostgreSQL (共享)      │  │   Redis   │  │
│  │  ├─ new-api 数据库      │  │  (共享)    │  │
│  │  └─ cliproxy 数据库     │  │           │  │
│  └─────────────────────────┘  └───────────┘  │
└─────────────────────────────────────────────┘
```

**关键设计：**
- PostgreSQL 共享：一个实例，两个独立数据库（`new-api` 和 `cliproxy`），数据隔离
- Redis 共享：一个实例，new-api 使用 DB 0
- Docker 网络：所有服务在同一个 bridge 网络内通信

---

## 服务器要求

| 配置项 | 最低要求 | 推荐配置 | 当前配置 |
|--------|---------|---------|---------|
| CPU | 2 核 | 4 核 | **8 核** |
| 内存 | 4 GB | 8 GB | **16 GB** |
| 磁盘 | 40 GB | 100 GB SSD | **100 GB** |
| 系统 | Ubuntu 20.04+ / CentOS 7+ | Ubuntu 22.04 LTS | - |
| Docker | 20.10+ | 24.0+ | - |
| Docker Compose | v2.0+ | v2.20+ | - |

---

## 资源分配（8核 16G 配置）

| 服务 | CPU 限制 | 内存限制 | CPU 保留 | 内存保留 |
|------|---------|---------|---------|---------|
| new-api | 3.0 核 | 4 GB | 1.0 核 | 1 GB |
|**CLIProxyAPI** | 1.0 核 | 2 GB | 0.5 核 | 512 MB |
|**PostgreSQL** | 2.0 核 | 4 GB | 0.5 核 | 1 GB |
|**Redis** | 0.5 核 | 1 GB | 0.1 核 | 256 MB |
|**总计** | **6.5 核** | **11 GB** | **2.1 核** | **2.8 GB** |

---

## 文件说明

```
production-deploy/
├── build.sh                # 本地一键构建打包脚本
├── deploy.sh               # 一键部署脚本（全流程）
├── init-server.sh          # 服务器环境初始化脚本
├── docker-compose.yml      # Docker Compose 编排文件
├── .env                    # 环境变量配置
├── .env.example            # 环境变量模板（脱敏）
├── init-db.sh              # PostgreSQL 数据库初始化脚本
├── config.example.yaml     # CLIProxyAPI 配置模板
├── production-deploy.service # systemd 服务文件（服务器自启动）
├── LOCAL-BUILD-ACR-PROD-DEPLOY.md # ACR 发布回退文档
└── README.md               # 本文档
```

---

## 部署流程

### 发布硬性规则

- 仓库根目录 `VERSION` 是应用版本和镜像 tag 的唯一来源。每次发布必须先递增 `VERSION`，单独提交并推送；不要在文档或聊天记录中维护另一个“当前版本”。
- ACR tag 是不可变发布标识。已有 tag 禁止覆盖，回退必须依赖原 tag 仍指向原镜像。
- 只允许从已提交、已推送且工作树干净的源码构建生产镜像。未提交或未跟踪的 IDone/Feishu 等本地内容不得进入发布提交或镜像。
- 当前受 Git 跟踪的生产前端位于 `web/`，生产主题为 `default`；不要把本地未跟踪目录当作生产前端。
- “构建并推送镜像”与“部署生产”是两个独立动作。推送成功不会自动重启生产服务。

### 方式一：本地构建 + ACR 推送 + 生产拉取（推荐）

详见 [LOCAL-BUILD-ACR-PROD-DEPLOY.md](./LOCAL-BUILD-ACR-PROD-DEPLOY.md)

```bash
# 本地：先更新并提交版本（示例版本必须替换为一个未使用的新版本）
printf '%s\n' '<新版本号>' > VERSION
git add VERSION
git commit -m "chore: bump version to <新版本号>"
git push origin HEAD

# 从干净的已推送工作树构建并推送
./production-deploy/build.sh

# 生产：完成备份并修改 .env 中 NEW_API_IMAGE 后，仅升级 new-api
sudo docker compose pull new-api
sudo docker compose up -d new-api --no-deps
```

### 方式二：仅在本地构建和验证

```bash
./production-deploy/build.sh --skip-push
```

该命令只生成并验证本地镜像，不推送，也不能用于生产部署。

### 首次服务器初始化

```bash
ssh root@production-server
cd /opt/production-deploy
chmod +x init-server.sh
./init-server.sh
```

---

## 访问地址

| 服务 | 地址 | 说明 |
|------|------|------|
| new-api | `http://<服务器IP>:3000` | AI API 网关管理面板 |
| CLIProxyAPI | `http://<服务器IP>:8317` | CLI 代理 API 端点 |
| CLIProxyAPI 管理面板 | `http://<服务器IP>:8085` | CLIProxyAPI 管理界面 |

---

## 日常管理（生产服务器，需要 sudo）

```bash
sudo docker compose ps              # 查看状态
sudo docker compose logs -f         # 查看日志
sudo docker compose restart new-api # 重启服务
sudo docker compose down            # 停止（保留数据）
```

---

## 数据库备份

```bash
BACKUP_DATE=$(date +%Y%m%d_%H%M%S)
sudo docker exec postgres pg_dumpall -U root > backup_${BACKUP_DATE}.sql
```

定时备份（每天凌晨 2 点）：

```bash
sudo crontab -e
# 0 2 * * * cd /opt/production-deploy && sudo docker exec postgres pg_dumpall -U root > backup_$(date +\%Y\%m\%d).sql && find . -name "backup_*.sql" -mtime +7 -delete
```

---

## 安全建议

1. **修改默认密码**：务必修改 `.env` 中的 `DB_PASSWORD` 和 `MANAGEMENT_PASSWORD`
2. **配置防火墙**：只开放必要端口（3000、8317、8085、SSH）
3. **启用 HTTPS**：建议使用 Nginx 反向代理 + Let's Encrypt 证书
4. **定期备份**：建议设置 cron 定时备份数据库
5. **SSH 加固**：禁用密码登录，使用密钥认证
