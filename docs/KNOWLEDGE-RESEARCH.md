# Knowledge base and Agent memory research

调研时间：2026-08-12；按 2026-08-14 的 schema v11 实现重新核对。这里记录候选项目与 ProjectBoard 当前实际采用的模式；GitHub 热度会变化，因此不固化 star 数。

## 当前实现对照

- 每个项目只有一棵 SQLite Markdown 知识树，支持任意层级、摘要、触发描述、FTS5 搜索、移动、乐观版本、完整修订历史和恢复。
- 项目开发者可以写知识，查看者只读。Agent 锁只保护当前节点免受 Agent 修改，不限制人类，也不继承到子节点。
- 任务关闭时事务性创建 `task_knowledge` 需求；达到项目天数或任务知识数量阈值时创建 `project_knowledge_compaction` 需求。
- 普通任务先接收小型知识目录，并可通过绑定当前项目的只读 STDIO MCP 主动搜索和读取正文；知识 Agent 接收完整只读快照并返回结构化节点操作。
- 当前不提供知识附件、外部文件引用、实时协同编辑、双链、向量索引或知识图谱。

## 共享知识库 / Wiki（20+）

| 项目 | 主要模式 | 对 ProjectBoard 的启发 |
|---|---|---|
| [Outline](https://github.com/outline/outline) | 团队知识库、集合、权限、修订 | 项目隔离、成员权限、历史 |
| [AppFlowy](https://github.com/AppFlowy-IO/AppFlowy) | 本地优先块编辑、层级页面 | 树导航与本地数据所有权 |
| [AFFiNE](https://github.com/toeverything/AFFiNE) | 文档/白板统一、协作 | 统一内容实体，但当前版本不引入块模型 |
| [Logseq](https://github.com/logseq/logseq) | Outliner、双链、本地文件 | 树形浏览与 Markdown 内容 |
| [SiYuan](https://github.com/siyuan-note/siyuan) | 层级文档、块引用、历史 | 节点历史与深层树交互 |
| [TriliumNext Notes](https://github.com/TriliumNext/Notes) | 深层树、克隆节点、保护 | 深树交互；当前版本坚持单父节点 |
| [BookStack](https://github.com/BookStackApp/BookStack) | 书/章/页、角色权限 | 清晰层级；本项目进一步统一为一种节点 |
| [Wiki.js](https://github.com/requarks/wiki) | 团队 Wiki、Markdown、搜索 | 简洁编辑与项目内搜索 |
| [Docmost](https://github.com/docmost/docmost) | 团队 Wiki、实时协作、空间 | 项目即空间；当前版本不做实时协同编辑 |
| [DokuWiki](https://github.com/dokuwiki/dokuwiki) | 无数据库 Wiki、ACL、修订 | 修订不可丢；不照搬文件存储 |
| [MediaWiki](https://github.com/wikimedia/mediawiki) | 成熟修订、权限与讨论 | 审计与恢复优先于复杂编辑器 |
| [XWiki](https://github.com/xwiki/xwiki-platform) | 企业 Wiki、权限、扩展 | 权限边界；不引入插件平台 |
| [HedgeDoc](https://github.com/hedgedoc/hedgedoc) | 协作 Markdown | Markdown 作为最小内容格式 |
| [SilverBullet](https://github.com/silverbulletmd/silverbullet) | 可编程 Markdown 空间、双链 | 单项目空间；当前版本不做脚本扩展 |
| [Memos](https://github.com/usememos/memos) | 轻量自托管笔记 | 保持服务与数据模型小 |
| [Flatnotes](https://github.com/Dullage/flatnotes) | 极简 Markdown 笔记与搜索 | 不引入重型编辑器或索引服务 |
| [Foam](https://github.com/foambubble/foam) | Git/Markdown 知识图谱 | Agent 可读 Markdown；暂不做图谱 |
| [Dendron](https://github.com/dendronhq/dendron) | 层级命名、Markdown PKM | 路径可作为稳定的人类上下文 |
| [Athens Research](https://github.com/athensresearch/athens) | 双链与知识图谱 | 图关系有价值，但不适合当前精简版本 |
| [Notea](https://github.com/notea-org/notea) | 自托管 Markdown 知识库 | 简单页面编辑与资源附件 |
| [Raneto](https://github.com/ryanlelek/Raneto) | Markdown 静态知识库 | Markdown 渲染的低复杂度基线 |
| [Pepperminty Wiki](https://github.com/sbrl/Pepperminty-Wiki) | 单文件 Wiki | 极简部署思路，但 ProjectBoard 已有 SQLite |

## Agent / LLM memory（20+）

| 项目 | 主要模式 | 本项目取舍 |
|---|---|---|
| [Mem0](https://github.com/mem0ai/mem0) | 提取、更新、检索长期记忆 | 采用异步整理；不采用向量依赖 |
| [Zep](https://github.com/getzep/zep) | 时序知识图谱与 Agent memory | 保留来源/时间；不引入图数据库 |
| [Letta](https://github.com/letta-ai/letta) | 有状态 Agent 与分层记忆 | 把记忆独立成项目资源，不延续会话 |
| [LangMem](https://github.com/langchain-ai/langmem) | 后台记忆抽取与管理 | 任务关闭后生成整理需求 |
| [Graphiti](https://github.com/getzep/graphiti) | 时序知识图谱 | 暂缓图关系，先做树与历史 |
| [Cognee](https://github.com/topoteretes/cognee) | 数据摄取、图/向量管线 | 说明管线很强，但不符合精简约束 |
| [MemOS](https://github.com/MemTensor/MemOS) | 多 memory cube、调度与检索 | 采用项目隔离和异步调度 |
| [A-MEM](https://github.com/agiresearch/A-mem) | Agentic/Zettelkasten 记忆组织 | 允许 Agent 整理，但服务端掌握写入事务 |
| [LightMem](https://github.com/zjunlp/LightMem) | 轻量分层记忆与整合 | 采用周期性全局整理概念 |
| [MemoryBank](https://github.com/zhongwanjun/MemoryBank-SiliconFriend) | 对话长期记忆与遗忘 | 过期知识交给全局整理，而非硬编码遗忘曲线 |
| [MemGPT](https://github.com/cpacker/MemGPT) | 分层上下文/虚拟内存（Letta 前身） | 快照注入上下文，但不让任务写正式知识 |
| [memary](https://github.com/kingjulio8238/Memary) | 开源记忆层与实体关系 | 采用显式记忆资源；不采用额外图层 |
| [Memoria](https://github.com/matrixorigin/Memoria) | 快照、分支、回滚、审计 | 采用完整历史与恢复，不引入独立数据库 |
| [OpenMemory](https://github.com/mem0ai/mem0-mcp) | MCP 共享记忆接口 | 共享很重要；本项目直接使用内部模块接口 |
| [Basic Memory](https://github.com/basicmachines-co/basic-memory) | Markdown + MCP 的本地知识 | 采用 Markdown、项目边界和请求级只读 MCP，不引入外部知识服务 |
| [MCP Memory Service](https://github.com/doobidoo/mcp-memory-service) | 跨客户端持久记忆 | 采用跨 Agent 共享，不扩大到跨项目 |
| [Supermemory](https://github.com/supermemoryai/supermemory) | 文档/记忆摄取与语义检索 | 文件清单与整理有用；当前版本只做关键词 |
| [Memobase](https://github.com/memodb-io/memobase) | 用户画像与长期记忆 | 不做用户画像，只保留项目知识 |
| [HippoRAG](https://github.com/OSU-NLP-Group/HippoRAG) | 图增强长期检索 | 检索效果优先级低于简单可控的写入 |
| [LightRAG](https://github.com/HKUDS/LightRAG) | 图 + 向量 RAG | 当前明确排除，避免运维和一致性成本 |
| [RAGFlow](https://github.com/infiniflow/ragflow) | 文档解析和 RAG 管线 | 文件摄取能力过重，本项目不引入文件引用 |
| [Memora](https://github.com/microsoft/Memora) | 事实/事件/程序记忆与混合检索 | 类型化记忆可后续演进，当前统一为节点 |

## 结论：本项目为什么这样写

共同的高价值模式是：作用域隔离、可审计修订、后台整合、显式来源、可恢复、受控写入。共同的复杂度来源是：块编辑、实时协同、双链/图谱、embedding 管线、多存储后端和长期会话续接。

ProjectBoard 当前版本只保留前一组：SQLite 树节点、Markdown、摘要/触发描述、FTS5、渐进披露、项目级只读 MCP、完整历史、Agent 锁、事务化结构操作、任务关闭触发整理。知识内容直接保存在节点中，不引入附件或外部文件引用。普通任务的 MCP 调用仍进入 `agent_requests.output_jsonl` 执行证据，不维护另一套检索生命周期。核心 seam 集中在 `agentrequest`、`knowledge` 与只读 `knowledgemcp` adapter，并由 `agentexec` 调度和应用结果。
