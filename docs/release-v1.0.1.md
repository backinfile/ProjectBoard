# ProjectBoard v1.0.1

本地项目任务与树状知识管理应用，面向单管理员使用。Windows x64 发布包内含程序、操作说明、设计与测试文档及第三方许可证。

## 功能

- 单个 Go 程序内嵌 Web 界面，SQLite 与附件保存在本地数据目录。
- 项目管理、看板/列表/月度时间线、独立任务页面、可选任务字段。
- 任务详细、评论与回复、检查清单、任务和知识关联。
- 图片缩放预览、普通文本阅读、Markdown 排版与源码切换。
- 内部知识树、点分地址、稳定引用、版本历史和项目独立标签。
- 提醒、重复任务、搜索、回收站、备份恢复和内容导出。
- 项目令牌与 37 个 MCP 工具，支持 Streamable HTTP 和 stdio。

## 本次更新

- 更新 README，补充下载安装、字段规则、知识树与标签、附件预览、升级和开发测试指南。
- 修复极小图片缩略图点击区域被文件名按钮遮挡的问题。
- 增加完整功能测试目录、独立部署黑盒脚本、白盒边界测试、真实浏览器测试和一键报告生成。
- 版本检查与 package.json 对齐，程序及发布包版本统一为 1.0.1。

## 安装和升级

下载 `ProjectBoard-1.0.1-windows-amd64.zip`，解压后运行 `projectboard.exe`。首次运行在浏览器中设置管理员密码。默认地址为 `http://127.0.0.1:7331`。

已有用户：导出 ZIP 备份 → 停止服务 → 替换程序 → 重新启动。继续使用原数据目录；默认 Windows 数据目录为 `%LOCALAPPDATA%\ProjectBoard`。

附件 `.zip.sha256` 提供安装包的 SHA-256 校验值。

## 验证

发布前执行本地完整回归：50 项 HTTP/MCP 黑盒、27 项 Edge 浏览器黑盒、80 项白盒子测试、14 项集成测试、7 项正式前端检查、41 项冻结预览回归、7 项独立运行检查。发布安装包另行执行独立运行检查。

10 项需要人工或专项环境的检查列在测试目录中，包括真实桌面通知、多机器 HTTPS 和磁盘故障场景。

[使用说明](https://github.com/backinfile/ProjectBoard/blob/v1.0.1/README.md) · [测试目录](https://github.com/backinfile/ProjectBoard/blob/v1.0.1/docs/test-catalog.md) · [测试报告](https://github.com/backinfile/ProjectBoard/blob/v1.0.1/docs/local-test-report.md)
