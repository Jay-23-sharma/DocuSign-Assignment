package documents

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Invitation is an in-memory handoff from a committed transaction to SMTP.
type Invitation struct {
	DocumentID, RecipientID, LinkID int64
	Email, Subject, Message         string
	Deadline                        time.Time
	Token                           string `json:"-"`
}

type ReleaseProblem struct{ Reason string }

func (e ReleaseProblem) Error() string { return e.Reason }

var ErrSigningClosed = errors.New("Signing is closed for this recipient or document.")

func issueLink(ctx context.Context, tx pgx.Tx, owner int64, invite *Invitation) error {
	var secret [32]byte
	rand.Read(secret[:])
	invite.Token = base64.RawURLEncoding.EncodeToString(secret[:])
	digest := sha256.Sum256([]byte(invite.Token))
	err := tx.QueryRow(ctx, `INSERT INTO signing_links(recipient_id,token_hash,expires_at)
 SELECT $1,$2,$3::timestamptz WHERE $3::timestamptz>clock_timestamp() RETURNING id`, invite.RecipientID, digest[:], invite.Deadline).Scan(&invite.LinkID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSigningClosed
	}
	if err != nil {
		return err
	}
	// This marker resets only this recipient's displayed outcome, atomically with issuance.
	_, err = tx.Exec(ctx, `INSERT INTO document_events(document_id,recipient_id,actor_user_id,event_type)
 VALUES($1,$2,$3,'invitation_issued')`, invite.DocumentID, invite.RecipientID, owner)
	return err
}

func (s Store) Release(ctx context.Context, owner, id int64) ([]Invitation, error) {
	var batch []Invitation
	err := s.editDraft(ctx, owner, id, func(tx pgx.Tx) error {
		var subject, message string
		var deadline *time.Time
		var hasPDF, future bool
		err := tx.QueryRow(ctx, `SELECT coalesce(email_subject,''),coalesce(email_message,''),expires_at,
    coalesce(expires_at>clock_timestamp(),false),
    EXISTS(SELECT 1 FROM document_files WHERE document_id=$1 AND kind='original')
    FROM documents WHERE id=$1`, id).Scan(&subject, &message, &deadline, &future, &hasPDF)
		if err != nil {
			return err
		}
		if !hasPDF {
			return ReleaseProblem{"Upload an original PDF before sending."}
		}
		if strings.TrimSpace(subject) == "" || strings.ContainsAny(subject, "\r\n") {
			return ReleaseProblem{"Enter a nonblank, single-line subject."}
		}
		if !future {
			return ReleaseProblem{"Choose a signing deadline in the future."}
		}
		rows, err := tx.Query(ctx, `SELECT r.id,r.email,r.name,EXISTS(SELECT 1 FROM document_fields f WHERE f.document_id=r.document_id
    AND f.recipient_id=r.id AND f.required AND f.field_type='signature')
    FROM recipients r WHERE r.document_id=$1 ORDER BY r.id`, id)
		if err != nil {
			return err
		}
		var missing []string
		var rid int64
		var address, name string
		var required bool
		_, err = pgx.ForEachRow(rows, []any{&rid, &address, &name, &required}, func() error {
			if !required {
				missing = append(missing, name+" ("+address+")")
			}
			batch = append(batch, Invitation{DocumentID: id, RecipientID: rid, Email: address, Subject: subject, Message: message, Deadline: *deadline})
			return nil
		})
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return ReleaseProblem{"Add at least one recipient."}
		}
		if len(missing) > 0 {
			return ReleaseProblem{"Add a required signature for " + strings.Join(missing, ", ") + "."}
		}
		for i := range batch {
			if err := issueLink(ctx, tx, owner, &batch[i]); err != nil {
				return err
			}
		}
		result, err := tx.Exec(ctx, "UPDATE documents SET status='sent', sent_at=clock_timestamp() WHERE id=$1 AND expires_at>clock_timestamp()", id)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ReleaseProblem{"Choose a signing deadline in the future."}
		}
		_, err = tx.Exec(ctx, "INSERT INTO document_events(document_id,actor_user_id,event_type) VALUES($1,$2,'sent')", id, owner)
		return err
	})
	if err != nil {
		return nil, err
	}
	return batch, nil
}

func (s Store) ReplaceInvitation(ctx context.Context, owner, id, rid int64) (Invitation, error) {
	invite := Invitation{DocumentID: id, RecipientID: rid}
	var rejected bool
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var open bool
		var deadline *time.Time
		err := tx.QueryRow(ctx, `SELECT status IN ('sent','viewed') AND coalesce(expires_at>clock_timestamp(),false),
    coalesce(email_subject,''),coalesce(email_message,''),expires_at FROM documents WHERE owner_id=$1 AND id=$2 FOR UPDATE`,
			owner, id).Scan(&open, &invite.Subject, &invite.Message, &deadline)
		if err != nil {
			return err
		}
		var pending bool
		err = tx.QueryRow(ctx, `SELECT email,status IN ('pending','viewed') FROM recipients WHERE document_id=$1 AND id=$2 FOR UPDATE`, id, rid).Scan(&invite.Email, &pending)
		if err != nil {
			return err
		}
		if !open || !pending {
			rejected = true
			_, err = ExpireLocked(ctx, tx, id)
			return err
		}
		invite.Deadline = *deadline
		if _, err = tx.Exec(ctx, `UPDATE signing_links SET revoked_at=clock_timestamp() WHERE recipient_id=$1 AND revoked_at IS NULL`, rid); err != nil {
			return err
		}
		return issueLink(ctx, tx, owner, &invite)
	})
	if rejected && err == nil {
		err = ErrSigningClosed
	}
	if err != nil {
		return Invitation{}, err
	}
	return invite, nil
}

// Record only an outcome for the link that is still current. Resend takes the same document lock.
func (s Store) RecordDelivery(ctx context.Context, invite Invitation, accepted bool) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT id FROM documents WHERE id=$1 FOR UPDATE", invite.DocumentID); err != nil {
			return err
		}
		event := "invitation_failed"
		if accepted {
			event = "invitation_accepted"
		}
		_, err := tx.Exec(ctx, `INSERT INTO document_events(document_id,recipient_id,event_type)
    SELECT r.document_id,r.id,$4 FROM recipients r JOIN signing_links l ON l.recipient_id=r.id
    WHERE r.document_id=$1 AND r.id=$2 AND l.id=$3 AND l.revoked_at IS NULL`, invite.DocumentID, invite.RecipientID, invite.LinkID, event)
		return err
	})
}
