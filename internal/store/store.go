package store

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"projectboard/internal/domain"
	"strings"
	"sync"
	"time"
)

var ErrConflict = errors.New("REVISION_CONFLICT：数据已更新，请保留草稿并刷新")

type Store struct {
	DB      *sql.DB
	Dir     string
	mu      sync.Mutex
	release func()
}

func Open(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	for _, name := range []string{"attachments", "backups", "logs"} {
		if e := os.MkdirAll(filepath.Join(dir, name), 0700); e != nil {
			return nil, e
		}
	}
	release, e := lockDirectory(filepath.Join(dir, "workspace.lock"))
	if e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", filepath.Join(dir, "projectboard-v2.sqlite"))
	if e != nil {
		release()
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db, Dir: dir, release: release}
	fail := func(e error) (*Store, error) { db.Close(); release(); return nil, e }
	var version int
	if e = db.QueryRow(`PRAGMA user_version`).Scan(&version); e != nil {
		return fail(e)
	}
	if version > 1 {
		return fail(fmt.Errorf("请使用更新版本的 ProjectBoard 打开此数据库"))
	}
	if _, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`); e != nil {
		return fail(e)
	}
	if _, e = db.Exec(`CREATE TABLE IF NOT EXISTS workspace(id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL, body BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS admin(id INTEGER PRIMARY KEY CHECK(id=1), password BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS sessions(hash TEXT PRIMARY KEY, csrf TEXT NOT NULL, expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS tokens(id TEXT PRIMARY KEY, project TEXT NOT NULL, name TEXT NOT NULL, hash TEXT UNIQUE NOT NULL, created TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS attachments(id TEXT PRIMARY KEY, project TEXT NOT NULL, body BLOB NOT NULL, created INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS notifications(id TEXT PRIMARY KEY, task TEXT NOT NULL, body TEXT NOT NULL, created TEXT NOT NULL, seen INTEGER NOT NULL DEFAULT 0);
 PRAGMA user_version=1;`); e != nil {
		return fail(e)
	}
	b, _ := json.Marshal(domain.Empty())
	_, e = db.Exec(`INSERT OR IGNORE INTO workspace VALUES(1,0,?)`, b)
	if e != nil {
		return fail(e)
	}
	return s, nil
}
func (s *Store) Close() error { e := s.DB.Close(); s.release(); return e }

// Retain staged uploads for a day so interrupted edits can still be completed.
func (s *Store) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, e := s.Read()
	if e != nil {
		return e
	}
	refs := st.Attachments()
	rows, e := s.DB.Query(`SELECT id,created FROM attachments`)
	if e != nil {
		return e
	}
	staged := map[string]int64{}
	for rows.Next() {
		var id string
		var created int64
		if e = rows.Scan(&id, &created); e != nil {
			rows.Close()
			return e
		}
		staged[id] = created
	}
	if e = rows.Close(); e != nil {
		return e
	}
	files, e := os.ReadDir(filepath.Join(s.Dir, "attachments"))
	if e != nil {
		return e
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, file := range files {
		id := file.Name()
		if file.IsDir() || !domain.ValidID(id) {
			continue
		}
		if _, ok := refs[id]; ok {
			continue
		}
		if created, ok := staged[id]; ok && created > cutoff.Unix() {
			continue
		}
		info, e := file.Info()
		if e != nil {
			return e
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if e = os.Remove(filepath.Join(s.Dir, "attachments", id)); e != nil {
			return e
		}
		if _, e = s.DB.Exec(`DELETE FROM attachments WHERE id=?`, id); e != nil {
			return e
		}
	}
	_, e = s.DB.Exec(`DELETE FROM sessions WHERE expires<?`, time.Now().Unix())
	return e
}
func (s *Store) Read() (domain.State, error) {
	var b []byte
	e := s.DB.QueryRow(`SELECT body FROM workspace WHERE id=1`).Scan(&b)
	var st domain.State
	if e == nil {
		e = json.Unmarshal(b, &st)
	}
	return st, e
}
func (s *Store) Update(expected int64, fn func(*domain.State) error) (domain.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(expected, fn, false)
}
func (s *Store) update(expected int64, fn func(*domain.State) error, restoring bool) (domain.State, error) {
	st, e := s.Read()
	if e != nil {
		return st, e
	}
	if st.Revision != expected {
		return st, ErrConflict
	}
	old := domain.Clone(st)
	if e = fn(&st); e != nil {
		return old, e
	}
	if !restoring {
		prior := map[string]string{}
		for _, n := range old.Nodes {
			prior[n.ID] = n.Project
		}
		for _, n := range st.Nodes {
			if project, ok := prior[n.ID]; ok && project != n.Project {
				return old, fmt.Errorf("请在原项目编辑知识节点")
			}
		}
	}
	if !restoring {
		s.recurring(&st, &old)
	}
	oldTasks := map[string]domain.Task{}
	for _, t := range old.Tasks {
		oldTasks[t.ID] = t
	}
	for i := range st.Projects {
		p := &st.Projects[i]
		next := 1
		if previous := old.Project(p.ID); previous != nil {
			next = max(next, previous.NextTaskNum)
		}
		for _, t := range old.Tasks {
			if t.Project == p.ID {
				next = max(next, t.Num+1)
			}
		}
		for j := range st.Tasks {
			t := &st.Tasks[j]
			if t.Project != p.ID {
				continue
			}
			if previous, exists := oldTasks[t.ID]; exists && !restoring {
				if previous.Project != t.Project {
					return old, fmt.Errorf("请在原项目编辑任务")
				}
				t.Num = previous.Num
			} else if !restoring {
				t.Num = next
				next++
			}
			next = max(next, t.Num+1)
		}
		p.NextTaskNum = next
	}
	if e = st.Validate(); e != nil {
		return old, e
	}
	if e = s.ingest(&st); e != nil {
		return old, e
	}
	st.Revision = old.Revision + 1
	b, e := json.Marshal(st)
	if e != nil {
		return old, e
	}
	res, e := s.DB.Exec(`UPDATE workspace SET body=?,revision=? WHERE id=1 AND revision=?`, b, st.Revision, old.Revision)
	if e != nil {
		return old, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return old, ErrConflict
	}
	return st, nil
}
func (s *Store) Save(st domain.State) (domain.State, error) {
	return s.Update(st.Revision, func(x *domain.State) error { *x = st; return nil })
}
func (s *Store) recurring(st, old *domain.State) {
	for i := 0; i < len(st.Tasks); i++ {
		t := &st.Tasks[i]
		if t.Repeat == "" || t.Deleted != "" {
			continue
		}
		prev := old.Task(t.ID)
		p := st.Project(t.Project)
		if prev == nil || p == nil || t.Deleted != "" || t.Repeat == "" {
			continue
		}
		if prev.RepeatedTo != "" {
			t.RepeatedTo = prev.RepeatedTo
			continue
		}
		done := false
		wasDone := false
		for _, x := range p.Statuses {
			done = done || x.ID == t.Status && x.Kind == "done"
			wasDone = wasDone || x.ID == prev.Status && x.Kind == "done"
		}
		if !done || wasDone {
			continue
		}
		exists := false
		for _, x := range st.Tasks {
			exists = exists || x.RepeatedFrom == t.ID
		}
		if exists {
			continue
		}
		next := domain.Clone(*t)
		next.ID = domain.ID()
		t.RepeatedTo = next.ID
		next.RepeatedTo = ""
		next.RepeatedFrom = t.ID
		next.Created = domain.Now()
		next.Updated = next.Created
		next.Status = p.InitialStatusID
		next.Progress = nil
		next.Notes = []domain.Comment{}
		next.Relations = []domain.Relation{}
		next.ReminderFired = ""
		next.Num = 1
		for _, x := range st.Tasks {
			if x.Project == t.Project && x.Num >= next.Num {
				next.Num = x.Num + 1
			}
		}
		advance := func(v, layout string) string {
			if v == "" {
				return ""
			}
			dt, e := time.ParseInLocation(layout, v, time.Local)
			if e != nil {
				return ""
			}
			switch t.Repeat {
			case "daily":
				dt = dt.AddDate(0, 0, 1)
			case "weekly":
				dt = dt.AddDate(0, 0, 7)
			case "monthly":
				day := dt.Day()
				first := time.Date(dt.Year(), dt.Month()+1, 1, dt.Hour(), dt.Minute(), 0, 0, dt.Location())
				maxDay := first.AddDate(0, 1, -1).Day()
				if day > maxDay {
					day = maxDay
				}
				dt = first.AddDate(0, 0, day-1)
			}
			return dt.Format(layout)
		}
		next.Start = advance(t.Start, "2006-01-02")
		next.Due = advance(t.Due, "2006-01-02")
		next.End = advance(t.End, "2006-01-02")
		next.Reminder = advance(t.Reminder, "2006-01-02T15:04")
		for j := range next.Checks {
			next.Checks[j].Done = false
			next.Checks[j].ID = domain.ID()
		}
		st.Tasks = append(st.Tasks, next)
	}
}
func (s *Store) Tick() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, e := s.Read()
	if e != nil {
		return e
	}
	now := time.Now().Format("2006-01-02T15:04")
	for _, t := range st.Tasks {
		if t.Deleted != "" || t.Reminder == "" || t.Reminder > now {
			continue
		}
		p := st.Project(t.Project)
		if p == nil || p.Archived {
			continue
		}
		done := false
		for _, v := range p.Statuses {
			done = done || v.ID == t.Status && domain.In(v.Kind, "done", "cancelled")
		}
		if done {
			continue
		}
		_, e = s.DB.Exec(`INSERT OR IGNORE INTO notifications(id,task,body,created) VALUES(?,?,?,?)`, t.ID+"/"+t.Reminder, t.ID, t.Title, domain.Now())
		if e != nil {
			return e
		}
	}
	return nil
}
func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (s *Store) PutAttachment(project, name, mime string, r io.Reader) (domain.Attachment, error) {
	b, e := io.ReadAll(io.LimitReader(r, (20<<20)+1))
	if e != nil {
		return domain.Attachment{}, e
	}
	if len(b) > 20<<20 {
		return domain.Attachment{}, fmt.Errorf("请选择 20 MB 以内的附件")
	}
	a := domain.Attachment{ID: domain.ID(), Name: filepath.Base(name), Type: mime, Size: int64(len(b)), SHA256: Hash(b)}
	if a.Name == "." || len(a.Name) > 255 || strings.ContainsAny(a.Name, "\r\n\x00") {
		return a, fmt.Errorf("请检查附件名称")
	}
	if e = s.writeAttachment(project, a, b); e != nil {
		return a, e
	}
	return a, nil
}
func (s *Store) writeAttachment(project string, a domain.Attachment, b []byte) error {
	if !domain.ValidID(a.ID) || a.Size != int64(len(b)) || Hash(b) != a.SHA256 {
		return fmt.Errorf("请检查附件数据")
	}
	path := filepath.Join(s.Dir, "attachments", a.ID)
	if old, e := os.ReadFile(path); e == nil {
		if Hash(old) != a.SHA256 {
			return fmt.Errorf("附件 ID 已使用，请重新上传")
		}
	} else {
		if e = os.WriteFile(path, b, 0600); e != nil {
			return e
		}
	}
	raw, _ := json.Marshal(a)
	_, e := s.DB.Exec(`INSERT OR IGNORE INTO attachments VALUES(?,?,?,?)`, a.ID, project, raw, time.Now().Unix())
	return e
}
func (s *Store) Attachment(project, id string) (domain.Attachment, []byte, error) {
	a, e := s.AttachmentMeta(project, id)
	if e != nil {
		return a, nil, e
	}
	b, e := os.ReadFile(filepath.Join(s.Dir, "attachments", id))
	if e == nil && Hash(b) != a.SHA256 {
		return a, nil, fmt.Errorf("请从备份恢复完整附件")
	}
	return a, b, e
}
func (s *Store) AttachmentMeta(project, id string) (domain.Attachment, error) {
	var raw []byte
	var a domain.Attachment
	if e := s.DB.QueryRow(`SELECT body FROM attachments WHERE id=? AND project=?`, id, project).Scan(&raw); e != nil {
		return a, fmt.Errorf("请选择当前项目附件")
	}
	if e := json.Unmarshal(raw, &a); e != nil {
		return a, e
	}
	return a, nil
}
func (s *Store) ingest(st *domain.State) error {
	var total int64
	for _, a := range st.Attachments() {
		total += a.Size
	}
	if total > 256<<20 {
		return fmt.Errorf("请将工作空间附件总量控制在 256 MB 内")
	}
	for i := range st.Tasks {
		t := &st.Tasks[i]
		groups := [][]domain.Attachment{t.DetailAttachments}
		for _, c := range t.Notes {
			groups = append(groups, c.Attachments)
		}
		for _, group := range groups {
			for j := range group {
				a := &group[j]
				if a.Data != "" || a.SHA256 == "" && a.Size == 0 {
					b, e := base64.StdEncoding.DecodeString(a.Data)
					if e != nil || int64(len(b)) != a.Size {
						return fmt.Errorf("请检查附件编码与大小")
					}
					a.SHA256 = Hash(b)
					a.Data = ""
					if e = s.writeAttachment(t.Project, *a, b); e != nil {
						return e
					}
				}
				stored, e := s.AttachmentMeta(t.Project, a.ID)
				if e != nil {
					return e
				}
				if a.Name != stored.Name || a.Type != stored.Type || a.Size != stored.Size {
					return fmt.Errorf("请重新上传附件")
				}
				*a = stored
			}
		}
	}
	return nil
}

// Backups are logical SQLite snapshots: one read of the committed workspace plus immutable attachment bytes.
// Credentials and sessions are deliberately excluded; restoring retains the current administrator.
func (s *Store) Backup() ([]byte, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.backup() }
func (s *Store) backup() ([]byte, error) {
	st, e := s.Read()
	if e != nil {
		return nil, e
	}
	body, _ := json.Marshal(st)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	manifest := map[string]string{"workspace.json": Hash(body)}
	add := func(name string, b []byte) error {
		f, e := w.Create(name)
		if e == nil {
			_, e = f.Write(b)
		}
		return e
	}
	if e = add("workspace.json", body); e != nil {
		return nil, e
	}
	for id, a := range st.Attachments() {
		b, e := os.ReadFile(filepath.Join(s.Dir, "attachments", id))
		if e != nil {
			return nil, e
		}
		if Hash(b) != a.SHA256 {
			return nil, fmt.Errorf("附件校验失败：%s", a.Name)
		}
		name := "attachments/" + id
		manifest[name] = Hash(b)
		if e = add(name, b); e != nil {
			return nil, e
		}
	}
	m, _ := json.Marshal(map[string]any{"format": 1, "files": manifest, "created": domain.Now()})
	if e = add("manifest.json", m); e != nil {
		return nil, e
	}
	if e = w.Close(); e != nil {
		return nil, e
	}
	return buf.Bytes(), nil
}
func (s *Store) Restore(b []byte, expected int64) (domain.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, e := s.Read()
	if e != nil {
		return current, e
	}
	if current.Revision != expected {
		return current, ErrConflict
	}
	var incoming domain.State
	files := map[string][]byte{}
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		e = json.Unmarshal(b, &incoming)
	} else {
		var z *zip.Reader
		z, e = zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if e == nil {
			var total uint64
			for _, f := range z.File {
				if f.Name != "manifest.json" && f.Name != "workspace.json" && !(strings.HasPrefix(f.Name, "attachments/") && domain.ValidID(strings.TrimPrefix(f.Name, "attachments/"))) {
					e = fmt.Errorf("请使用 ProjectBoard 备份文件")
					break
				}
				total += f.UncompressedSize64
				if total > 512<<20 || f.UncompressedSize64 > 64<<20 || files[f.Name] != nil {
					e = fmt.Errorf("请检查备份大小与文件名")
					break
				}
				r, err := f.Open()
				if err != nil {
					e = err
					break
				}
				files[f.Name], e = io.ReadAll(io.LimitReader(r, 64<<20+1))
				r.Close()
				if e != nil {
					break
				}
			}
		}
		if e == nil {
			var m struct {
				Format int               `json:"format"`
				Files  map[string]string `json:"files"`
			}
			e = json.Unmarshal(files["manifest.json"], &m)
			if e == nil && (m.Format != 1 || len(m.Files) != len(files)-1) {
				e = fmt.Errorf("请检查备份清单")
			}
			if e == nil {
				for name, hash := range m.Files {
					if Hash(files[name]) != hash {
						e = fmt.Errorf("请重新选择完整备份")
						break
					}
				}
			}
			if e == nil {
				e = json.Unmarshal(files["workspace.json"], &incoming)
			}
		}
	}
	if e != nil {
		return current, e
	}
	if e = incoming.Validate(); e != nil {
		return current, e
	}
	for _, t := range incoming.Tasks {
		groups := [][]domain.Attachment{t.DetailAttachments}
		for _, c := range t.Notes {
			groups = append(groups, c.Attachments)
		}
		for _, g := range groups {
			for _, a := range g {
				if a.Data != "" || a.SHA256 == "" && a.Size == 0 {
					continue
				}
				raw, ok := files["attachments/"+a.ID]
				if !ok || Hash(raw) != a.SHA256 || int64(len(raw)) != a.Size {
					return current, fmt.Errorf("请检查备份附件 %s", a.Name)
				}
			}
		}
	}
	backup, e := s.backup()
	if e != nil {
		return current, e
	}
	if e = os.WriteFile(filepath.Join(s.Dir, "backups", "before-restore-"+time.Now().Format("20060102-150405")+"-"+domain.ID()+".zip"), backup, 0600); e != nil {
		return current, e
	}
	// Remap attachment IDs to avoid overwriting any bytes referenced by the current workspace.
	mapping := map[string]domain.Attachment{}
	for i := range incoming.Tasks {
		t := &incoming.Tasks[i]
		groups := [][]domain.Attachment{t.DetailAttachments}
		for _, c := range t.Notes {
			groups = append(groups, c.Attachments)
		}
		for _, g := range groups {
			for j := range g {
				a := &g[j]
				key := t.Project + "/" + a.ID
				if prev, ok := mapping[key]; ok {
					*a = prev
					continue
				}
				raw := files["attachments/"+a.ID]
				if a.Data != "" || a.SHA256 == "" && a.Size == 0 {
					raw, e = base64.StdEncoding.DecodeString(a.Data)
					if e != nil || int64(len(raw)) != a.Size {
						return current, fmt.Errorf("请检查附件数据")
					}
				}
				a.ID = domain.ID()
				a.Data = ""
				a.SHA256 = Hash(raw)
				if e = s.writeAttachment(t.Project, *a, raw); e != nil {
					return current, e
				}
				mapping[key] = *a
			}
		}
	}
	incoming.Revision = current.Revision
	return s.update(current.Revision, func(st *domain.State) error { *st = incoming; return nil }, true)
}
