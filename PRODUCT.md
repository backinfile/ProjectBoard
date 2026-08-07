# ProjectBoard 产品说明

ProjectBoard 是成员与 Codex Agent 共用的安全任务队列。人类定义任务、讨论和验收边界；Agent 通过 SSH MCP 领取一个任务、实现、记录证据并回写结果，然后继续等待下一项。

## 产品边界

- 服务端不运行模型，不接收 Agent 私钥，不保存长期 GitHub Token。
- Agent 是组织级身份，每个项目单独授权，默认不能认领未指派任务。
- Agent 可登记多把 Ed25519 公钥；每个 Agent 只允许一个在线 MCP 会话。
- ProjectBoard 只支持 GitHub，并以有效 execution 和租约签发单仓库短期凭据。
- 任务的讨论结论、环境步骤、执行结果、验证、提交和验收记录保持可审计。

## 主流程

1. 管理员创建 Agent、登记公钥并将 Agent 授权给项目。
2. Codex 使用固定的 SSH host key 连接 `projectboard-mcp`。
3. Codex 优先领取明确指派的任务；项目授权允许时，再领取未指派任务。
4. Codex取得任务租约和 `projectboard/<project-key>/<task-number>` 分支，使用 SSH Git credential helper 获取当前仓库短期凭据。
5. Codex自主准备环境并记录实际操作，持续心跳，回写进度、附件和交付证据。
6. 成功时提交执行结果；无法完成时记录阻塞与已完成工作、释放租约并继续等待。

## 安全语义

删除会话使用的公钥会断开连接、撤销 Git 凭据并释放租约。普通断网只撤销 Git 凭据，Agent 可在租约期内重连恢复。每次 MCP 动作都会重新验证 Agent、密钥、项目授权和租约。

Runner 和 GitLab 不属于当前产品，未来条件见 `docs/FUTURE_RUNNER.md` 与 `docs/FUTURE_GITLAB.md`。
