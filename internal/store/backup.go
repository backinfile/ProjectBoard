package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func backupBeforeMigration(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	dir := filepath.Join(filepath.Dir(path), "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	target := filepath.Join(dir, "projectboard-"+time.Now().Format("2006-01-02")+"-premigration.db")
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err = db.Exec("VACUUM INTO " + sqliteQuote(target)); err != nil {
		return err
	}
	entries, err := filepath.Glob(filepath.Join(dir, "projectboard-*-premigration.db"))
	if err != nil {
		return err
	}
	sort.Strings(entries)
	if len(entries) > 7 {
		for _, old := range entries[:len(entries)-7] {
			if err := os.Remove(old); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Backup(ctx context.Context, dir string) (string, error) {
	if s.path == "" || s.path == ":memory:" {
		return "", fmt.Errorf("in-memory database cannot be backed up")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "projectboard-"+time.Now().Format("20060102-150405")+".db")
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO "+sqliteQuote(target)); err != nil {
		return "", err
	}
	return target, nil
}

func sqliteQuote(value string) string {
	out := "'"
	for _, r := range value {
		if r == '\'' {
			out += "''"
		} else {
			out += string(r)
		}
	}
	return out + "'"
}
