package documents

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type Recipient struct {
	Status   string `json:"status,omitempty"`
	Delivery string `json:"delivery_status,omitempty"`
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

var ErrNotDraft = errors.New("This document is no longer a draft.")

func (s Store) editDraft(ctx context.Context, owner, id int64, edit func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, "SELECT status FROM documents WHERE owner_id=$1 AND id=$2 FOR UPDATE", owner, id).Scan(&status)
		if err != nil {
			return err
		}
		if status != "draft" {
			return ErrNotDraft
		}
		return edit(tx)
	})
}

func (s Store) SaveRecipient(ctx context.Context, owner, id, recipientID int64, name, email *string) (Recipient, error) {
	var recipient Recipient
	err := s.editDraft(ctx, owner, id, func(tx pgx.Tx) error {
		var row pgx.Row
		if recipientID == 0 {
			row = tx.QueryRow(ctx, `INSERT INTO recipients(document_id,name,email) VALUES($1,$2,$3) RETURNING id,name,email`, id, name, email)
		} else {
			row = tx.QueryRow(ctx, `UPDATE recipients SET name=coalesce($3,name), email=coalesce($4,email)
    WHERE document_id=$1 AND id=$2 RETURNING id,name,email`, id, recipientID, name, email)
		}
		return row.Scan(&recipient.ID, &recipient.Name, &recipient.Email)
	})
	return recipient, err
}

var ErrFieldCountChanged = errors.New("The saved fields changed. Review the deletion again.")

func (s Store) DeleteRecipient(ctx context.Context, owner, id, recipientID int64, expectedCount *int64) error {
	return s.editDraft(ctx, owner, id, func(tx pgx.Tx) error {
		removed, err := tx.Exec(ctx, "DELETE FROM document_fields WHERE document_id=$1 AND recipient_id=$2", id, recipientID)
		if err != nil {
			return err
		}
		result, err := tx.Exec(ctx, "DELETE FROM recipients WHERE document_id=$1 AND id=$2", id, recipientID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if expectedCount != nil && removed.RowsAffected() != *expectedCount {
			return ErrFieldCountChanged
		}
		return nil
	})
}
