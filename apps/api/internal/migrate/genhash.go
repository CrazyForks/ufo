//go:build ignore

package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zeebo/blake3"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	migrationsDir := filepath.Join(dir, "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		fail(err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	var b strings.Builder
	for _, name := range files {
		sql, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			fail(err)
		}
		sum := blake3.Sum256(sql)
		fmt.Fprintf(&b, "%s blake3:%s\n", name, hex.EncodeToString(sum[:]))
	}
	path := filepath.Join(migrationsDir, "migrations.sum")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fail(err)
	}
	fmt.Println("wrote", path)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "migrate hash: %v\n", err)
	os.Exit(1)
}
