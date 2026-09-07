package server

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"projectboard/internal/domain"
	"projectboard/internal/store"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Version = "1.0.1"

type Server struct {
	Store       *store.Store
	Assets      fs.FS
	SetupToken  string
	Instance    string
	AllowedHost string
	mu          sync.Mutex
	attempts    map[string][]time.Time
	plans       map[string]DeletePlan
	mcp         http.Handler
	Shutdown    func()
}

func Secret() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func New(st *store.Store, assets fs.FS) *Server {
	s := &Server{Store: st, Assets: assets, SetupToken: Secret(), Instance: Secret(), attempts: map[string][]time.Time{}, plans: map[string]DeletePlan{}}
	s.mcp = s.mcpHandler()
	return s
}
func (s *Server) Initialized() bool {
	var n int
	_ = s.Store.DB.QueryRow(`SELECT COUNT(*) FROM admin`).Scan(&n)
	return n > 0
}
func jsonReply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, msg string) {
	jsonReply(w, code, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	if e := d.Decode(v); e != nil {
		return fmt.Errorf("请检查请求内容与大小")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("请提供一个 JSON 对象")
	}
	return nil
}
func (s *Server) session(r *http.Request) (string, string, bool) {
	c, e := r.Cookie("pb_session")
	if e != nil {
		return "", "", false
	}
	h := store.Hash([]byte(c.Value))
	var csrf string
	e = s.Store.DB.QueryRow(`SELECT csrf FROM sessions WHERE hash=? AND expires>?`, h, time.Now().Unix()).Scan(&csrf)
	return h, csrf, e == nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	host, _, e := net.SplitHostPort(r.Host)
	if e != nil {
		host = r.Host
	}
	allowed := host == "localhost" || net.ParseIP(host) != nil
	if s.AllowedHost != "" {
		allowed = strings.EqualFold(r.Host, s.AllowedHost)
	}
	if !allowed {
		fail(w, 403, "请通过本地服务地址访问")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if e != nil || u.Host != r.Host || u.Scheme != scheme {
			fail(w, 403, "请在当前应用页面操作")
			return
		}
	}
	if r.URL.Path == "/api/health" && r.Method == "GET" {
		jsonReply(w, 200, map[string]any{"app": "ProjectBoard", "version": Version, "instance": s.Instance})
		return
	}
	if r.URL.Path == "/api/mcp" {
		s.mcp.ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api/session" && r.Method == "GET" {
		_, csrf, ok := s.session(r)
		jsonReply(w, 200, map[string]any{"initialized": s.Initialized(), "authenticated": ok, "csrf": csrf, "version": Version})
		return
	}
	if r.URL.Path == "/api/setup" || r.URL.Path == "/api/login" {
		if r.Method != "POST" || r.Header.Get("X-Requested-With") != "ProjectBoard" {
			fail(w, 403, "请通过登录页面继续")
			return
		}
		s.login(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		hash, csrf, ok := s.session(r)
		if !ok {
			fail(w, 401, "请登录工作空间")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) != 1 {
				fail(w, 403, "请刷新页面后继续")
				return
			}
		}
		switch r.URL.Path {
		case "/api/shutdown":
			if r.Method == "POST" {
				jsonReply(w, 202, map[string]bool{"ok": true})
				if s.Shutdown != nil {
					time.AfterFunc(100*time.Millisecond, s.Shutdown)
				}
				return
			}
		case "/api/revision":
			if r.Method == "GET" {
				var rev int64
				if e := s.Store.DB.QueryRow(`SELECT revision FROM workspace WHERE id=1`).Scan(&rev); e != nil {
					fail(w, 500, "请重新连接工作空间")
				} else {
					jsonReply(w, 200, map[string]int64{"revision": rev})
				}
				return
			}
		case "/api/logout":
			if r.Method == "POST" {
				_, _ = s.Store.DB.Exec(`DELETE FROM sessions WHERE hash=?`, hash)
				http.SetCookie(w, &http.Cookie{Name: "pb_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
				jsonReply(w, 200, map[string]bool{"ok": true})
				return
			}
		case "/api/password":
			if r.Method == "POST" {
				var v struct {
					Old      string `json:"old"`
					Password string `json:"password"`
				}
				if e := decode(w, r, &v, 4096); e != nil {
					fail(w, 400, e.Error())
					return
				}
				if !s.checkPassword(v.Old) {
					fail(w, 400, "请重新输入当前密码")
					return
				}
				if e := s.ResetPassword(v.Password); e != nil {
					fail(w, 400, e.Error())
					return
				}
				s.issueSession(w, r)
				return
			}
		case "/api/state":
			if r.Method == "GET" {
				st, e := s.Store.Read()
				if e != nil {
					fail(w, 500, "请重新打开工作空间")
				} else {
					jsonReply(w, 200, st)
				}
				return
			}
			if r.Method == "PUT" {
				var st domain.State
				if e := decode(w, r, &st, 64<<20); e != nil {
					fail(w, 400, e.Error())
					return
				}
				next, e := s.Store.Save(st)
				s.stateResult(w, next, e)
				return
			}
		case "/api/backup":
			if r.Method == "GET" {
				b, e := s.Store.Backup()
				if e != nil {
					fail(w, 500, e.Error())
					return
				}
				w.Header().Set("Content-Type", "application/zip")
				w.Header().Set("Content-Disposition", `attachment; filename="projectboard-backup.zip"`)
				w.Write(b)
				return
			}
		case "/api/restore":
			if r.Method == "POST" {
				rev, e := strconv.ParseInt(r.Header.Get("X-Revision"), 10, 64)
				if e != nil {
					fail(w, 400, "请刷新后重新导入")
					return
				}
				b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 512<<20))
				if e != nil {
					fail(w, 400, "请选择 512 MB 以内的备份")
					return
				}
				st, e := s.Store.Restore(b, rev)
				s.stateResult(w, st, e)
				return
			}
		case "/api/attachments":
			s.attachment(w, r)
			return
		case "/api/tokens":
			s.tokens(w, r)
			return
		case "/api/tools":
			if r.Method == "GET" {
				jsonReply(w, 200, ToolDefinitions())
				return
			}
		case "/api/call":
			if r.Method == "POST" {
				var v struct {
					Tool string         `json:"tool"`
					Args map[string]any `json:"args"`
				}
				if e := decode(w, r, &v, 32<<20); e != nil {
					fail(w, 400, e.Error())
					return
				}
				project, _ := v.Args["project_id"].(string)
				result, e := s.Call(project, v.Tool, v.Args)
				if e != nil {
					fail(w, 400, e.Error())
				} else {
					jsonReply(w, 200, result)
				}
				return
			}
		case "/api/notifications":
			if r.Method == "GET" {
				_ = s.Store.Tick()
				rows, e := s.Store.DB.Query(`SELECT id,task,body,created FROM notifications WHERE seen=0 ORDER BY created DESC LIMIT 100`)
				if e != nil {
					fail(w, 500, "请重试读取提醒")
					return
				}
				defer rows.Close()
				out := []map[string]string{}
				for rows.Next() {
					var id, task, body, at string
					rows.Scan(&id, &task, &body, &at)
					out = append(out, map[string]string{"id": id, "task": task, "text": body, "at": at})
				}
				jsonReply(w, 200, out)
				return
			}
			if r.Method == "POST" {
				var v struct {
					ID string `json:"id"`
				}
				if e := decode(w, r, &v, 4096); e != nil {
					fail(w, 400, e.Error())
					return
				}
				_, e := s.Store.DB.Exec(`UPDATE notifications SET seen=1 WHERE id=?`, v.ID)
				if e != nil {
					fail(w, 500, "请重试")
				} else {
					jsonReply(w, 200, map[string]bool{"ok": true})
				}
				return
			}
		case "/api/info":
			if r.Method == "GET" {
				jsonReply(w, 200, map[string]any{"version": Version, "dataDir": s.Store.Dir, "attachmentLimit": 20 << 20})
				return
			}
		}
		fail(w, 404, "请选择有效操作")
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "请使用 GET 访问页面")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if !fs.ValidPath(path) {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(path, "projects/") {
		http.Redirect(w, r, "/#/"+path, http.StatusTemporaryRedirect)
		return
	}
	b, e := fs.ReadFile(s.Assets, path)
	if e != nil {
		if strings.HasPrefix(path, "projects/") {
			b, e = fs.ReadFile(s.Assets, "index.html")
		}
		if e != nil {
			http.NotFound(w, r)
			return
		}
	}
	typ := mime.TypeByExtension("." + strings.Split(path, ".")[len(strings.Split(path, "."))-1])
	if strings.HasPrefix(path, "projects/") {
		typ = "text/html; charset=utf-8"
	}
	w.Header().Set("Content-Type", typ)
	w.Write(b)
}
func (s *Server) stateResult(w http.ResponseWriter, st domain.State, e error) {
	if e != nil {
		code := 400
		if errors.Is(e, store.ErrConflict) {
			code = 409
		}
		fail(w, code, e.Error())
		return
	}
	jsonReply(w, 200, st)
}
func (s *Server) checkPassword(pass string) bool {
	var hash []byte
	if e := s.Store.DB.QueryRow(`SELECT password FROM admin WHERE id=1`).Scan(&hash); e != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword(hash, []byte(pass)) == nil
}
func passwordHash(pass string) ([]byte, error) {
	if len(pass) < 8 || len(pass) > 72 {
		return nil, fmt.Errorf("请设置 8–72 字节的密码")
	}
	return bcrypt.GenerateFromPassword([]byte(pass), 12)
}
func (s *Server) ResetPassword(pass string) error {
	hash, e := passwordHash(pass)
	if e != nil {
		return e
	}
	tx, e := s.Store.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`INSERT INTO admin VALUES(1,?) ON CONFLICT(id) DO UPDATE SET password=excluded.password`, hash); e != nil {
		return e
	}
	if _, e = tx.Exec(`DELETE FROM sessions`); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.mu.Lock()
	recent := []time.Time{}
	for _, t := range s.attempts[host] {
		if time.Since(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= 10 {
		s.mu.Unlock()
		fail(w, 429, "请稍后再试")
		return
	}
	s.attempts[host] = append(recent, time.Now())
	s.mu.Unlock()
	var v struct {
		Password string `json:"password"`
		Token    string `json:"token"`
	}
	if e := decode(w, r, &v, 4096); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if r.URL.Path == "/api/setup" {
		if subtle.ConstantTimeCompare([]byte(v.Token), []byte(s.SetupToken)) != 1 {
			fail(w, 403, "请使用启动窗口中的初始化链接")
			return
		}
		hash, e := passwordHash(v.Password)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
		if _, e = s.Store.DB.Exec(`INSERT INTO admin VALUES(1,?)`, hash); e != nil {
			fail(w, 409, "管理员已就绪，请登录")
			return
		}
	} else if !s.checkPassword(v.Password) {
		fail(w, 401, "请重新输入密码")
		return
	}
	s.issueSession(w, r)
}
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request) {
	token, csrf := Secret(), Secret()
	_, e := s.Store.DB.Exec(`INSERT INTO sessions VALUES(?,?,?)`, store.Hash([]byte(token)), csrf, time.Now().Add(30*24*time.Hour).Unix())
	if e != nil {
		fail(w, 500, "请重新登录")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "pb_session", Value: token, Path: "/", MaxAge: 30 * 86400, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
	jsonReply(w, 200, map[string]string{"csrf": csrf})
}
func (s *Server) attachment(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	st, e := s.Store.Read()
	if e != nil || st.Project(project) == nil {
		fail(w, 400, "请选择项目")
		return
	}
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, (20<<20)+(1<<20))
		if e = r.ParseMultipartForm(1 << 20); e != nil {
			fail(w, 400, "请选择 20 MB 以内的文件")
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, h, e := r.FormFile("file")
		if e != nil {
			fail(w, 400, "请选择文件")
			return
		}
		defer f.Close()
		a, e := s.Store.PutAttachment(project, h.Filename, h.Header.Get("Content-Type"), f)
		if e != nil {
			fail(w, 400, e.Error())
		} else {
			jsonReply(w, 200, a)
		}
		return
	}
	if r.Method == "GET" {
		a, b, e := s.Store.Attachment(project, r.URL.Query().Get("id"))
		if e != nil {
			fail(w, 404, "请重新选择附件")
			return
		}
		if r.URL.Query().Get("preview") == "1" {
			v, e := PreviewAttachment(a, b)
			if e != nil {
				fail(w, 400, e.Error())
			} else {
				jsonReply(w, 200, v)
			}
			return
		}
		typ := "application/octet-stream"
		disposition := "attachment"
		if r.URL.Query().Get("inline") == "1" && domain.In(a.Type, "image/png", "image/jpeg", "image/gif", "image/webp") {
			typ = a.Type
			disposition = "inline"
		}
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.Name}))
		w.Write(b)
		return
	}
	fail(w, 405, "请选择附件操作")
}
func (s *Server) tokens(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	st, e := s.Store.Read()
	if e != nil || st.Project(project) == nil {
		fail(w, 400, "请选择项目")
		return
	}
	switch r.Method {
	case "GET":
		rows, e := s.Store.DB.Query(`SELECT id,name,created FROM tokens WHERE project=?`, project)
		if e != nil {
			fail(w, 500, "请重试读取接入令牌")
			return
		}
		defer rows.Close()
		out := []map[string]string{}
		for rows.Next() {
			var id, name, at string
			rows.Scan(&id, &name, &at)
			out = append(out, map[string]string{"id": id, "name": name, "created": at})
		}
		jsonReply(w, 200, out)
	case "POST":
		var v struct {
			Name string `json:"name"`
		}
		if e := decode(w, r, &v, 4096); e != nil {
			fail(w, 400, e.Error())
			return
		}
		if strings.TrimSpace(v.Name) == "" {
			v.Name = "本地客户端"
		}
		id, token := domain.ID(), Secret()
		_, e = s.Store.DB.Exec(`INSERT INTO tokens VALUES(?,?,?,?,?)`, id, project, v.Name, store.Hash([]byte(token)), domain.Now())
		if e != nil {
			fail(w, 500, "请重试创建令牌")
		} else {
			jsonReply(w, 201, map[string]string{"id": id, "token": token, "project": project})
		}
	case "DELETE":
		_, e = s.Store.DB.Exec(`DELETE FROM tokens WHERE id=? AND project=?`, r.URL.Query().Get("id"), project)
		if e != nil {
			fail(w, 500, "请重试撤销令牌")
		} else {
			jsonReply(w, 200, map[string]bool{"ok": true})
		}
	default:
		fail(w, 405, "请选择令牌操作")
	}
}
func (s *Server) TokenProject(token string) (string, error) {
	var project string
	e := s.Store.DB.QueryRow(`SELECT project FROM tokens WHERE hash=?`, store.Hash([]byte(token))).Scan(&project)
	if e == sql.ErrNoRows {
		return "", fmt.Errorf("请使用有效的项目令牌")
	}
	return project, e
}
