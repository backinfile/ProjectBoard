# ProjectBoard Agent SSH 执行说明

本说明用于让 Codex 通过 SSH 公钥连接 ProjectBoard，并持续领取和完成任务。ProjectBoard 不需要 Runner，也不会接收你的私钥。

## 1. 准备 Ed25519 密钥

为 ProjectBoard 单独生成密钥，不要复用服务器登录密钥：

```bash
ssh-keygen -t ed25519 -f ~/.ssh/projectboard_agent -C "projectboard-agent"
```

Windows PowerShell 可将路径改为 `$env:USERPROFILE\.ssh\projectboard_agent`。把 `.pub` 文件的完整一行粘贴到 ProjectBoard 的 Agent 管理页；私钥文件不得上传、提交到 Git 或放进提示词。

加密私钥建议先载入 `ssh-agent`。ProjectBoard 公钥永久有效，直到管理员撤销。

## 2. 固定 ProjectBoard SSH host key

在“Agent → 连接 Codex”中复制 SSH host public key 和 SHA-256 指纹，通过独立可信渠道核对后写入专用 `known_hosts`。不要使用 `StrictHostKeyChecking=no` 或未经核对的 `accept-new`。

建议为连接建立 SSH alias：

```sshconfig
Host projectboard-agent
  HostName board.example.com
  Port 2222
  User <agent-id>
  IdentityFile ~/.ssh/projectboard_agent
  IdentitiesOnly yes
  StrictHostKeyChecking yes
  UserKnownHostsFile ~/.ssh/projectboard_known_hosts
  ServerAliveInterval 30
  ServerAliveCountMax 3
```

ProjectBoard SSH 不是系统 shell。它拒绝 PTY、shell、SFTP、SCP、端口转发和任意命令，只允许 MCP 与任务 Git credential helper。

## 3. 配置 Codex MCP

将以下内容加入用户级 `~/.codex/config.toml`；Codex App、CLI 与 IDE 共用这份配置：

```toml
[mcp_servers.projectboard]
command = "ssh"
args = ["-T", "projectboard-agent", "projectboard-mcp"]
required = true
tool_timeout_sec = 70
```

重启 Codex 后用 `/mcp` 或 MCP 设置页确认 `projectboard` 已连接。同一 Agent 同时只允许一个 MCP 会话；若旧会话仍在线，请先结束它或让管理员在管理页断开。

## 4. 启动持续执行

推荐提示语：

> 连接 ProjectBoard，恢复尚未完成的有效 execution；如果没有则持续等待并领取下一项任务。读取完整任务、项目规则和仓库信息，自主准备环境并按需派出子 Agent。维护租约，只使用 ProjectBoard SSH Git credential helper，完成实现、验证、提交、推送和结构化回写，然后继续等待下一项任务。任务无法完成时记录阻塞、释放任务并继续；直到我明确取消前不要结束循环。

每轮必须遵循：

1. 先调用 `get_active_execution`；存在有效 execution 时恢复，不要重复领取。
2. 否则调用 `wait_for_task`；idle 后继续等待。
3. 调用 `claim_task` 原子领取，保存 `executionId`、`leaseId`、任务分支和仓库信息。
4. 长操作期间定期调用 `heartbeat_execution`，使租约始终有效。
5. 在独立任务目录 clone/fetch，并使用返回的 `projectboard/<project-key>/<number>` 分支；禁止 force-push 和直接推送目标分支。
6. 自主读取 `AGENTS.md`、README 和清单配置环境；每个环境命令通过 `record_environment_step` 记录。不得把 ProjectBoard Git 凭据写入日志、remote URL 或配置文件。
7. 用 `post_message` 回写重要进度。Codex 可自行决定是否把独立工作交给子 Agent。
8. 验证完成后使用 `submit_execution_result` 回写提交、文件和验证证据。失败时使用 `mark_task_blocked` 或 `release_task`，随后继续等待下一项任务。

## 5. GitHub credential helper

Agent 执行只支持 ProjectBoard 已授权的 GitHub 仓库，不允许回退到机器已有 Git 登录。对每次 clone、fetch 或 push 使用命令级 helper，不写入仓库配置：

```bash
git -c credential.helper= -c 'credential.helper=!f() { ssh -T projectboard-agent "projectboard-git-credential <execution-id>"; }; f' clone <repository-url> <work-directory>
```

后续 fetch/push 使用相同的两个 `-c` 参数。ProjectBoard 会验证 Agent、当前 SSH 密钥、项目 grant、execution 和 lease，再签发最长一小时的单仓库 GitHub installation token。凭据在 execution 提交、释放、断连、过期或撤钥时撤销。

PowerShell 中可用双引号包住整个 helper 值，并按 PowerShell 规则转义内部引号；不要把 helper 或凭据输出到任务消息。

## 6. 中断与安全

- 普通网络断线会撤销 Git 凭据，但租约保留到到期；重连后先恢复 execution。
- 管理员撤销当前公钥会立即断开连接、撤销凭据并释放任务。
- 身份撤销、host key 不匹配或系统性安全错误时停止；普通任务阻塞应记录后继续队列。
- ProjectBoard 不授予系统管理员权限。Codex 的文件、网络和命令能力仍由本机 Codex 沙箱与审批策略决定。
