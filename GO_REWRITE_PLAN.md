# ProjectBoard Go + 静态网页重写计划

## 1. 目标

删除当前 Node.js、TypeScript、React 和 Vite 实现，从产品行为重新实现 ProjectBoard：

- 后端、Runner、MCP、备份与恢复全部使用 Go。
- 浏览器端使用静态 HTML、CSS 和原生 JavaScript，不使用 React、Vue 或前端构建工具。
- 静态资源通过 `go:embed` 编译进主程序。
- UI 以 [`projectboard-ui-prototype.html`](./projectboard-ui-prototype.html) 为视觉与交互基准。
- 保持现有产品功能和安全约束，但不按旧文件或旧代码逐行翻译。
- 最终只发布 `projectboard` 和 `projectboard-runner` 两个可执行文件。

## 2. 非目标

- 不保留旧 TypeScript 内部接口或目录结构。
- 不为了兼容旧实现而复制浅层模块。
- 不自动合并、发布、部署或推送目标分支。
- 不在服务端运行模型；Codex 仍由受信任 Runner 在本机执行。
- 不删除用户数据、密钥、环境配置或历史提交。

## 3. 重写前的安全措施

这是大功能，必须遵循仓库的开发流程：

1. 先检查并整理当前 `dev` 分支的未提交修改。
2. 将当前可运行状态提交，建立 `pre-go-rewrite` 标签或等价可恢复点。
3. 从 `dev` 创建 `codex/go-rewrite` 功能分支。
4. 记录旧版测试结果、数据库结构和 HTTP 行为，作为黑盒基线。
5. `data/`、`.env`、密钥文件和本地 Runner 状态不进入删除范围。

旧代码删除后通过 Git 历史查阅，不在新树中保留一份 `legacy/` 副本。

## 4. 删除与保留范围

### 删除

- `src/**/*.ts`
- `web/src/`
- TypeScript 测试和 Playwright 测试
- `package.json`、`pnpm-lock.yaml`、`pnpm-workspace.yaml`
- Vite、Vitest、TypeScript、ESLint、Playwright 配置
- Node.js Runner 打包脚本和旧 `dist/` 产物
- 仅服务于 Node.js 运行方式的部署文件

### 保留并更新

- `AGENTS.md`
- `PLAN.md`、`PRODUCT.md`、`DESIGN.md`、`CONTEXT.md`
- `projectboard-ui-prototype.html`，仅作为设计参考，不进入正式运行路径
- `README.md`，在完成重写后改为面向 Go 版本用户的安装与使用说明
- `.env.example`，改写为 Go 版本配置
- `deploy/`，更新为新的二进制与启动命令
- `data/` 和 `.env`，始终视为用户数据，不删除、不覆盖

## 5. 最终交付形态

```text
projectboard
├── projectboard serve       # Web、API、静态网页、Git Broker、HTTP MCP
├── projectboard mcp         # stdio MCP，供本地客户端启动
├── projectboard migrate     # 显式数据库迁移
├── projectboard backup
└── projectboard restore

projectboard-runner
├── connect
├── register
├── poll
├── execute
├── submit
├── pause
└── resume
```

普通部署只需要启动 `projectboard serve`。Runner 独立发布和运行，因为它需要访问本机仓库、Git、Codex CLI 和项目验证命令。

## 6. 建议目录结构

```text
cmd/
├── projectboard/
└── projectboard-runner/

internal/
├── app/            # 程序装配、配置和生命周期
├── identity/       # 用户、密码、会话、CSRF、系统角色
├── projects/       # 项目、成员、Agent 授权、项目配置
├── workqueue/      # 工单状态机、对话、尝试、依赖、租约
├── providers/      # 项目 Git 授权与 GitHub/GitLab 适配器
├── runner/         # 配对、轮询、执行提交协议
├── audit/          # 只追加审计与统一脱敏
├── store/          # SQLite、事务和迁移
├── httpui/         # HTTP 路由、JSON、Cookie、静态网页
└── mcp/            # HTTP/stdio 传输，共用同一工具实现

web/
├── index.html
├── assets/
│   ├── styles.css
│   ├── app.js
│   ├── api.js
│   ├── i18n.js
│   └── icons.svg
└── views/          # 按页面拆分的原生 JS 模块

migrations/
tests/
deploy/
```

不要为每张表建立一套 repository/interface/implementation。只有确实存在生产与测试两种适配器的外部依赖才设置 seam，例如 GitHub、GitLab、时钟和命令执行器。

## 7. 深模块划分

### Identity

小接口后隐藏密码哈希、登录限速、会话、Cookie、CSRF、Origin、账户禁用和会话撤销。使用 Argon2id；敏感 Token 只存哈希。

### Projects

负责项目生命周期、成员角色、Agent 项目授权、分支策略、验证命令、禁止路径和配置版本。项目配置变化必须使相关 Runner 映射失效并要求重新确认。

