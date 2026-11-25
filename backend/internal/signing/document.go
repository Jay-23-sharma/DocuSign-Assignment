package signing

import (
	"context"
	"github.com/jackc/pgx/v5"
	"os"
	"signing-app/backend/internal/documents"
)

type Field struct {
	ID int64 `json:"id"`
	documents.Field
}
type Document struct {
	Title  string  `json:"title"`
	Name   string  `json:"recipient_name"`
	Pages  int     `json:"page_count"`
	Fields []Field `json:"fields"`
}

func (s Service) State(ctx context.Context, c Credentials) (state string, err error) {
	err = s.withSession(ctx, c, false, func(tx pgx.Tx, g session) error {
		if g.verified && (g.recipientStatus == "signed" || g.recipientStatus == "declined") {
			state = g.recipientStatus
			return nil
		}
		if !g.open() {
			return closed
		}
		state = "code"
		if g.verified {
			state = "verified"
		}
		return nil
	})
	return
}
func fields(ctx context.Context, tx pgx.Tx, g session) ([]Field, error) {
	rows, e := tx.Query(ctx, `SELECT id,recipient_id,field_type,page_number,x,y,width,height,required FROM document_fields WHERE document_id=$1 AND recipient_id=$2 ORDER BY page_number,y,x,id`, g.doc, g.recipient)
	if e != nil {
		return nil, e
	}
	var f Field
	out := []Field{}
	_, e = pgx.ForEachRow(rows, []any{&f.ID, &f.RecipientID, &f.Type, &f.Page, &f.X, &f.Y, &f.Width, &f.Height, &f.Required}, func() error { out = append(out, f); return nil })
	return out, e
}
func (s Service) Document(ctx context.Context, c Credentials) (out Document, err error) {
	err = s.withSession(ctx, c, true, func(tx pgx.Tx, g session) error {
		if !g.open() {
			return closed
		}
		e := tx.QueryRow(ctx, `SELECT d.title,r.name,f.page_count FROM documents d JOIN document_files f ON f.document_id=d.id AND f.kind='original' JOIN recipients r ON r.document_id=d.id WHERE d.id=$1 AND r.id=$2`, g.doc, g.recipient).Scan(&out.Title, &out.Name, &out.Pages)
		if e != nil {
			return e
		}
		out.Fields, e = fields(ctx, tx, g)
		return e
	})
	return
}
func (s Service) Original(ctx context.Context, c Credentials, view bool) (file *os.File, err error) {
	err = s.withSession(ctx, c, true, func(tx pgx.Tx, g session) error {
		if !g.open() {
			return closed
		}
		var key string
		e := tx.QueryRow(ctx, "SELECT storage_key FROM document_files WHERE document_id=$1 AND kind='original'", g.doc).Scan(&key)
		if e != nil {
			return e
		}
		file, e = os.OpenInRoot(s.Documents.Dir, key)
		if e != nil || !view {
			return e
		}
		tag, e := tx.Exec(ctx, "UPDATE recipients SET first_viewed_at=clock_timestamp(),status='viewed' WHERE id=$1 AND first_viewed_at IS NULL", g.recipient)
		if e != nil || tag.RowsAffected() == 0 {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE documents SET status='viewed' WHERE id=$1 AND status='sent'", g.doc); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "INSERT INTO document_events(document_id,recipient_id,event_type) VALUES($1,$2,'viewed')", g.doc, g.recipient)
		return e
	})
	if err != nil && file != nil {
		file.Close()
		file = nil
	}
	return
}
