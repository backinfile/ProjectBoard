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

func TestOpenCreatesOnlyLocalExecutorSchema(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, table := range []string{
		"system_settings", "provider_authorizations", "git_provider_settings",
		"project_repository_grants", "agents", "agent_executions", "work_items",
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
	} {
		var count int
		if err = database.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("removed table %s still exists", table)
		}
	}
}

func TestOpenMigratesGlobalAgentConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v6.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`CREATE TABLE schema_metadata(id INTEGER PRIMARY KEY, version INTEGER NOT NULL, created_at TEXT NOT NULL);
		INSERT INTO schema_metadata VALUES(1,6,CURRENT_TIMESTAMP);
		CREATE TABLE agent_project_grants(agent_id TEXT NOT NULL, project_id TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(agent_id,project_id));
		INSERT INTO agent_project_grants VALUES('agent','project',CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	var version, grants int
	if err = migrated.DB.QueryRow("SELECT version FROM schema_metadata WHERE id=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err = migrated.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agent_project_grants'").Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion || grants != 0 {
		t.Fatalf("migration left schema version %d and %d project-grant tables", version, grants)
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
