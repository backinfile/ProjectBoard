# ProjectBoard 完整产品计划

## 1. 产品定义

ProjectBoard 是面向人类与 Agent 协作的安全工单队列。人创建工单并定义结果，人类成员或 Agent 在同一条连续对话中完成讨论、执行和验收，Runner 负责本地 Codex 调用、Git、验证、租约与结构化结果回写。

产品不是通用 Jira，也不在服务端运行模型或保存 Git 凭据。服务端负责状态、权限、审计和协作；代码执行只发生在登记过的 Runner 主机。

### 1.1 完成标准

- 工单严格使用 `todo → discussion → execution → acceptance → completed` 主流程，并支持从任意未完成阶段进入 `abandoned`。
- 阻塞使用 `blocked_at + blocked_reason` 附加标记，不改变工单主阶段。
- 分配执行者后，待办工单进入讨论；任意未完成阶段都能更换执行者，旧租约立即终止，全部历史保留。
- 讨论、执行和验收共用一条支持 Markdown 与附件的连续对话时间线。
- 进入执行前必须冻结结构化、可版本化的讨论结论；范围实质变化必须产生新版本。
- 执行结果只有在任务分支推送且必需验证通过后才能进入验收，不能直接完成。
- 验收必须产生不可覆盖的结构化结论；通过才完成，要求修改退回执行，范围不清退回讨论。
- Git commit message 包含完整工单 ID 时，提交自动成为对话中的系统证据事件；单个提交不能触发完成。
- 所有状态变化、执行者变化、结论版本、提交、验证和验收尝试可审计。

### 1.2 产品边界

不自动执行以下操作：

- 直接推送目标分支、force-push、创建或推送 tag。
- 自动创建或合并 PR、发布、部署、删除远端任务分支。
- 把 ProjectBoard Token、Git 凭据或用户会话传给 Codex 子进程。
- 用阻塞标记替代依赖关系，或用 Git 提交替代验收结论。

## 2. 总体架构

```text
Browser
  -> Web UI / JSON API
       -> Service
            -> SQLite
            -> append-only activity log

Git Credential Broker
  -> one GitHub App installation per account / selected repositories
  -> down-scoped observer and Runner installation tokens
  -> explicit human/Runner commit sync notification
  -> provider private key / client secret in isolated secret storage

Runner Host
  -> Agent poll / heartbeat
  -> one-time device pairing
  -> trusted local repository mapping
  -> short-lived repository credential request
  -> isolated worktree
  -> Codex CLI
  -> build / test / lint
  -> task-branch push
  -> structured result + conversation events
```

- Web 服务不保存 OpenAI API Key 或 ChatGPT 凭据；系统管理员在网页中写入的 GitHub App 私钥与 GitLab client secret 由 Git Credential Broker 使用独立 master key 加密，读取 API 不回显明文。
- ProjectBoard 对 GitHub 使用一个申请 `Contents: read & write` 的 GitHub App；Broker 按用途和单仓库降权签发最长一小时的 installation token：观察用途为 `read`，有效执行租约的 Runner 为 `write`。
- Runner 复用本机 Codex 登录态，但不保存长期 Git 提供商凭据；短期 Git token 只交给 Runner 的受控 Git credential adapter，不进入 Codex 子进程环境、提示词或日志。
- 服务端只保存 Agent Token 哈希；创建 Agent 时浏览器只展示十分钟有效的一次性配对码，Runner 原子消费后才取得只显示一次的 Agent Token，并存入本机安全凭据库。
- SQLite 写入使用事务、外键、唯一索引和幂等记录保护。

## 3. 身份、成员与权限

### 3.1 人类用户

- `administrator`：系统级角色；管理用户、项目、仓库执行配置、成员、Agent、Token、全部工单和审计。
- `developer`：项目级角色；创建和修改工单、参与讨论、人工执行、分配执行者、提交结果、验收和放弃工单。
- `viewer`：项目级只读角色。
- 用户是组织级账户；`ProjectMembership` 决定用户在单个项目中的 `developer | viewer` 角色。
- 系统管理员通过独立的用户管理页创建账户、设置系统角色、重置密码以及禁用/启用账户；创建账户不会自动加入任何项目。
- 禁用账户立即失去登录和新指派资格，但历史署名永久保留。
- 禁用用户会撤销其全部登录会话，并禁止新的人工验收；项目成员关系、消息、验收记录和审计署名保留。当前登录账户不能禁用自己，系统不能禁用最后一个可用管理员。
- 个人账号页支持修改显示名称、密码和撤销登录会话。

