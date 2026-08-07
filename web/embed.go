package web

import "embed"

// Files contains the production UI. Keeping the assets next to this package lets
// the Go compiler produce a self-contained ProjectBoard binary.
//
//go:embed index.html agent-execution.md assets/*
var Files embed.FS
