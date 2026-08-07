package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/projectboard/projectboard/internal/ops"
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
		fmt.Fprintln(os.Stderr, "usage: projectboard <serve|backup|restore>")
		os.Exit(2)
	}
	handler, err := server.New(server.Config{
		DataDir:           dataDir,
		DatabasePath:      databasePath,
		BootstrapUsername: env("PROJECTBOARD_BOOTSTRAP_USERNAME", "admin"),
		BootstrapPassword: os.Getenv("PROJECTBOARD_BOOTSTRAP_PASSWORD"),
		Production:        env("PROJECTBOARD_ENV", "development") == "production",
		SSHListenAddress:  env("PROJECTBOARD_SSH_LISTEN_ADDRESS", ":2222"),
		SSHPublicHost:     os.Getenv("PROJECTBOARD_SSH_PUBLIC_HOST"),
		SSHPublicPort:     env("PROJECTBOARD_SSH_PUBLIC_PORT", "2222"),
		SSHHostKeyPath:    os.Getenv("PROJECTBOARD_SSH_HOST_KEY_PATH"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer handler.Close()
	if err = handler.StartSSH(); err != nil {
		log.Fatal(err)
	}
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

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