### 3.2 Agent 身份

- Agent 是组织级独立身份，不冒充人类用户；每个 Runner 使用一个命名 Agent。
- 所有项目都能看到全部 Agent，`AgentProjectGrant` 决定当前项目是否启用。
- 新 Agent 默认不在任何项目启用。
- 创建 Agent 只要求名称和用途说明；创建后生成十分钟有效、只能消费一次的 Runner 配对码，不要求选择项目、仓库或 Git 权限。
- Runner 使用配对码提交设备身份和公钥摘要；成功后配对码失效，Agent Token 只返回给 Runner，管理员浏览器不接触明文 Token。
- 所有 Runner/Agent 的仓库能力固定为可读可写；未在项目启用时不能取得该项目的任何 Git 凭据，启用后也只有在成为当前执行者并持有有效租约时才能取得当前仓库的短期写 token。
- 创建或启用新 Agent 不需要再次进入 GitHub/GitLab；只有项目绑定了尚未授权给提供商安装的新仓库，或安装权限发生变化时才要求平台管理员确认。
- 同一 Agent 可以有多个待接受工单，但同时只能持有一个活跃租约。
- 负责人控件只列出当前项目有效 developer、系统管理员和已启用 Agent。

### 3.3 内容与审计安全

- 所有 Markdown 按不可信输入处理：禁原始 HTML，链接协议白名单，附件下载使用安全响应头。
- 创建、编辑、分配、更换执行者、租约、阶段转换、讨论结论、Git 证据、验收、阻塞、放弃、成员和 Token 操作都写入只追加事件。
- 敏感字段在日志、导出、错误响应和附件元数据中统一脱敏。

## 4. 领域模型

### 4.1 核心实体

```text
User
Session
Project
ProjectMembership
AgentIdentity
AgentToken
AgentProjectGrant
RunnerPairingCode
RunnerDevice
GitProviderInstallation
RepositoryBinding
GitCredentialIssuance
WorkItem
WorkItemDependency
Assignment
Lease
ConversationEntry
Attachment
DiscussionConclusion
ExecutionAttempt
GitCommitEvidence
ValidationRun
AcceptanceAttempt
DecompositionProposal
ActivityEvent
IdempotencyRecord
RunnerStatus
```

### 4.2 Project

项目保存：

- 名称、唯一标识、Markdown 说明、归档状态。
- Git 仓库 URL和远端名（默认 `origin`）。
- 默认目标分支与允许目标分支的精确名称或受限 glob，例如 `main`、`develop`、`release/*`。
- build/test/lint 命令；每条包含名称、命令、是否必需和超时。
- 禁止修改路径 glob 与项目级 Agent 工作规则。
- 默认 `discussion_mode`、`execution_mode`、`acceptance_mode`。

创建项目只要求名称、唯一标识和 Git 仓库 URL；其余字段写入安全默认值并在项目设置中完善。创建者成为首个 developer，所有 Agent 默认不启用。

### 4.2.1 GitProviderInstallation、RepositoryBinding 与凭据签发

- `GitProviderInstallation` 保存 provider、外部 installation/application ID、授权仓库集合、同步模式和权限摘要；不保存可直接使用的 provider token。
- GitHub 使用单个 GitHub App 完成仓库读取和 Runner 写入，不订阅 Webhook。安装级权限为 `Contents: read & write`，每次签发再用 `repository_ids` 和 `permissions` 缩小到单仓库、单用途。
- `RepositoryBinding` 把一个 Project 精确绑定到安装中的一个外部 repository ID、规范化 clone URL、默认分支和 provider；不能只以可变仓库名称作为身份。
- Git Credential Broker 是深 Module；Interface 只暴露 `issueObserverCredential(repositoryId)` 与 `issueRunnerCredential(runId, leaseId)`。GitHub App 与 GitLab OAuth/Token 是该 seam 上的 Adapter，调用者不能自行传入任意权限或仓库。
- `issueObserverCredential` 只返回读取提交、分支和文件变化所需的权限；`issueRunnerCredential` 从 run、assignment、AgentProjectGrant、lease 和 RepositoryBinding 推导唯一仓库及写权限。
- `GitCredentialIssuance` 只记录 provider、installation、repository、Agent、run、用途、权限摘要、签发/过期/撤销时间与结果，不保存 token 明文。
- GitHub App 私钥由管理员网页写入后只以加密形式保存，并仅由 Broker 解密使用；管理页必须通过 HTTPS 提供且读取 API 不返回明文。部署时仍可把 Broker 隔离为独立进程，并保持独立 Interface。
- GitHub/GitLab 的仓库写权限不能代替分支级保护；`main`、`develop`、`release/*` 和 tag 由 provider Ruleset/Protected Branch 阻止直接写入，ProjectBoard App/Token 不得加入 bypass。

