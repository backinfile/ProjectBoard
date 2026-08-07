# 部署

`projectboard.service` 用于以普通系统用户运行 ProjectBoard。部署时除 HTTPS 端口外，还必须开放独立的 Agent SSH 端口（默认 TCP 2222），并持久化 `PROJECTBOARD_DATA_DIR`。

请配置 `PROJECTBOARD_SSH_PUBLIC_HOST`、`PROJECTBOARD_SSH_PUBLIC_PORT` 和 `PROJECTBOARD_SSH_HOST_KEY_PATH`，备份 host key，并在防火墙和反向代理之外直接转发 SSH TCP 流量。SSH 启动失败会使整个服务启动失败。

ProjectBoard 不再附带 Runner、托盘程序或 Runner systemd unit。
