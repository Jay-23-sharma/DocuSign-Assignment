package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"signing-app/backend/internal/auth"
	"signing-app/backend/internal/documents"
)

func prepareError(w http.ResponseWriter, err error) {
	if errors.Is(err, documents.ErrNotDraft) || errors.Is(err, documents.ErrFieldCountChanged) {
		registrationError(w, 409, err.Error())
		return
	}
	documentError(w, err)
}

func recipientMutation(store documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, s auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		var rid int64
		if r.Method != http.MethodPost {
			var err error
			rid, err = strconv.ParseInt(r.PathValue("recipientId"), 10, 64)
			if err != nil || rid < 1 {
				registrationError(w, 404, "Recipient not found.")
				return
			}
		}
		if r.Method == http.MethodDelete {
			var expected *int64
			if raw := r.URL.Query().Get("savedFields"); raw != "" {
				n, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || n < 0 {
					registrationError(w, 400, "Invalid saved field count.")
					return
				}
				expected = &n
			}
			if err := store.DeleteRecipient(r.Context(), s.ID, id, rid, expected); err != nil {
				prepareError(w, err)
				return
			}
			w.WriteHeader(204)
			return
		}
		var input struct {
			Name  *string `json:"name"`
			Email *string `json:"email"`
		}
		if !decode(w, r, &input) {
			return
		}
		if (rid == 0 && (input.Name == nil || input.Email == nil)) || (input.Name == nil && input.Email == nil) {
			registrationError(w, 400, "Provide recipient details.")
			return
		}
		if input.Name != nil {
			*input.Name = strings.TrimSpace(*input.Name)
			if *input.Name == "" || utf8.RuneCountInString(*input.Name) > 200 || strings.ContainsRune(*input.Name, 0) {
				registrationError(w, 400, "Name must contain 1 to 200 characters.")
				return
			}
		}
		if input.Email != nil {
			*input.Email = normalizeEmail(*input.Email)
			if !validEmail(*input.Email) {
				registrationError(w, 400, "Enter a valid email address.")
				return
			}
		}
		recipient, err := store.SaveRecipient(r.Context(), s.ID, id, rid, input.Name, input.Email)
		if err != nil {
			prepareError(w, err)
			return
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(201)
		}
		json.NewEncoder(w).Encode(recipient)
	}
}

func saveFields(store documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, s auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		var input struct {
			Fields json.RawMessage `json:"fields"`
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			registrationError(w, 413, "Layout exceeds 2 MiB.")
			return
		}
		if err != nil || json.Unmarshal(body, &input) != nil {
			registrationError(w, 400, "Expected a JSON object containing fields.")
			return
		}
		err = store.SaveFields(r.Context(), s.ID, id, input.Fields)
		switch {
		case errors.Is(err, documents.ErrRecipient):
			registrationError(w, 404, err.Error())
		case errors.Is(err, documents.ErrLayout):
			registrationError(w, 400, err.Error())
		case err != nil:
			prepareError(w, err)
		default:
			w.WriteHeader(204)
		}
	}
}
