package store

import (
	"database/sql"
	"path/filepath"
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
