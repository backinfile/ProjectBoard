# ProjectBoard

ProjectBoard 是人类成员与服务器本地 Codex Agent 协作的项目工作台。单个 Go 服务提供 Web UI、SQLite 持久化、任务与讨论、项目知识库、通知审计和内置 Agent 调度器。

当前实现只操作服务器本地 Git 仓库，并由服务进程直接调用本地 `codex` CLI；不依赖 SSH、MCP、外部 Runner、向量数据库、图数据库或 Git 托管平台授权。

## 当前功能

- 账户与权限：管理员和普通用户、项目开发者和查看者、登录限流、会话撤销、CSRF 保护、密码修改与重置。
- 项目：绑定唯一且不可修改的本地绝对路径，配置目标分支、校验命令、禁止路径、Agent 规则、通用提示词和知识整理阈值。
- 任务：四阶段标准流程、三阶段简单对话流程、优先级、父子任务、负责人、关注者、依赖、阻塞、Markdown 消息和不超过 25 MiB 的附件。
- Agent：本地执行身份、模型、思考强度、并发容量、单轮超时、接取/拒绝标签、Token 用量、实时输出、暂停、继续、取消、重试和计划批准。
- 知识库：项目级树状节点、Markdown、关键词搜索、移动、Agent 锁、完整修订历史和版本恢复。
- 工作台：任务看板、知识库、Agent 需求、通知中心，以及账户、Agent、用户、项目、审计和系统服务六个设置页签。
- 审计与运维：追加式活动记录、分页与 NDJSON 导出、健康检查、SQLite 备份和恢复。

## 前置条件

- 从源码构建需要 Go 1.26。
- `git` 与 `codex` 位于 ProjectBoard 服务账户的 `PATH`。
- 服务账户已完成 Codex 登录，并对所有项目路径及其相邻 `worktrees/` 目录拥有读写权限。

ProjectBoard 不保存 Git 提供商凭据，也不会自动执行 `fetch`、`pull` 或 `push`。Agent 仍可能根据任务中的明确指令使用服务账户现有的 Git 环境。

## 构建与启动

```bash
go build -o projectboard ./cmd/projectboard
PROJECTBOARD_BOOTSTRAP_PASSWORD='change-this-password' ./projectboard serve
```

默认仅监听 `http://127.0.0.1:3333`，数据目录为 `./data`。首次启动会创建管理员；若未设置启动密码，随机密码只会在控制台输出一次。生产环境应通过 TLS 反向代理提供 HTTPS。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `3333` | 回环地址上的监听端口 |
| `PROJECTBOARD_DATA_DIR` | `./data` | 数据库、附件和临时 Agent 工作空间的根目录 |
| `PROJECTBOARD_DB` | `<data-dir>/projectboard.db` | SQLite 数据库文件 |
| `PROJECTBOARD_BOOTSTRAP_USERNAME` | `admin` | 首次启动的管理员用户名 |
| `PROJECTBOARD_BOOTSTRAP_PASSWORD` | 随机生成 | 首次启动密码，必须为 12–256 位且同时包含字母和数字 |
| `PROJECTBOARD_ENV` | `development` | 设为 `production` 时启用 Secure Cookie |

公开地址保存在系统设置 `public_url` 中；非本机地址必须使用 HTTPS。`.env.example` 仅是配置参考，程序不会自行加载 `.env` 文件。

## 创建项目

创建项目时必须指定服务器上的绝对文件夹路径。路径必须在项目间唯一，创建后不可修改。

- 文件夹不存在时自动创建。
- 文件夹不是 Git 仓库时，自动初始化 `main` 分支并创建基线提交；已有文件会进入基线提交，空目录会创建空提交。
- 已有 Git 仓库必须位于仓库根目录且工作区干净，当前分支成为默认目标分支。
- 创建任务时，目标分支必须已经存在。

管理员可管理组织用户和本地 Agent；项目开发者可写项目内容，查看者只能读取项目数据。

## 任务与 Agent 工作流

标准任务使用 `创建 → 进行中 → 完成 → 关闭`。Agent 在项目相邻的 `worktrees/<project-key>-<task-number>` 中使用 `projectboard/<project-key>/<task-number>` 分支工作，随后由独立合并需求在主仓库中执行 `--no-ff` 合并。

简单对话任务使用 `创建 → 进行中 → 关闭`，直接在目标分支工作。对项目主仓库的写入按项目串行。系统不会自动 reset、stash 或执行 Git 网络操作。

每次计划、执行、计划批准、重试、返工、合并或知识整理都是独立的 Agent 需求，并使用新的 Codex 会话。每个 Agent 可指定 Codex 模型和思考强度；留空时继承服务账户配置。需求会保留配置快照、原始 JSONL、最终消息、错误、请求谱系和 Token 用量。没有符合标签或容量条件的 Agent 时，需求保持排队。

运行中的任务被标记为阻塞时，只终止该任务的 Codex 进程树。连续 10 分钟没有 CLI 输出或活动时，执行会停止并安全暂停。关闭后的标准 worktree 默认保留，可从任务详情中清理。

## 知识与数据

任务关闭时会在同一事务中创建任务知识整理需求。项目默认在 7 天或累计 20 次任务知识整理后创建全局整理需求；任一阈值设为 `0` 可单独关闭该触发条件。

知识 Agent 只能提交完整、结构化的节点操作。服务端在一个事务中校验项目归属、版本和 Agent 锁；锁只阻止 Agent 修改当前节点，不阻止成员修改，也不继承到子节点。当前知识节点不支持附件或外部文件引用。

SQLite 使用 WAL，当前 schema 为 v10。程序会自动把 v9 数据库迁移到 v10 并保留数据；其他 schema 会被拒绝。升级前仍应先备份。

```bash
./projectboard backup ./backups/projectboard.db
./projectboard restore ./backups/projectboard.db
```

`backup` 和 `restore` 只处理 SQLite 数据库。`data/attachments/` 等文件数据需要另外备份；恢复操作应在服务停止时执行。完整说明见 [deploy/README.md](deploy/README.md)。

## 验证

```bash
go test ./...
go vet ./...
go build ./cmd/projectboard
```

架构见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，知识方案调研见 [docs/KNOWLEDGE-RESEARCH.md](docs/KNOWLEDGE-RESEARCH.md)。
