package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

func hashPassword(password string) string {
	const memory, iterations, parallelism = 19 * 1024, 2, 1
	var salt [16]byte
	rand.Read(salt[:])
	hash := argon2.IDKey([]byte(password), salt[:], iterations, memory, parallelism, 32)
	encode := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, iterations, parallelism, encode(salt[:]), encode(hash))
}

// Register is idempotent for an existing sender email.
func Register(ctx context.Context, pool *pgxpool.Pool, name, email, password string) (Verification, error) {
	passwordHash := hashPassword(password)
	var message Verification
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var userID int64
		err := tx.QueryRow(ctx, `
			INSERT INTO users (name, email, password_hash)
			VALUES ($1, $2, $3)
			ON CONFLICT (lower(email)) DO NOTHING
			RETURNING id, email`, name, email, passwordHash).Scan(&userID, &message.Email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
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
