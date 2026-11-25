package signing

import (
	"context"
	"log"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type Answer struct {
	ID        int64   `json:"id"`
	Text      *string `json:"text,omitempty"`
	Checked   *bool   `json:"checked,omitempty"`
	Method    *string `json:"method,omitempty"`
	Reference *string `json:"reference,omitempty"`
}

var invalidAnswer = Problem{422, "Check the assigned fields. Complete every required answer and check each required checkbox."}

func (s Service) value(g session, f Field, a Answer) (key *string, err error) {
	if f.Type == "checkbox" {
		if a.Text != nil || a.Method != nil || a.Reference != nil || a.Checked == nil || (f.Required && !*a.Checked) {
			return nil, invalidAnswer
		}
		return nil, nil
	}
	if a.Checked != nil {
		return nil, invalidAnswer
	}
	if f.Type == "signature" {
		if a.Method == nil {
			return nil, invalidAnswer
		}
		if *a.Method != "typed" {
			return s.imageAnswer(g, a)
		}
	} else if a.Method != nil {
		return nil, invalidAnswer
	}
	if a.Reference != nil || a.Text == nil || strings.TrimSpace(*a.Text) == "" || strings.ContainsRune(*a.Text, 0) || utf8.RuneCountInString(*a.Text) > 2000 {
		return nil, invalidAnswer
	}
	switch f.Type {
	case "date":
		if _, e := time.Parse("2006-01-02", *a.Text); e != nil {
			return nil, invalidAnswer
		}
	case "email":
		addr, e := mail.ParseAddress(*a.Text)
		if e != nil || addr.Address != *a.Text || len(*a.Text) > 254 {
			return nil, invalidAnswer
		}
	}
	return nil, nil
}
func (s Service) Submit(ctx context.Context, c Credentials, answers []Answer) error {
	var finalizeID int64
	err := s.withSession(ctx, c, true, func(tx pgx.Tx, g session) error {
		if g.recipientStatus == "signed" {
			if g.status == "signed" {
				finalizeID = g.doc
			}
			return nil
		}
		if !g.open() {
			return closed
		}
		assigned, e := fields(ctx, tx, g)
		if e != nil {
			return e
		}
		provided := make(map[int64]Answer, len(answers))
		for _, a := range answers {
			if _, dup := provided[a.ID]; dup {
				return invalidAnswer
			}
			provided[a.ID] = a
		}
		keys := map[int64]*string{}
		for _, f := range assigned {
			a, ok := provided[f.ID]
			if !ok {
				if f.Required {
					return invalidAnswer
				}
				continue
			}
			keys[f.ID], e = s.value(g, f, a)
			if e != nil {
				return e
			}
			delete(provided, f.ID)
		}
		if len(provided) > 0 {
			return invalidAnswer
		}
		for _, a := range answers {
			_, e = tx.Exec(ctx, `UPDATE document_fields SET text_value=$1,checked_value=$2,signature_method=$3,signature_storage_key=$4,submitted_at=clock_timestamp() WHERE id=$5 AND recipient_id=$6 AND document_id=$7`, a.Text, a.Checked, a.Method, keys[a.ID], a.ID, g.recipient, g.doc)
			if e != nil {
				return e
			}
		}
		if err := decide(ctx, tx, g, true); err != nil {
			return err
		}
		var ready bool
		if err := tx.QueryRow(ctx, "SELECT status='signed' FROM documents WHERE id=$1", g.doc).Scan(&ready); err != nil {
			return err
		}
		if ready {
			finalizeID = g.doc
		}
		return nil
	})
	if err == nil && finalizeID != 0 {
		// A rendering outage must not undo a committed recipient decision. The sender can retry.
		if renderErr := s.Documents.Complete(context.WithoutCancel(ctx), finalizeID, nil); renderErr != nil {
			log.Printf("completed PDF generation pending for document %d (%T)", finalizeID, renderErr)
		}
	}
	return err
}
func (s Service) Decline(ctx context.Context, c Credentials) error {
	return s.withSession(ctx, c, true, func(tx pgx.Tx, g session) error {
		if !g.open() {
			return closed
		}
		return decide(ctx, tx, g, false)
	})
}
func decide(ctx context.Context, tx pgx.Tx, g session, sign bool) error {
	status := "declined"
	if sign {
		status = "signed"
	}
	// Check the three clocks again after validation and before accepting the decision.
	tag, e := tx.Exec(ctx, `UPDATE recipients r SET status=$1,
 signed_at=CASE WHEN $1='signed' THEN clock_timestamp() ELSE signed_at END,
 declined_at=CASE WHEN $1='declined' THEN clock_timestamp() ELSE declined_at END
 FROM documents d, signing_sessions ss,signing_links l WHERE r.id=$2 AND d.id=r.document_id AND ss.id=$3 AND l.id=ss.signing_link_id
 AND least(d.expires_at,ss.expires_at,l.expires_at)>clock_timestamp()`, status, g.recipient, g.id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return unavailable
	}
	_, e = tx.Exec(ctx, `UPDATE documents SET status=$1 WHERE id=$2 AND ($1='declined' OR NOT EXISTS(SELECT 1 FROM recipients WHERE document_id=$2 AND status<>'signed'))`, status, g.doc)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO document_events(document_id,recipient_id,event_type) VALUES($1,$2,$3)", g.doc, g.recipient, status)
	return e
}
