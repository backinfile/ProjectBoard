# ProjectBoard 全量功能覆盖测试报告

- 测试时间：2026-08-13（Asia/Shanghai）
- 服务地址：`http://127.0.0.1:3333`
- 分支：`dev`
- 持久测试项目：`Full Coverage 20260813134659`
- 项目标识：`fc20260813134659`
- 项目 ID：`3lg2u8VNn8G3vAOTmR1KPcbd`
- 本地仓库：`D:\Github\ProjectBoardTestData\full-coverage-20260813134659`
- 数据策略：只追加和更新测试数据；删除类能力仅在隔离临时数据库中执行。

## 结论

公开路由共 67 条，均已通过实时 API、外部 Chrome 或隔离 Go 集成测试获得覆盖证据。Go 全量测试、`go vet`、覆盖率运行和构建全部通过。桌面、840px 以下和平板/手机 620px 以下断点均无横向溢出或浏览器原生按钮样式回流。

发现 2 个需要后续处理的产品问题：

1. 一次真实的任务知识 Agent 请求进入本地 Codex CLI 后失败。ProjectBoard 正确记录为 `failed` 并保留日志，但本机 Codex 环境存在模型缓存字段、未安装插件和 `127.0.0.1:8000/mcp` 无法连接等外部依赖错误。
2. “系统服务”页面目前只显示本地 Git 与运行状态；`GET/PUT /api/system/settings` 已支持公开地址，但最终 Web UI 没有提供编辑入口。

项目管理抽屉已经重新核对：自动化、本地仓库和成员三个页签均可访问，与当前实现一致。

## 持久测试数据

| 类型 | 数据 |
| --- | --- |
| 项目 | 1 个独立本地 Git 项目，含自定义提示词和知识压缩阈值 |
| 用户 | 开发者、观察者、停用账号各 1 个；另保留管理员 |
| 成员 | 2 名开发者、1 名观察者 |
| Agent | 1 个启用 Agent、1 个停用 Agent，含容量、超时、接取/拒绝标签 |
| 任务 | 11 个，覆盖创建、进行、完成、关闭、阻塞/解除、四种优先级、人工/Agent 指派、父子任务、标准/简单工作流 |
| 附件 | 1 个 Markdown 附件，已绑定消息，可内联预览和下载 |
| 知识 | 3 个节点，覆盖树结构、搜索、移动、锁、更新、5 个版本和恢复 |
| Agent 请求 | queued、cancelled、failed 三种状态 |
| 通知 | 管理员与观察者通知，覆盖项目筛选、单条已读和全部已读 |
| 审计 | 24 条以上追加式记录，覆盖详情、搜索、分页和 NDJSON 导出 |

## 公开接口覆盖（67/67）

### 服务、认证与个人账户

- `GET /health`：实时通过。
- `GET /`：静态工作区与缓存策略测试通过。
- `POST /api/auth/login`、`POST /api/auth/logout`：管理员、开发者、观察者实时登录；注销后 `/api/me` 返回 401。
- `GET /api/me`、`PATCH /api/me`：实时读取与资料更新通过。
- `POST /api/me/change-password`：开发者账号实时改密并重新登录通过。
- `GET /api/me/sessions`、`DELETE /api/me/sessions/{id}`：会话列表与撤销通过。

### 系统设置

- `GET /api/system/settings`、`PUT /api/system/settings`：公开地址读取与同值保存通过，审计记录已生成。

### 项目与成员

- `GET /api/projects`、`POST /api/projects`、`PATCH /api/projects/{id}`：实时通过；本地目录自动初始化 Git。
- `GET /api/projects/{id}/members`：开发者/观察者列表通过。
- `POST /api/projects/{id}/members`、`PATCH /api/projects/{id}/members/{userId}`：成员添加和角色往返更新通过。
- `DELETE /api/projects/{id}/members/{userId}`：隔离集成测试通过；未删除持久样本。

### 用户管理

- `GET /api/users`、`GET /api/users/{id}`：实时列表与详情通过。
- `POST /api/users`、`PATCH /api/users/{id}`：三类账号创建和资料更新通过。
- `POST /api/users/{id}/disable`、`POST /api/users/{id}/enable`：实时状态转换通过，保留一个停用样本。
- `POST /api/users/{id}/reset-password`：实时通过。
- `DELETE /api/users/{id}`：逻辑删除与历史保留在隔离集成测试中通过；未删除持久样本。

### 本地 Agent

- `GET /api/agents`、`POST /api/agents`、`PATCH /api/agents/{id}`：配置、容量、超时和标签规则通过。
- `GET /api/agents/{id}/executions/current`：实时返回当前负载，空闲样本为 0。
- `POST /api/agents/{id}/disable`、`POST /api/agents/{id}/enable`：实时往返通过，另保留停用样本。
- `DELETE /api/agents/{id}`：逻辑删除和名称复用在隔离集成测试中通过；未删除持久样本。

