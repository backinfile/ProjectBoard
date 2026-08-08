# Engineering context

- Agent runtime: local `codex` CLI started by the ProjectBoard service process.
- Scheduling: built-in event-woken loop with polling fallback and per-Agent capacity.
- Task workflow: `created -> in_progress -> completed -> closed`, with an independent block flag.
- Continuity: one Codex session ID and isolated workspace per task.
- Git: short-lived GitHub App credentials scoped to Git child processes.
- Human surface: embedded web UI and session/CSRF-protected HTTPS API.
- Deliberately absent: SSH Agent transport, MCP Agent server, external Runner and remote execution.
