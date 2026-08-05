# ProjectBoard

ProjectBoard 是一个供人类与 Agent 协作的项目工单台。服务端、Runner、MCP、迁移和备份均由 Go 实现；浏览器端是嵌入二进制的静态 HTML、CSS 与原生 JavaScript，不需要 Node.js、pnpm 或独立前端服务。

## 安装与启动

需要 Go 1.26 或下载已编译的 `projectboard` 与 `projectboard-runner`。

```bash
go build -o projectboard ./cmd/projectboard
go build -o projectboard-runner ./cmd/projectboard-runner
projectboard serve
```

默认地址为 `http://localhost:3333`，数据写入 `./data`。首次启动会创建管理员；未设置密码时，随机密码只在服务日志中显示一次。自动化部署可以设置：

```text
PORT=3333
PROJECTBOARD_DATA_DIR=./data
PROJECTBOARD_DB=./data/projectboard.db
PROJECTBOARD_BOOTSTRAP_USERNAME=admin
PROJECTBOARD_BOOTSTRAP_PASSWORD=<至少 12 位强密码>
PROJECTBOARD_ENV=production
```

`PROJECTBOARD_ENV=production` 会启用 Secure 会话 Cookie，请在 TLS 反向代理后使用。

## 项目授权

管理员先保存 GitHub App 或 GitLab 的“提供商授权”。长期私钥或 Token 使用与 SQLite 数据库分离的主密钥进行 AES-GCM 加密。随后每个项目必须单独创建仓库 Grant，明确绑定授权、仓库、读写级别、批准人和批准时间。

多个项目可以复用同一个提供商授权，也可以分别创建授权；复用不会合并项目 Grant。撤销一个项目的 Grant 不影响其他项目，仍被项目使用的提供商授权不能直接撤销。`github-app.pem` 属于 GitHub App/提供商授权，不需要为每个项目复制。

## Runner

在网页中创建 Agent、为项目启用它并生成一次性配对码：

```bash
set PROJECTBOARD_URL=http://localhost:3333
projectboard-runner connect PB-XXXX-XXXX
projectboard-runner register <project-id> <本地仓库路径>
projectboard-runner poll --watch
projectboard-runner claim <work-item-id> <version>
```

Agent Token 只存入当前用户的系统配置目录，文件权限为仅当前用户可读写；它不会进入项目仓库。Runner 不保存 Git 提供商长期密钥。

## MCP

MCP 已内置，不需要单独安装服务：

- `projectboard serve` 在 `/mcp` 提供带 Agent Bearer Token 的 HTTP MCP。
- `projectboard mcp` 提供 stdio MCP，并通过 `PROJECTBOARD_URL` 与 `PROJECTBOARD_AGENT_TOKEN` 连接同一个服务。

## 数据库、备份与恢复

```bash
projectboard migrate
projectboard backup ./backups/projectboard.db
# 恢复前停止服务
projectboard restore ./backups/projectboard.db
```

恢复前会执行 SQLite 完整性检查，并把原数据库保留为 `projectboard.db.before-restore`。`data/secrets/master.key` 必须与数据库分别安全备份；缺少它将无法解密提供商授权。

## 验证

```bash
go test ./...
go vet ./...
go build ./cmd/projectboard
go build ./cmd/projectboard-runner
```

正式发布前按仓库流程先在 `dev` 完成测试和 README 检查，再合并到 `release`。
