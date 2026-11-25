package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"signing-app/backend/internal/auth"
	"signing-app/backend/internal/documents"
	"signing-app/backend/internal/signing"
)

func Handler(pool *pgxpool.Pool, sender accountMailer, cookies *auth.SenderCookies, docs documents.Store) http.Handler {
	mux := http.NewServeMux()
	key, secure := cookies.GuestConfig()
	guestAPI{signing.Service{Documents: docs, CodeKey: key}, secure, sender}.routes(mux)
	mux.HandleFunc("POST /api/documents/{id}/void", withSender(pool, cookies, voidDocument(docs)))
	mux.HandleFunc("POST /api/documents/{id}/send", withSender(pool, cookies, sendDocument(docs, sender)))
	mux.HandleFunc("POST /api/documents/{id}/recipients/{recipientId}/resend", withSender(pool, cookies, resendInvitation(docs, sender)))
	mux.HandleFunc("PATCH /api/documents/{id}", withSender(pool, cookies, patchSettings(docs)))
	mux.HandleFunc("PUT /api/documents/{id}/fields", withSender(pool, cookies, saveFields(docs)))
	recipients := withSender(pool, cookies, recipientMutation(docs))
	mux.HandleFunc("POST /api/documents/{id}/recipients", recipients)
	mux.HandleFunc("PATCH /api/documents/{id}/recipients/{recipientId}", recipients)
	mux.HandleFunc("DELETE /api/documents/{id}/recipients/{recipientId}", recipients)
	mux.HandleFunc("POST /api/documents", withSender(pool, cookies, uploadDocument(docs)))
	mux.HandleFunc("GET /api/documents", withSender(pool, cookies, listDocuments(docs)))
	mux.HandleFunc("GET /api/documents/{id}", withSender(pool, cookies, documentDetail(docs)))
	mux.HandleFunc("GET /api/documents/{id}/history", withSender(pool, cookies, documentHistory(docs)))
	mux.HandleFunc("POST /api/documents/{id}/complete", withSender(pool, cookies, completeDocument(docs)))
	mux.HandleFunc("GET /api/documents/{id}/files/completed", withSender(pool, cookies, completedPDF(docs)))
	mux.HandleFunc("GET /api/documents/{id}/files/original", withSender(pool, cookies, originalPDF(docs)))
	mux.HandleFunc("GET /api/auth/me", me(pool, cookies))
	mux.HandleFunc("POST /api/auth/login", login(pool, cookies))
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.SetCookie(w, cookies.Expire())
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/auth/register", register(pool, sender))
	mux.HandleFunc("POST /api/auth/verify-email", verifyEmail(pool))
	mux.HandleFunc("POST /api/auth/resend-verification", resendVerification(pool, sender))
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")
		if err := pool.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}
