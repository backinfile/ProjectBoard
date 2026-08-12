# Engineering context

- Agent runtime: local `codex` CLI started by the ProjectBoard service process.
- Projects: immutable absolute paths to server-local Git repository roots.
- Scheduling: event-woken loop with polling fallback, per-Agent capacity, and accept/reject tag filters.
- Standard workflow: `created -> in_progress -> completed -> closed`, isolated worktree, then a separate merge invocation.
- Simple conversation workflow: `created -> in_progress -> closed`, direct main-repository operation.
- Continuity: one Codex session ID per task across plan, work, review continuation, and merge.
- Git: ordinary local Git only; no stored authentication and no automatic network operations.
- Human surface: embedded web UI and session/CSRF-protected HTTPS API.
- Compatibility: schema v8 rejects all older databases; no migration or legacy API compatibility.
