# ProjectBoard MCP 设计

当前状态：已实现 37 个工具、Streamable HTTP 与 stdio，使用官方 Go SDK v1.7.0。工具通过同一 Go / SQLite 服务操作正式数据。

## 参考与取舍

[Kaneo 官方 MCP 文档](https://kaneo.app/docs/core/integrations/mcp)提供同进程 HTTP 入口和 stdio 入口，覆盖项目、任务、评论及标签。ProjectBoard 采用相同的接入思路，删除工作空间成员、负责人和多用户授权流程，并增加知识树工具。Kaneo 的工作空间标签在这里改为严格的项目标签。

## 运行与项目范围

同一个可执行程序常规启动提供 Web / API / Streamable HTTP；`projectboard mcp --project <id>` 提供 stdio 适配入口，连接正在运行的服务。HTTP 地址为 `http://127.0.0.1:7331/api/mcp`。stdio 通过环境变量 PROJECTBOARD_TOKEN 获取令牌，可使用 --url 指定服务地址。

每个接入令牌绑定一个项目。所有工具参数显式携带 project_id，必须与连接绑定项目相等；传入任务、节点、标签 ID 时还要在业务层验证归属。修改项目参数不能扩大范围。stdio 同样通过绑定项目的令牌连接服务。

管理员在项目 MCP 页创建和撤销令牌。默认监听回环地址，令牌仅显示一次并以摘要保存；浏览器会话与 MCP 令牌独立。

## 工具契约

所有列表工具提供 limit（默认 50，上限 200）与 cursor。工具返回结构化数据；知识节点包含稳定 ID、项目 ID、完整点分地址。更新使用 expected_revision；批量变更必须事务提交，任何目标越界或无效则整批失败。

| 工具组 | 拟定工具 | 关键语义 |
| --- | --- | --- |
| 项目 | get_project、update_project、list_project_statuses | 仅当前绑定项目；读取状态 ID 后修改任务 |
| 任务 | list_tasks、get_task、create_task、update_task、move_task、delete_task、restore_task | 支持状态、优先级、日期、项目标签；删除进入回收站 |
| 评论与附件 | list_task_comments、create_task_comment、update_task_comment、delete_task_comment、restore_task_comment、upload_comment_attachment、get_comment_attachment | 评论主体，支持回复与附件，严格校验项目与任务归属 |
| 知识 | list_knowledge_nodes、get_knowledge_node、get_knowledge_subtree、create_knowledge_node、update_knowledge_node、move_knowledge_node、restore_knowledge_node | 按 ID 或完整点分地址读取；二者同时给出时必须一致；改名移动不破坏引用 |
| 知识关联 | link_task_knowledge、unlink_task_knowledge、link_knowledge_nodes、unlink_knowledge_nodes | 双端必须属于当前项目 |
| 标签 | list_labels、create_label、rename_label、delete_label、attach_labels、detach_labels | 任务与节点共用项目词表；改名可显式合并；删除标签保留内容 |
| 查找 | search | type=all/task/node，tags + match=all/any，叠加关键词 |
| 批量删除 | preview_delete、commit_delete | 预览任务与完整子树集合，确认后按预览修订号提交 |

批量删除先返回影响对象、完整地址、数量与短期有效的 plan_id。客户端展示后调用 commit_delete；如果期间层级或匹配集合变化，拒绝旧计划并要求重新预览。首版 MCP 不提供永久删除、重置管理员或覆盖恢复整库工具。

标签保存在项目词表与任务、知识节点的 tags 中，名称规范化并去重。服务端统一验证项目归属。删除/合并标签同时覆盖任务与知识节点（包括回收站）；历史快照保存原始标签名称，恢复时仅在原项目重建名称。

任务字段统一规则见 [任务页面与字段](task-fields.md)。create_task 默认包含“创建”状态、标题与系统元数据；update_task 不传字段保持原值，传 null / 空数组清空。状态空值重置为“创建”；优先级等其他字段不自动填值。

## 错误与测试

错误使用 INVALID_ARGUMENT、NOT_FOUND、PROJECT_SCOPE_MISMATCH、REVISION_CONFLICT、PLAN_EXPIRED、SAVE_FAILED 等稳定代码。非法请求先验证再写入，不返回其他项目内容。工具调用日志记录目标和结果，不记录令牌。

正式接入验收必须包含协议初始化、tools/list、tools/call、HTTP 与 stdio 等价、撤销令牌、跨项目拒绝、分页、版本冲突、事务回滚、知识改名后 ID 引用、子树删除计划过期，以及 Web 修改后 MCP 立即可见、MCP 修改后 Web 可见。

## 页面调用与字段示例

项目 MCP 页提供六个常用调用的参数示例，运行后操作真实服务端数据。外部 MCP 客户端可使用 tools/list 获取全部 37 个工具的 schema。

读取结果包含 revision；列表返回 items、next_cursor 和 revision。写工具必填 expected_revision，get_knowledge_node 使用 id / address 定位。任务更新使用 fields 对象，例如：

```json
{"project_id":"项目ID","id":"任务ID","expected_revision":12,"fields":{"priority":null,"progress":0,"description":"任务详细"}}
```

评论工具使用 task_id、comment_id、text；创建评论可传 reply_to 和 attachments（已上传附件 ID 数组）。upload_comment_attachment 使用 task_id、filename、mime、data（Base64），返回附件元数据；get_comment_attachment 返回元数据与 Base64 字节。

知识创建使用 name、content、parent、tags；更新使用 id 和 fields，移动/恢复使用 id、parent（根节点为 null）。关联使用 task_id / node_id 或 node_id / other_id。

标签工具使用 name、new_name、merge；批量标记使用 target_type=task/node、target_ids、tags。preview_delete 使用 task_ids、node_ids，commit_delete 使用 plan_id 与 expected_revision。计划有效期 5 分钟，工作空间修订改变后重新预览。

HTTP 与 stdio 初始化、读写等价、分页、令牌撤销和项目隔离已在集成测试中验证。
