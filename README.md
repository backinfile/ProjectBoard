# ProjectBoard

ProjectBoard 是供成员与本地 Codex Agent 协作的任务看板。单个 Go 服务提供 Web 界面、SQLite 持久化和调度器，并直接调用同一台服务器上的 `codex` CLI。

## 前置条件

- 从源码构建时需要 Go 1.26
- `git` 与 `codex` 位于 ProjectBoard 服务账户的 `PATH`
- 服务账户已经完成 Codex 登录
- ProjectBoard 对配置的项目文件夹拥有读写权限

ProjectBoard 不配置或保存 Git 提供商凭据，也不会自动执行 `fetch`、`pull` 或 `push`。任务中的明确 Agent 指令仍可使用服务器现有的 Git 环境。

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
- 文件夹不是 Git 仓库时，自动初始化 `main` 分支并创建基线提交；已有文件会纳入该提交，空目录会创建空提交。
- 已有 Git 仓库必须位于仓库根目录且工作区干净，当前分支会成为默认目标分支。
- 创建任务时目标分支必须已经存在。

## 工作流程

创建任务时选择工作流程，创建后不可修改。

### 标准流程（默认）

状态为 `创建 → 进行 → 完成 → 关闭`。Agent 从目标分支创建：

- 分支：`projectboard/<project-key>/<task-number>`
- worktree：项目仓库同级的 `worktrees/<project-key>-<task-number>`

实现完成并验证工作区干净后，任务进入完成阶段。随后使用同一 Codex session 发起独立的合并调用，在主仓库执行 `--no-ff` 合并；同一项目的主仓库写入会串行执行。勾选“完成前暂停”时，会在合并前等待成员选择继续修改或确认合并。

### 简单对话流程

状态为 `创建 → 进行 → 关闭`。Agent 直接在项目主仓库和默认目标分支工作，不创建任务 worktree。勾选“完成前暂停”时，会在关闭前等待成员选择继续修改或确认关闭。

两种流程都可以勾选“计划完成时暂停”，后续调用会恢复同一个 Codex session。失败不会自动 reset 或 stash，任务会安全暂停并保留现场。

## Agent 标签路由

每个 Agent 可配置：

- 接取标签：非空时，只接取至少包含其中一个标签的任务；空表示全部接取。
- 拒绝标签：包含任一拒绝标签的任务不会被接取；拒绝规则优先。

每次 Agent 调用都会重新选择满足标签、状态与容量条件的 Agent。没有匹配项时任务保持排队，界面显示“无可用 Agent”。

关闭后的标准流程 worktree 默认保留，可在任务页手动安全清理。

## 数据与验证

SQLite 使用 WAL，当前 schema 为 v8。此版本不兼容任何旧数据或旧代码，启动时会拒绝旧数据库，不执行迁移。

```bash
./projectboard backup --output ./backups
./projectboard restore --input ./backups/<backup-directory>
go test ./...
go build ./cmd/projectboard
```
