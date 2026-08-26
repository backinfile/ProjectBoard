package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/projectboard/projectboard/internal/agent"
	"github.com/projectboard/projectboard/internal/events"
	"github.com/projectboard/projectboard/internal/runmanager"
	"github.com/projectboard/projectboard/internal/server"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workspace"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:5173", "HTTP listen address")
	dataDir := flag.String("data-dir", defaultDataDir(), "ProjectBoard data directory")
	openBrowser := flag.Bool("open", true, "open the browser after startup")
	flag.Parse()
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	dbPath := filepath.Join(*dataDir, "projectboard.db")
	st, err := store.Open(context.Background(), dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	appServer := agent.AppServer{}
	if err := appServer.Check(context.Background()); err != nil {
		log.Fatal(err)
	}
	bus := events.New()
	wm := workspace.NewManager(filepath.Join(*dataDir, "worktrees"))
	url := "http://" + *listen
	runs := runmanager.New(st, bus, wm, appServer, 10, url+"/mcp")
	srv := &http.Server{Addr: *listen, Handler: server.NewWithRuntime(st, bus, wm, runs, *listen).Handler(), ReadHeaderTimeout: 10 * time.Second}
	if *openBrowser {
		go func() { time.Sleep(450 * time.Millisecond); _ = launchBrowser(url) }()
	}
	fmt.Printf("ProjectBoard 已启动：%s\n数据目录：%s\n", url, *dataDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func defaultDataDir() string {
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		return filepath.Join(v, "ProjectBoard")
	}
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "ProjectBoard")
}
func launchBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