### WorkQueue

负责完整业务规则：

- `todo -> discussion -> execution -> acceptance -> completed`
- `abandoned` 为终态
- blocked 是附加标记，不是阶段
- 乐观版本、幂等请求、连续对话
- 不可覆盖的讨论结论、执行尝试和验收尝试
- 两级拆分、依赖校验、循环检测
- 每工单和每 Agent 单活租约

HTTP 和 MCP 都只能调用该模块，不能各自重新实现状态规则。

### Provider Authorizations

分成两层：

1. 可复用的提供商授权：GitHub App、GitLab OAuth/PAT 及加密密钥。
2. 每个项目独立的仓库授权绑定：项目、授权、仓库、权限、批准人、批准时间和撤销状态。

每个项目必须显式授权，但可以复用已有提供商授权。临时 Token 的签发必须同时校验项目 Grant、仓库、任务、Runner 和有效租约。

### Runner Control

服务端部分管理一次性配对、Agent Token、轮询、租约、心跳、凭据签发和结构化结果提交。客户端部分管理可信仓库登记、配置摘要、worktree、Codex 子进程、验证命令和受控推送。

### Audit

所有关键命令统一写入只追加审计。脱敏在模块内部完成，调用者不能选择跳过。

## 8. 数据与密钥

- 使用 SQLite WAL、外键、busy timeout 和短事务。
- 优先使用纯 Go SQLite 驱动，保持 `CGO_ENABLED=0` 和交叉编译能力。
- 保留兼容旧数据库的迁移路径；禁止要求用户删除数据库重新开始。
- 提供商长期密钥加密落盘，短期访问 Token 只保存在内存。
- GitHub App 私钥按“提供商授权”保存一次，项目 Grant 只引用它，不复制 PEM。
- 主加密密钥与数据库分开备份；恢复文档必须明确两者缺一不可。

## 9. HTTP、静态网页与 MCP

- 尽量保持现有 `/api` JSON 路径和错误结构，减少行为迁移风险。
- 使用 Go `net/http`；只有在路由确实明显简化时才引入轻量路由库。
- `//go:embed` 嵌入 `web/`，不依赖外部静态目录。
- HTML 入口禁止长缓存；带版本指纹的 CSS、JS 和图标允许 immutable 缓存。
- SPA fallback 只处理接受 HTML 的 GET 请求，绝不能吞掉 `/api`、`/mcp` 或下载错误。
- MCP 工具实现只写一次，同时提供：
  - `projectboard mcp` 的 stdio 传输
  - `projectboard serve` 下的 Streamable HTTP `/mcp`
- HTTP、MCP 和 Runner 接口共用身份、权限、幂等与审计模块。

## 10. UI 重建规则

正式 UI 不直接运行原型文件，而是提取其设计语言重新实现：

- 桌面使用全局顶栏、248px 项目导航和连续工作表面。
- 任务队列采用无卡片行、明确优先级、阶段、负责人和更新时间。
- 工单详情保留五阶段轨道、连续对话、证据和阶段操作。
- 阅读和编辑使用覆盖式 Drawer，不改变或滚动底层布局。
- 840px 以下导航变成横向控制带；600px 以下表格转为单列。
- 五阶段轨道在窄屏保持横向可读，不压缩成不可辨认图标。
- 中英文切换不丢失项目、页面、筛选条件或已打开的工单上下文。
- 图标全部来自本地 Lucide SVG sprite，禁止表情符号和 CDN 依赖。
- Lime 只表示当前、选中或可行动状态；blocked、abandoned 和负责人不能只靠颜色表达。
- 所有交互支持键盘焦点、可读标签、关闭 Drawer 的 Escape 和焦点恢复。

需要实现的页面：

- 登录与首次管理员引导
- 无项目空状态
- 项目切换与任务队列
- 工单详情、创建、分配、讨论、执行、验收、阻塞和放弃
- 成员、Agents 与 Runner 配对
- 项目设置和独立 Git 授权
- 用户管理
- 项目管理
- 活动记录
- 我的账号、密码和会话
- Runner 下载与安装说明

## 11. 实施阶段

### 阶段 0：冻结行为基线

- 运行当前类型检查、测试、E2E 和构建，记录既有失败。
- 导出当前 SQLite schema、路由清单和代表性 JSON 响应。
- 建立状态机、权限、Git 授权和 UI 页面验收矩阵。

### 阶段 1：清空旧实现并建立 Go 骨架

- 在功能分支删除 Node/TypeScript 实现和工具链。
- 初始化 `go.mod`、命令入口、配置加载、日志和优雅退出。
- 完成嵌入静态首页、`/health`、SQLite 打开与迁移。

验收：全新环境中一个 `projectboard serve` 可启动并打开静态页面。

### 阶段 2：身份与项目纵向切片