### 4.3 WorkItem

```text
id
project_id
number
parent_id
title
description_markdown
acceptance_criteria_markdown
priority                    # urgent | high | medium | low
stage                       # todo | discussion | execution | acceptance |
                            # completed | abandoned
discussion_mode             # manual | auto
execution_mode              # manual | auto
acceptance_mode             # human | independent_agent | auto
target_branch
assignee_kind               # human | agent | null
assignee_id
assignment_state            # reserved | active | null
blocked_at
blocked_reason
abandoned_at
abandoned_reason
version
created_at
updated_at
completed_at
```

- 新工单默认 `todo`，使用项目默认模式和默认目标分支。
- 阻塞是附加标记；阻塞工单仍保持原 `stage`，解除阻塞只清空阻塞字段。
- `completed` 和 `abandoned` 是终态。
- 目标分支在开始讨论前可直接修改；开始后修改属于范围变化，递增版本、终止租约并返回讨论。
- Git、验证和结论数据不重复存入工单主表。

### 4.4 ConversationEntry 与 Attachment

讨论、执行和验收共用一条按时间排序的连续时间线。每条记录包含：

- `kind = message | stage_transition | assignment_changed | conclusion_frozen | git_commit | validation | acceptance_result | blocked | unblocked | abandoned`。
- 所属工单、发生时的阶段、作者类型/身份、时间、不可变载荷和关联版本。
- 普通消息支持 Markdown、MD 文档、图片和其他附件。
- 附件保存原始文件名、MIME、大小、哈希、存储键和上传者；下载必须鉴权。
- 系统事件不可编辑；普通消息的编辑与删除保留修订记录。

阶段转换通过系统分隔事件插入同一时间线，例如“进入讨论”“讨论结论 v2 已冻结”“进入执行”“进入验收”。

### 4.5 DiscussionConclusion

进入执行前必须冻结讨论结论：

```text
version
goal_markdown
scope_markdown
out_of_scope_markdown
implementation_plan_markdown
acceptance_criteria_markdown
risks_markdown
created_by
created_at
supersedes_version
```

- `discussion_mode=manual`：developer 或管理员确认结论后进入执行。
- `discussion_mode=auto`：Agent 生成结构化结论并通过 Schema 校验后自动进入执行。
- 自动模式只是自动退出讨论，仍必须生成结论、版本和系统事件。
- 执行或验收中发生实质范围变化时，创建新版本并返回讨论；旧版本永久保留。

### 4.6 ExecutionAttempt 与验证

每次进入或返回执行创建新的 `ExecutionAttempt`：

- 记录执行者、讨论结论版本、目标分支、精确基线 commit、任务分支、租约代次、开始/结束时间和结束原因。
- 产物包括摘要、commit hashes、改动文件、验证结果、剩余风险、附件和发现工单。
- 返工复用同一任务分支并追加 commit，不改写已推送历史。
- `execution_mode=manual`：人确认执行提交和验证后进入验收。
- `execution_mode=auto`：任务分支 push、worktree 干净、禁止路径检查通过且所有必需命令成功后自动进入验收。
- 自动执行不能越过验收直接完成。

### 4.7 GitCommitEvidence

- Runner 获取或接收提交后检查 commit message 是否包含完整工单 ID，例如 `PB-148`。
- 必须按完整 ID 边界匹配，不能让 `PB-14` 命中 `PB-148`。
- 服务端校验项目仓库、允许的任务分支、提交可达性和工单项目归属。
- 使用 `(repository_id, commit_sha)` 唯一约束去重。
- 合法提交作为 `git_commit` 系统事件插入连续时间线，展示 SHA、message、作者、分支和文件变化摘要。
- 提交事件只是执行证据，不转换工单状态，也不能替代执行提交或验收结论。

### 4.8 AcceptanceAttempt

进入验收时创建新的、不可覆盖的验收尝试。验收界面和对话时间线展示：

- 当前讨论结论版本与验收标准。
- Git 提交、文件变化、任务分支和基线。
- build/test/lint 结果、日志摘要和补充附件。
- 验收者、开始时间、结构化结论和说明。

结论：

- `pass`：进入 `completed`。
- `changes_requested`：返回 `execution`，创建新的执行尝试。
- `scope_unclear`：返回 `discussion`，需要新讨论结论版本。
- `abandon`：进入 `abandoned`。

