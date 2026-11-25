package signing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"signing-app/backend/internal/documents"
)

type Problem struct {
	Status  int
	Message string
}

func (p Problem) Error() string { return p.Message }

var unavailable = Problem{403, "Signing access is closed or this invitation is no longer available. Open your current invitation."}
var closed = Problem{409, "Signing is closed for this recipient or document."}
var needCode = Problem{403, "Verify the email code first."}

type Service struct {
	Documents documents.Store
	CodeKey   []byte
}
type Credentials struct {
	Cookie   string
	Expected int64
}
type session struct {
	id, link, recipient, doc       int64
	email, status, recipientStatus string
	verified                       bool
}
type Ticket struct {
	ID     int64
	Secret string
	Expiry time.Time
}

func digest(s string) []byte { sum := sha256.Sum256([]byte(s)); return sum[:] }
func secret() string {
	var b [32]byte
	rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func validSecret(s string) bool {
	b, e := base64.RawURLEncoding.Strict().DecodeString(s)
	return e == nil && len(b) == 32 && len(s) == 43
}
func accessError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return unavailable
	}
	return err
}

// Lock the document first, then read current eligibility in a fresh READ COMMITTED statement.
// Release, resend, submit, decline and first-view transitions all serialize on this row.
func (s Service) Exchange(ctx context.Context, token string) (out Ticket, err error) {
	if !validSecret(token) {
		return out, unavailable
	}
	var rejected bool
	err = pgx.BeginFunc(ctx, s.Documents.Pool, func(tx pgx.Tx) error {
		var doc int64
		hash := digest(token)
		e := tx.QueryRow(ctx, `SELECT d.id FROM documents d JOIN recipients r ON r.document_id=d.id JOIN signing_links l ON l.recipient_id=r.id WHERE l.token_hash=$1 FOR UPDATE OF d`, hash).Scan(&doc)
		if e != nil {
			return accessError(e)
		}
		out.Secret = secret()
		e = tx.QueryRow(ctx, `INSERT INTO signing_sessions(signing_link_id,session_token_hash,expires_at)
 SELECT l.id,$2,least(l.expires_at,d.expires_at,clock_timestamp()+interval '2 hours') FROM signing_links l
 JOIN recipients r ON r.id=l.recipient_id JOIN documents d ON d.id=r.document_id
 WHERE l.token_hash=$1 AND l.revoked_at IS NULL AND l.expires_at>clock_timestamp() AND d.expires_at>clock_timestamp()
 AND d.status IN ('sent','viewed') AND r.status IN ('pending','viewed') RETURNING id,expires_at`, hash, digest(out.Secret)).Scan(&out.ID, &out.Expiry)
		if errors.Is(e, pgx.ErrNoRows) {
			rejected = true
			_, e = documents.ExpireLocked(ctx, tx, doc)
		}
		return e
	})
	if err == nil && rejected {
		err = unavailable
	}
	return
}
func (g session) open() bool {
	return (g.status == "sent" || g.status == "viewed") && (g.recipientStatus == "pending" || g.recipientStatus == "viewed")
}
func (s Service) withSession(ctx context.Context, c Credentials, verified bool, fn func(pgx.Tx, session) error) error {
	if c.Expected <= 0 || !validSecret(c.Cookie) {
		return unavailable
	}
	var rejection error
	err := pgx.BeginFunc(ctx, s.Documents.Pool, func(tx pgx.Tx) error {
		var g session
		hash := digest(c.Cookie)
		e := tx.QueryRow(ctx, `SELECT d.id FROM documents d JOIN recipients r ON r.document_id=d.id JOIN signing_links l ON l.recipient_id=r.id
 JOIN signing_sessions ss ON ss.signing_link_id=l.id WHERE ss.session_token_hash=$1 AND ss.id=$2 FOR UPDATE OF d`, hash, c.Expected).Scan(&g.doc)
		if e != nil {
			return accessError(e)
		}
		var beforeDeadline, validAccess bool
		e = tx.QueryRow(ctx, `SELECT ss.id,l.id,r.id,r.email,d.status,r.status,ss.email_verified_at IS NOT NULL,
 d.expires_at>clock_timestamp(), ss.expires_at>clock_timestamp() AND l.revoked_at IS NULL AND l.expires_at>clock_timestamp()
 FROM signing_sessions ss JOIN signing_links l ON l.id=ss.signing_link_id JOIN recipients r ON r.id=l.recipient_id JOIN documents d ON d.id=r.document_id
 WHERE ss.id=$1 AND d.id=$2`, c.Expected, g.doc).Scan(&g.id, &g.link, &g.recipient, &g.email, &g.status, &g.recipientStatus, &g.verified, &beforeDeadline, &validAccess)
		if e != nil {
			return accessError(e)
		}
		if !beforeDeadline {
			if _, e = documents.ExpireLocked(ctx, tx, g.doc); e != nil {
				return e
			}
			rejection = unavailable
			if validAccess && g.verified && (g.status == "expired" || g.open()) {
				rejection = Problem{403, "Signing has closed. This document has expired."}
			}
			return nil
		}
		if !validAccess {
			return unavailable
		}
		if verified && !g.verified {
			return needCode
		}
		// A decision can cross the deadline during validation. Roll back its writes
		// before committing the independently valid expiration transition.
		e = pgx.BeginFunc(ctx, tx, func(action pgx.Tx) error { return fn(action, g) })
		if errors.Is(e, unavailable) {
			expired, err := documents.ExpireLocked(ctx, tx, g.doc)
			if err != nil {
				return err
			}
			if expired {
				rejection = unavailable
				return nil
			}
		}
		if errors.Is(e, closed) && g.verified {
			switch g.status {
			case "voided", "declined", "completed":
				return Problem{409, "Signing has closed. This document is " + g.status + "."}
			}
		}
		return e
	})
	if err != nil {
		return err
	}
	return rejection
}
