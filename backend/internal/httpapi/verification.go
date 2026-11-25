package httpapi

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"signing-app/backend/internal/auth"
)

func verifyEmail(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token string `json:"token"`
		}
		if !decode(w, r, &input) {
			return
		}
		err := auth.Verify(r.Context(), pool, input.Token)
		if errors.Is(err, auth.ErrInvalidLink) {
			registrationError(w, http.StatusBadRequest, "This verification link is invalid or has expired.")
			return
		}
		if err != nil {
			registrationError(w, http.StatusInternalServerError, "verification unavailable")
			return
		}
		w.Write([]byte(`{"status":"verified"}`))
	}
}
func resendVerification(pool *pgxpool.Pool, sender verificationSender) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Email string `json:"email"`
		}
		if !decode(w, r, &input) {
			return
		}
		email := normalizeEmail(input.Email)
		if !validEmail(email) {
			registrationError(w, http.StatusBadRequest, "enter a valid email address")
			return
		}
		message, err := auth.Resend(r.Context(), pool, email)
		if err != nil {
			registrationError(w, http.StatusInternalServerError, "verification unavailable")
			return
		}
		acknowledge(w, sender, message)
	}
}
