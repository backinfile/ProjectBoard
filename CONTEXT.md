# Engineering context

- Agent authentication: registered, non-revoked OpenSSH Ed25519 public keys.
- Transport: built-in SSH server; MCP runs as stdio inside `projectboard-mcp`.
- Execution: `internal/agentexec`, one active lease per Agent.
- Git: GitHub App installation tokens scoped to the execution's single repository.
- Human surface: embedded web UI and session/CSRF-protected HTTPS API.
- Removed surfaces: Agent Bearer tokens, pairing/devices, HTTP MCP, local MCP forwarding, Runner and GitLab.
