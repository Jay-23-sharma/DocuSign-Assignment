package documents

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Field struct {
	RecipientID int64   `json:"recipient_id"`
	Type        string  `json:"field_type"`
	Page        int     `json:"page_number"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	Required    bool    `json:"required"`
}

var ErrLayout = errors.New("Invalid fields. Check the field type, page, position, size and Required setting.")
var ErrRecipient = errors.New("A recipient is no longer available on this document.")

// Keep decimal coordinates in their original JSON representation for the exact numeric checks and insert.
const inputFields = `jsonb_to_recordset($2::jsonb) AS f(recipient_id bigint,field_type text,page_number integer,
 x numeric,y numeric,width numeric,height numeric,required boolean)`

func (s Store) SaveFields(ctx context.Context, owner, id int64, raw json.RawMessage) error {
	return s.editDraft(ctx, owner, id, func(tx pgx.Tx) error {
		var fields []Field
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return ErrLayout
		}
		var unrelated, invalid bool
		err := tx.QueryRow(ctx, `SELECT coalesce(bool_or(r.id IS NULL),false),
    coalesce(bool_or((f.field_type IN ('signature','initials','name','date','email','text','checkbox')
       AND f.page_number BETWEEN 1 AND pdf.page_count AND f.x>=0 AND f.y>=0 AND f.width>0 AND f.height>0
       AND f.x+f.width<=1 AND f.y+f.height<=1 AND f.required IS NOT NULL) IS NOT TRUE),false)
    FROM `+inputFields+`
    LEFT JOIN recipients r ON r.document_id=$1 AND r.id=f.recipient_id
    LEFT JOIN document_files pdf ON pdf.document_id=$1 AND pdf.kind='original'`, id, raw).Scan(&unrelated, &invalid)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && len(pgErr.Code) >= 2 && pgErr.Code[:2] == "22" {
				return ErrLayout
			}
			return err
		}
		if unrelated {
			return ErrRecipient
		}
		if invalid {
			return ErrLayout
		}
		if _, err = tx.Exec(ctx, "DELETE FROM document_fields WHERE document_id=$1", id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO document_fields(document_id,recipient_id,field_type,page_number,x,y,width,height,required)
   SELECT $1,f.recipient_id,f.field_type,f.page_number,f.x,f.y,f.width,f.height,f.required FROM `+inputFields, id, raw)
		return err
	})
}
