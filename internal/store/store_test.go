package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenRejectsUnversionedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("CREATE TABLE users(id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path); err == nil {
		t.Fatal("expected an unversioned database to be rejected")
	}
}

func TestOpenCreatesOnlyCurrentAuthorizationTables(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, table := range []string{"system_settings", "provider_authorizations", "git_provider_settings", "git_provider_setup_flows", "project_repository_grants"} {
		var count int
		if err = database.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("current table %s missing", table)
		}
	}
	for _, table := range []string{"git_installations", "repository_bindings"} {
		var count int
		if err = database.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("compatibility table %s still exists", table)
		}
	}
}

func TestMigrationFiveInvalidatesLegacyAuthAndPreservesExecutionHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v4.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		"PRAGMA foreign_keys=ON",
		"CREATE TABLE schema_metadata(id INTEGER PRIMARY KEY,version INTEGER NOT NULL,created_at TEXT NOT NULL)",
		"INSERT INTO schema_metadata VALUES(1,4,CURRENT_TIMESTAMP)",
		"CREATE TABLE agents(id TEXT PRIMARY KEY,name TEXT,purpose TEXT,status TEXT,created_at TEXT,revoked_at TEXT)",
		"CREATE TABLE projects(id TEXT PRIMARY KEY,project_key TEXT,name TEXT,repository_url TEXT,created_at TEXT,updated_at TEXT)",
		"CREATE TABLE agent_project_grants(agent_id TEXT,project_id TEXT,created_at TEXT,PRIMARY KEY(agent_id,project_id))",
		"CREATE TABLE work_items(id TEXT PRIMARY KEY,project_id TEXT,number INTEGER,title TEXT,stage TEXT,assignment_state TEXT,version INTEGER,updated_at TEXT)",
		"CREATE TABLE assignments(id TEXT PRIMARY KEY,work_item_id TEXT,assignee_kind TEXT,assignee_id TEXT,state TEXT,generation INTEGER,created_at TEXT)",
		"CREATE TABLE leases(id TEXT PRIMARY KEY,work_item_id TEXT,assignment_id TEXT,agent_id TEXT,generation INTEGER,expires_at TEXT,released_at TEXT,release_reason TEXT,created_at TEXT)",
		"CREATE TABLE runner_runs(id TEXT PRIMARY KEY,work_item_id TEXT,agent_id TEXT,lease_id TEXT,state TEXT,created_at TEXT,ended_at TEXT,end_reason TEXT)",
		"CREATE TABLE agent_tokens(id TEXT PRIMARY KEY,agent_id TEXT,token_hash TEXT,created_at TEXT,revoked_at TEXT)",
		"CREATE TABLE agent_key_settings(agent_id TEXT PRIMARY KEY,expiry_policy TEXT,updated_at TEXT)",
		"CREATE TABLE runner_pairing_codes(id TEXT PRIMARY KEY,agent_id TEXT,code_hash TEXT,expires_at TEXT,created_at TEXT)",
		"CREATE TABLE runner_devices(id TEXT PRIMARY KEY,agent_id TEXT)",
		"CREATE TABLE runner_repository_mappings(agent_id TEXT,project_id TEXT)",
		"CREATE TABLE runner_status(agent_id TEXT PRIMARY KEY)",
		"CREATE TABLE provider_authorizations(id TEXT PRIMARY KEY,provider TEXT,status TEXT)",
		"CREATE TABLE provider_authorization_repositories(authorization_id TEXT,repository_id TEXT)",
		"CREATE TABLE provider_authorization_metadata(authorization_id TEXT PRIMARY KEY)",
		"CREATE TABLE project_repository_grants(id TEXT PRIMARY KEY,authorization_id TEXT)",
		"CREATE TABLE git_connection_flows(id TEXT PRIMARY KEY,provider TEXT)",
		"CREATE TABLE git_provider_settings(provider TEXT PRIMARY KEY)",
		"CREATE TABLE activity_events(id TEXT PRIMARY KEY,project_id TEXT,actor_type TEXT,actor_id TEXT,event_type TEXT,object_type TEXT,object_id TEXT,source TEXT,payload_json TEXT,created_at TEXT)",
		"INSERT INTO agents VALUES('agent-1','legacy','legacy','active',CURRENT_TIMESTAMP,NULL)",
		"INSERT INTO projects VALUES('project-1','PB','Legacy','https://gitlab.example/repo',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)",
		"INSERT INTO agent_project_grants VALUES('agent-1','project-1',CURRENT_TIMESTAMP)",
		"INSERT INTO work_items VALUES('PB-1','project-1',1,'preserved','execution','active',1,CURRENT_TIMESTAMP)",
		"INSERT INTO assignments VALUES('assignment-1','PB-1','agent','agent-1','active',1,CURRENT_TIMESTAMP)",
		"INSERT INTO leases VALUES('lease-1','PB-1','assignment-1','agent-1',1,'9999-01-01T00:00:00Z',NULL,NULL,CURRENT_TIMESTAMP)",
		"INSERT INTO runner_runs VALUES('run-1','PB-1','agent-1','lease-1','execution',CURRENT_TIMESTAMP,NULL,NULL)",
		"INSERT INTO agent_tokens VALUES('token-1','agent-1','hash',CURRENT_TIMESTAMP,NULL)",
		"INSERT INTO runner_status VALUES('agent-1')",
		"INSERT INTO provider_authorizations VALUES('gitlab-1','gitlab','active')",
		"INSERT INTO project_repository_grants VALUES('grant-1','gitlab-1')",
		"INSERT INTO git_provider_settings VALUES('gitlab')",
	}
	for _, statement := range statements {
		if _, err = database.Exec(statement); err != nil {
			t.Fatalf("v4 fixture statement failed: %s: %v", statement, err)
		}
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	for _, legacy := range []string{"agent_tokens", "agent_key_settings", "runner_pairing_codes", "runner_devices", "runner_repository_mappings", "runner_status", "runner_runs"} {
		var count int
		if err = migrated.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", legacy).Scan(&count); err != nil || count != 0 {
			t.Fatalf("legacy table %s remains", legacy)
		}
	}
	var releasedAt, releaseReason, endedAt, endReason string
	if err = migrated.DB.QueryRow("SELECT released_at,release_reason FROM leases WHERE id='lease-1'").Scan(&releasedAt, &releaseReason); err != nil || releaseReason != "auth_migration" {
		t.Fatalf("legacy lease was not invalidated: %q %q %v", releasedAt, releaseReason, err)
	}
	if _, err = time.Parse("2006-01-02 15:04:05", releasedAt); err != nil {
		t.Fatalf("migration release timestamp is invalid: %q", releasedAt)
	}
	if err = migrated.DB.QueryRow("SELECT ended_at,end_reason FROM agent_executions WHERE id='run-1'").Scan(&endedAt, &endReason); err != nil || endReason != "auth_migration" {
		t.Fatalf("execution history was not migrated: %q %q %v", endedAt, endReason, err)
	}
	var assignmentState string
	if err = migrated.DB.QueryRow("SELECT assignment_state FROM work_items WHERE id='PB-1'").Scan(&assignmentState); err != nil || assignmentState != "reserved" {
		t.Fatalf("migrated task was not restored to the queue: %q %v", assignmentState, err)
	}
	var migrationEvents int
	if err = migrated.DB.QueryRow("SELECT COUNT(*) FROM activity_events WHERE event_type='auth_migration' AND object_id='PB-1'").Scan(&migrationEvents); err != nil || migrationEvents != 1 {
		t.Fatalf("auth migration audit missing: %d %v", migrationEvents, err)
	}
	var gitlabCount int
	if err = migrated.DB.QueryRow("SELECT COUNT(*) FROM provider_authorizations WHERE provider='gitlab'").Scan(&gitlabCount); err != nil || gitlabCount != 0 {
		t.Fatalf("GitLab authorization survived migration: %d %v", gitlabCount, err)
	}
}
