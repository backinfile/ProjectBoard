# ProjectBoard

ProjectBoard 是供成员与 Codex Agent 协作的安全任务队列。服务端以一个 Go 二进制同时提供 HTTPS 工作台、SQLite 持久化和独立 SSH MCP 服务；Agent 只需登记 Ed25519 公钥，不需要 Runner、Bearer Token 或专用客户端。

当前 Git 提供商仅支持 GitHub。ProjectBoard 根据 Agent、项目授权、任务指派、execution 和有效租约推导唯一仓库，并签发最长一小时、单仓库 `contents:write` 的 GitHub installation token。短期 Token 只通过 SSH Git credential protocol 交给 Git，不进入提示词、remote URL、数据库、审计或日志。

## 构建与启动

需要 Go 1.26：

```bash
go build -o projectboard ./cmd/projectboard
PROJECTBOARD_BOOTSTRAP_PASSWORD='change-this-password' ./projectboard serve
```

默认监听：

- Web：`:3333`（生产环境应通过 TLS 反向代理发布为 HTTPS）
- Agent SSH：`:2222`
- 数据目录：`./data`

常用环境变量：

```text
PORT=3333
PROJECTBOARD_DATA_DIR=./data
PROJECTBOARD_SSH_LISTEN_ADDRESS=:2222
PROJECTBOARD_SSH_PUBLIC_HOST=board.example.com
PROJECTBOARD_SSH_PUBLIC_PORT=2222
PROJECTBOARD_SSH_HOST_KEY_PATH=./data/secrets/ssh_host_ed25519
PROJECTBOARD_PUBLIC_URL=https://board.example.com
```

SSH 监听或 host key 初始化失败时，`serve` 会直接失败。首次启动会在配置路径生成权限受限的 Ed25519 host key。

## 连接 Codex

1. 在“系统设置 → Agent SSH 密钥”创建 Agent，并记录 Agent ID。
2. 在 Codex 主机执行 `ssh-keygen -t ed25519 -f ~/.ssh/projectboard_agent`，只把 `.pub` 内容登记到 ProjectBoard。
3. 从管理页复制 host key 指纹和精确的 `known_hosts` 行，核对后固定保存；不要使用盲目信任未知主机的选项。
4. 将 SSH MCP 写入用户级 `~/.codex/config.toml`，让 App、CLI 和 IDE 共用：

```toml
[mcp_servers.projectboard]
command = "ssh"
args = ["-T", "-p", "2222", "-i", "/absolute/path/projectboard_agent", "<agent-id>@board.example.com", "projectboard-mcp"]
startup_timeout_sec = 20
tool_timeout_sec = 70
```

5. 让 Codex 持续执行：等待任务、领取、准备仓库与环境、实现并回写证据，然后继续等待，直到用户取消。

完整 Windows/POSIX 操作、安全限制、Git credential helper 和持续执行提示见 [`web/agent-execution.md`](web/agent-execution.md)，运行中的服务也公开在 `/docs/agent-execution.md`。

## Agent 行为与安全边界

- 每个 Agent 可登记多把永久有效的 Ed25519 公钥；删除前一直有效。
- 每个 Agent 同时只有一个 MCP SSH 会话。管理员可显式断开。
- 删除当前会话使用的密钥会立即断连、撤销 Git 凭据、释放租约并恢复任务；普通网络断线只撤销凭据，租约到期前可重连恢复。
- 项目授权默认不允许领取未指派任务。只有打开 `allowUnassignedClaim` 后才可自动认领。
- 每个 Agent 同时只持有一个任务租约。已指派任务优先，其次是允许认领的未指派任务；同类按 `urgent`、`high`、`medium`、`low` 和创建时间排序。
- Agent 自主读取仓库说明并配置环境，但必须记录实际命令、结果和风险；仍受 Codex 沙箱、审批和操作系统权限约束。
- 只允许 `projectboard-mcp` 与 `projectboard-git-credential <execution-id>` SSH 命令；shell、PTY、SFTP/SCP 和端口转发均被拒绝。

## GitHub 配置

管理员在系统设置中保存 ProjectBoard 公开地址，然后从项目设置启动 GitHub App 配置。GitHub App 安装权限需要 `Contents: read & write`；运行时凭据仍会按单 execution、单仓库降权。

Agent 对每项任务使用独立工作目录和 `projectboard/<project-key>/<task-number>` 分支。不得 force-push、不得直接推送目标分支，也不得回退到机器已有的 Git 登录。

## 数据、备份与升级

SQLite 使用 WAL。备份和恢复：

```bash
./projectboard backup --output ./backups
./projectboard restore --input ./backups/<backup-directory>
```

Schema v5 是不兼容的认证迁移：旧 Agent Token、配对、设备、Runner 状态和 GitLab 数据会被删除；旧活动租约以 `auth_migration` 结束，历史执行记录迁移为 `agent_executions`。

## 验证

```bash
go test ./...
go build ./cmd/projectboard
```

未来 Runner 与 GitLab 的重新引入条件分别记录在 [`docs/FUTURE_RUNNER.md`](docs/FUTURE_RUNNER.md) 和 [`docs/FUTURE_GITLAB.md`](docs/FUTURE_GITLAB.md)，当前版本不包含代码桩或隐藏入口。
