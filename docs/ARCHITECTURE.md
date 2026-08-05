# Architecture and trust boundaries

`Browser → Go Web/API → domain modules → SQLite` 负责身份、项目权限、状态转换和审计。静态网页通过 `go:embed` 内置，不存在前端构建或 Node 运行时。系统管理员可在网页中写入 GitHub App 与 GitLab OAuth 配置；密钥只在保存请求中出现，之后不再由 API 回显。

`Runner → Runner API` 负责受信任机器上的仓库与代码执行。Runner 只保存 Agent Token 和本地项目映射，不保存 GitHub App 私钥或 GitLab 长期 Token。

Git Broker 是 Go 服务内的 `providers` 深模块，不开放独立端口。管理员写入的 GitHub App、GitLab OAuth 与 Webhook secret 使用独立 `master.key` 经 AES-GCM 加密后存入 SQLite；普通读取接口只暴露非敏感摘要。Broker 用一次性 state/nonce、PKCE 和限时 flow 处理回调，枚举并验证仓库与 Webhook，再把可复用提供商授权和每项目独立 Grant 分开。任何临时仓库凭据签发仍必须同时验证项目 Grant、仓库、任务、Runner 与活动租约；部署时可把同一 Interface 后的 Broker 移到独立进程。

HTTP MCP 位于 `/mcp`，stdio MCP 由 `projectboard mcp` 提供。两种传输均使用 Agent 身份，并复用 Runner API 的授权语义。

事务中不执行网络、Git、Codex 或长时间命令。SQLite 启用 WAL、外键、busy timeout 和短事务。Markdown 始终按不可信文本显示，HTTP 响应使用严格 CSP、安全响应头与同源会话策略。
