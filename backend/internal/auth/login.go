package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

type Sender struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

var ErrInvalidCredentials = errors.New("invalid credentials")

// A fixed registration-format verifier for the missing-user path.
const dummyHash = "$argon2id$v=19$m=19456,t=2,p=1$bG9jYWxzaWduLWR1bW15IQ$gyfI9Hjdkm5v5V+bKdK1x1y0JQNeo9hx8RHboS8VMC8"

var argonEncoding = regexp.MustCompile(`^\$argon2id\$v=19\$m=([0-9]{1,6}),t=([0-9]{1,2}),p=([0-9]{1,2})\$([A-Za-z0-9+/]{11,86})\$([A-Za-z0-9+/]{22,86})$`)

func verifyPassword(encoded, password string) bool {
	if len(encoded) > 256 {
		return false
	}
	parts := argonEncoding.FindStringSubmatch(encoded)
	if parts == nil {
		return false
	}
	// The pattern bounds each decimal before conversion. Cap work before allocation.
	memory, _ := strconv.Atoi(parts[1])
	iterations, _ := strconv.Atoi(parts[2])
	parallelism, _ := strconv.Atoi(parts[3])
	if parallelism < 1 || parallelism > 8 || memory < 8*parallelism || memory > 64*1024 || iterations < 1 || iterations > 6 {
		return false
	}
	encoding := base64.RawStdEncoding.Strict()
	salt, err := encoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := encoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallelism), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func Login(ctx context.Context, pool *pgxpool.Pool, email, password string) (Sender, error) {
	var sender Sender
	var encoded string
	var verified bool
	err := pool.QueryRow(ctx, `SELECT id,name,email,password_hash,email_verified_at IS NOT NULL
        FROM users WHERE lower(email)=lower($1)`, email).Scan(&sender.ID, &sender.Name, &sender.Email, &encoded, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		encoded = dummyHash
	} else if err != nil {
		return Sender{}, err
	}
	if !verifyPassword(encoded, password) || !verified {
		return Sender{}, ErrInvalidCredentials
	}
	return sender, nil
}
