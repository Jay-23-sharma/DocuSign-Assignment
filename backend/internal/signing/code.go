package signing

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
)

func (s Service) codeDigest(id int64, code string) []byte {
	mac := hmac.New(sha256.New, s.CodeKey)
	fmt.Fprintf(mac, "%d:%s", id, code)
	return mac.Sum(nil)
}

var limited = Problem{429, "Code limit reached. Wait before requesting or checking another code."}
var wrong = Problem{422, "Invalid or expired code."}

// Code expiration is always issuance + ten minutes. This gives us an issuance
// window even after consumption, without adding a timestamp to the existing schema.
const recentCodes = ` FROM signing_email_codes c JOIN signing_sessions ss ON ss.id=c.session_id
 WHERE ss.signing_link_id=$1 AND c.expires_at>clock_timestamp()-interval '50 minutes'`

func (s Service) IssueCode(ctx context.Context, c Credentials) (email, code string, err error) {
	err = s.withSession(ctx, c, false, func(tx pgx.Tx, g session) error {
		if !g.open() || g.verified {
			return closed
		}
		var count, attempts int
		var cooldown bool
		e := tx.QueryRow(ctx, `SELECT count(*),coalesce(sum(attempt_count),0),coalesce(max(c.expires_at)>clock_timestamp()+interval '9 minutes',false)`+recentCodes, g.link).Scan(&count, &attempts, &cooldown)
		if e != nil {
			return e
		}
		if cooldown || count >= 5 || attempts >= 15 {
			return limited
		}
		n, e := rand.Int(rand.Reader, big.NewInt(1000000))
		if e != nil {
			return e
		}
		code = fmt.Sprintf("%06d", n)
		email = g.email
		if _, e = tx.Exec(ctx, `UPDATE signing_email_codes c SET used_at=clock_timestamp() FROM signing_sessions ss WHERE ss.id=c.session_id AND ss.signing_link_id=$1 AND c.used_at IS NULL`, g.link); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "INSERT INTO signing_email_codes(session_id,code_digest,expires_at) VALUES($1,$2,clock_timestamp()+interval '10 minutes')", g.id, s.codeDigest(g.id, code))
		return e
	})
	return
}
func (s Service) VerifyCode(ctx context.Context, c Credentials, code string) error {
	var rejected bool
	err := s.withSession(ctx, c, false, func(tx pgx.Tx, g session) error {
		if !g.open() {
			return closed
		}
		if g.verified {
			return nil
		}
		var tries int
		e := tx.QueryRow(ctx, "SELECT coalesce(sum(attempt_count),0)"+recentCodes, g.link).Scan(&tries)
		if e != nil {
			return e
		}
		if tries >= 15 {
			return limited
		}
		var id int64
		var hash []byte
		e = tx.QueryRow(ctx, `UPDATE signing_email_codes SET attempt_count=attempt_count+1 WHERE session_id=$1 AND used_at IS NULL AND expires_at>clock_timestamp() AND attempt_count<5 RETURNING id,code_digest`, g.id).Scan(&id, &hash)
		if e == pgx.ErrNoRows {
			return wrong
		}
		if e != nil {
			return e
		}
		if !hmac.Equal(hash, s.codeDigest(g.id, code)) {
			rejected = true
			return nil
		}
		if _, e = tx.Exec(ctx, "UPDATE signing_email_codes SET used_at=clock_timestamp() WHERE id=$1", id); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE signing_sessions SET email_verified_at=clock_timestamp() WHERE id=$1", g.id)
		return e
	})
	if err == nil && rejected {
		return wrong
	}
	return err
}