`acceptance_mode`：

- `human`：developer 或管理员验收。
- `independent_agent`：由不同于当前执行 Agent 的已启用 Agent 验收。
- `auto`：独立验收运行满足结构化规则后自动提交结论。

无论模式如何，每次验收尝试、证据和结论都永久保留。

## 5. 工单状态机

### 5.1 主流程

```text
todo
  --分配执行者--> discussion

discussion
  --冻结讨论结论--> execution

execution
  --提交结果且满足验证--> acceptance

acceptance
  --pass--> completed
  --changes_requested--> execution
  --scope_unclear--> discussion

todo | discussion | execution | acceptance
  --abandon--> abandoned
```

- `blocked_at` 可以附着于任意未完成阶段；阻塞期间禁止新执行动作，但允许讨论、补充附件、更换执行者、解除阻塞或放弃。
- 任何阶段变化都写入对话分隔事件和活动审计。
- `completed` 不允许重新打开；后续工作创建新工单并建立来源关系。

### 5.2 分配与更换执行者

- `todo` 分配执行者后进入 `discussion`。
- `discussion | execution | acceptance` 可更换执行者；验收阶段的验收者与执行者分别记录。
- 更换时在同一事务中终止旧租约、递增租约代次、写入 `assignment_changed` 事件并保留旧尝试。
- 人工负责人不使用心跳租约；Agent 接受保留指派后创建滑动租约。
- 两个并发分配请求只有一个成功；冲突响应包含最新版本和当前负责人。

### 5.3 阻塞与放弃

- 阻塞必须提供原因，可选关联依赖或发现工单。
- Runner 阻塞时结束当前活跃尝试并释放租约，但工单主阶段不变。
- 解除阻塞不创建新阶段；重新工作时创建新的执行尝试或继续讨论。
- 放弃必须提供原因，终止租约并写入终态事件；历史和附件不删除。

## 6. 父子工单与依赖

- 工单支持父工单与直接子工单两层；子工单不能继续拆分。
- 讨论结论可以附带一次结构化拆分提案；全部子工单与依赖必须原子创建。
- 拆分提案至少 2 项、最多 20 项，校验唯一 key、无环、同父工单引用和非空验收条件。
- 子工单创建为 `todo`，继承目标分支与自动模式。
- 父工单拆分后保持 `execution`，派生状态显示“等待子项”，不进入 Agent 队列。
- 只有依赖完成且未阻塞的叶子工单可被领取。
- 全部直接子工单完成后，父工单进入 `acceptance`；通过后才完成。
- 已产生对话、尝试、Git 或验收记录的子工单不能删除，只能放弃。

## 7. 租约与并发

- Agent 接受指派或自领后创建 `active_lease`；保留指派使用 `reserved`。
- 同一工单同一时刻最多一个执行租约，同一 Agent 最多一个活跃租约。
- Runner 每 5 分钟心跳一次，把租约延长到未来 30 分钟。
- 租约过期、强制释放、更换执行者或版本冲突后，旧租约提交返回 `409`，不能覆盖新状态。
- Runner 得知租约失效后终止自己持有句柄且身份匹配的 Codex 进程树，保留 worktree 供检查。
- 所有领取、续租、释放和冲突写入审计。

## 8. Git 与 Runner 协议

### 8.1 Git provider 与短期凭据

- 管理员先在全局系统设置保存 ProjectBoard 回调地址，再从项目设置点击“自动配置 Git”；GitHub 通过 App Manifest Flow 自动提交最小 App 配置并在服务端交换 App ID、slug 与 PEM，管理员只在 GitHub 官方页明确确认，随后安装、选择仓库并保存权限摘要。GitLab 在官方 OAuth 页完成确认。
- GitHub 使用一个 GitHub App。ProjectBoard 观察提交时由 Broker 签发单仓库 `contents: read` token；Runner 执行时签发单仓库 `contents: write` token，最长一小时且任务结束后主动撤销。
- AgentProjectGrant 只表达 Agent 是否可用于当前项目，不存 Git 权限等级；所有已启用 Agent 的 Git 能力一致，但只有有效 run + assignment + lease 能触发写 token 签发。
- Runner 通过受控 credential adapter 使用 token 完成 clone、fetch 和任务分支 push；token 不写入仓库 remote URL、Git config、磁盘日志或 Codex 环境。
- 提供商回调、installation 变化、仓库增删、按需提交查询、token 签发/撤销与失败原因全部写入审计；repository/SHA 和 requestId 都必须幂等。
- 自动配置可以检测分支保护；创建或修改 provider Ruleset/Protected Branch 是独立的高影响操作，必须展示变更预览并再次确认，且不保留 Administration 权限。

