# 部署 ProjectBoard

ProjectBoard 是单进程服务，默认只监听 `127.0.0.1:3333`。生产环境应由普通系统用户运行，并通过同机反向代理提供 HTTPS；不需要开放额外的 SSH、Agent 或数据库端口。

## 运行账户

服务账户必须满足以下条件：

- `PATH` 中可以调用 `git` 和 `codex`，且已完成 Codex 登录。
- 对 `PROJECTBOARD_DATA_DIR`、配置的项目绝对路径，以及项目相邻的 `worktrees/` 目录拥有读写权限。
- 有足够的 CPU、内存和磁盘承载 Agent 并发、Git worktree、请求日志与上传附件。

不要以 root 运行服务。Git 身份、Codex 凭据和项目文件权限都应配置在同一个专用账户下。

## 构建与配置

```bash
go build -o /opt/projectboard/projectboard ./cmd/projectboard
```

核心环境变量如下：

```text
PORT=3333
PROJECTBOARD_DATA_DIR=/var/lib/projectboard
PROJECTBOARD_DB=/var/lib/projectboard/projectboard.db
PROJECTBOARD_BOOTSTRAP_USERNAME=admin
PROJECTBOARD_BOOTSTRAP_PASSWORD=replace-with-a-strong-password
PROJECTBOARD_ENV=production
```

密码必须为 12–256 位且同时包含字母和数字。只有数据库中尚无用户时，启动用户名和密码才会用于创建管理员。程序不会自行读取 `.env` 文件，应由服务管理器显式注入环境变量。

## systemd 示例

仓库当前不附带可直接安装的 unit 文件。可按实际用户和路径创建 `/etc/systemd/system/projectboard.service`：

```ini
[Unit]
Description=ProjectBoard
After=network.target

[Service]
Type=simple
User=projectboard
Group=projectboard
WorkingDirectory=/opt/projectboard
Environment=PORT=3333
Environment=PROJECTBOARD_DATA_DIR=/var/lib/projectboard
Environment=PROJECTBOARD_ENV=production
EnvironmentFile=-/etc/projectboard/projectboard.env
ExecStart=/opt/projectboard/projectboard serve
Restart=on-failure
RestartSec=3
UMask=0077

[Install]
WantedBy=multi-user.target
```

安装或修改后执行：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now projectboard
curl --fail http://127.0.0.1:3333/health
```

## 反向代理

代理只需转发 HTTP 到 `127.0.0.1:3333`，并保留 `Host`、`X-Forwarded-For` 与 `X-Forwarded-Proto`。外部地址必须使用 HTTPS。`PROJECTBOARD_ENV=production` 会为会话 Cookie 添加 `Secure`，因此不要用纯 HTTP 暴露生产实例。

公开地址是数据库中的管理员系统设置，不由 `PROJECTBOARD_PUBLIC_URL` 环境变量加载。非本机地址必须是无路径、查询或片段的完整 HTTPS origin。

## 持久化与备份

数据目录可能包含：

- `projectboard.db` 及 SQLite WAL/SHM 文件；
- `attachments/` 中的任务附件；
- `workspaces/` 中的请求级临时工作空间。

标准任务 worktree 位于项目仓库相邻的 `worktrees/`，不在数据目录中。项目仓库和这些 worktree 也应纳入运维容量与备份策略。

在线创建一致的 SQLite 备份：

```bash
/opt/projectboard/projectboard backup /var/backups/projectboard/projectboard.db
```

数据库备份不包含附件。请另外备份 `attachments/` 和项目仓库。恢复前先停止服务：

```bash
sudo systemctl stop projectboard
/opt/projectboard/projectboard restore /var/backups/projectboard/projectboard.db
sudo systemctl start projectboard
```

恢复会校验备份数据库，并把原数据库保留为 `projectboard.db.before-restore`。当前 schema 为 v9；其他版本会被拒绝且不会自动迁移。

## 发布检查

```bash
go test ./...
go vet ./...
go build ./cmd/projectboard
curl --fail http://127.0.0.1:3333/health
```

发布前还应验证登录、创建本地测试项目、任务阶段变更、附件读写、知识节点修改、Agent 排队与审计导出，并确认服务账户的 Codex 环境可以独立执行。
