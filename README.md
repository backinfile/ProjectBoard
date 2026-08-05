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

管理员在项目设置中点击“自动配置 Git”，再到 GitHub 或 GitLab 官方页面确认授权。回调成功后，ProjectBoard 会验证权限与 Webhook、读取可用仓库、按当前项目的 `repositoryUrl` 自动匹配，并在最终绑定前明确展示仓库和读写范围。授权或安装可以复用于多个项目，但每个项目仍创建独立、可撤销、可审计的仓库 Grant。

自动流程不会扩大提供商权限。GitHub installation token 只用于验证安装和枚举仓库；Runner 写凭据仍必须由单一项目 Grant、已启用 Agent、有效指派、Run 和租约共同约束。撤销一个项目的 Grant 不影响复用同一授权的其他项目，仍被项目使用的提供商授权不能直接撤销。

### 在网页中配置 GitHub App

GitHub App 仍需先在 GitHub 创建，但 ProjectBoard 侧不再使用环境变量或本地 PEM 路径。系统管理员进入“项目设置 → 自动配置 Git → Configure GitHub”，在网页中填写 Public ProjectBoard URL、App ID、App slug、Webhook URL、PEM 私钥和 Webhook secret。App 至少需要：

- Repository permission：`Contents: Read and write`；不授予 Administration，也不要加入分支保护 bypass。
- Webhook URL：`https://<ProjectBoard>/api/git/webhooks/github`，设置独立随机 secret，并订阅 Push 与 installation/repository 变化事件。
- Setup URL：`https://<ProjectBoard>/api/git/connections/github/callback`，开启安装更新后的重定向。

提交配置需要系统管理员身份与有效 CSRF 会话。PEM 和 Webhook secret 仅在保存时由浏览器提交，随后使用 `data/secrets/master.key` 进行 AES-GCM 加密；读取配置的 API 只返回“已配置”标志与非敏感字段，不回显密钥。请始终通过 HTTPS 使用管理页面，并保护、备份 `master.key`。ProjectBoard 使用 App JWT 验证 installation，按 installation 枚举仓库，并检查 GitHub App Webhook URL 与网页配置一致。

### 在网页中配置 GitLab OAuth

在 GitLab 创建 OAuth Application，回调 URL 设置为 `https://<ProjectBoard>/api/git/connections/gitlab/callback` 并授予 `api` scope。然后进入“项目设置 → 自动配置 Git → Configure GitLab”，填写 Public ProjectBoard URL、Application ID、Application secret、GitLab Base URL 和 Webhook secret。

GitLab 流程使用 authorization code、PKCE、一次性 state/nonce 和 15 分钟回调窗口。最终确认项目绑定时才创建并验证项目 Push Webhook。操作者对所选仓库需要足够的项目权限；选择写入范围时至少需要 Developer。若无法创建 GitLab OAuth Application，界面仍提供明确标记的“高级 GitLab Token”降级入口。

授权失败、用户取消、回调过期、没有可用/匹配仓库、权限不足和 Webhook 验证失败都会保留为可恢复状态，不会静默创建项目 Grant。提供商应用密钥、GitLab Token 与 refresh token 均使用与 SQLite 分离的 `data/secrets/master.key` 进行 AES-GCM 加密。

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
