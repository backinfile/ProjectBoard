package web

import (
	"embed"
	"io/fs"
)

//go:embed assets/*
var files embed.FS

func Assets() fs.FS { f, _ := fs.Sub(files, "assets"); return f }