### 8.2 本地仓库登记

- Runner 保存 Project ID 到可信本地仓库根目录的映射。
- 首次登记验证 `origin` URL 与项目配置一致。
- 仓库 URL、分支策略、命令、禁止路径或工作规则变化后暂停项目，直到本机重新确认摘要。
- Runner 使用普通用户权限，不请求管理员或 root 权限。

### 8.3 Worktree 与任务分支

- 每个执行尝试使用独立 worktree；任务分支为 `projectboard/<project-key>/<task-number>`。
- 创建前 fetch 目标分支并记录 `origin/<target-branch>` 的精确基线 SHA。
- 讨论只需要仓库信息时使用只读 worktree；执行使用可写 worktree；验收使用独立只读 worktree。
- 返工复用任务分支并追加 commit；不 force-push，不自动删除远端分支。

### 8.4 执行提交

Runner 提交执行结果前必须：

1. 验证租约、工单版本和讨论结论版本仍有效。
2. 解析 Codex JSONL 事件和最终结构化输出。
3. 确认 worktree 干净或无代码结果说明成立。
4. 记录 commits、changed files，并拒绝禁止路径修改。
5. 独立执行必需 build/test/lint，保存退出码、耗时和截断日志。
6. 确认远端任务分支无未知提交或非快进冲突。
7. 只把任务分支 fast-forward push 到 `origin`。
8. Push 成功后提交执行结果；服务端进入或等待进入验收。

### 8.5 Commit 自动关联

- 人类在项目设置点击同步，或持有有效 Run 与租约的 Runner 在 push 后通知 ProjectBoard；ProjectBoard 使用单仓库只读 token 查询最近提交。
- Runner 在 fetch、提交、push 和验收准备时扫描新 commit message，并通过显式通知触发服务端查询；系统不监听 Webhook，也不运行后台轮询。
- 只接受可从当前项目任务分支到达、且消息包含完整工单 ID 的 commit。
- 服务端再次校验外部 repository ID、分支、ID 边界、提交可达性与 `(repo, SHA)` 去重后写入对话事件。

## 9. Runner 生命周期

- 状态：停止、连接中、空闲、暂停、待接受、讨论中、执行中、验收中、等待人工、错误。
- 暂停只停止领取新任务，当前工作继续心跳。
- 停止活跃 Runner 必须确认；终止受控子进程、提交中断事件并释放租约。
- Windows 提供系统托盘管理；Linux 提供前台 CLI 与 systemd unit。
- 不按进程名清理所有 Node、Codex 或 Git 进程。

## 10. Web UI

### 10.1 导航与页面

- 项目切换器及项目管理/创建。
- 任务队列。
- “成员”与 “Agents” 是两个独立的项目级页面，均显示组织内全部身份和当前项目关系。
- 项目设置是项目级入口，紧邻 Agents 下方。
- 侧栏底部系统级入口依次为用户管理、项目管理、活动记录；用户管理仅管理员可见，用于管理人类登录账户生命周期，不用于配置项目角色。
- 点击右上角头像进入个人账号管理。
- 头像左侧提供语言切换按钮，支持 `zh-CN` 与 `en`，默认 `zh-CN`；切换语言不改变当前项目、页面、筛选、已选工单或抽屉上下文。

### 10.2 队列

- 页签：全部、待办、讨论、执行、验收、完成、放弃。
- 阻塞作为独立筛选和行内标记，不是状态页签。
- 行展示优先级、父子关系、主阶段、负责人、目标分支、自动模式和更新时间。
- 点击行从右侧抽屉预览；可进入完整工单页。

### 10.3 完整工单

- 顶部只保留当前阶段、执行者、讨论/执行/验收模式、阻塞标记与关键动作。
- 顶部使用横向五节点轨道展示 `待办 → 讨论 → 执行 → 验收 → 完成`：已完成、当前和待进入节点必须有文字与形态双重区分，不能只依赖颜色。
- 轨道同时展示 `当前序号 / 5` 和当前阶段摘要；阻塞以当前节点附加标记呈现，不替换主阶段；放弃标明从哪个阶段退出。
- 窄屏不压缩或截断阶段语义，流程轨道允许横向滚动，并保持当前节点与进度摘要可读。
- 主体是一条连续对话时间线，不再拆成概览/活动/证据页签。
- 消息明确标记所属阶段；阶段变化、执行者变化、结论冻结、Git commit、验证和验收结果使用系统事件。
- 讨论结论以可版本化固定块出现在进入执行的分隔处。
- 验收事件内联展示验收标准、commits、文件变化和验证结果。
- 评论、附件、编辑、分配、阶段确认、验收结论和放弃使用右侧抽屉；抽屉打开时原页面不移动。

