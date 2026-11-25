package documents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommitOutcome(t *testing.T) {
	for _, c := range []struct {
		err        error
		rolledBack bool
	}{
		{pgx.ErrTxCommitRollback, true}, {&pgconn.PgError{Code: "23503"}, true},
		{&pgconn.PgError{Code: "40001"}, true}, {&pgconn.PgError{Code: "40003"}, false},
		{errors.New("connection lost"), false},
	} {
		if confirmedRollback(c.err) != c.rolledBack {
			t.Fatalf("wrong classification for %v", c.err)
		}
	}
}

func TestFileCleanupOnDeferredConstraintFailure(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL for PostgreSQL checks")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("invalid database configuration")
	}
	// Session-local tables force PostgreSQL to reject COMMIT after file placement.
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE documents (LIKE public.documents INCLUDING ALL);
 CREATE TEMP TABLE document_files (LIKE public.document_files INCLUDING ALL);
 CREATE TEMP TABLE permitted_names(name text PRIMARY KEY);
 ALTER TABLE document_files ADD FOREIGN KEY(original_filename) REFERENCES permitted_names(name) DEFERRABLE INITIALLY DEFERRED`)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(pool, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(store.Dir, "staged")
	if err := os.WriteFile(staged, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = store.Save(ctx, 1, "title", staged, Original{Filename: "test.pdf", Size: 7, Pages: 1}, make([]byte, 32))
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("expected deferred failure, got %v", err)
	}
	entries, err := os.ReadDir(store.Dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "staged" {
		t.Fatal("published file survived rollback", err)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM documents)+(SELECT count(*) FROM document_files)`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("rows survived rollback", err)
	}
}
