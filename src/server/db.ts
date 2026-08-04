import { DatabaseSync } from 'node:sqlite';
import { mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { randomUUID } from 'node:crypto';

const schema = `
PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE, display_name TEXT NOT NULL, password_hash TEXT NOT NULL, system_role TEXT NOT NULL CHECK(system_role IN('administrator','user')), status TEXT NOT NULL DEFAULT 'active' CHECK(status IN('active','disabled')), must_change_password INTEGER NOT NULL DEFAULT 1, disabled_reason TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_active_at TEXT);
CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), token_hash TEXT NOT NULL UNIQUE, csrf_hash TEXT NOT NULL, user_agent TEXT, ip TEXT, expires_at TEXT NOT NULL, revoked_at TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS login_attempts(id TEXT PRIMARY KEY, username TEXT NOT NULL, ip TEXT NOT NULL, succeeded INTEGER NOT NULL, created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS login_attempt_window ON login_attempts(username,ip,created_at);
CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY, project_key TEXT NOT NULL UNIQUE COLLATE NOCASE, name TEXT NOT NULL, description_markdown TEXT NOT NULL DEFAULT '', archived_at TEXT, repository_url TEXT NOT NULL, remote_name TEXT NOT NULL DEFAULT 'origin', default_target_branch TEXT NOT NULL DEFAULT 'main', allowed_target_branches_json TEXT NOT NULL DEFAULT '["main"]', validation_commands_json TEXT NOT NULL DEFAULT '[]', forbidden_paths_json TEXT NOT NULL DEFAULT '[]', agent_rules_markdown TEXT NOT NULL DEFAULT '', discussion_mode TEXT NOT NULL DEFAULT 'manual', execution_mode TEXT NOT NULL DEFAULT 'manual', acceptance_mode TEXT NOT NULL DEFAULT 'human', config_version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS project_memberships(project_id TEXT NOT NULL REFERENCES projects(id), user_id TEXT NOT NULL REFERENCES users(id), role TEXT NOT NULL CHECK(role IN('developer','viewer')), created_at TEXT NOT NULL, PRIMARY KEY(project_id,user_id));
CREATE TABLE IF NOT EXISTS agents(id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE, purpose TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, revoked_at TEXT);
CREATE TABLE IF NOT EXISTS agent_tokens(id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id), token_hash TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, revoked_at TEXT);
CREATE TABLE IF NOT EXISTS agent_project_grants(agent_id TEXT NOT NULL REFERENCES agents(id), project_id TEXT NOT NULL REFERENCES projects(id), created_at TEXT NOT NULL, PRIMARY KEY(agent_id,project_id));
CREATE TABLE IF NOT EXISTS runner_pairing_codes(id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id), code_hash TEXT NOT NULL UNIQUE, expires_at TEXT NOT NULL, consumed_at TEXT, invalidated_at TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS runner_devices(id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id), device_name TEXT NOT NULL, os TEXT NOT NULL, version TEXT NOT NULL, public_key_digest TEXT NOT NULL, paired_at TEXT NOT NULL, last_heartbeat_at TEXT, status TEXT NOT NULL DEFAULT 'idle');
CREATE TABLE IF NOT EXISTS git_installations(id TEXT PRIMARY KEY, provider TEXT NOT NULL, external_installation_id TEXT NOT NULL, application_id TEXT, repositories_json TEXT NOT NULL, webhook_status TEXT NOT NULL, permissions_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(provider,external_installation_id));
CREATE TABLE IF NOT EXISTS repository_bindings(id TEXT PRIMARY KEY, project_id TEXT NOT NULL UNIQUE REFERENCES projects(id), installation_id TEXT NOT NULL REFERENCES git_installations(id), provider TEXT NOT NULL, external_repository_id TEXT NOT NULL, clone_url TEXT NOT NULL, default_branch TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(provider,external_repository_id));
CREATE TABLE IF NOT EXISTS work_items(id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), number INTEGER NOT NULL, parent_id TEXT REFERENCES work_items(id), title TEXT NOT NULL, description_markdown TEXT NOT NULL, acceptance_criteria_markdown TEXT NOT NULL, priority TEXT NOT NULL, stage TEXT NOT NULL, discussion_mode TEXT NOT NULL, execution_mode TEXT NOT NULL, acceptance_mode TEXT NOT NULL, target_branch TEXT NOT NULL, assignee_kind TEXT, assignee_id TEXT, assignment_state TEXT, blocked_at TEXT, blocked_reason TEXT, abandoned_at TEXT, abandoned_reason TEXT, abandoned_from_stage TEXT, version INTEGER NOT NULL DEFAULT 1, lease_generation INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, completed_at TEXT, UNIQUE(project_id,number));
CREATE TABLE IF NOT EXISTS work_item_dependencies(work_item_id TEXT NOT NULL REFERENCES work_items(id), depends_on_id TEXT NOT NULL REFERENCES work_items(id), created_at TEXT NOT NULL, PRIMARY KEY(work_item_id,depends_on_id), CHECK(work_item_id<>depends_on_id));
CREATE TABLE IF NOT EXISTS assignments(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), assignee_kind TEXT NOT NULL, assignee_id TEXT NOT NULL, state TEXT NOT NULL, generation INTEGER NOT NULL, created_at TEXT NOT NULL, ended_at TEXT, ended_reason TEXT);
CREATE TABLE IF NOT EXISTS leases(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), assignment_id TEXT NOT NULL REFERENCES assignments(id), agent_id TEXT NOT NULL REFERENCES agents(id), generation INTEGER NOT NULL, expires_at TEXT NOT NULL, released_at TEXT, release_reason TEXT, created_at TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS one_active_lease_per_item ON leases(work_item_id) WHERE released_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS one_active_lease_per_agent ON leases(agent_id) WHERE released_at IS NULL;
CREATE TABLE IF NOT EXISTS runner_runs(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), agent_id TEXT NOT NULL REFERENCES agents(id), lease_id TEXT NOT NULL REFERENCES leases(id), state TEXT NOT NULL, created_at TEXT NOT NULL, ended_at TEXT, end_reason TEXT);
CREATE UNIQUE INDEX IF NOT EXISTS one_active_run_per_lease ON runner_runs(lease_id) WHERE ended_at IS NULL;
CREATE TABLE IF NOT EXISTS conversation_entries(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), kind TEXT NOT NULL, stage TEXT NOT NULL, author_type TEXT NOT NULL, author_id TEXT, payload_json TEXT NOT NULL, related_version INTEGER, created_at TEXT NOT NULL, edited_at TEXT, deleted_at TEXT);
CREATE TABLE IF NOT EXISTS message_revisions(id TEXT PRIMARY KEY, entry_id TEXT NOT NULL REFERENCES conversation_entries(id), payload_json TEXT NOT NULL, created_at TEXT NOT NULL, editor_id TEXT);
CREATE TABLE IF NOT EXISTS attachments(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), entry_id TEXT REFERENCES conversation_entries(id), original_name TEXT NOT NULL, mime TEXT NOT NULL, size INTEGER NOT NULL, sha256 TEXT NOT NULL, storage_key TEXT NOT NULL, uploader_type TEXT NOT NULL, uploader_id TEXT, created_at TEXT NOT NULL, UNIQUE(work_item_id,sha256));
CREATE TABLE IF NOT EXISTS discussion_conclusions(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), version INTEGER NOT NULL, goal_markdown TEXT NOT NULL, scope_markdown TEXT NOT NULL, out_of_scope_markdown TEXT NOT NULL, implementation_plan_markdown TEXT NOT NULL, acceptance_criteria_markdown TEXT NOT NULL, risks_markdown TEXT NOT NULL, created_by_type TEXT NOT NULL, created_by_id TEXT, created_at TEXT NOT NULL, supersedes_version INTEGER, UNIQUE(work_item_id,version));
CREATE TABLE IF NOT EXISTS execution_attempts(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), number INTEGER NOT NULL, assignee_kind TEXT, assignee_id TEXT, conclusion_version INTEGER NOT NULL, target_branch TEXT NOT NULL, baseline_commit TEXT, task_branch TEXT NOT NULL, lease_generation INTEGER NOT NULL, summary_markdown TEXT, commits_json TEXT, changed_files_json TEXT, remaining_risks_markdown TEXT, pushed_at TEXT, started_at TEXT NOT NULL, ended_at TEXT, end_reason TEXT, UNIQUE(work_item_id,number));
CREATE TABLE IF NOT EXISTS validation_runs(id TEXT PRIMARY KEY, execution_attempt_id TEXT NOT NULL REFERENCES execution_attempts(id), name TEXT NOT NULL, command TEXT, required INTEGER NOT NULL, exit_code INTEGER NOT NULL, duration_ms INTEGER NOT NULL, log_summary TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS git_commit_evidence(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), repository_id TEXT NOT NULL, commit_sha TEXT NOT NULL, message TEXT NOT NULL, author TEXT, branch TEXT NOT NULL, files_json TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(repository_id,commit_sha));
CREATE TABLE IF NOT EXISTS acceptance_attempts(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), number INTEGER NOT NULL, execution_attempt_id TEXT REFERENCES execution_attempts(id), acceptor_type TEXT NOT NULL, acceptor_id TEXT, conclusion_version INTEGER NOT NULL, outcome TEXT, note_markdown TEXT, criteria_results_json TEXT, started_at TEXT NOT NULL, completed_at TEXT, UNIQUE(work_item_id,number));
CREATE TABLE IF NOT EXISTS decomposition_proposals(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), items_json TEXT NOT NULL, dependencies_json TEXT NOT NULL, status TEXT NOT NULL, created_by_type TEXT NOT NULL, created_by_id TEXT, created_at TEXT NOT NULL, materialized_at TEXT);
CREATE TABLE IF NOT EXISTS activity_events(id TEXT PRIMARY KEY, project_id TEXT REFERENCES projects(id), actor_type TEXT NOT NULL, actor_id TEXT, event_type TEXT NOT NULL, object_type TEXT NOT NULL, object_id TEXT NOT NULL, source TEXT NOT NULL, payload_json TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS activity_project_time ON activity_events(project_id,created_at DESC);
CREATE INDEX IF NOT EXISTS activity_actor_time ON activity_events(actor_type,actor_id,created_at DESC);
CREATE TABLE IF NOT EXISTS idempotency_records(scope TEXT NOT NULL, request_id TEXT NOT NULL, response_json TEXT NOT NULL, status_code INTEGER NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(scope,request_id));
CREATE TABLE IF NOT EXISTS git_credential_issuances(id TEXT PRIMARY KEY, provider TEXT NOT NULL, installation_id TEXT NOT NULL, repository_id TEXT NOT NULL, agent_id TEXT, run_id TEXT, lease_id TEXT, purpose TEXT NOT NULL, permissions_json TEXT NOT NULL, issued_at TEXT, expires_at TEXT, revoked_at TEXT, result TEXT NOT NULL, error_code TEXT);
CREATE TABLE IF NOT EXISTS webhook_deliveries(provider TEXT NOT NULL, delivery_id TEXT NOT NULL, received_at TEXT NOT NULL, result TEXT NOT NULL, PRIMARY KEY(provider,delivery_id));
CREATE TABLE IF NOT EXISTS runner_repository_mappings(id TEXT PRIMARY KEY, device_id TEXT NOT NULL REFERENCES runner_devices(id), project_id TEXT NOT NULL REFERENCES projects(id), local_path TEXT NOT NULL, config_digest TEXT NOT NULL, confirmed_at TEXT NOT NULL, paused_at TEXT, UNIQUE(device_id,project_id));
CREATE TABLE IF NOT EXISTS runner_status(agent_id TEXT PRIMARY KEY REFERENCES agents(id), device_id TEXT REFERENCES runner_devices(id), status TEXT NOT NULL, current_run_id TEXT REFERENCES runner_runs(id), paused INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL);
`;

export class Db {
  readonly raw: DatabaseSync;
  private transactionDepth = 0;
  constructor(path = process.env.PROJECTBOARD_DB ?? './data/projectboard.db') {
    const absolute = resolve(path); mkdirSync(dirname(absolute), { recursive: true });
    this.raw = new DatabaseSync(absolute); this.raw.exec(schema);
  }
  now() { return new Date().toISOString(); }
  id() { return randomUUID(); }
  transaction<T>(fn: () => T): T {
    const nested = this.transactionDepth > 0;
    const savepoint = `pb_nested_${this.transactionDepth}`;
    this.raw.exec(nested ? `SAVEPOINT ${savepoint}` : 'BEGIN IMMEDIATE'); this.transactionDepth++;
    try { const out = fn(); this.transactionDepth--; this.raw.exec(nested ? `RELEASE ${savepoint}` : 'COMMIT'); return out; }
    catch (error) { this.transactionDepth--; this.raw.exec(nested ? `ROLLBACK TO ${savepoint}; RELEASE ${savepoint}` : 'ROLLBACK'); throw error; }
  }
  get<T>(sql: string, ...params: any[]): T | undefined { return this.raw.prepare(sql).get(...params) as T | undefined; }
  all<T>(sql: string, ...params: any[]): T[] { return this.raw.prepare(sql).all(...params) as T[]; }
  run(sql: string, ...params: any[]) { return this.raw.prepare(sql).run(...params); }
  close() { this.raw.close(); }
}
