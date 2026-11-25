package documents

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrCannotVoid = errors.New("Only an outstanding request can be voided.")

// ExpireLocked checks the wall clock after the caller has acquired the document lock.
func ExpireLocked(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	tag, err := tx.Exec(ctx, `WITH changed AS (
 UPDATE documents SET status='expired' WHERE id=$1 AND status IN ('sent','viewed')
 AND expires_at<=clock_timestamp() RETURNING id)
 INSERT INTO document_events(document_id,event_type,occurred_at)
 SELECT id,'expired',clock_timestamp() FROM changed`, id)
	return tag.RowsAffected() > 0, err
}

func (s Store) Void(ctx context.Context, owner, id int64) error {
	var rejected bool
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, "SELECT status FROM documents WHERE id=$1 AND owner_id=$2 FOR UPDATE", id, owner).Scan(&status); err != nil {
			return err
		}
		if status == "voided" {
			return nil
		}
		tag, err := tx.Exec(ctx, `WITH changed AS (UPDATE documents SET status='voided' WHERE id=$1 AND status IN ('sent','viewed') AND expires_at>clock_timestamp() RETURNING id)
 INSERT INTO document_events(document_id,actor_user_id,event_type,occurred_at) SELECT id,$2,'voided',clock_timestamp() FROM changed`, id, owner)
		if err != nil || tag.RowsAffected() == 1 {
			return err
		}
		rejected = true
		_, err = ExpireLocked(ctx, tx, id)
		return err
	})
	if err == nil && rejected {
		return ErrCannotVoid
	}
	return err
}

// Sweep in short transactions. Other workers and active decisions can finish independently.
func (s Store) ExpireOverdue(ctx context.Context) error {
	for {
		tag, err := s.Pool.Exec(ctx, `WITH due AS (SELECT id FROM documents WHERE status IN ('sent','viewed')
 AND expires_at<=clock_timestamp() ORDER BY expires_at,id LIMIT 100 FOR UPDATE SKIP LOCKED),
 changed AS (UPDATE documents d SET status='expired' FROM due WHERE d.id=due.id AND d.status IN ('sent','viewed')
 AND d.expires_at<=clock_timestamp() RETURNING d.id)
 INSERT INTO document_events(document_id,event_type,occurred_at) SELECT id,'expired',clock_timestamp() FROM changed`)
		if err != nil || tag.RowsAffected() < 100 {
			return err
		}
	}
}