### 10.4 新建工单与项目设置

- 新工单默认待办，可选负责人、目标分支、优先级、`discussion_mode` 和 `execution_mode`；验收模式可配置为 human、independent_agent 或 auto。
- 分配负责人后进入讨论；未分配时保持待办。
- 项目设置为每个字段提供悬浮、键盘聚焦和触屏可访问的说明文字。

### 10.5 国际化

- 界面语言支持中文 `zh-CN` 与英文 `en`，默认中文。
- 语言按钮位于右上角头像左侧；中文界面显示 `EN`，英文界面显示 `中文`，按钮名称应向辅助技术说明将切换到的目标语言。
- 导航、状态、阶段、表单、抽屉、系统事件、占位文字、Tooltip、页面标题和无障碍标签随语言切换。
- 项目名称、工单标题、描述、普通消息、附件名称和其他用户创建内容保持原文，不执行机器翻译。
- 动态渲染的队列行、完整工单、管理页和抽屉必须继承当前语言，不得闪回默认文案或重置操作上下文。
- 页面根节点同步正确的 `lang` 属性；中英文长度变化不能造成按钮截断、固定宽度溢出或关键操作隐藏。

### 10.6 用户管理

- 用户列表展示显示名称、用户名、系统角色、项目关系数量、最后活动和账户状态；搜索覆盖名称与用户名。
- 创建用户通过右侧抽屉完成，至少录入显示名称、唯一用户名、系统角色和临时密码；默认要求首次登录修改密码。
- 账户详情从右侧抽屉展示组织级状态和全部项目成员关系，但项目角色只能在成员页修改。
- 禁用属于需要明确确认的破坏性操作，必须填写原因，并在提交前说明会话撤销、指派限制和历史保留范围。
- 禁用账户可以重新启用；启用不会恢复旧登录会话，也不会创建或修改项目成员关系。
- 管理员可以重置用户密码；临时密码仅显示/交付一次，重置后撤销该用户的现有登录会话。

### 10.7 身份活动记录

- 成员、用户管理和 Agent 详情都提供“查看活动”入口；进入活动记录页后自动按该身份筛选，并保留当前项目上下文。
- 身份筛选栏展示头像/标记、身份名称、人类或 Agent 类型、当前项目和事件数量，并提供返回全部活动的明确操作。
- 活动表按时间倒序展示时间、事件类型、活动摘要、执行者和对象；事件类型至少覆盖讨论、执行、验收、指派、租约、Git、登录、账户和成员关系。
- 点击事件从右侧抽屉展示不可变事件 ID、发生时间、执行者及身份类型、对象、项目、来源和脱敏后的结构化负载。
- 人类活动可包含工单操作、成员关系、登录和账户事件；Agent 活动可包含领取/租约、心跳、消息、执行结果、Git 证据和验证事件。
- 项目成员页进入的身份活动只展示当前项目内授权可见的事件；用户管理中的登录、密码和账户生命周期事件仅系统管理员可见。
- 搜索和事件类型筛选与身份筛选叠加；清除身份筛选不会清除用户主动输入的其他筛选条件。

### 10.8 Agent 创建、Runner 配对与 Git 集成

- 创建 Agent 抽屉只收集名称和用途说明；提交后原抽屉进入配对结果状态，显示一次性配对码、十分钟倒计时和可复制的 `projectboard-runner connect <code>` 命令。
- 配对状态支持等待、连接中、成功、过期和失败；过期或失败时可以生成新配对码，旧码立即失效。
- 配对成功后展示 Runner 设备、操作系统、版本、最近心跳和 Agent Token 已安全保存的确认，不在浏览器展示明文 Token。
- 新 Agent 仍对所有项目可见且默认未启用；Agent 详情中的“在当前项目启用”是唯一项目授权动作，不展示只读/读写选择器。
- Agent 详情展示“Git 仓库能力：随任务签发读写凭据”“长期 Git 凭据：无”，以及当前 run/lease 和最近一次短期凭据签发状态。
- 项目设置中的 Git 集成向导对 GitHub 只安装一个 App，明确说明安装权限为 Contents 读写、ProjectBoard 观察 token 运行时降为只读、Runner token 只在有效租约内签发。
- 创建、配对或启用 Agent 不重新触发 GitHub/GitLab 授权；当项目仓库未包含在现有安装中时，UI 才显示“前往提供商添加仓库”。

