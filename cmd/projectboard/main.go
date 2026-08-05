package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/projectboard/projectboard/internal/mcpstdio"
	"github.com/projectboard/projectboard/internal/ops"
	"github.com/projectboard/projectboard/internal/providers"
	"github.com/projectboard/projectboard/internal/server"
)

func main() {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	dataDir := env("PROJECTBOARD_DATA_DIR", "./data")
	databasePath := env("PROJECTBOARD_DB", filepath.Join(dataDir, "projectboard.db"))
	switch command {
	case "mcp":
		if err := mcpstdio.RunEnvironment(); err != nil {
			log.Fatal(err)
		}
		return
	case "backup":
		if len(os.Args) != 3 {
			log.Fatal("usage: projectboard backup <destination.db>")
		}
		if err := ops.Backup(databasePath, os.Args[2]); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("backup written to %s\n", os.Args[2])
		return
	case "restore":
		if len(os.Args) != 3 {
			log.Fatal("usage: projectboard restore <backup.db>")
		}
		if err := ops.Restore(databasePath, os.Args[2]); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("database restored from %s\n", os.Args[2])
		return
	case "serve":
	default:
		fmt.Fprintln(os.Stderr, "usage: projectboard <serve|mcp|backup|restore>")
		os.Exit(2)
	}
	handler, err := server.New(server.Config{
		DataDir:           dataDir,
		DatabasePath:      databasePath,
		BootstrapUsername: env("PROJECTBOARD_BOOTSTRAP_USERNAME", "admin"),
		BootstrapPassword: os.Getenv("PROJECTBOARD_BOOTSTRAP_PASSWORD"),
		Production:        env("PROJECTBOARD_ENV", "development") == "production",
		GitConnect: providers.GitConnectConfig{
			PublicURL:           strings.TrimRight(os.Getenv("PROJECTBOARD_PUBLIC_URL"), "/"),
			GitHubAppID:         os.Getenv("PROJECTBOARD_GITHUB_APP_ID"),
			GitHubAppSlug:       os.Getenv("PROJECTBOARD_GITHUB_APP_SLUG"),
			GitHubPrivateKey:    readSecretFile(os.Getenv("PROJECTBOARD_GITHUB_PRIVATE_KEY_FILE")),
			GitHubWebhookURL:    env("PROJECTBOARD_GITHUB_WEBHOOK_URL", strings.TrimRight(os.Getenv("PROJECTBOARD_PUBLIC_URL"), "/")+"/api/git/webhooks/github"),
			GitHubWebhookSecret: os.Getenv("PROJECTBOARD_GITHUB_WEBHOOK_SECRET"),
			GitLabClientID:      os.Getenv("PROJECTBOARD_GITLAB_CLIENT_ID"),
			GitLabClientSecret:  os.Getenv("PROJECTBOARD_GITLAB_CLIENT_SECRET"),
			GitLabBaseURL:       env("PROJECTBOARD_GITLAB_BASE_URL", "https://gitlab.com"),
			GitLabWebhookSecret: os.Getenv("PROJECTBOARD_GITLAB_WEBHOOK_SECRET"),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer handler.Close()
	address := ":" + env("PORT", "3333")
	log.Printf("ProjectBoard listening on http://localhost%s", address)
	httpServer := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errors := make(chan error, 1)
	go func() { errors <- httpServer.ListenAndServe() }()
	select {
	case err = <-errors:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err = httpServer.Shutdown(shutdown); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
	}
}

func readSecretFile(file string) string {
	if file == "" {
		return ""
	}
	data, err := os.ReadFile(file)
	if err != nil {
		log.Printf("cannot read configured secret file %s: %v", file, err)
		return ""
	}
	return string(data)
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
