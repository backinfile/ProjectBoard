# ProjectBoard

ProjectBoard 是人类成员与本地 Codex Agent 协作的项目工作台。单个 Go 服务提供 Web 界面、SQLite 持久化、任务流、项目知识库和内置 Agent 调度器；不依赖 SSH、MCP、外部 Runner、向量数据库、图数据库或 Git 托管平台认证。

## 主要能力

- 项目直接绑定服务器上的本地 Git 仓库；标准任务使用隔离 worktree，简单对话任务直接操作目标分支。
- 每个项目有一棵共享知识树，支持 Markdown、子节点、项目文件引用、关键词搜索、版本历史和恢复。
- 开发者可锁定单个知识节点，阻止 Agent 修改该节点，但仍允许成员修改以及在其下创建子节点。
- 所有 Agent 执行统一为一次性“Agent 需求”：计划、执行、合并、任务知识整理和项目知识全局整理。
- 每次重试、返工和计划批准都会创建新需求和新 Codex 会话，旧结果与原始 JSONL 保留。
- 每次任务关闭自动创建知识整理需求；项目默认在 7 天或累计 20 次任务知识整理后创建全局整理需求，阈值可设为 `0` 关闭。

## 前置条件

- 从源码构建需要 Go 1.26。
- `git` 与 `codex` 位于 ProjectBoard 服务账户的 `PATH`。
- 服务账户已完成 Codex 登录，并对配置的项目文件夹拥有读写权限。

ProjectBoard 不配置或保存 Git 提供商凭据，也不会自动执行 `fetch`、`pull` 或 `push`。任务中的明确 Agent 指令仍可使用服务器已有的 Git 环境。

## 构建与启动

```bash
go build -o projectboard ./cmd/projectboard
PROJECTBOARD_BOOTSTRAP_PASSWORD='change-this-password' ./projectboard serve
```

默认地址为 `http://localhost:3333`，数据目录为 `./data`。生产环境应通过 TLS 反向代理提供 HTTPS。

```text
PORT=3333
PROJECTBOARD_DATA_DIR=./data
PROJECTBOARD_BOOTSTRAP_USERNAME=admin
PROJECTBOARD_BOOTSTRAP_PASSWORD=change-this-password
PROJECTBOARD_PUBLIC_URL=https://board.example.com
```

## 创建项目

创建项目时必须指定服务器上的绝对文件夹路径。路径必须在项目间唯一，创建后不可修改。

- 文件夹不存在时自动创建。
- 文件夹不是 Git 仓库时，自动初始化 `main` 分支并创建基线提交；已有文件会纳入基线提交，空目录会创建空提交。
- 已有 Git 仓库必须位于仓库根目录且工作区干净，当前分支成为默认目标分支。
- 创建任务时，目标分支必须已存在。

## Agent 工作流

管理员创建本地 Codex Agent，并设置并发容量、单次需求超时、接取标签与拒绝标签。没有匹配 Agent 时，需求保持排队。

标准任务在项目旁的 `worktrees/<project-key>-<task-number>` 中工作，分支名为 `projectboard/<project-key>/<task-number>`。实现完成后，独立的合并需求在项目仓库中以 `--no-ff` 合并目标分支。简单对话任务直接在目标分支执行。系统不会自动 reset、stash、fetch、pull 或 push。

任务状态为 `创建 → 进行中 → 完成 → 关闭`，阻塞为独立标记。知识与 Agent 需求是独立的一等项目资源。关闭后的标准 worktree 默认保留，可在任务页手动安全清理。

## 知识与数据

知识文件最大 25 MB，按项目 SHA-256 去重，写入后不可变。知识整理 Agent 只能提交结构化节点操作，服务端在单个事务中校验锁、版本、项目归属和文件引用。

SQLite 使用 WAL，当前 schema 为 v9。该版本不兼容旧数据，启动时会明确拒绝旧数据库，不执行迁移；升级时请使用新的数据目录。

```bash
./projectboard backup --output ./backups
./projectboard restore --input ./backups/<backup-directory>
```

## 验证

```bash
go test ./...
go build ./cmd/projectboard
```

设计取舍与同类项目调研见 [docs/KNOWLEDGE-RESEARCH.md](docs/KNOWLEDGE-RESEARCH.md)。