## 11. JSON API 与 MCP

### 11.1 Web / Runner API

- `GET /api/projects`、`POST /api/projects`
- `GET|POST /api/users`
- `GET|PATCH /api/users/:id`
- `POST /api/users/:id/disable`、`POST /api/users/:id/enable`
- `POST /api/users/:id/reset-password`
- `GET /api/activity?projectId=&actorType=&actorId=&eventType=&query=&cursor=`
- `GET|PATCH /api/me`
- `POST /api/me/change-password`
- `GET /api/me/sessions`、`DELETE /api/me/sessions/:id`
- `GET|POST /api/projects/:id/members`
- `PATCH|DELETE /api/projects/:id/members/:userId`
- `GET /api/projects/:id/agents`
- `PUT|DELETE /api/projects/:id/agents/:agentId`
- `POST /api/agents`
- `POST /api/agents/:id/pairing-codes`
- `POST /api/agent/pair`
- `GET /api/git/installations`、`POST /api/git/installations/:provider/start`
- `GET /api/git/installations/:provider/callback`
- `POST /api/projects/:id/sync-commits`、`POST /api/agent/projects/:id/sync-commits`
- `POST /api/projects/:id/repository-binding`
- `POST /api/work-items`
- `PATCH /api/work-items/:id`
- `POST /api/work-items/:id/assign`
- `POST /api/work-items/:id/unassign`
- `POST /api/work-items/:id/abandon`
- `POST /api/work-items/:id/block`
- `POST /api/work-items/:id/unblock`
- `POST /api/work-items/:id/messages`
- `POST /api/work-items/:id/attachments`
- `POST /api/work-items/:id/discussion-conclusions`
- `POST /api/work-items/:id/submit-execution`
- `POST /api/work-items/:id/acceptance-attempts`
- `POST /api/work-items/:id/decomposition-proposals`
- `POST /api/work-items/:id/children`
- `POST /api/agent/poll`
- `POST /api/agent/assignments/:id/accept`
- `POST /api/agent/runs/:id/heartbeat`
- `POST /api/agent/runs/:id/events`
- `POST /api/agent/runs/:id/git-credential`
- `DELETE /api/agent/runs/:id/git-credential`
- `POST /api/agent/runs/:id/commits`
- `POST /api/agent/runs/:id/submit`
- `POST /api/agent/runs/:id/release`

### 11.2 MCP 工具

```text
list_projects
list_work_items
get_work_item
create_work_item
update_work_item
assign_work_item
claim_next_task
claim_task
heartbeat_task
post_message
upload_attachment
freeze_discussion_conclusion
submit_execution_result
submit_acceptance_result
submit_decomposition
mark_task_blocked
clear_task_blocked
abandon_work_item
release_task
```

所有写操作必须带 `requestId`；修改现有工单还必须带 `expectedVersion`。服务端不能信任客户端传入的项目、阶段、执行者或 Git 归属。

## 12. 持久化与事务约束

- SQLite 开启 WAL、foreign_keys、busy_timeout 和定期备份。
- 工单编号对 `(project_id, number)` 唯一。
- 依赖对、项目成员、Agent 项目授权和附件哈希建立唯一或必要索引。
- Git 证据对 `(repository_id, commit_sha)` 唯一。
- 同一工单只能有一个活跃执行租约；同一 Agent 只能有一个活跃租约。
- 状态转换、更换执行者、结束旧租约、创建新尝试、写对话事件和递增版本必须在同一事务中完成。
- 事务内不执行 Git、网络、Codex 或长命令。
- 历史讨论结论、执行尝试、验收尝试和系统事件不可覆盖。

## 13. 测试计划

### 13.1 状态与权限

- 覆盖全部合法转换和非法跳转，特别是执行不能直接完成。
- 覆盖任意未完成阶段放弃、阻塞附加/解除、completed/abandoned 终态保护。
- 覆盖 developer、viewer、administrator、Agent 项目授权和跨项目 ID 拒绝。

### 13.2 自动模式

- discussion auto 仍生成结构化结论和版本事件。
- execution auto 只有在 push 与全部必需验证成功后进入验收。
- acceptance human、independent_agent、auto 都生成不可变验收尝试。
- independent_agent 排除执行 Agent。

### 13.3 并发、租约与更换执行者

- 并发分配只有一个成功。
- 更换执行者原子终止旧租约，旧提交无法覆盖新执行者。
- 心跳、过期、暂停、强制释放和进程终止行为可重复验证。

