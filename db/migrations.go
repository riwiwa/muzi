package db

// Versioned migrations for changes that must run exactly once, like data fixes. Table and
// column creation stays in CreateAllTables, which is idempotent and runs on every start.
//
// To add one, append it to migrations with the next version number. Never edit or renumber a
// migration that has shipped. Each runs in its own transaction and is recorded in
// schema_migrations, so they apply once even if releases merge them out of order.

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

type migration struct {
	version int
	name    string
	run     func(ctx context.Context, tx pgx.Tx) error
}

var migrations = []migration{
	{1, "mark images that existed before automatic artwork as custom", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE artists SET image_source = 'custom' WHERE image_url IS NOT NULL AND image_source IS NULL;
			UPDATE albums SET cover_source = 'custom' WHERE cover_url IS NOT NULL AND cover_source IS NULL;`)
		return err
	}},
}

// Applies any migrations that haven't run yet, in version order
func RunMigrations() error {
	return runMigrations(context.Background(), migrations)
}

func runMigrations(ctx context.Context, list []migration) error {
	_, err := Pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := Pool.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	seen := map[int]bool{}
	for _, m := range list {
		if seen[m.version] {
			return fmt.Errorf("duplicate migration version %d", m.version)
		}
		seen[m.version] = true
		if applied[m.version] {
			continue
		}

		tx, err := Pool.Begin(ctx)
		if err != nil {
			return err
		}
		if err := m.run(ctx, tx); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", m.version, m.name); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Applied migration %d: %s\n", m.version, m.name)
	}
	return nil
}
