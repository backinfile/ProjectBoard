package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }

func Open(file string) (*Store, error) {
	info, statErr := os.Stat(file)
	existing := statErr == nil && info.Size() > 0
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", file)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if existing {
		var version int
		if err = db.QueryRow("SELECT version FROM schema_metadata WHERE id=1").Scan(&version); err != nil {
			db.Close()
			return nil, fmt.Errorf("read database schema version: %w", err)
		}
		if version > SchemaVersion {
			db.Close()
			return nil, fmt.Errorf("unsupported database schema version %d", version)
		}
		if err = migrate(db, version); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate database schema: %w", err)
		}
	}
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	return &Store{DB: db}, nil
}

const SchemaVersion = 4

func migrate(db *sql.DB, version int) error {
	if version < 2 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		statements := []string{
			"ALTER TABLE work_items ADD COLUMN created_by_user_id TEXT REFERENCES users(id)",
			"CREATE TABLE work_item_followers(work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,user_id TEXT NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,PRIMARY KEY(work_item_id,user_id))",
			"CREATE INDEX work_item_followers_user ON work_item_followers(user_id,work_item_id)",
			"CREATE TABLE notifications(id TEXT PRIMARY KEY,user_id TEXT NOT NULL REFERENCES users(id),project_id TEXT REFERENCES projects(id),work_item_id TEXT REFERENCES work_items(id),kind TEXT NOT NULL,title TEXT NOT NULL,body TEXT NOT NULL,actor_type TEXT,actor_id TEXT,read_at TEXT,created_at TEXT NOT NULL)",
			"CREATE INDEX notifications_user_time ON notifications(user_id,created_at DESC)",
			"UPDATE schema_metadata SET version=2 WHERE id=1",
		}
		for _, statement := range statements {
			if _, err = tx.Exec(statement); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		version = 2
	}
	if version < 3 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		statements := []string{
			"CREATE TABLE agent_key_settings(agent_id TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,expiry_policy TEXT NOT NULL DEFAULT 'permanent',updated_at TEXT NOT NULL)",
			"CREATE TABLE project_commit_sync_state(project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,last_synced_at TEXT NOT NULL,updated_by_type TEXT NOT NULL,updated_by_id TEXT)",
			"UPDATE schema_metadata SET version=3 WHERE id=1",
		}
		for _, statement := range statements {
			if _, err = tx.Exec(statement); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		version = 3
	}
	if version < 4 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		statements := []string{
			"ALTER TABLE projects ADD COLUMN allow_agent_execution INTEGER NOT NULL DEFAULT 1",
			"ALTER TABLE projects ADD COLUMN allow_agent_auto_close INTEGER NOT NULL DEFAULT 0",
			"ALTER TABLE projects ADD COLUMN allow_subtasks INTEGER NOT NULL DEFAULT 1",
			"ALTER TABLE projects ADD COLUMN allow_agent_auto_close_subtasks INTEGER NOT NULL DEFAULT 0",
			"ALTER TABLE projects ADD COLUMN agent_prompts_json TEXT NOT NULL DEFAULT '[]'",
			"UPDATE schema_metadata SET version=4 WHERE id=1",
		}
		for _, statement := range statements {
			if _, err = tx.Exec(statement); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		return tx.Commit()
	}
	return nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

const schema = `
CREATE TABLE IF NOT EXISTS schema_metadata(id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL, created_at TEXT NOT NULL);
INSERT OR IGNORE INTO schema_metadata(id,version,created_at) VALUES(1,4,CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE, display_name TEXT NOT NULL, password_hash TEXT NOT NULL, system_role TEXT NOT NULL CHECK(system_role IN('administrator','user')), status TEXT NOT NULL DEFAULT 'active', must_change_password INTEGER NOT NULL DEFAULT 0, disabled_reason TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_active_at TEXT);
CREATE TABLE IF NOT EXISTS system_settings(key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_by TEXT NOT NULL REFERENCES users(id), updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), token_hash TEXT NOT NULL UNIQUE, csrf_hash TEXT NOT NULL, user_agent TEXT, ip TEXT, expires_at TEXT NOT NULL, revoked_at TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS login_attempts(id TEXT PRIMARY KEY, username TEXT NOT NULL, ip TEXT NOT NULL, succeeded INTEGER NOT NULL, created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS login_attempt_window ON login_attempts(username,ip,created_at);
CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY, project_key TEXT NOT NULL UNIQUE COLLATE NOCASE, name TEXT NOT NULL, description_markdown TEXT NOT NULL DEFAULT '', archived_at TEXT, repository_url TEXT NOT NULL, remote_name TEXT NOT NULL DEFAULT 'origin', default_target_branch TEXT NOT NULL DEFAULT 'main', allowed_target_branches_json TEXT NOT NULL DEFAULT '["main"]', validation_commands_json TEXT NOT NULL DEFAULT '[]', forbidden_paths_json TEXT NOT NULL DEFAULT '[]', agent_rules_markdown TEXT NOT NULL DEFAULT '', discussion_mode TEXT NOT NULL DEFAULT 'manual', execution_mode TEXT NOT NULL DEFAULT 'manual', acceptance_mode TEXT NOT NULL DEFAULT 'human', allow_agent_execution INTEGER NOT NULL DEFAULT 1, allow_agent_auto_close INTEGER NOT NULL DEFAULT 0, allow_subtasks INTEGER NOT NULL DEFAULT 1, allow_agent_auto_close_subtasks INTEGER NOT NULL DEFAULT 0, agent_prompts_json TEXT NOT NULL DEFAULT '[]', config_version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS project_memberships(project_id TEXT NOT NULL REFERENCES projects(id), user_id TEXT NOT NULL REFERENCES users(id), role TEXT NOT NULL CHECK(role IN('developer','viewer')), created_at TEXT NOT NULL, PRIMARY KEY(project_id,user_id));
CREATE TABLE IF NOT EXISTS agents(id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE, purpose TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, revoked_at TEXT);
CREATE TABLE IF NOT EXISTS agent_tokens(id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id), token_hash TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, revoked_at TEXT);
CREATE TABLE IF NOT EXISTS agent_project_grants(agent_id TEXT NOT NULL REFERENCES agents(id), project_id TEXT NOT NULL REFERENCES projects(id), created_at TEXT NOT NULL, PRIMARY KEY(agent_id,project_id));
CREATE TABLE IF NOT EXISTS runner_pairing_codes(id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id), code_hash TEXT NOT NULL UNIQUE, expires_at TEXT NOT NULL, consumed_at TEXT, invalidated_at TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS agent_key_settings(agent_id TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,expiry_policy TEXT NOT NULL DEFAULT 'permanent',updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS runner_devices(id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id), device_name TEXT NOT NULL, os TEXT NOT NULL, version TEXT NOT NULL, public_key_digest TEXT NOT NULL, paired_at TEXT NOT NULL, last_heartbeat_at TEXT, status TEXT NOT NULL DEFAULT 'idle');
CREATE TABLE IF NOT EXISTS provider_authorizations(id TEXT PRIMARY KEY, provider TEXT NOT NULL, name TEXT NOT NULL, base_url TEXT NOT NULL, app_id TEXT, installation_id TEXT, encrypted_secret TEXT NOT NULL, webhook_secret TEXT, status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS git_provider_settings(provider TEXT PRIMARY KEY CHECK(provider IN('github','gitlab')), encrypted_config TEXT NOT NULL, updated_by TEXT NOT NULL REFERENCES users(id), updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS git_provider_setup_flows(id TEXT PRIMARY KEY, provider TEXT NOT NULL CHECK(provider='github'), user_id TEXT NOT NULL REFERENCES users(id), public_url TEXT NOT NULL, state_hash TEXT NOT NULL, status TEXT NOT NULL, error_code TEXT, error_message TEXT, expires_at TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS provider_authorization_metadata(authorization_id TEXT PRIMARY KEY REFERENCES provider_authorizations(id), credential_kind TEXT NOT NULL, external_account TEXT, permissions_json TEXT NOT NULL, webhook_status TEXT NOT NULL, webhook_url TEXT, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS provider_authorization_repositories(authorization_id TEXT NOT NULL REFERENCES provider_authorizations(id), repository_id TEXT NOT NULL, repository_name TEXT NOT NULL, clone_url TEXT NOT NULL, default_branch TEXT NOT NULL, web_url TEXT, can_write INTEGER NOT NULL, verified_at TEXT NOT NULL, PRIMARY KEY(authorization_id,repository_id));
CREATE TABLE IF NOT EXISTS git_connection_flows(id TEXT PRIMARY KEY, provider TEXT NOT NULL, project_id TEXT NOT NULL REFERENCES projects(id), user_id TEXT NOT NULL REFERENCES users(id), state_hash TEXT NOT NULL, nonce_hash TEXT NOT NULL, sealed_verifier TEXT NOT NULL, status TEXT NOT NULL, error_code TEXT, error_message TEXT, authorization_id TEXT REFERENCES provider_authorizations(id), repositories_json TEXT, matched_repository_id TEXT, permissions_json TEXT, webhook_status TEXT, webhook_url TEXT, expires_at TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS project_repository_grants(id TEXT PRIMARY KEY, project_id TEXT NOT NULL UNIQUE REFERENCES projects(id), authorization_id TEXT NOT NULL REFERENCES provider_authorizations(id), repository_id TEXT NOT NULL, repository_name TEXT NOT NULL, clone_url TEXT NOT NULL, default_branch TEXT NOT NULL, access_level TEXT NOT NULL, approved_by TEXT NOT NULL, approved_at TEXT NOT NULL, revoked_at TEXT);
CREATE TABLE IF NOT EXISTS project_commit_sync_state(project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,last_synced_at TEXT NOT NULL,updated_by_type TEXT NOT NULL,updated_by_id TEXT);
CREATE TABLE IF NOT EXISTS work_items(id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), number INTEGER NOT NULL, parent_id TEXT REFERENCES work_items(id), title TEXT NOT NULL, description_markdown TEXT NOT NULL, acceptance_criteria_markdown TEXT NOT NULL, priority TEXT NOT NULL, stage TEXT NOT NULL, discussion_mode TEXT NOT NULL, execution_mode TEXT NOT NULL, acceptance_mode TEXT NOT NULL, target_branch TEXT NOT NULL, assignee_kind TEXT, assignee_id TEXT, assignment_state TEXT, blocked_at TEXT, blocked_reason TEXT, abandoned_at TEXT, abandoned_reason TEXT, abandoned_from_stage TEXT, version INTEGER NOT NULL DEFAULT 1, lease_generation INTEGER NOT NULL DEFAULT 0, created_by_user_id TEXT REFERENCES users(id), created_at TEXT NOT NULL, updated_at TEXT NOT NULL, completed_at TEXT, UNIQUE(project_id,number));
CREATE TABLE IF NOT EXISTS work_item_followers(work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,user_id TEXT NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,PRIMARY KEY(work_item_id,user_id));
CREATE INDEX IF NOT EXISTS work_item_followers_user ON work_item_followers(user_id,work_item_id);
CREATE TABLE IF NOT EXISTS notifications(id TEXT PRIMARY KEY,user_id TEXT NOT NULL REFERENCES users(id),project_id TEXT REFERENCES projects(id),work_item_id TEXT REFERENCES work_items(id),kind TEXT NOT NULL,title TEXT NOT NULL,body TEXT NOT NULL,actor_type TEXT,actor_id TEXT,read_at TEXT,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS notifications_user_time ON notifications(user_id,created_at DESC);
CREATE TABLE IF NOT EXISTS work_item_dependencies(work_item_id TEXT NOT NULL REFERENCES work_items(id), depends_on_id TEXT NOT NULL REFERENCES work_items(id), created_at TEXT NOT NULL, PRIMARY KEY(work_item_id,depends_on_id), CHECK(work_item_id<>depends_on_id));
CREATE TABLE IF NOT EXISTS assignments(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), assignee_kind TEXT NOT NULL, assignee_id TEXT NOT NULL, state TEXT NOT NULL, generation INTEGER NOT NULL, created_at TEXT NOT NULL, ended_at TEXT, ended_reason TEXT);
CREATE TABLE IF NOT EXISTS leases(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), assignment_id TEXT NOT NULL REFERENCES assignments(id), agent_id TEXT NOT NULL REFERENCES agents(id), generation INTEGER NOT NULL, expires_at TEXT NOT NULL, released_at TEXT, release_reason TEXT, created_at TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS one_active_lease_per_item ON leases(work_item_id) WHERE released_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS one_active_lease_per_agent ON leases(agent_id) WHERE released_at IS NULL;
CREATE TABLE IF NOT EXISTS runner_runs(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), agent_id TEXT NOT NULL REFERENCES agents(id), lease_id TEXT NOT NULL REFERENCES leases(id), state TEXT NOT NULL, created_at TEXT NOT NULL, ended_at TEXT, end_reason TEXT);
CREATE TABLE IF NOT EXISTS runner_status(agent_id TEXT PRIMARY KEY REFERENCES agents(id), device_id TEXT REFERENCES runner_devices(id), status TEXT NOT NULL, current_run_id TEXT REFERENCES runner_runs(id), paused INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS runner_repository_mappings(id TEXT PRIMARY KEY, device_id TEXT NOT NULL REFERENCES runner_devices(id), project_id TEXT NOT NULL REFERENCES projects(id), local_path TEXT NOT NULL, config_digest TEXT NOT NULL, confirmed_at TEXT NOT NULL, paused_at TEXT, UNIQUE(device_id,project_id));
CREATE TABLE IF NOT EXISTS conversation_entries(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), kind TEXT NOT NULL, stage TEXT NOT NULL, author_type TEXT NOT NULL, author_id TEXT, payload_json TEXT NOT NULL, related_version INTEGER, created_at TEXT NOT NULL, edited_at TEXT, deleted_at TEXT);
CREATE TABLE IF NOT EXISTS attachments(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), entry_id TEXT REFERENCES conversation_entries(id), original_name TEXT NOT NULL, mime TEXT NOT NULL, size INTEGER NOT NULL, sha256 TEXT NOT NULL, storage_key TEXT NOT NULL, uploader_type TEXT NOT NULL, uploader_id TEXT, created_at TEXT NOT NULL, UNIQUE(work_item_id,sha256));
CREATE TABLE IF NOT EXISTS discussion_conclusions(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), version INTEGER NOT NULL, goal_markdown TEXT NOT NULL, scope_markdown TEXT NOT NULL, out_of_scope_markdown TEXT NOT NULL, implementation_plan_markdown TEXT NOT NULL, acceptance_criteria_markdown TEXT NOT NULL, risks_markdown TEXT NOT NULL, created_by_type TEXT NOT NULL, created_by_id TEXT, created_at TEXT NOT NULL, supersedes_version INTEGER, UNIQUE(work_item_id,version));
CREATE TABLE IF NOT EXISTS execution_attempts(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), number INTEGER NOT NULL, assignee_kind TEXT, assignee_id TEXT, conclusion_version INTEGER NOT NULL, target_branch TEXT NOT NULL, baseline_commit TEXT, task_branch TEXT NOT NULL, lease_generation INTEGER NOT NULL, summary_markdown TEXT, commits_json TEXT, changed_files_json TEXT, remaining_risks_markdown TEXT, pushed_at TEXT, started_at TEXT NOT NULL, ended_at TEXT, end_reason TEXT, UNIQUE(work_item_id,number));
CREATE TABLE IF NOT EXISTS validation_runs(id TEXT PRIMARY KEY, execution_attempt_id TEXT NOT NULL REFERENCES execution_attempts(id), name TEXT NOT NULL, command TEXT, required INTEGER NOT NULL, exit_code INTEGER NOT NULL, duration_ms INTEGER NOT NULL, log_summary TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS git_commit_evidence(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), repository_id TEXT NOT NULL, commit_sha TEXT NOT NULL, message TEXT NOT NULL, author TEXT, branch TEXT NOT NULL, files_json TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(repository_id,commit_sha));
CREATE TABLE IF NOT EXISTS acceptance_attempts(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), number INTEGER NOT NULL, execution_attempt_id TEXT REFERENCES execution_attempts(id), acceptor_type TEXT NOT NULL, acceptor_id TEXT, conclusion_version INTEGER NOT NULL, outcome TEXT, note_markdown TEXT, criteria_results_json TEXT, started_at TEXT NOT NULL, completed_at TEXT, UNIQUE(work_item_id,number));
CREATE TABLE IF NOT EXISTS decomposition_proposals(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), items_json TEXT NOT NULL, dependencies_json TEXT NOT NULL, status TEXT NOT NULL, created_by_type TEXT NOT NULL, created_by_id TEXT, created_at TEXT NOT NULL, materialized_at TEXT);
CREATE TABLE IF NOT EXISTS activity_events(id TEXT PRIMARY KEY, project_id TEXT REFERENCES projects(id), actor_type TEXT NOT NULL, actor_id TEXT, event_type TEXT NOT NULL, object_type TEXT NOT NULL, object_id TEXT NOT NULL, source TEXT NOT NULL, payload_json TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS activity_project_time ON activity_events(project_id,created_at DESC);
CREATE TABLE IF NOT EXISTS idempotency_records(scope TEXT NOT NULL, request_id TEXT NOT NULL, response_json TEXT NOT NULL, status_code INTEGER NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(scope,request_id));
CREATE TABLE IF NOT EXISTS webhook_deliveries(provider TEXT NOT NULL, delivery_id TEXT NOT NULL, received_at TEXT NOT NULL, result TEXT NOT NULL, PRIMARY KEY(provider,delivery_id));
`
