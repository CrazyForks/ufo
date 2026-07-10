package migrate

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql migrations/migrations.sum
var migrationsFS embed.FS

//go:generate go run genhash.go

const migrationLockKey int64 = 8675309

func Run(ctx context.Context, pool *pgxpool.Pool) error {
	if err := verifyEmbeddedChecksums(); err != nil {
		return err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn for migration lock: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "select pg_advisory_lock($1)", migrationLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(ctx, "select pg_advisory_unlock($1)", migrationLockKey)
	}()

	files, err := listSQLMigrations(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	if _, err := conn.Exec(ctx,
		`create table if not exists schema_migrations (
			name text primary key,
			checksum text not null,
			applied_at timestamptz not null default now()
		)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	for _, name := range files {
		sql, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", name, err)
		}
		checksum := migrationChecksum(sql)

		var appliedChecksum string
		if err := conn.QueryRow(ctx,
			"select checksum from schema_migrations where name = $1", name,
		).Scan(&appliedChecksum); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check migration %q: %w", name, err)
		} else if err == nil {
			if appliedChecksum != checksum {
				return fmt.Errorf("migration %q was modified after it was applied", name)
			}
			continue
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %q: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %q: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			"insert into schema_migrations (name, checksum) values ($1, $2)", name, checksum,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %q: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %q: %w", name, err)
		}
		log.Printf("applied migration %s", name)
	}
	return nil
}
