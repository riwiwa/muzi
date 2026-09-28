package db

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRunMigrations(t *testing.T) {
	useTestDB(t)
	ctx := context.Background()
	Pool.Exec(ctx, "DROP TABLE IF EXISTS migration_test; DELETE FROM schema_migrations WHERE version >= 9000")
	t.Cleanup(func() {
		Pool.Exec(ctx, "DROP TABLE IF EXISTS migration_test; DELETE FROM schema_migrations WHERE version >= 9000")
	})

	runs := 0
	create := migration{9001, "create test table", func(ctx context.Context, tx pgx.Tx) error {
		runs++
		_, err := tx.Exec(ctx, "CREATE TABLE migration_test (n INT)")
		return err
	}}
	for i := 0; i < 2; i++ {
		if err := runMigrations(ctx, []migration{create}); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 1 {
		t.Errorf("migration ran %d times, want once", runs)
	}

	// a failing migration rolls back its changes and isn't recorded
	failing := migration{9002, "insert then fail", func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO migration_test VALUES (1)"); err != nil {
			return err
		}
		return errors.New("boom")
	}}
	if err := runMigrations(ctx, []migration{create, failing}); err == nil {
		t.Fatal("expected the failing migration to return an error")
	}
	var rowsLeft, recorded int
	Pool.QueryRow(ctx, "SELECT count(*) FROM migration_test").Scan(&rowsLeft)
	Pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE version = 9002").Scan(&recorded)
	if rowsLeft != 0 || recorded != 0 {
		t.Errorf("failed migration left %d rows and %d records; want 0 and 0", rowsLeft, recorded)
	}

	if err := runMigrations(ctx, []migration{create, create}); err == nil {
		t.Error("expected an error for duplicate versions")
	}
}
