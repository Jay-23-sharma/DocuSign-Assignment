package httpapi

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"signing-app/backend/internal/auth"
	"signing-app/backend/internal/documents"
)

func documentError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		registrationError(w, 404, "Document not found.")
		return
	}
	registrationError(w, 500, "Could not load the document.")
}
func documentID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		documentError(w, pgx.ErrNoRows)
		return 0, false
	}
	return id, true
}
func listDocuments(store documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, s auth.Sender) {
		filters := r.URL.Query()
		docs, err := store.List(r.Context(), s.ID, filters.Get("q"), filters.Get("status"))
		if err != nil {
			documentError(w, err)
			return
		}
		json.NewEncoder(w).Encode(docs)
	}
}
func documentDetail(store documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, s auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		doc, err := store.Detail(r.Context(), s.ID, id)
		if err != nil {
			documentError(w, err)
			return
		}
		json.NewEncoder(w).Encode(doc)
	}
}
func originalPDF(store documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, s auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		key, name, err := store.OriginalKey(r.Context(), s.ID, id)
		if err != nil {
			documentError(w, err)
			return
		}
		f, err := os.OpenInRoot(store.Dir, key)
		if err != nil {
			documentError(w, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, time.Time{}, f)
	}
}

func completeDocument(s documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, sender auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		err := s.Complete(r.Context(), id, &sender.ID)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, pgx.ErrNoRows):
			documentError(w, err)
		case errors.Is(err, documents.ErrNotSigned):
			registrationError(w, 409, "Only fully signed documents can be completed.")
		default:
			registrationError(w, 500, "Could not generate the completed PDF. The accepted signatures are saved.")
		}
	}
}
func completedPDF(s documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, sender auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		file, name, err := s.CompletedFile(r.Context(), sender.ID, id)
		if err != nil {
			documentError(w, err)
			return
		}
		defer file.Close()
		mode := "inline"
		if r.URL.Query().Get("download") == "1" {
			mode = "attachment"
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", mime.FormatMediaType(mode, map[string]string{"filename": name}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, time.Time{}, file)
	}
}

func voidDocument(store documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, s auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		err := store.Void(r.Context(), s.ID, id)
		if errors.Is(err, documents.ErrCannotVoid) {
			registrationError(w, 409, err.Error())
			return
		}
		if err != nil {
			documentError(w, err)
			return
		}
		w.WriteHeader(204)
	}
}

func documentHistory(store documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, s auth.Sender) {
		id, ok := documentID(w, r)
		if !ok {
			return
		}
		events, err := store.History(r.Context(), s.ID, id)
		if err != nil {
			documentError(w, err)
			return
		}
		json.NewEncoder(w).Encode(events)
	}
}
