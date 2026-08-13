# Current implementation status and roadmap

## Implemented

- Local `codex` CLI execution with per-Agent model, reasoning effort, capacity, timeout and tag-based routing.
- Standard four-stage tasks, simple three-stage conversations, isolated worktrees and serialized main-repository writes.
- Accounts, sessions, system/project roles, project memberships, task followers, notifications and append-only activity records.
- Task discussions, Markdown attachments, parent/child relationships, dependencies, blocking and Agent pause/continue controls.
- One-shot Agent requests for planning, execution, merge and knowledge maintenance, including cancellation, retry, plan approval and raw output retention.
- Versioned project knowledge trees with search, move, lock, restore and transactional Agent operations.
- Per-request Token accounting with per-Agent aggregate statistics in the API and Web UI.
- Embedded responsive Web UI, SQLite WAL persistence, health checks, database backup/restore and schema v10 compatibility enforcement.

## Current operational constraints

- A usable Agent run depends on the service account's authenticated local Codex environment and any tools required by the target repository.
- Standard worktrees live beside the project repository; request-scoped temporary workspaces live under the ProjectBoard data directory.
- Database backup does not copy uploaded attachments. Operations must back up file data separately.
- The service listens only on loopback; remote production access requires an HTTPS reverse proxy.

## Deliberately out of scope

- Other Agent runtimes, SSH/MCP runners, remote execution and user-selectable CLI path, profile or arbitrary arguments.
- Git provider authorization, webhook ingestion and automatic `fetch`, `pull`, `push` or background commit synchronization.
- Priority-aware Agent scheduling, automatic conflict resolution, vector search, knowledge graphs and real-time collaborative editing.
- Automatic migration from pre-v9 databases or compatibility with removed APIs and execution models. The only supported migration is v9 to v10.

Future work should be added only after the user-facing contract in `PRODUCT.md` and the invariants in `docs/ARCHITECTURE.md` are updated together.
