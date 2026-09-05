# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

一个人在本机管理多个项目，以浏览器使用本地安装的应用。

## Product Purpose

轻量部署的任务管理与项目知识库。唯一管理员，移除团队与用户权限管理。

## Capabilities and Constraints

知识库是内部管理的树状节点，每个节点可同时具有内容与子节点。点分地址在项目内定位节点；内部引用使用稳定 ID。任务与知识节点共用严格限定在项目内的标签，支持按标签查找、批量管理和删除恢复；不同项目同名标签互不影响。需要参考 Kaneo 提供项目 MCP，覆盖任务、知识树和项目标签。

任务详细是固定字段，显示在评论流上方，支持文字和图片/文件附件；下方评论/对话流同样支持附件上传；任务面板提供看板、列表和时间线。

任务必须可在独立页面打开；参考 Vikunja 统一字段，新任务只填标题，状态默认为“创建”，去掉“待处理”；其他业务字段默认未设置，按需添加和清空。已有任务保留已填值。

当前交付为内嵌前端的 Go 单文件应用，SQLite 和附件目录保存正式数据。当前预览已冻结，正式界面保持已确认的交互；已实现管理员认证、备份恢复、真实 HTTP / stdio MCP，以及图片、普通文本和 Markdown 附件预览。

## Brand Commitments

用户明确要求以 Kaneo 的实际应用界面作为整体框架参考。界面使用 Lucide 图标。

## Evidence on Hand

docs/product-design.md、preview/index.html、preview/browser-test-data.json。Kaneo 官方首页包含应用交互演示，已通过外部浏览器查看。
