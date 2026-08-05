package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/projectboard/projectboard/internal/store"
)

func TestBackupAndRestore(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "projectboard.db")
	database, err := store.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("INSERT INTO users(id,username,display_name,password_hash,system_role,status,created_at,updated_at) VALUES('u1','admin','Admin','hash','administrator','active','now','now')"); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(dir, "backups", "snapshot.db")
	if err = Backup(databasePath, backupPath); err != nil {
		t.Fatal(err)
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(databasePath); err != nil {
		t.Fatal(err)
	}
	if err = Restore(databasePath, backupPath); err != nil {
		t.Fatal(err)
	}
	restored, err := store.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var count int
	if err = restored.DB.QueryRow("SELECT COUNT(*) FROM users WHERE username='admin'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("restored users = %d", count)
	}
}
