# Future Runner

Runner is intentionally absent from the current release. Codex connects directly to ProjectBoard's SSH MCP service and owns task polling, workspace setup, environment preparation and execution.

A future Runner may return as a separately downloadable daemon. It must reuse the same SSH/MCP protocol, public-key identity, Agent Execution module, lease rules and execution-scoped Git credential broker. Its responsibilities may include maintaining the continuous poll loop, preparing isolated workspaces, recording environment steps and invoking the locally installed Codex CLI.

It must not introduce a second Bearer Agent API, pairing protocol, hidden local stdio bridge, duplicated execution domain logic or a long-lived Git credential. No placeholder implementation is retained in this release.
