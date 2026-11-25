package documents

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLayoutRollbackAfterDelete(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL for PostgreSQL checks")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	cfg.MaxConns = 1
	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// A session-private constraint rejects the INSERT only after layout validation and deletion.
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE documents(id bigint,owner_id bigint,status text);
 CREATE TEMP TABLE recipients(id bigint, document_id bigint);
 CREATE TEMP TABLE document_files(document_id bigint,kind text,page_count integer);
 CREATE TEMP TABLE document_fields (LIKE public.document_fields INCLUDING ALL);
 ALTER TABLE document_fields ADD CHECK (field_type <> 'checkbox');
 INSERT INTO documents VALUES(1,1,'draft');
 INSERT INTO recipients VALUES(1,1);
 INSERT INTO document_files VALUES(1,'original',2);
 INSERT INTO document_fields(document_id,recipient_id,field_type,page_number,x,y,width,height)
 VALUES(1,1,'signature',1,0,0,0.2,0.1)`)
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Pool: pool}
	err = store.SaveFields(ctx, 1, 1, json.RawMessage(`[{"recipient_id":1,"field_type":"checkbox","page_number":2,"x":0,"y":0,"width":0.2,"height":0.1,"required":true}]`))
	var failed *pgconn.PgError
	if !errors.As(err, &failed) || failed.Code != "23514" {
		t.Fatalf("expected insert rejection: %v", err)
	}
	var kind string
	if err := pool.QueryRow(ctx, "SELECT field_type FROM document_fields WHERE id=1").Scan(&kind); err != nil || kind != "signature" {
		t.Fatal("original field lost", err)
	}
}
