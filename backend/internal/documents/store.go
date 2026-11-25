package documents

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Pool     *pgxpool.Pool
	Dir      string
	MaxBytes int64
}

const Overhead = 64 << 10

func NewStore(pool *pgxpool.Pool, dir, size string) (Store, error) {
	if dir == "" {
		dir = "../storage/documents"
	}
	limit := int64(20 << 20)
	if size != "" {
		var err error
		limit, err = strconv.ParseInt(size, 10, 64)
		if err != nil || limit <= 0 || limit > math.MaxInt64-Overhead {
			return Store{}, errors.New("PDF_MAX_BYTES must be a positive byte limit")
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Store{}, err
	}
	return Store{pool, dir, limit}, nil
}

type Draft struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type Original struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size_bytes"`
	Pages    int    `json:"page_count"`
}

func (s Store) Save(ctx context.Context, owner int64, title, tempPath string, file Original, digest []byte) (Draft, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Draft{}, err
	}
	defer tx.Rollback(context.Background())
	var draft Draft
	err = tx.QueryRow(ctx, `INSERT INTO documents(owner_id,title) VALUES($1,$2) RETURNING id,title,status`, owner, title).Scan(&draft.ID, &draft.Title, &draft.Status)
	if err != nil {
		return Draft{}, err
	}
	key := fmt.Sprintf("%d/original.pdf", draft.ID)
	_, err = tx.Exec(ctx, `INSERT INTO document_files(document_id,kind,storage_key,original_filename,size_bytes,page_count,sha256)
 VALUES($1,'original',$2,$3,$4,$5,$6)`, draft.ID, key, file.Filename, file.Size, file.Pages, digest)
	if err != nil {
		return Draft{}, err
	}
	final := filepath.Join(s.Dir, key)
	dir := filepath.Dir(final)
	if err = os.Mkdir(dir, 0700); err != nil {
		return Draft{}, err
	}
	// Publish the staged inode atomically without replacing another file. The caller unlinks the temporary name.
	if err = os.Link(tempPath, final); err != nil {
		os.Remove(dir)
		return Draft{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		if confirmedRollback(err) {
			os.Remove(final)
			os.Remove(dir)
		}
		// A transport error can hide a successful COMMIT; retain the file in that case.
		return Draft{}, err
	}
	return draft, nil
}

// Only discard the published file when PostgreSQL confirms a rollback.
// In particular, SQLSTATE 40003 means the statement outcome is unknown.
func confirmedRollback(err error) bool {
	if errors.Is(err, pgx.ErrTxCommitRollback) {
		return true
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return strings.HasPrefix(pgErr.Code, "23") || (strings.HasPrefix(pgErr.Code, "40") && pgErr.Code != "40003")
}
