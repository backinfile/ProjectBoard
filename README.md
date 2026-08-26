# ProjectBoard

ProjectBoard 是一个 Windows 优先的本地 AI 项目执行中心。它把项目、任务、Codex 对话、Git worktree、修改审核、验证证据、知识和审计放在同一个深色界面中。数据默认只保存在本机，不需要 ProjectBoard 账户，也不保存 API Key。

## 当前能力

- 16 个产品页面：总览、任务看板与列表、任务摘要、对话工作台、修改审核、知识、Agent、搜索、收件箱、自动化、模板、审计、设置、执行队列与 Git 现场。
- 任务内使用 `@Agent名称` 切换独立、可恢复的 Codex 对话；一次提及多个 Agent 时先展示并行计划，确认后创建隔离的子 worktree。
- 每个任务拥有主分支和主 worktree；同任务默认串行，不同任务受全局并发上限调度。
- Codex app-server 结构化事件、危险操作批准、SSE 实时状态和 SQLite 审计。
- 按 Run 签发并撤销的 MCP 能力令牌，支持按权限检索知识和读取同任务对话。
- SQLite WAL、迁移前备份、手动备份、FTS5 搜索和日志秘密脱敏。
- React SPA 被嵌入单个 Go 可执行文件；启动服务后自动打开默认浏览器。

## 环境要求

- Windows 10/11
- Go 1.24 或更新版本
- Node.js 20 或更新版本（仅从源码构建前端时需要）
- 已安装并登录的 Codex CLI，且 `codex app-server --help` 可正常运行
- Git

ProjectBoard 复用 Codex CLI 的现有登录与基础配置，不读取或保存 API Key。

## 从源码运行

```powershell
npm install
npm run build
go run ./cmd/projectboard
```

默认地址为 `http://127.0.0.1:5173`，数据库位于 `%LOCALAPPDATA%\ProjectBoard\projectboard.db`。

开发前端时分别启动服务和 Vite：

```powershell
go run ./cmd/projectboard --open=false
npm run dev
```

Vite 使用 `http://127.0.0.1:5174`，并把 `/api` 代理到 Go 服务。

## 构建单文件版本

```powershell
npm ci
npm run build
go build -trimpath -ldflags="-s -w" -o projectboard.exe ./cmd/projectboard
```

`web/dist` 通过 Go `embed` 打入 `projectboard.exe`。可用参数：

```text
--listen 127.0.0.1:5173
--data-dir D:\ProjectBoardData
--open=false
```

把监听地址改为非回环地址会向局域网暴露项目和执行接口。首版没有网络鉴权，界面会持续显示高危警告；除非位于受信网络，否则不要这样配置。

## 使用流程

1. 添加本地目录并创建任务。
2. 非 Git 目录首次执行前，在确认界面创建 Git 仓库与基线提交。
3. 进入任务对话，向默认 Agent 发送消息，或使用 `@Agent名称`。
4. 多 Agent 计划确认后，首个 Agent 使用主 worktree，其余 Agent 使用一次性子 worktree。
5. 审核候选修改和验证证据，将接受的结果整合到任务主分支。
6. 用户确认后 squash 为任务级提交、合并默认分支，并显式完成任务。

AI 不会自动批准危险操作、合并代码或把任务标记为完成。

## 验证

```powershell
go test ./internal/... ./cmd/...
npm run typecheck
npm test
npm run lint
npm run build
go build ./cmd/projectboard
```

## 本地数据与恢复

- 数据库：`%LOCALAPPDATA%\ProjectBoard\projectboard.db`
- 任务 worktree：`%LOCALAPPDATA%\ProjectBoard\worktrees`
- 备份：数据库同级 `backups` 目录，迁移前自动生成并保留最近 7 份

子 Agent 候选成果在清理前会保留提交与补丁索引。清理失败时工作区进入待清理状态，不会静默丢弃现场。
