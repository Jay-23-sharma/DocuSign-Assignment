package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"signing-app/backend/internal/auth"
)

type registration struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (input *registration) validate() string {
	input.Name = strings.TrimSpace(input.Name)
	input.Email = normalizeEmail(input.Email)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 200 || strings.ContainsRune(input.Name, 0) {
		return "name must contain 1 to 200 characters"
	}
	if !validEmail(input.Email) {
		return "enter a valid email address"
	}
	if n := utf8.RuneCountInString(input.Password); n < 12 || n > 128 {
		return "password must contain 12 to 128 characters"
	}
	return ""
}

func normalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && len(value) <= 254 && !strings.ContainsAny(value, "\r\n")
}
func decode(w http.ResponseWriter, r *http.Request, input any) bool {
	w.Header().Set("Content-Type", "application/json")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		registrationError(w, http.StatusRequestEntityTooLarge, "request body exceeds 16 KiB")
		return false
	}

	if err != nil || !utf8.Valid(body) || json.Unmarshal(body, input) != nil {
		registrationError(w, http.StatusBadRequest, "expected a JSON object")
		return false
	}
	return true
}

type verificationSender interface {
	Send(email, token string) error
}

func acknowledge(w http.ResponseWriter, sender verificationSender, message auth.Verification) {
	if message.Token != "" {
		if err := sender.Send(message.Email, message.Token); err != nil {
			log.Print("verification email delivery failed")
		}
	}
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"accepted"}`))
}
func register(pool *pgxpool.Pool, sender verificationSender) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input registration
		if !decode(w, r, &input) {
			return
		}
		if message := input.validate(); message != "" {
			registrationError(w, http.StatusBadRequest, message)
			return
		}
		message, err := auth.Register(r.Context(), pool, input.Name, input.Email, input.Password)
		if err != nil {
			registrationError(w, http.StatusInternalServerError, "registration unavailable")
			return
		}
		acknowledge(w, sender, message)
	}
}

func registrationError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
