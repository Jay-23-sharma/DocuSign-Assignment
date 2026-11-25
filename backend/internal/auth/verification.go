package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Verification carries the committed invitation only as far as the mail sender.
type Verification struct{ Email, Token string }

func newToken(ctx context.Context, tx pgx.Tx, userID int64) (string, error) {
	var secret [32]byte
	rand.Read(secret[:])
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	digest := sha256.Sum256([]byte(token))
	_, err := tx.Exec(ctx, `INSERT INTO email_verification_tokens (user_id,token_hash,expires_at)
        VALUES ($1,$2,now() + interval '24 hours')`, userID, digest[:])
	return token, err
}

var ErrInvalidLink = errors.New("invalid verification link")

func Verify(ctx context.Context, pool *pgxpool.Pool, token string) error {
	digest := sha256.Sum256([]byte(token))
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		// Both verify and resend take the user lock before changing tokens.
		var userID int64
		err := tx.QueryRow(ctx, `SELECT u.id FROM users u JOIN email_verification_tokens t ON t.user_id=u.id
            WHERE t.token_hash=$1 AND u.email_verified_at IS NULL FOR UPDATE OF u`, digest[:]).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidLink
		}
		if err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `UPDATE email_verification_tokens SET used_at=clock_timestamp()
            WHERE user_id=$1 AND token_hash=$2 AND used_at IS NULL AND expires_at>clock_timestamp()`, userID, digest[:])
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrInvalidLink
		}
		_, err = tx.Exec(ctx, `UPDATE users SET email_verified_at=clock_timestamp() WHERE id=$1`, userID)
		return err
	})
}

func Resend(ctx context.Context, pool *pgxpool.Pool, email string) (Verification, error) {
	var message Verification
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var userID int64
		err := tx.QueryRow(ctx, `SELECT id,email FROM users WHERE lower(email)=lower($1)
            AND email_verified_at IS NULL FOR UPDATE`, email).Scan(&userID, &message.Email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// An invalidated token records a replacement. Allow the first resend immediately.
		var limited bool
		err = tx.QueryRow(ctx, `SELECT count(*) >= 3 OR count(*) FILTER
            (WHERE used_at > clock_timestamp()-interval '1 minute') > 0
            FROM email_verification_tokens WHERE user_id=$1
            AND used_at > clock_timestamp()-interval '1 hour'`, userID).Scan(&limited)
		if err != nil || limited {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE email_verification_tokens SET used_at=clock_timestamp()
            WHERE user_id=$1 AND used_at IS NULL`, userID)
		if err != nil {
			return err
		}
		message.Token, err = newToken(ctx, tx, userID)
		return err
	})
	if err != nil {
		return Verification{}, err
	}
	return message, nil
}
