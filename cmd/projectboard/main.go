package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/term"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"projectboard/internal/server"
	"projectboard/internal/store"
	"projectboard/web"
	"runtime"
	"strings"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		return stdio(os.Args[2:])
	}
	flags := flag.NewFlagSet("projectboard", flag.ContinueOnError)
	base, _ := os.UserCacheDir()
	dir := flags.String("data-dir", filepath.Join(base, "ProjectBoard"), "数据目录")
	listen := flags.String("listen", "127.0.0.1:7331", "监听地址")
	noBrowser := flags.Bool("no-browser", false, "手动打开浏览器")
	cert := flags.String("tls-cert", "", "HTTPS 证书")
	key := flags.String("tls-key", "", "HTTPS 私钥")
	allowed := flags.String("host", "", "允许访问的 Host（例如 board.local:7331）")
	reset := flags.Bool("reset-password", false, "在本机重设管理员密码")
	version := flags.Bool("version", false, "版本")
	if e := flags.Parse(os.Args[1:]); e != nil {
		return e
	}
	if *version {
		fmt.Println(server.Version)
		return nil
	}
	absolute, e := filepath.Abs(*dir)
	if e != nil {
		return e
	}
	if *reset {
		st, e := store.Open(absolute)
		if e != nil {
			return e
		}
		defer st.Close()
		fmt.Fprint(os.Stderr, "新密码：")
		b, e := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if e != nil {
			return e
		}
		s := server.New(st, web.Assets())
		return s.ResetPassword(string(b))
	}
	scheme := "http"
	if *cert != "" || *key != "" {
		if *cert == "" || *key == "" {
			return fmt.Errorf("请同时提供 --tls-cert 与 --tls-key")
		}
		scheme = "https"
	}
	ln, e := net.Listen("tcp", *listen)
	if e != nil {
		client := &http.Client{Timeout: time.Second}
		address := scheme + "://" + *listen
		resp, err := client.Get(address + "/api/health")
		if err == nil {
			defer resp.Body.Close()
			var v map[string]string
			json.NewDecoder(resp.Body).Decode(&v)
			instance, _ := os.ReadFile(filepath.Join(absolute, "instance"))
			if v["app"] == "ProjectBoard" && v["instance"] == string(instance) {
				if token, e := os.ReadFile(filepath.Join(absolute, "initialization-token")); e == nil && len(token) > 0 {
					address += "/#setup/" + string(token)
				}
				if !*noBrowser {
					openBrowser(address)
				}
				fmt.Println("ProjectBoard 已运行：", address)
				return nil
			}
		}
		return fmt.Errorf("端口 %s 已占用，请使用 --listen 127.0.0.1:7332：%w", *listen, e)
	}
	defer ln.Close()
	st, e := store.Open(absolute)
	if e != nil {
		return e
	}
	defer st.Close()
	s := server.New(st, web.Assets())
	s.AllowedHost = *allowed
	if e = os.WriteFile(filepath.Join(absolute, "instance"), []byte(s.Instance), 0600); e != nil {
		return e
	}
	if !s.Initialized() {
		if e = os.WriteFile(filepath.Join(absolute, "initialization-token"), []byte(s.SetupToken), 0600); e != nil {
			return e
		}
	} else {
		_ = os.Remove(filepath.Join(absolute, "initialization-token"))
	}
	logPath := filepath.Join(absolute, "logs", "server.log")
	if info, e := os.Stat(logPath); e == nil && info.Size() > 5<<20 {
		_ = os.Remove(logPath + ".1")
		_ = os.Rename(logPath, logPath+".1")
	}
	logFile, e := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer logFile.Close()
	logger := log.New(io.MultiWriter(os.Stderr, logFile), "", log.LstdFlags)
	httpServer := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20, ErrorLog: logger}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	s.Shutdown = stop
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		if e := st.Cleanup(); e != nil {
			logger.Printf("附件整理：%v", e)
		}
		ticks := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ticks++
				if ticks%240 == 0 {
					if e := st.Cleanup(); e != nil {
						logger.Printf("附件整理：%v", e)
					}
				}
				if e := st.Tick(); e != nil {
					logger.Printf("提醒检查：%v", e)
				}
			}
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	address := scheme + "://" + net.JoinHostPort(host, port)
	if *allowed != "" {
		address = scheme + "://" + *allowed
	}
	entry := address
	if !s.Initialized() {
		entry += "/#setup/" + s.SetupToken
	}
	fmt.Println("ProjectBoard", server.Version, "\n数据目录：", absolute, "\n打开：", entry, "\n按 Ctrl+C 停止服务")
	logger.Printf("启动 %s", address)
	if !*noBrowser {
		go openBrowser(entry)
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdown)
	}()
	if scheme == "https" {
		e = httpServer.ServeTLS(ln, *cert, *key)
	} else {
		e = httpServer.Serve(ln)
	}
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
func openBrowser(address string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", address)
	case "darwin":
		cmd = exec.Command("open", address)
	default:
		cmd = exec.Command("xdg-open", address)
	}
	_ = cmd.Run()
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r)
}
func stdio(args []string) error {
	f := flag.NewFlagSet("mcp", flag.ContinueOnError)
	project := f.String("project", "", "项目 ID")
	endpoint := f.String("url", "http://127.0.0.1:7331/api/mcp", "本地 MCP 地址")
	if e := f.Parse(args); e != nil {
		return e
	}
	token := strings.TrimSpace(os.Getenv("PROJECTBOARD_TOKEN"))
	if *project == "" || token == "" {
		return fmt.Errorf("请设置 --project 和 PROJECTBOARD_TOKEN")
	}
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "ProjectBoard stdio", Version: server.Version}, nil)
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: *endpoint, HTTPClient: &http.Client{Transport: bearerTransport{token, http.DefaultTransport}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		return e
	}
	defer session.Close()
	defs, e := session.ListTools(ctx, nil)
	if e != nil {
		return e
	}
	proxy := mcp.NewServer(&mcp.Implementation{Name: "ProjectBoard", Version: server.Version}, nil)
	for _, def := range defs.Tools {
		name := def.Name
		proxy.AddTool(def, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a map[string]any
			if e := json.Unmarshal(r.Params.Arguments, &a); e != nil {
				return nil, e
			}
			if a["project_id"] != *project {
				return nil, fmt.Errorf("PROJECT_SCOPE_MISMATCH")
			}
			return session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: a})
		})
	}
	return proxy.Run(ctx, &mcp.StdioTransport{})
}
