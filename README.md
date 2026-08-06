# ProjectBoard

ProjectBoard 是一个供人类与 Agent 协作的项目任务台。服务端、Runner、MCP、迁移和备份均由 Go 实现；浏览器端是嵌入二进制的静态 HTML、CSS 与原生 JavaScript，不需要 Node.js、pnpm 或独立前端服务。

## 安装与启动

需要 Go 1.26 或下载已编译的 `projectboard` 与 Runner。Runner 的核心是平台无关 CLI；Windows 发行版在核心外提供可直接双击运行的系统托盘壳。

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

管理员先在“系统设置”中保存一次 ProjectBoard 地址，再从项目设置点击“自动配置 Git”，到 GitHub 或 GitLab 官方页面确认授权。回调成功后，ProjectBoard 会验证权限、读取可用仓库、按当前项目的 `repositoryUrl` 自动匹配，并在最终绑定前明确展示仓库和读写范围。授权或安装可以复用于多个项目，但每个项目仍创建独立、可撤销、可审计的仓库 Grant。

自动流程不会扩大提供商权限。GitHub installation token 只用于验证安装和枚举仓库；Runner 写凭据仍必须由单一项目 Grant、已启用 Agent、有效指派、Run 和租约共同约束。撤销一个项目的 Grant 不影响复用同一授权的其他项目，仍被项目使用的提供商授权不能直接撤销。

### 一键配置 GitHub App

系统管理员进入“项目设置 → 自动配置 Git → Set up GitHub automatically”，再在 GitHub 点击一次“Create GitHub App”。ProjectBoard 使用全局 ProjectBoard 地址和 GitHub App Manifest Flow 自动提交以下固定配置：

- Repository permission：`Contents: Read and write`；不申请 Administration，也不加入分支保护 bypass。
- Setup URL：自动填写 `https://<ProjectBoard>/api/git/connections/github/callback`。
- App ID、App slug 与 PEM 私钥：由 GitHub 生成并通过一次性、限时、带 state 校验的服务端回调自动交给 ProjectBoard。
- Webhook 与后台轮询：均不启用。

如果 App 需要由 GitHub 组织持有，在向导的可选项中填写组织账号 slug；留空时由当前个人账号创建。GitHub 创建页仍会展示名称、权限和可见性，管理员必须明确确认，不会静默扩大权限。旧的逐字段表单保留在“Advanced manual configuration”中，仅作为降级入口。

本地运行直接受支持。将全局 ProjectBoard 地址保存为 `http://127.0.0.1:3333` 或 `http://localhost:3333` 即可；GitHub 的回调由当前浏览器返回本机，不需要公网隧道。

GitHub 返回的 PEM 不会显示给浏览器，而是直接使用 `data/secrets/master.key` 经 AES-GCM 加密后写入 SQLite；读取 API 只返回非敏感摘要。请保护并备份 `master.key`。ProjectBoard 随后使用 App JWT 验证 installation 并枚举仓库。

### 在网页中配置 GitLab OAuth

在 GitLab 创建 OAuth Application，回调 URL 设置为 `<全局 ProjectBoard 地址>/api/git/connections/gitlab/callback` 并授予 `api` scope。然后进入“项目设置 → 自动配置 Git → Configure GitLab”，填写 Application ID、Application secret 和 GitLab Base URL。

GitLab 流程使用 authorization code、PKCE、一次性 state/nonce 和 15 分钟回调窗口。操作者对所选仓库需要足够的项目权限；选择写入范围时至少需要 Developer。若无法创建 GitLab OAuth Application，界面仍提供明确标记的“高级 GitLab Token”降级入口。

授权失败、用户取消、回调过期、没有可用/匹配仓库、权限不足和提交查询失败都会保留为可恢复状态，不会静默创建项目 Grant。提供商应用密钥、GitLab Token 与 refresh token 均使用与 SQLite 分离的 `data/secrets/master.key` 进行 AES-GCM 加密。

### 按需同步最近提交

ProjectBoard 不监听 Webhook，也不在后台定时轮询。项目开发者可在“项目设置”点击“同步提交”；持有当前项目有效 Run 与租约的 Runner 可执行 `projectboard-runner sync <project-id>`。首次同步分析最近一个月，之后严格按“上次同步时间—本次点击时间”的窗口查询默认分支提交，把提交信息中包含完整任务 ID 的记录去重写入任务证据和连续对话，并记录同步审计事件。项目暂无任务或时间窗内没有提交时不会执行关联检测，但仍会保存本次时间戳，供下次同步继续使用。

## Runner

登录网页后可从“Runner 下载”页面直接下载与 ProjectBoard 服务端同目录中的平台二进制。Windows 用户可直接通过托盘完成登录；平台无关的 `projectboard-runner` CLI 保留仓库注册和诊断命令。

在网页中创建 Agent、为项目启用它并生成一次性 Agent Key。Windows 双击启动 `projectboard-runner.exe`，在托盘菜单点击“登录”，输入 ProjectBoard 主页地址和 Agent Key；验证成功后 Runner 会保存设备凭据并自动开始轮询，不需要浏览器回调或终端。

非 Windows 环境可通过 CLI 使用同一个一次性 Agent Key：

```bash
set PROJECTBOARD_URL=http://localhost:3333
projectboard-runner connect PB-XXXX-XXXX
projectboard-runner register <project-id> <本地仓库路径>
projectboard-runner agent list
projectboard-runner agent use codex
projectboard-runner poll --watch
projectboard-runner claim <work-item-id> <version>
projectboard-runner sync <project-id>
```

Runner 默认使用本机已登录的 Codex CLI。可通过 `agent list` 查看 Codex CLI、OpenCode CLI 的安装与选中状态，通过 `agent use codex|opencode` 切换；Windows 托盘也可在“本地 Agent”子菜单中直接选择。Runner 会拒绝选择未安装或不在 `PATH` 中的 CLI。Codex 以非交互、仓库可写沙箱运行，OpenCode 以非交互自动授权模式运行；两者都从传入的本地仓库目录启动。可用 `projectboard-runner run-agent <本地仓库路径> <任务提示>` 单独验证当前适配器。

Agent Token 只存入当前用户的系统配置目录，文件权限为仅当前用户可读写；它不会进入项目仓库。Runner 不保存 Git 提供商长期密钥。

### Windows 托盘

Windows 发行包包含两个入口：

- `projectboard-runner-cli.exe`：平台无关核心的命令行入口，用于仓库 `register`、手动 `connect` 和诊断命令。
- `projectboard-runner.exe`：Windows 托盘壳。可直接双击启动，无需预先配置或打开终端；在“登录”中填写 ProjectBoard 主页地址与 Agent Key 后会自动连接并开始轮询。托盘同时提供切换登录、打开 ProjectBoard、启动、停止、重启、暂停/恢复领取、查看日志、打开配置目录和退出。

托盘程序只允许当前用户启动一个实例。配置与日志位于当前用户的系统配置目录 `ProjectBoard` 下；活动日志超过 2 MiB 后会在下次启动时归档保留。退出托盘程序时会先停止其持有的 Runner 轮询。Windows 发布构建命令为：

```powershell
go build -o projectboard-runner-cli.exe ./cmd/projectboard-runner
go build -ldflags="-H=windowsgui" -o projectboard-runner.exe ./cmd/projectboard-runner-windows
```

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
go build -ldflags="-H=windowsgui" ./cmd/projectboard-runner-windows
```

正式发布前按仓库流程先在 `dev` 完成测试和 README 检查，再合并到 `release`。
