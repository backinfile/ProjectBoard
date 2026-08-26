# ProjectBoard

ProjectBoard 是一个 Windows 本地 AI 项目管理工具。你可以在浏览器中管理项目和任务，与 Codex Agent 对话，查看执行状态、代码修改和验证结果。

数据默认保存在本机。ProjectBoard 不要求注册账户，也不会保存 API Key，而是使用已有的 Codex CLI 登录状态。

## 主要功能

- 添加本地项目，使用看板或列表管理任务
- 在任务中与 Codex Agent 对话，并通过 `@Agent名称` 指定 Agent
- 使用独立 Git worktree 隔离任务和并行修改
- 审核代码差异、运行结果和危险操作请求
- 管理项目知识、通知、模板、自动化和审计记录
- 搜索任务、对话、知识、文件及执行日志

## 使用前准备

- Windows 10 或 Windows 11
- Git
- Go 1.24 或更高版本
- Node.js 20 或更高版本
- 已安装并登录 Codex CLI，且 `codex app-server --help` 可以正常运行

## 启动

在项目目录中运行：

```powershell
npm install
npm run build
go run ./cmd/projectboard
```

ProjectBoard 默认打开 `http://127.0.0.1:5173`。如果浏览器没有自动打开，请手动访问该地址。

也可以构建为单个可执行文件：

```powershell
npm ci
npm run build
go build -trimpath -ldflags="-s -w" -o projectboard.exe ./cmd/projectboard
```

然后运行：

```powershell
.\projectboard.exe
```

## 基本使用

1. 点击“添加本地项目”，浏览并选择项目目录。
2. 创建任务，并在任务详情中输入要交给 Agent 的指令。
3. 使用 `@Agent名称` 指定 Agent；同时指定多个 Agent 时，需要先确认并行计划。
4. 在修改审核中检查代码差异和验证结果。
5. 确认修改后再提交、合并，并由用户手动完成任务。

危险命令、额外目录访问和并行 worktree 创建等操作需要用户确认。AI 不会自行完成任务或静默合并代码。

## 本地数据

默认数据目录：

```text
%LOCALAPPDATA%\ProjectBoard
```

其中包含数据库、备份、日志和任务 worktree。迁移或重装前，建议备份整个目录。

可通过启动参数修改设置：

```powershell
.\projectboard.exe --listen 127.0.0.1:5173 --data-dir D:\ProjectBoardData --open=false
```

不要轻易将监听地址改为非本机地址。当前版本没有网络访问鉴权，暴露到局域网可能允许其他设备访问项目和执行接口。
