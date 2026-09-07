package store

import (
	"errors"
	"os"
	"path/filepath"
	"projectboard/internal/domain"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentCompareAndSwap(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Update(st.Revision, func(x *domain.State) error { x.Theme = "dark"; return nil })
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflicts != 15 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	after, err := s.Read()
	if err != nil || after.Revision != st.Revision+1 || after.Theme != "dark" {
		t.Fatal("non-atomic commit", err)
	}
}

func TestRestartExclusiveDirectoryAndMonotonicNumbers(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	other, e := Open(dir)
	if e == nil {
		other.Close()
		t.Fatal("second process acquired workspace")
	}
	st := domain.Empty()
	st.Projects = []domain.Project{{ID: "p", Name: "项目", Code: "PB", Statuses: domain.DefaultStatuses(), InitialStatusID: "created"}}
	st.Tasks = []domain.Task{domain.NewTask("p", "one", "第一个", 1)}
	st, e = s.Save(st)
	if e != nil {
		t.Fatal(e)
	}
	st.Tasks = []domain.Task{}
	st, e = s.Save(st)
	if e != nil {
		t.Fatal(e)
	}
	st.Tasks = []domain.Task{domain.NewTask("p", "two", "第二个", 1)}
	st, e = s.Save(st)
	if e != nil {
		t.Fatal(e)
	}
	if st.Tasks[0].Num != 2 {
		t.Fatal("task number reused")
	}
	revision := st.Revision
	s.Close()
	s, e = Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	st, e = s.Read()
	if e != nil || st.Revision != revision || st.Tasks[0].Title != "第二个" {
		t.Fatal("restart lost committed state")
	}
	_, e = s.Update(revision-1, func(st *domain.State) error { st.Tasks = nil; return nil })
	if !errors.Is(e, ErrConflict) {
		t.Fatal("stale write accepted")
	}
}

func TestCleanupRetainsReferencedAttachments(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, e := s.PutAttachment("p", "keep.txt", "text/plain", strings.NewReader("keep"))
	if e != nil {
		t.Fatal(e)
	}
	unused, e := s.PutAttachment("p", "unused.txt", "text/plain", strings.NewReader("unused"))
	if e != nil {
		t.Fatal(e)
	}
	st := domain.Empty()
	st.Projects = []domain.Project{{ID: "p", Name: "项目", Code: "PB", Statuses: domain.DefaultStatuses(), InitialStatusID: "created"}}
	task := domain.NewTask("p", "task", "任务", 1)
	task.Deleted = domain.Now()
	task.DetailAttachments = []domain.Attachment{a}
	st.Tasks = []domain.Task{task}
	if _, e = s.Save(st); e != nil {
		t.Fatal(e)
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, id := range []string{a.ID, unused.ID} {
		s.DB.Exec(`UPDATE attachments SET created=? WHERE id=?`, old.Unix(), id)
		os.Chtimes(filepath.Join(s.Dir, "attachments", id), old, old)
	}
	if e = s.Cleanup(); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Attachment("p", a.ID); e != nil {
		t.Fatal("trash attachment removed")
	}
	if _, _, e = s.Attachment("p", unused.ID); e == nil {
		t.Fatal("unreferenced attachment retained")
	}
}
func TestFutureDatabaseVersionIsRejected(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(`PRAGMA user_version=99`); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(dir)
	if e == nil {
		s.Close()
		t.Fatal("future database accepted")
	}
}