### 13.4 对话与附件

- Markdown 清洗、消息修订、阶段分隔事件排序和跨阶段连续性。
- MD、图片和其他附件鉴权、MIME、大小、哈希与安全下载头。
- 范围变化生成新讨论结论版本并返回讨论。

### 13.5 Git 与验证

- 单个 GitHub App installation 覆盖选择仓库，观察 token 降权为 read、Runner token 限定单仓库并为 write；未启用 Agent、错误项目、错误 run、过期 lease 和跨仓库请求全部拒绝。
- 配对码只能消费一次且十分钟过期；成功配对后浏览器无法读取 Agent Token，撤销 Agent Token 或停用项目授权会停止新的 Git token 签发。
- token 明文不会进入数据库、日志、remote URL、Codex 环境或审计负载；任务结束、租约失效和主动释放均撤销活动 token。
- 人类/Runner 同步授权、repository/SHA 幂等、完整工单 ID 关联、installation 仓库移除和权限降级均有测试。
- 临时远端覆盖 main、develop、release/* 基线和任务分支 fast-forward push。
- 完整工单 ID 边界匹配；错误仓库、错误分支、短 ID 和未知 commit 拒绝。
- `(repo, SHA)` 去重，同一提交不会重复进入时间线。
- commit 事件不会触发状态变化。
- build/test/lint、禁止路径、脏 worktree、无代码结果和 push 失败。

### 13.6 验收与端到端

- 讨论 → 冻结结论 → 执行 → 验收 → 通过完成。
- 验收要求修改返回执行并创建新执行/验收尝试。
- 验收范围不清返回讨论并创建新结论版本。
- 父工单拆分、子工单依赖、全部完成后父工单进入验收。
- 人类执行、Agent 执行、独立 Agent 验收和自动验收均有完整对话与审计。

### 13.7 Web UI 与国际化

- 五阶段轨道覆盖待办、讨论、执行、验收、完成、阻塞附加和从不同阶段放弃的展示状态。
- 在窄屏、200% 缩放和较长英文文案下验证流程轨道、顶部操作区和右侧抽屉不存在不可达内容。
- 默认加载中文；中英文连续切换不会改变项目、页面、队列筛选、选中工单、滚动位置或抽屉内容。
- 动态创建的消息、系统事件、管理列表和表单控件使用当前界面语言；用户创建内容保持原文。
- 校验 `html[lang]`、语言按钮的目标语言标签、键盘焦点和屏幕阅读器状态通知。
- 用户管理页覆盖创建、搜索、查看、禁用、启用和密码重置抽屉；成员页仍只改变项目成员关系。
- 覆盖禁止自我禁用、禁止禁用最后一个可用管理员、用户名唯一性、临时密码策略、会话撤销和历史保留。
- 从成员、用户管理和 Agent 详情进入活动页时，验证身份筛选、当前项目边界、事件类型、空状态、清除筛选和事件详情抽屉。
- 覆盖普通项目活动与管理员专属账户/登录事件的权限隔离，防止通过 actor ID 查询其他项目或敏感账户事件。

## 14. 交付顺序

1. 身份、项目、成员、Agent、Token、会话和项目设置。
2. WorkItem 最终状态机、连续对话、附件、阻塞和放弃。
3. 分配、租约、更换执行者、讨论结论版本和自动讨论。
4. Runner、worktree、执行结果、验证、push 和 commit 自动关联。
5. human / independent_agent / auto 验收及返工闭环。
6. 父子拆分、依赖、审计导出、备份恢复、Windows 托盘与 Linux systemd。

## 15. 固定约束

- 每个项目关联一个 Git 仓库和一套分支策略。
- Runner 主机预装并登录 Codex CLI；所有 Runner/Agent 的 Git 能力统一为读写，但只有当前项目已启用且持有有效执行租约时，才能经 Git Credential Broker 取得当前仓库的短期写 token。
- GitHub 使用一个安装级 Contents 读写 App；观察和执行通过签发时降权区分，不为每个 Agent 创建 GitHub 身份、Deploy Key 或长期 Token。
- Runner 不安装语言工具链或依赖；环境缺失时附加阻塞标记并报告原因。
- 自动化不能绕过项目授权、版本、租约、幂等、禁止路径、验证、验收和审计。
- Web UI 默认使用中文，并提供不改变工作上下文的中英文切换；系统文案可翻译，用户创建内容保持原文。
- 所有实现以本计划的完成状态为准，不保留简化版状态机或兼容旧三阶段模型。
