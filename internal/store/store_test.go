package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
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

func TestOpenCreatesOnlyLocalProjectSchema(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, table := range []string{
		"system_settings", "projects", "agents", "agent_executions", "agent_requests",
		"knowledge_nodes", "knowledge_node_revisions", "knowledge_files", "work_items",
	} {
		var count int
		if err = database.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("current table %s missing", table)
		}
	}
	for _, table := range []string{
		"agent_ssh_keys", "agent_ssh_sessions", "leases", "assignments",
		"runner_runs", "runner_devices", "runner_status", "discussion_conclusions",
		"execution_attempts", "validation_runs", "acceptance_attempts", "agent_project_grants",
		"provider_authorizations", "git_provider_settings", "git_provider_setup_flows",
		"provider_authorization_metadata", "provider_authorization_repositories",
		"git_connection_flows", "project_repository_grants", "project_commit_sync_state", "webhook_deliveries",
	} {
		var count int
		if err = database.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("removed table %s still exists", table)
		}
	}
}

func TestOpenRejectsPreviousSchemaWithoutMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v8.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`CREATE TABLE schema_metadata(id INTEGER PRIMARY KEY, version INTEGER NOT NULL, created_at TEXT NOT NULL);
		INSERT INTO schema_metadata VALUES(1,8,CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err = Open(path); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("expected schema 8 to be rejected, got %v", err)
	}
}

func TestOpenMigratesSchemaNineWithoutLosingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v9.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`CREATE TABLE schema_metadata(id INTEGER PRIMARY KEY, version INTEGER NOT NULL, created_at TEXT NOT NULL);
		INSERT INTO schema_metadata VALUES(1,9,CURRENT_TIMESTAMP);
		CREATE TABLE agents(id TEXT PRIMARY KEY,name TEXT NOT NULL,purpose TEXT NOT NULL,runtime_type TEXT NOT NULL,max_concurrent_tasks INTEGER NOT NULL,turn_timeout_minutes INTEGER NOT NULL,accept_tags_json TEXT NOT NULL,reject_tags_json TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL,revoked_at TEXT);
		INSERT INTO agents VALUES('a','Existing','kept','local_codex_cli',1,120,'[]','[]','active',CURRENT_TIMESTAMP,NULL);
		CREATE TABLE agent_requests(id TEXT PRIMARY KEY,assigned_agent_id TEXT);
		CREATE TABLE projects(id TEXT PRIMARY KEY);
		INSERT INTO projects VALUES('p');
		CREATE TABLE knowledge_nodes(id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), parent_id TEXT REFERENCES knowledge_nodes(id), title TEXT NOT NULL, markdown TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0, locked_for_agents INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1, deleted_at TEXT, created_by_type TEXT NOT NULL, created_by_id TEXT, created_at TEXT NOT NULL, updated_by_type TEXT NOT NULL, updated_by_id TEXT, updated_at TEXT NOT NULL);
		INSERT INTO knowledge_nodes VALUES('n','p',NULL,'Runbook','preserved body',0,0,1,NULL,'human','u','now','human','u','now');
		CREATE TABLE knowledge_node_revisions(id TEXT PRIMARY KEY, node_id TEXT NOT NULL REFERENCES knowledge_nodes(id), version INTEGER NOT NULL, parent_id TEXT, title TEXT NOT NULL, markdown TEXT NOT NULL, sort_order INTEGER NOT NULL, locked_for_agents INTEGER NOT NULL, file_ids_json TEXT NOT NULL DEFAULT '[]', actor_type TEXT NOT NULL, actor_id TEXT, reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, UNIQUE(node_id,version));
		INSERT INTO knowledge_node_revisions VALUES('r','n',1,NULL,'Runbook','preserved body',0,0,'[]','human','u','created','now');`); err != nil {
		t.Fatal(err)
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	if err = migrateV9ToV10(migrated); err != nil {
		t.Fatal(err)
	}
	var version int
	var name, model, effort string
	if err = migrated.QueryRow("SELECT version FROM schema_metadata WHERE id=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err = migrated.QueryRow("SELECT name,model,reasoning_effort FROM agents WHERE id='a'").Scan(&name, &model, &effort); err != nil {
		t.Fatal(err)
	}
	var body, summary string
	if err = migrated.QueryRow(`SELECT version FROM schema_metadata WHERE id=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err = migrated.QueryRow(`SELECT markdown,summary FROM knowledge_nodes WHERE id='n'`).Scan(&body, &summary); err != nil {
		t.Fatal(err)
	}
	var indexed int
	if err = migrated.QueryRow(`SELECT COUNT(*) FROM knowledge_search WHERE knowledge_search MATCH 'preserved'`).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion || name != "Existing" || model != "" || effort != "" || body != "preserved body" || summary != "" || indexed != 1 {
		t.Fatalf("migration result version=%d agent=%q model=%q effort=%q body=%q summary=%q indexed=%d", version, name, model, effort, body, summary, indexed)
	}
}

func TestOpenRejectsPreviousSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`CREATE TABLE schema_metadata(
		id INTEGER PRIMARY KEY,
		version INTEGER NOT NULL,
		created_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("INSERT INTO schema_metadata VALUES(1,5,CURRENT_TIMESTAMP)"); err != nil {
		t.Fatal(err)
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(path)
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("expected an explicit incompatibility error, got %v", err)
	}
}
