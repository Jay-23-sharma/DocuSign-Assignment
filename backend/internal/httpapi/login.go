package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"signing-app/backend/internal/auth"
)

func login(pool *pgxpool.Pool, cookies *auth.SenderCookies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		w.Header().Set("Cache-Control", "no-store")
		if !decode(w, r, &input) {
			return
		}
		sender, err := auth.Login(r.Context(), pool, normalizeEmail(input.Email), input.Password)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			registrationError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		if err != nil {
			registrationError(w, http.StatusInternalServerError, "login unavailable")
			return
		}
		cookie, err := cookies.Issue(sender.ID)
		if err != nil {
			registrationError(w, http.StatusInternalServerError, "login unavailable")
			return
		}
		http.SetCookie(w, cookie)
		json.NewEncoder(w).Encode(sender)
	}
}
