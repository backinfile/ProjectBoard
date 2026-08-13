# Engineering context

- Runtime: the ProjectBoard service starts the local `codex` CLI directly. There are no SSH, MCP-Agent, external-runner, or remote-execution adapters.
- Projects: each project owns one immutable absolute path to a server-local Git repository root.
- Scheduling: an event-woken loop with polling fallback applies per-Agent capacity and accept/reject tag filters.
- Standard tasks: `created -> in_progress -> completed -> closed`, with isolated worktree execution followed by a separate merge request.
- Simple-conversation tasks: `created -> in_progress -> closed`, with serialized writes in the project repository.
- Continuity: every Agent request is immutable and one-shot. Plan approval, retry, rework, merge, and knowledge maintenance create a new request and a fresh Codex session while preserving lineage and raw output.
- Knowledge: one versioned Markdown tree per project, with discovery summaries, trigger descriptions, FTS5 search, project-scoped read-only MCP retrieval, revisions, restore, optimistic writes, and node-local Agent locks.
- Access: system roles are `administrator` and `user`; project roles are `developer` and `viewer`.
- Human surface: an embedded Web UI and cookie-session HTTP API protected by CSRF and security headers. The binary binds to loopback; production HTTPS terminates at a reverse proxy.
- Git: ordinary local Git only. ProjectBoard stores no provider credentials and performs no automatic network operations.
- Persistence: SQLite WAL schema v10 plus file attachments and temporary Agent workspaces under the data directory. Schema v9 migrates transactionally to v10; other versions are rejected.
