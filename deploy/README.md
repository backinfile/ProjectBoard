# Deployment

`projectboard serve` 是唯一的 Web/API 进程，并内置静态网页、Git 授权封装与 HTTP MCP。提供商长期密钥由数据目录中的独立主密钥加密，不再启动 Broker 进程或开放 Broker 端口。

Linux Runner 可使用 `projectboard-runner.service`。Windows 请以已配对的普通用户运行 `projectboard-runner poll --watch`，也可把同一命令注册到任务计划程序；无需管理员权限。

生产环境应在 TLS 反向代理后运行服务、持久化数据目录、限制目录 ACL，并保护 `main`、`develop`、`release/*` 与标签。不要给 ProjectBoard 集成配置受保护分支绕过权限。
