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
		if version != SchemaVersion {
			db.Close()
			return nil, fmt.Errorf("database schema %d is incompatible with this release; create a new data directory", version)
		}
	}
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	return &Store{DB: db}, nil
}

const SchemaVersion = 8

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
INSERT OR IGNORE INTO schema_metadata(id,version,created_at) VALUES(1,8,CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE, display_name TEXT NOT NULL, password_hash TEXT NOT NULL, system_role TEXT NOT NULL CHECK(system_role IN('administrator','user')), status TEXT NOT NULL DEFAULT 'active', must_change_password INTEGER NOT NULL DEFAULT 0, disabled_reason TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_active_at TEXT);
CREATE TABLE IF NOT EXISTS system_settings(key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_by TEXT NOT NULL REFERENCES users(id), updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), token_hash TEXT NOT NULL UNIQUE, csrf_hash TEXT NOT NULL, user_agent TEXT, ip TEXT, expires_at TEXT NOT NULL, revoked_at TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS login_attempts(id TEXT PRIMARY KEY, username TEXT NOT NULL, ip TEXT NOT NULL, succeeded INTEGER NOT NULL, created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS login_attempt_window ON login_attempts(username,ip,created_at);
CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY, project_key TEXT NOT NULL UNIQUE COLLATE NOCASE, name TEXT NOT NULL, description_markdown TEXT NOT NULL DEFAULT '', archived_at TEXT, project_path TEXT NOT NULL UNIQUE COLLATE NOCASE, default_target_branch TEXT NOT NULL DEFAULT 'main', validation_commands_json TEXT NOT NULL DEFAULT '[]', forbidden_paths_json TEXT NOT NULL DEFAULT '[]', agent_rules_markdown TEXT NOT NULL DEFAULT '', allow_subtasks INTEGER NOT NULL DEFAULT 1, agent_prompts_json TEXT NOT NULL DEFAULT '[]', config_version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS project_memberships(project_id TEXT NOT NULL REFERENCES projects(id), user_id TEXT NOT NULL REFERENCES users(id), role TEXT NOT NULL CHECK(role IN('developer','viewer')), created_at TEXT NOT NULL, PRIMARY KEY(project_id,user_id));
CREATE TABLE IF NOT EXISTS agents(id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE, purpose TEXT NOT NULL, runtime_type TEXT NOT NULL DEFAULT 'local_codex_cli' CHECK(runtime_type='local_codex_cli'), max_concurrent_tasks INTEGER NOT NULL DEFAULT 1 CHECK(max_concurrent_tasks>0), turn_timeout_minutes INTEGER NOT NULL DEFAULT 120 CHECK(turn_timeout_minutes>0), accept_tags_json TEXT NOT NULL DEFAULT '[]', reject_tags_json TEXT NOT NULL DEFAULT '[]', status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, revoked_at TEXT);
CREATE TABLE IF NOT EXISTS work_items(id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), number INTEGER NOT NULL, parent_id TEXT REFERENCES work_items(id), title TEXT NOT NULL, description_markdown TEXT NOT NULL, acceptance_criteria_markdown TEXT NOT NULL, priority TEXT NOT NULL, stage TEXT NOT NULL CHECK(stage IN('created','in_progress','completed','closed')), workflow_type TEXT NOT NULL DEFAULT 'standard' CHECK(workflow_type IN('standard','simple_conversation')), target_branch TEXT NOT NULL, assignee_kind TEXT, assignee_id TEXT, is_agent_task INTEGER NOT NULL DEFAULT 0, pause_after_plan INTEGER NOT NULL DEFAULT 0, pause_before_completion INTEGER NOT NULL DEFAULT 0, plan_pause_consumed INTEGER NOT NULL DEFAULT 0, agent_phase TEXT NOT NULL DEFAULT 'work' CHECK(agent_phase IN('work','merge','merge_review','close_review')), agent_state TEXT NOT NULL DEFAULT 'idle' CHECK(agent_state IN('idle','queued','running','paused_plan','paused_completion','paused_failure','finished')), assigned_agent_id TEXT REFERENCES agents(id), codex_thread_id TEXT, workspace_path TEXT, base_commit_sha TEXT, resume_requested INTEGER NOT NULL DEFAULT 0, blocked_at TEXT, blocked_reason TEXT, version INTEGER NOT NULL DEFAULT 1, created_by_user_id TEXT REFERENCES users(id), created_at TEXT NOT NULL, updated_at TEXT NOT NULL, completed_at TEXT, closed_at TEXT, UNIQUE(project_id,number));
CREATE TABLE IF NOT EXISTS work_item_followers(work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,user_id TEXT NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,PRIMARY KEY(work_item_id,user_id));
CREATE INDEX IF NOT EXISTS work_item_followers_user ON work_item_followers(user_id,work_item_id);
CREATE TABLE IF NOT EXISTS work_item_tags(work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,tag TEXT NOT NULL COLLATE NOCASE,created_at TEXT NOT NULL,PRIMARY KEY(work_item_id,tag));
CREATE INDEX IF NOT EXISTS work_item_tags_tag ON work_item_tags(tag,work_item_id);
CREATE TABLE IF NOT EXISTS notifications(id TEXT PRIMARY KEY,user_id TEXT NOT NULL REFERENCES users(id),project_id TEXT REFERENCES projects(id),work_item_id TEXT REFERENCES work_items(id),kind TEXT NOT NULL,title TEXT NOT NULL,body TEXT NOT NULL,actor_type TEXT,actor_id TEXT,read_at TEXT,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS notifications_user_time ON notifications(user_id,created_at DESC);
CREATE TABLE IF NOT EXISTS work_item_dependencies(work_item_id TEXT NOT NULL REFERENCES work_items(id), depends_on_id TEXT NOT NULL REFERENCES work_items(id), created_at TEXT NOT NULL, PRIMARY KEY(work_item_id,depends_on_id), CHECK(work_item_id<>depends_on_id));
CREATE TABLE IF NOT EXISTS agent_executions(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), agent_id TEXT NOT NULL REFERENCES agents(id), attempt_number INTEGER NOT NULL, state TEXT NOT NULL, thread_id TEXT, command_json TEXT NOT NULL DEFAULT '[]', prompt_markdown TEXT NOT NULL, result_json TEXT, output_jsonl TEXT NOT NULL DEFAULT '', final_message TEXT, workspace_path TEXT NOT NULL, started_at TEXT NOT NULL, ended_at TEXT, error_message TEXT, UNIQUE(work_item_id,attempt_number));
CREATE INDEX IF NOT EXISTS agent_executions_active_agent ON agent_executions(agent_id,state,started_at);
CREATE TABLE IF NOT EXISTS conversation_entries(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), kind TEXT NOT NULL, stage TEXT NOT NULL, author_type TEXT NOT NULL, author_id TEXT, payload_json TEXT NOT NULL, related_version INTEGER, created_at TEXT NOT NULL, edited_at TEXT, deleted_at TEXT);
CREATE TABLE IF NOT EXISTS attachments(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), entry_id TEXT REFERENCES conversation_entries(id), original_name TEXT NOT NULL, mime TEXT NOT NULL, size INTEGER NOT NULL, sha256 TEXT NOT NULL, storage_key TEXT NOT NULL, uploader_type TEXT NOT NULL, uploader_id TEXT, created_at TEXT NOT NULL, UNIQUE(work_item_id,sha256));
CREATE TABLE IF NOT EXISTS git_commit_evidence(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), repository_id TEXT NOT NULL, commit_sha TEXT NOT NULL, message TEXT NOT NULL, author TEXT, branch TEXT NOT NULL, files_json TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(repository_id,commit_sha));
CREATE TABLE IF NOT EXISTS decomposition_proposals(id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id), items_json TEXT NOT NULL, dependencies_json TEXT NOT NULL, status TEXT NOT NULL, created_by_type TEXT NOT NULL, created_by_id TEXT, created_at TEXT NOT NULL, materialized_at TEXT);
CREATE TABLE IF NOT EXISTS activity_events(id TEXT PRIMARY KEY, project_id TEXT REFERENCES projects(id), actor_type TEXT NOT NULL, actor_id TEXT, event_type TEXT NOT NULL, object_type TEXT NOT NULL, object_id TEXT NOT NULL, source TEXT NOT NULL, payload_json TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS activity_project_time ON activity_events(project_id,created_at DESC);
CREATE TABLE IF NOT EXISTS idempotency_records(scope TEXT NOT NULL, request_id TEXT NOT NULL, response_json TEXT NOT NULL, status_code INTEGER NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(scope,request_id));
`
