package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"signing-app/backend/internal/auth"
)

type senderHandler func(http.ResponseWriter, *http.Request, auth.Sender)

func withSender(pool *pgxpool.Pool, cookies *auth.SenderCookies, next senderHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		cookie, err := r.Cookie(auth.SenderCookieName)
		if err != nil {
			registrationError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id, err := cookies.Decode(cookie.Value)
		if err != nil {
			registrationError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var sender auth.Sender
		err = pool.QueryRow(r.Context(), `SELECT id, name, email FROM users
			WHERE id = $1 AND email_verified_at IS NOT NULL`, id).Scan(&sender.ID, &sender.Name, &sender.Email)
		if errors.Is(err, pgx.ErrNoRows) {
			registrationError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err != nil {
			registrationError(w, http.StatusInternalServerError, "sender lookup unavailable")
			return
		}
		next(w, r, sender)
	}
}

func me(pool *pgxpool.Pool, cookies *auth.SenderCookies) http.HandlerFunc {
	return withSender(pool, cookies, func(w http.ResponseWriter, r *http.Request, sender auth.Sender) { json.NewEncoder(w).Encode(sender) })
}