### 任务与工作流

- `GET /api/work-items`、`POST /api/work-items`、`GET /api/work-items/{id}`、`PATCH /api/work-items/{id}`：实时通过。
- `POST /api/work-items/{id}/stage`：标准工作流 `created → in_progress → completed → closed` 与简单工作流 `created → in_progress → closed` 通过。
- `POST /api/work-items/{id}/assign`：人工与 Agent 指派通过。
- `POST /api/work-items/{id}/messages`：管理员与开发者会话、提及和通知通过。
- `POST /api/work-items/{id}/agent-action`：暂停、继续、评审动作在隔离工作队列测试中通过。
- `PUT /api/work-items/{id}/followers`：关注者更新和通知通过。
- `POST /api/work-items/{id}/block`、`POST /api/work-items/{id}/unblock`：实时阻塞与解除通过，并保留阻塞和已解除样本。
- `DELETE /api/work-items/{id}/workspace`：非关闭任务返回预期冲突；关闭工作区清理在隔离执行模块测试中覆盖，未清理持久样本。

### 附件

- `POST /api/work-items/{id}/attachments`：实时 Markdown 上传和消息绑定通过。
- `GET /api/attachments/{attachmentId}`：200、89 B、`Content-Disposition: inline`，外部 Chrome 内联预览和下载入口通过。

### Agent 请求

- `GET /api/agent-requests`、`GET /api/agent-requests/{id}`：queued/cancelled/failed 列表与详情通过。
- `POST /api/work-items/{id}/agent-request`：计划请求创建通过。
- `POST /api/agent-requests/{id}/cancel`、`POST /api/agent-requests/{id}/retry`：实时通过。
- `POST /api/agent-requests/{id}/approve-plan`：隔离 Agent 请求集成测试通过。
- 真实本地 Codex 执行：调度、工作区和失败留痕正确；执行结果因本机 Codex 外部依赖错误为 `failed`。

### 知识库

- `GET /api/projects/{id}/knowledge`、`POST /api/projects/{id}/knowledge`：树列表、创建与关键字搜索通过。
- `GET /api/knowledge/nodes/{id}`、`PATCH /api/knowledge/nodes/{id}`：读取和更新通过。
- `POST /api/knowledge/nodes/{id}/move`：跨父节点移动通过。
- `POST /api/knowledge/nodes/{id}/lock`：Agent 锁定通过，持久样本保持锁定。
- `GET /api/knowledge/nodes/{id}/revisions`、`POST /api/knowledge/nodes/{id}/restore`：5 个版本和恢复通过。
- `DELETE /api/knowledge/nodes/{id}`：锁定子树与原子性删除在隔离测试通过；未删除持久样本。
- 观察者可读、不可写：实时验证返回 403。

### 通知与审计

- `GET /api/notifications`：列表、分页、项目筛选、未读计数通过。
- `POST /api/notifications/{id}/read`：管理员单条已读后未读数 2 → 1。
- `POST /api/notifications/read-all`：观察者未读数 9 → 0。
- `GET /api/activity`：搜索、分页和详情通过。
- `GET /api/activity/export`：NDJSON 导出 7167 B，实时通过。

## 外部 Chrome 页面覆盖

- 任务：四列看板、11 个任务、搜索/标签/优先级/阻塞/日期筛选、新建表单、预览、完整详情、阶段表单、配置表单、附件预览、消息时间线。
- 知识库：树、折叠层级、搜索、新建/修改入口、锁状态、版本历史、恢复入口。
- Agent 需求：queued/cancelled/failed 列表和详情，中断/重试操作入口、失败原始日志。
- 我的消息：项目筛选、未读计数、分页控件、任务跳转入口。
- 设置六页签：我的账号、本地 Agent、用户管理、项目管理、审计、系统服务。
- 账户：资料、密码、会话抽屉。
- Agent：容量摘要、启用/停用开关、详情、新建表单。
- 用户：搜索、正常/停用状态、详情、成员关系、新建/重置/禁用/删除入口。
- 项目：搜索、详情、本地 Git、编辑和新建入口。
- 审计：24 条以上记录、详情载荷、两页分页。
- 国际化：中文 → English → 中文通过。
- 布局：侧栏收起/展开、800px 和 600px 断点均无横向溢出，设置页签全部可达，无原生 `outset` 按钮。

## 自动化结果

- `go vet ./...`：通过。
- `go test ./... -count=1`：全部包通过。
- `go test ./... -cover -count=1`：全部包通过。
- `go build -o build/projectboard-full-coverage.exe ./cmd/projectboard`：通过。
- 关键覆盖率：`internal/workqueue 63.5%`、`internal/ops 62.5%`、`internal/projectrepo 62.0%`、`internal/store 58.8%`、`internal/agentrequest 56.6%`、`internal/agentexec 48.1%`、`internal/knowledge 48.3%`、`internal/server 46.1%`。
