# ProjectBoard 产品说明

ProjectBoard 是人类成员与本地 Codex Agent 共用的任务协作空间。人类创建任务、讨论、阻塞、确认与关闭；ProjectBoard 调度已授权的 Agent，并在本机隔离目录中调用 Codex CLI 完成工作。

## 产品边界

- Agent 运行类型目前仅支持本地 Codex CLI。
- 不提供 SSH、MCP Agent、外部 Runner、远程执行或自定义 CLI 路径。
- Git 仓库当前仅支持通过 GitHub App 授权。
- 每个 Agent 可设置并发容量和单轮超时；任务按当前负载与容量比选择 Agent。
- 每个任务保留对话、附件、关注者、审计事件和 execution 原始证据。

## 主流程

1. 管理员创建本地 Codex Agent，并授权它参与项目。
2. 成员创建任务，选择是否交给 Agent，并按需设置计划暂停与完成暂停。
3. 调度器原子领取可执行任务，在独立工作目录调用 `codex exec`；后续轮次使用 session ID 恢复对话。
4. Agent 只在返回有效 `completed` 结果且工作区提交干净时推进任务；暂停或失败会释放容量并等待成员。
5. 未设置完成暂停的 Agent 任务自动关闭；设置后停在完成，成员可继续执行或手动关闭。

任务状态只有创建、进行、完成、关闭。阻塞是独立标记，关闭是只读终态。
