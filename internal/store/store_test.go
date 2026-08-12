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
