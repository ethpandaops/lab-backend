package web

import (
	"embed"
	"io/fs"
)

//go:embed all:frontend/*
var embeddedFiles embed.FS

// GetFS returns the embedded filesystem, with "frontend" prefix stripped.
// In production (Docker), this contains the Lab frontend files.
// In development, this will be empty (allowing fallback to local fs).
func GetFS() (fs.FS, error) {
	return fs.Sub(embeddedFiles, "frontend")
}

// Exists checks if embedded files exist.
// Returns true in production (files embedded), false in dev (empty embed).
// Used to determine dev vs prod mode.
func Exists() bool {
	entries, err := embeddedFiles.ReadDir("frontend")
	if err != nil {
		return false
	}

	// If directory is empty, consider it non-existent
	return len(entries) > 0
}
