package web

import "embed"

// Dist contains the production React build.
//
//go:embed dist/*
var Dist embed.FS
