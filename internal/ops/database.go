package ops

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Backup(databasePath, destination string) error {
	if destination == "" {
		return fmt.Errorf("backup destination is required")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err = db.Exec("VACUUM INTO ?", destination); err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	return nil
}

func Restore(databasePath, source string) error {
	if source == "" {
		return fmt.Errorf("restore source is required")
	}
	check, err := sql.Open("sqlite", "file:"+filepath.ToSlash(source)+"?mode=ro")
	if err != nil {
		return err
	}
	var result string
	err = check.QueryRow("PRAGMA quick_check").Scan(&result)
	_ = check.Close()
	if err != nil || result != "ok" {
		return fmt.Errorf("backup integrity check failed: %s", result)
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err = os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return err
	}
	temporary := databasePath + ".restore.tmp"
	out, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("copy backup: %v %v %v", copyErr, syncErr, closeErr)
	}
	previous := databasePath + ".before-restore"
	_ = os.Remove(previous)
	if _, statErr := os.Stat(databasePath); statErr == nil {
		if err = os.Rename(databasePath, previous); err != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("preserve current database: %w", err)
		}
	}
	if err = os.Rename(temporary, databasePath); err != nil {
		_ = os.Rename(previous, databasePath)
		_ = os.Remove(temporary)
		return fmt.Errorf("replace database: %w", err)
	}
	return nil
}
