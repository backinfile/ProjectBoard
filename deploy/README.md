# 部署

`projectboard.service` 用于以普通系统用户运行 ProjectBoard。只需发布 HTTPS/Web 端口并持久化 `PROJECTBOARD_DATA_DIR`，不需要开放额外的 SSH 端口。

运行服务的系统账户必须能从 `PATH` 调用 `git` 和 `codex`，并已完成 Codex 登录。请评估 Agent 并发数、单轮超时以及 `workspaces/` 中保留的 clone 对 CPU、内存和磁盘的影响。