- 实现管理员引导、登录、登出、CSRF、账户与会话。
- 实现项目、成员、项目切换和无项目状态。
- 同步完成对应静态页面，而不是先写完全部后端。

验收：管理员可以登录、创建项目、创建用户并分配项目角色。

### 阶段 3：工单主流程纵向切片

- 实现 WorkQueue 深模块和数据库事务。
- 先完成一条完整路径：创建、讨论结论、执行结果、验收、完成。
- 再补阻塞、返工、放弃、附件、拆分、依赖和并发冲突。
- UI 同步完成队列、工单详情、阶段轨道和连续对话。

验收：完整主流程及所有回退结果通过 Go 集成测试和浏览器测试。

### 阶段 4：Agent 与 Runner

- 实现 Agent 生命周期、项目授权、一次性配对和 Token 轮换。
- 重写 Go Runner 的登记、轮询、租约、worktree、Codex、验证和提交。
- Windows 托盘启动器和 Linux systemd 文件改为调用 Go Runner。

验收：Runner 可在真实测试仓库完成领取、执行、验证、推送任务分支和回写结果。

### 阶段 5：项目级 Git 授权

- 实现可复用 Provider Authorization 与独立 Project Grant。
- 实现 GitHub App Manifest/Installation、GitLab 授权和密钥加密。
- 实现 Webhook 验签、幂等、重新读取、仓库/分支校验和提交去重。
- 实现按单仓库和用途降权的临时凭据签发。

验收：两个项目可复用一个授权并独立撤销；也可各自建立完全独立授权。

### 阶段 6：MCP 与运维命令

- 用同一工具实现接入 stdio 和 HTTP MCP。
- 实现 migrate、backup、restore 和 Runner 下载。
- 验证备份恢复后用户、项目、授权元数据、工单和审计一致。

### 阶段 7：UI 完整度与安全加固

- 对照原型完成全部页面、Drawer、响应式布局与双语。
- 完成 CSP、安全响应头、上传限制、Markdown 清理和下载隔离。
- 完成登录限速、Token 脱敏、进程环境清理和异常恢复测试。

### 阶段 8：切换与发布准备

- 删除残余 Node 文档、脚本和依赖说明。
- 更新 README、架构文档、安装命令和升级说明。
- 执行全量测试、Windows/Linux 构建和从旧数据库升级演练。
- 将功能分支合并回 `dev`；Release 前按仓库流程再次测试并检查 README。

## 12. 测试策略

所有新测试使用 Go：

- 表驱动测试：状态转换、权限矩阵、分支规则、路径规则和错误码。
- SQLite 集成测试：事务、幂等、并发领取、租约和迁移。
- `httptest`：登录、Cookie、CSRF、Origin、JSON、上传和下载。
- Provider Mock Adapter：GitHub/GitLab Token、Webhook 和仓库查询。
- Runner 测试：使用临时 Git 仓库和可控命令执行器，不调用真实 Codex。
- 浏览器测试：使用 Go 驱动的 Chrome 自动化验证关键 UI 流程。
- 恢复测试：备份、损坏拒绝、离线恢复和旧数据库升级。

测试只跨模块接口验证可观察行为，不复刻旧实现的内部调用结构。

## 13. 完成标准

满足以下条件才视为重写完成：

- 仓库中无 TypeScript、React、Vite、pnpm、Node Runner 或 Node 运行依赖。
- `go test ./...`、`go vet ./...`、浏览器测试和安全测试全部通过。
- `CGO_ENABLED=0` 可构建 Windows、Linux amd64 和 Linux arm64。
- 新安装只需一个主程序即可提供网页和功能完整的 API。
- 旧 SQLite 数据可以升级，失败时事务回滚且原库可恢复。
- 每个项目必须拥有独立、可撤销、可审计的仓库 Grant，并可复用已有提供商授权。
- Runner 不持有长期 Git 凭据，Codex 子进程不继承 ProjectBoard 或 Git 提供商密钥。
- UI 与原型在布局、阶段表达、Drawer、响应式和中英文行为上达到一致体验。
- README 面向最终用户说明安装、首次启动、Git 授权、Runner、MCP、备份与升级。

## 14. 主要风险与控制

- **一次性删除旧实现导致功能遗漏**：先建立行为矩阵和可恢复 Git 标签，再删除代码。
- **数据库兼容失败**：迁移前自动备份，迁移在事务中执行，并做真实旧库副本演练。
- **原生 JavaScript 可维护性下降**：按页面和领域拆 ES Modules，统一状态容器、请求层和 Drawer 控制器。
- **内置 Broker 扩大进程权限**：密钥封装在 providers 模块，不开放 Broker 端口，临时 Token 最小化并统一脱敏。
- **复用授权破坏项目隔离**：项目 Grant 是凭据签发的强制条件，任何授权本身都不能直接换取 Token。
- **Runner 跨平台差异**：进程树、文件权限、托盘和 Git 凭据分别做 Windows/Linux 集成测试。

