# ProjectBoard

ProjectBoard is a production-oriented work-item queue for human and Agent collaboration. The implementation follows [PLAN.md](./PLAN.md) and the information architecture in [projectboard-ui-prototype.html](./projectboard-ui-prototype.html); neither source file is generated or replaced by the application.

## Included

- Final six-value work-item stage model (`todo → discussion → execution → acceptance → completed`, plus terminal `abandoned`) with additive blockers, optimistic versions, idempotency, immutable discussion/execution/acceptance attempts, continuous conversation, attachments, dependencies and two-level decomposition.
- Organization users, Argon2id passwords, server-side sessions, CSRF/Origin checks, administrator lifecycle controls, per-project developer/viewer membership, separate Agent identities, one-time 10-minute Runner pairing and hashed Agent tokens.
- SQLite WAL persistence with foreign keys, busy timeout, partial unique indexes for one live lease per work item and Agent, append-only audit events, NDJSON export, online-safe `VACUUM INTO` backup and offline restore tooling.
- GitHub App and GitLab project-token adapters behind a separately started Credential Broker. GitHub installation tokens are reduced to one repository and `contents:read` for observers or `contents:write` for a valid leased Runner. GitLab creates per-project read/write repository tokens and revokes them. Plaintext provider tokens are held only in Broker memory and never persisted.
- Signed/idempotent provider webhooks, observer re-fetch/reconciliation, exact work-item-ID commit matching, repository/branch ownership validation and `(repository, SHA)` deduplication.
- Runner pairing, trusted local repository/config digest registration, isolated worktrees, sanitized Codex environment, controlled credential helper, exact task branches, protected-path checks, configured validation commands, fast-forward-only push and structured result submission. Linux systemd and Windows tray launchers are in `deploy/` and `src/runner/`.
- JSON Web/Runner API and all MCP tools listed in the plan.
- Responsive Chinese/English React UI with project queue, members, Agents, settings, users, projects, activity, account and continuous work-item pages. The five-stage track remains horizontally readable on narrow screens. Drawers are fixed overlays and do not resize or scroll the underlying page.

## Local setup

Requires Node.js 24+ and pnpm 11+.

```bash
pnpm install
copy .env.example .env
# Set a strong PROJECTBOARD_BOOTSTRAP_PASSWORD in the environment.
pnpm build
pnpm start
```

Run the Credential Broker under a separate OS identity/environment:

```bash
pnpm broker
```

The Web/API listens on `http://localhost:3333`; the Broker listens only on `127.0.0.1:3334`. For development, `pnpm dev` starts Vite and the API. The first start creates the configured bootstrap administrator and requires a password change.

## Runner

After an administrator creates an Agent in the UI, run the one-time command shown only in the pairing drawer:

```bash
projectboard-runner connect PB-XXXX-XXXX
projectboard-runner register <project-id> /trusted/local/repository
projectboard-runner poll --watch
projectboard-runner execute PB-123
projectboard-runner submit PB-123 <run-id> <lease-id>
```

Runner expects Codex CLI to be preinstalled and logged in. It deliberately does not install project toolchains. Missing commands fail validation and should be reported as a blocker.

For MCP, set `PROJECTBOARD_URL` and `PROJECTBOARD_AGENT_TOKEN`, then start `projectboard-mcp` over stdio.

## Verification

```bash
pnpm typecheck
pnpm lint
pnpm test
pnpm test:e2e
pnpm build
pnpm security:check
```

Back up and restore:

```bash
pnpm backup -- ./data/backups/projectboard.db
# Stop Web/API and Broker first:
pnpm restore -- ./data/backups/projectboard.db
```

## Deployment-provided configuration

The code is complete without embedding deployment secrets. Operators must provide:

- a strong bootstrap password, Broker shared secret and HTTPS/public base URL;
- one GitHub App ID/private-key file/webhook secret, configured with selected repositories and installation-level `Contents: read & write`; or GitLab OAuth application metadata plus a Broker-only Maintainer/Owner token file;
- provider Rulesets/Protected Branches for `main`, `develop`, `release/*` and tags, with the ProjectBoard integration excluded from bypass;
- TLS termination, persistent `data/` storage, filesystem ACLs for Runner state/Broker keys, and scheduled backup retention.

Provider authorization cannot be fabricated locally: an organization administrator must install the GitHub App/select repositories or authorize GitLab once. Creating, pairing or enabling additional Agents never repeats that authorization.
