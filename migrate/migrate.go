package migrate

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

func TableExists(name string, conn *pgx.Conn) bool {
	var exists bool
	err := conn.QueryRow(
		context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND 
		tablename = $1);`,
		name,
	).
		Scan(&exists)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT EXISTS failed: %v\n", err)
		return false
	}
	return exists
}

func DbExists() bool {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		return false
	}
	defer conn.Close(context.Background())
	return true
}

func CreateDB() error {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to PostgreSQL: %v\n", err)
		return err
	}
	defer conn.Close(context.Background())
	_, err = conn.Exec(context.Background(), "CREATE DATABASE muzi")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create muzi database: %v\n", err)
		return err
	}
	return nil
}
