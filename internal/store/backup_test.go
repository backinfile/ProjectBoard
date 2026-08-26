package store_test

import (
	"context"
	"github.com/projectboard/projectboard/internal/store"
	"path/filepath"
	"testing"
)

func TestExistingDatabaseIsBackedUpBeforeMigration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "projectboard.db")
	first, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	matches, err := filepath.Glob(filepath.Join(dir, "backups", "projectboard-*-premigration.db"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one daily pre-migration backup, got %v", matches)
	}
}
