# ProjectBoard

ProjectBoard 是人类成员与本地 Codex Agent 协作的项目工作台。单个 Go 服务提供 Web 界面、SQLite 持久化、任务流、项目知识库和内置 Agent 调度器；不依赖 SSH、MCP、外部 Runner、向量数据库或图数据库。

## 主要能力

- 每个项目一棵共享知识树。每个节点都有标题、Markdown、子节点、项目文件引用、完整版本历史和恢复能力。
- 成员可查看和修改知识；查看者只读。开发者可锁定单个节点，阻止 Agent 修改该节点，但不阻止 Agent 在其下创建子节点。
- 所有 Agent 执行统一为一次性“Agent 需求”：任务计划、任务执行、任务知识整理、项目知识全局整理。
- 每次重试、返工和计划批准都会创建新需求和新 Codex 会话，旧记录与原始 JSONL 永久保留。
- 每个任务关闭时自动创建任务知识整理需求。项目默认在 7 天或累计 20 次任务知识整理后创建一次全局整理需求；两个阈值都可独立设为 `0` 关闭。
- 普通 Agent 获取只读的知识树快照与项目文件清单；知识整理 Agent 只能提交结构化节点操作，由服务端在一个事务中校验并应用。
- SQLite 关键词搜索，不提供 embeddings/RAG。

## 前置条件

- Go 1.26（从源码构建时）
- `git` 与 `codex` 位于 ProjectBoard 服务账户的 `PATH`
- 服务账户已完成 Codex 登录
- Agent 需要写入仓库时，管理员已在界面中配置 GitHub App 授权

## 构建与启动

```bash
go build -o projectboard ./cmd/projectboard
PROJECTBOARD_BOOTSTRAP_PASSWORD='change-this-password' ./projectboard serve
```

默认 Web 地址为 `http://localhost:3333`，数据目录为 `./data`。生产环境应通过 TLS 反向代理提供 HTTPS。

```text
PORT=3333
PROJECTBOARD_DATA_DIR=./data
PROJECTBOARD_BOOTSTRAP_USERNAME=admin
PROJECTBOARD_BOOTSTRAP_PASSWORD=change-this-password
PROJECTBOARD_PUBLIC_URL=https://board.example.com
```

## 使用方式

1. 管理员创建本地 Codex Agent，设置并发容量和单次需求超时。
2. 成员从任务页选择“交给 Agent”，直接执行或先生成计划；也可在创建任务时请求 Agent。
3. 计划成功后，批准计划会创建新的任务执行需求，不恢复旧会话。
4. Agent 成功完成任务后，ProjectBoard 推送干净分支；按任务配置停在“完成”或自动“关闭”。
5. 每次关闭都会生成新的任务知识整理需求。知识整理结果直接更新正式知识树，并遵守节点锁与乐观版本。
6. 在“Agent 需求”页查看排队、运行、成功、失败和取消记录。开发者可中断或创建重试需求；查看者只能查看状态与结果，不能查看原始日志。

任务状态固定为 `创建 → 进行中 → 完成 → 关闭`。关闭是只读终态；阻塞是独立标记。知识与 Agent 需求分别是独立的一等项目资源，不嵌入任务执行状态。

## 数据、文件与升级

SQLite 使用 WAL。知识文件最大 25 MB，按项目 SHA-256 去重，写入后不可变；即使节点后续取消引用，文件仍为历史版本保留。

```bash
./projectboard backup --output ./backups
./projectboard restore --input ./backups/<backup-directory>
```

当前 schema 为 v8，与旧 schema 不兼容。旧数据库启动时会返回明确错误，不执行数据迁移；请使用新的数据目录。

## 验证

```bash
go test ./...
go build ./cmd/projectboard
```

设计取舍与同类项目调研见 [docs/KNOWLEDGE-RESEARCH.md](docs/KNOWLEDGE-RESEARCH.md)。
