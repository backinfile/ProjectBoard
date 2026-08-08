# ProjectBoard

ProjectBoard 是供人类成员与本地 Codex Agent 协作的任务看板。它以单个 Go 服务提供 Web 界面、SQLite 持久化和内置调度器；Agent 不需要 SSH、MCP 或外部 Runner，服务进程会直接调用同一台机器上的 `codex` CLI。

## 前置条件

- Go 1.26（从源码构建时）
- `git` 与 `codex` 位于 ProjectBoard 服务账户的 `PATH`
- 该服务账户已完成 Codex 登录
- 使用 Agent 写入仓库时，管理员已在界面中配置并授权 GitHub App

## 构建与启动

```bash
go build -o projectboard ./cmd/projectboard
PROJECTBOARD_BOOTSTRAP_PASSWORD='change-this-password' ./projectboard serve
```

默认 Web 地址为 `http://localhost:3333`，数据目录为 `./data`。生产环境应通过 TLS 反向代理提供 HTTPS。

常用环境变量：

```text
PORT=3333
PROJECTBOARD_DATA_DIR=./data
PROJECTBOARD_BOOTSTRAP_USERNAME=admin
PROJECTBOARD_BOOTSTRAP_PASSWORD=change-this-password
PROJECTBOARD_PUBLIC_URL=https://board.example.com
```

## Agent 使用方式

管理员创建 Agent 时，运行类型固定为“本地 Codex CLI”，并设置：

- 最大并发任务数，默认 `1`
- 单轮超时分钟数，默认 `120`
- 允许参与的项目

任务创建时可以勾选“是否是 Agent 任务”。只有勾选后才显示同组的两个暂停选项，且默认都关闭：

- 计划完成时暂停：首次轮次只生成计划，成员点击“继续执行”并发送可编辑确认消息后恢复同一 Codex session。
- 任务完成时暂停：Agent 成功后停在“完成”，等待成员返工或手动关闭；未勾选时自动关闭。

任务状态固定为 `创建 → 进行 → 完成 → 关闭`。关闭是不可恢复的只读终态；阻塞是独立标记，阻塞的 Agent 任务不会被调度。

## 本地执行与资源

每个 Agent 任务使用 `PROJECTBOARD_DATA_DIR/workspaces/` 下的独立 clone 和 `projectboard/<project-key>/<task-number>` 分支。ProjectBoard 使用：

```text
codex exec --json --sandbox workspace-write --output-schema ... -C <workspace>
codex exec resume <session-id> ...
```

Codex 仍受自身沙箱和审批规则约束。非交互轮次需要额外审批、超时、异常退出或返回无效结构化结果时，任务保持“进行”并进入失败暂停。原始 JSONL 保存在 execution 历史中，任务对话只记录最终消息和系统事件。

GitHub 短期凭据仅注入 Git 子进程的进程级配置，不写入仓库、remote URL、提示词或数据库。关闭任务后工作目录默认保留，可在任务页查看占用并人工清理；未关闭任务不能清理。

## 数据与升级

SQLite 使用 WAL。备份和恢复：

```bash
./projectboard backup --output ./backups
./projectboard restore --input ./backups/<backup-directory>
```

当前 schema 为 v6，与旧 schema 不兼容；旧数据库启动时会返回明确错误，不执行数据迁移。

## 验证

```bash
go test ./...
go build ./cmd/projectboard
```
