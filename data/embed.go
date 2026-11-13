// Package data holds files embedded into the rockets executable.
package data

import (
	"embed"
	"io/fs"
)

//go:embed migrations
var embedded embed.FS

// Migrations returns the production migration set embedded in the executable.
func Migrations() fs.FS {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic(err) // the directory is embedded at compile time
	}
	return sub
}
