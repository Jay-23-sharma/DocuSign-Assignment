package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"signing-app/backend/internal/pdf"
)

var ErrNotSigned = errors.New("The document is still awaiting signatures.")
var ErrIntegrity = errors.New("PDF integrity check failed.")

func stageFile(root, key, destination string) ([]byte, error) {
	src, err := os.OpenInRoot(root, key)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	dst, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(dst, hash), src)
	closeErr := dst.Close()
	if err == nil {
		err = closeErr
	}
	return hash.Sum(nil), err
}
func (s Store) marks(ctx context.Context, tx pgx.Tx, id int64, dir string, pages int) ([]pdf.Mark, error) {
	rows, err := tx.Query(ctx, `SELECT id,page_number,x,y,width,height,field_type,text_value,checked_value,signature_method,signature_storage_key,submitted_at
 FROM document_fields WHERE document_id=$1 AND (required OR submitted_at IS NOT NULL) ORDER BY page_number,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pdf.Mark{}
	for rows.Next() {
		var f pdf.Mark
		var fieldID int64
		var method, key *string
		var submitted *time.Time
		if err = rows.Scan(&fieldID, &f.Page, &f.X, &f.Y, &f.Width, &f.Height, &f.Type, &f.Text, &f.Checked, &method, &key, &submitted); err != nil {
			return nil, err
		}
		if submitted == nil || f.Page < 1 || f.Page > pages {
			return nil, errors.New("missing accepted field")
		}
		switch {
		case f.Type == "signature" && method != nil && (*method == "uploaded" || *method == "drawn"):
			if key == nil {
				return nil, errors.New("missing signature image")
			}
			f.Image = fmt.Sprintf("signature-%d.png", fieldID)
			if _, err = stageFile(s.Dir, *key, filepath.Join(dir, f.Image)); err != nil {
				return nil, err
			}
		case f.Type == "checkbox":
			if f.Checked == nil {
				return nil, errors.New("missing checkbox answer")
			}
		default:
			if f.Text == nil {
				return nil, errors.New("missing text answer")
			}
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Complete serializes generation, publication and retries on the same document lock as signing.
// A nil owner is reserved for the internal call after authenticated guest submission.
func (s Store) Complete(parent context.Context, id int64, owner *int64) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var status string
	err = tx.QueryRow(ctx, "SELECT status FROM documents WHERE id=$1 AND ($2::bigint IS NULL OR owner_id=$2) FOR UPDATE", id, owner).Scan(&status)
	if err != nil {
		return err
	}
	if status == "completed" {
		return nil
	}
	if status != "signed" {
		return ErrNotSigned
	}
	var key, name string
	var pages int
	var originalDigest []byte
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT storage_key,original_filename,page_count,sha256,
 EXISTS(SELECT 1 FROM recipients WHERE document_id=$1) AND NOT EXISTS(SELECT 1 FROM recipients WHERE document_id=$1 AND (status<>'signed' OR signed_at IS NULL))
 FROM document_files WHERE document_id=$1 AND kind='original'`, id).Scan(&key, &name, &pages, &originalDigest, &eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return ErrNotSigned
	}

	docDir := filepath.Join(s.Dir, fmt.Sprint(id))
	temp, err := os.MkdirTemp(docDir, ".finalize-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	sum, err := stageFile(s.Dir, key, filepath.Join(temp, "original.pdf"))
	if err != nil {
		return err
	}
	if !bytes.Equal(sum, originalDigest) {
		return ErrIntegrity
	}
	fields, err := s.marks(ctx, tx, id, temp, pages)
	if err != nil {
		return err
	}
	if err = pdf.Render(ctx, temp, pages, fields); err != nil {
		return err
	}
	result := filepath.Join(temp, "result.pdf")
	file, err := os.OpenFile(result, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if err = file.Chmod(0600); err != nil {
		return err
	}
	conf := model.NewStatelessConfiguration()
	conf.Offline = true
	parsed, err := api.ReadAndValidate(ctx, file, conf)
	if err != nil {
		return err
	}
	if parsed.PageCount != pages {
		return ErrIntegrity
	}
	if _, err = file.Seek(0, 0); err != nil {
		return err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	finalKey := filepath.Join(fmt.Sprint(id), "completed.pdf")
	_, err = tx.Exec(ctx, `INSERT INTO document_files(document_id,kind,storage_key,original_filename,size_bytes,page_count,sha256)
 VALUES($1,'completed',$2,$3,$4,$5,$6)`, id, finalKey, "completed-"+name, size, pages, hash.Sum(nil))
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE documents SET status='completed',completed_at=clock_timestamp() WHERE id=$1", id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO document_events(document_id,event_type) VALUES($1,'completed')", id); err != nil {
		return err
	}
	// Only a signed, locked row can reach here. Any existing final path is an orphan
	// from an interrupted publication. Completed rows return above and are never rewritten.
	if err = os.Rename(result, filepath.Join(s.Dir, finalKey)); err != nil {
		return err
	}
	dir, err := os.Open(docDir)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) CompletedFile(ctx context.Context, owner, id int64) (*os.File, string, error) {
	var key, name string
	var expected []byte
	err := s.Pool.QueryRow(ctx, `SELECT f.storage_key,f.original_filename,f.sha256 FROM document_files f JOIN documents d ON d.id=f.document_id
 WHERE d.owner_id=$1 AND d.id=$2 AND d.status='completed' AND f.kind='completed'`, owner, id).Scan(&key, &name, &expected)
	if err != nil {
		return nil, "", err
	}
	file, err := os.OpenInRoot(s.Dir, key)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err == nil && !bytes.Equal(expected, hash.Sum(nil)) {
		err = ErrIntegrity
	}
	if err == nil {
		_, err = file.Seek(0, 0)
	}
	if err != nil {
		file.Close()
		return nil, "", err
	}
	return file, name, nil
}
