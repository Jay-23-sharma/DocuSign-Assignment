package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"signing-app/backend/internal/auth"
	"signing-app/backend/internal/documents"
	"signing-app/backend/internal/mail"
)

func patchSettings(store documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, s auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		var input struct {
			Subject  *string         `json:"email_subject"`
			Message  *string         `json:"email_message"`
			Deadline json.RawMessage `json:"expires_at"`
		}
		if !decode(w, r, &input) {
			return
		}
		settings := documents.SendSettings{Subject: input.Subject, Message: input.Message}
		if input.Subject != nil && (strings.ContainsAny(*input.Subject, "\r\n\x00") || len(*input.Subject) > 800) {
			registrationError(w, 400, "Subject must be one line, up to 800 bytes.")
			return
		}
		if input.Message != nil && strings.ContainsRune(*input.Message, 0) {
			registrationError(w, 400, "Invalid message.")
			return
		}
		if input.Deadline != nil && string(input.Deadline) != "null" {
			var deadline time.Time
			if json.Unmarshal(input.Deadline, &deadline) != nil {
				registrationError(w, 400, "Use a timestamp with a time zone for the deadline.")
				return
			}
			settings.Deadline = &deadline
		}
		if err := store.SaveSettings(r.Context(), s.ID, id, settings, input.Deadline != nil); err != nil {
			prepareError(w, err)
			return
		}
		w.WriteHeader(204)
	}
}

type accountMailer interface {
	SendCode(email, code string) error
	verificationSender
	SendInvitation(email, subject, message string, deadline time.Time, token string) error
}

func deliver(ctx context.Context, store documents.Store, sender accountMailer, invite documents.Invitation) {
	err := sender.SendInvitation(invite.Email, invite.Subject, invite.Message, invite.Deadline, invite.Token)
	if errors.Is(err, mail.ErrUnconfirmed) {
		return
	}
	// A disconnected sender must not cancel the outcome write after SMTP accepted the message.
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := store.RecordDelivery(record, invite, err == nil); err != nil {
		log.Print("invitation delivery outcome could not be recorded")
	}
}

func sendDocument(store documents.Store, sender accountMailer) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, s auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		batch, err := store.Release(r.Context(), s.ID, id)
		var invalid documents.ReleaseProblem
		switch {
		case errors.Is(err, documents.ErrNotDraft):
			registrationError(w, 409, "This document is no longer a draft. Reload to see its current status.")
			return
		case errors.Is(err, documents.ErrSigningClosed):
			registrationError(w, 422, "Choose a signing deadline in the future.")
			return
		case errors.As(err, &invalid):
			registrationError(w, 422, invalid.Error())
			return
		case err != nil:
			documentError(w, err)
			return
		}
		for _, invite := range batch {
			deliver(r.Context(), store, sender, invite)
		}
		w.WriteHeader(204)
	}
}

func resendInvitation(store documents.Store, sender accountMailer) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, s auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		rid, err := strconv.ParseInt(r.PathValue("recipientId"), 10, 64)
		if err != nil || rid < 1 {
			registrationError(w, 404, "Recipient not found.")
			return
		}
		invite, err := store.ReplaceInvitation(r.Context(), s.ID, id, rid)
		if errors.Is(err, documents.ErrSigningClosed) {
			registrationError(w, 409, err.Error())
			return
		}
		if err != nil {
			documentError(w, err)
			return
		}
		deliver(r.Context(), store, sender, invite)
		w.WriteHeader(204)
	}
}
