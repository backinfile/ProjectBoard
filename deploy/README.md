# Deployment

`projectboard serve` 是唯一的 Web/API 进程，并内置静态网页、Git 授权封装与 HTTP MCP。提供商长期密钥由数据目录中的独立主密钥加密，不再启动 Broker 进程或开放 Broker 端口。

Linux Runner 可使用 `projectboard-runner.service`。Windows 普通用户直接运行 `projectboard-runner.exe`，再从托盘“登录”中填写 ProjectBoard 主页地址和一次性 Agent Key；该 GUI 可执行文件会保存设备凭据、自动开始轮询并常驻系统托盘，不需要终端或管理员权限。仓库注册和诊断仍可使用 `projectboard-runner-cli.exe`。

生产环境应在 TLS 反向代理后运行服务、持久化数据目录、限制目录 ACL，并保护 `main`、`develop`、`release/*` 与标签。不要给 ProjectBoard 集成配置受保护分支绕过权限。
