package documents

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type SendSettings struct {
	Subject  *string    `json:"email_subject"`
	Message  *string    `json:"email_message"`
	Deadline *time.Time `json:"expires_at"`
}

func (s Store) SaveSettings(ctx context.Context, owner, id int64, change SendSettings, setDeadline bool) error {
	return s.editDraft(ctx, owner, id, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE documents SET email_subject=coalesce($2,email_subject), email_message=coalesce($3,email_message),
   expires_at=CASE WHEN $4 THEN $5::timestamptz ELSE expires_at END WHERE id=$1`, id, change.Subject, change.Message, setDeadline, change.Deadline)
		return err
	})
}
