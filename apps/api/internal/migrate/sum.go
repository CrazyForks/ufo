package migrate

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/zeebo/blake3"
)

const checksumsFileName = "migrations.sum"

func migrationChecksum(sql []byte) string {
	sum := blake3.Sum256(sql)
	return "blake3:" + hex.EncodeToString(sum[:])
}

func listSQLMigrations(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func computeChecksums(fsys fs.FS, dir string) (map[string]string, error) {
	files, err := listSQLMigrations(fsys, dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(files))
	for _, name := range files {
		sql, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return nil, err
		}
		out[name] = migrationChecksum(sql)
	}
	return out, nil
}

func formatChecksumsFile(sums map[string]string) []byte {
	names := make([]string, 0, len(sums))
	for name := range sums {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte(' ')
		b.WriteString(sums[name])
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func parseChecksumsFile(data []byte) (map[string]string, error) {
	out := make(map[string]string)
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, sum, ok := strings.Cut(line, " ")
		if !ok || name == "" || sum == "" || strings.Contains(name, " ") {
			return nil, fmt.Errorf("invalid checksums line %q", line)
		}
		sum = strings.TrimSpace(sum)
		if !strings.HasPrefix(sum, "blake3:") {
			return nil, fmt.Errorf("invalid checksum for %q", name)
		}
		if _, exists := out[name]; exists {
			return nil, fmt.Errorf("duplicate checksum entry %q", name)
		}
		out[name] = sum
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func verifyEmbeddedChecksums() error {
	got, err := computeChecksums(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	raw, err := fs.ReadFile(migrationsFS, "migrations/"+checksumsFileName)
	if err != nil {
		return fmt.Errorf("read %s: %w (run: go generate ./internal/migrate)", checksumsFileName, err)
	}
	want, err := parseChecksumsFile(raw)
	if err != nil {
		return fmt.Errorf("parse %s: %w", checksumsFileName, err)
	}
	if len(got) != len(want) {
		return fmt.Errorf("%s out of date: %d sql files, %d checksum entries (run: go generate ./internal/migrate)",
			checksumsFileName, len(got), len(want))
	}
	for name, sum := range got {
		w, ok := want[name]
		if !ok {
			return fmt.Errorf("%s missing entry for %q (run: go generate ./internal/migrate)", checksumsFileName, name)
		}
		if w != sum {
			return fmt.Errorf("%s mismatch for %q (run: go generate ./internal/migrate)", checksumsFileName, name)
		}
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			return fmt.Errorf("%s has extra entry %q (run: go generate ./internal/migrate)", checksumsFileName, name)
		}
	}
	return nil
}
