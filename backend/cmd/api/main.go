package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"signing-app/backend/internal/auth"
	"signing-app/backend/internal/database"
	"signing-app/backend/internal/documents"
	"signing-app/backend/internal/httpapi"
	"signing-app/backend/internal/mail"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()

	smtpAddr := os.Getenv("SMTP_ADDR")
	if smtpAddr == "" {
		smtpAddr = "127.0.0.1:1025"
	}
	base := os.Getenv("FRONTEND_BASE_URL")
	if base == "" {
		base = "http://127.0.0.1:5173"
	}
	sender, err := mail.New(smtpAddr, base)
	if err != nil {
		return err
	}
	// mail.New has already validated the public frontend origin.
	origin, _ := url.Parse(base)
	localHTTP := origin.Scheme == "http" && (origin.Hostname() == "localhost" || origin.Hostname() == "127.0.0.1" || origin.Hostname() == "::1")
	cookies, err := auth.NewSenderCookies(os.Getenv("SENDER_COOKIE_KEY"), !localHTTP)
	if err != nil {
		return err
	}
	docs, err := documents.NewStore(pool, os.Getenv("DOCUMENT_STORAGE_DIR"), os.Getenv("PDF_MAX_BYTES"))
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           httpapi.Handler(pool, sender, cookies, docs),
		ReadHeaderTimeout: 5 * time.Second,
	}
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if err := docs.ExpireOverdue(workerCtx); err != nil && workerCtx.Err() == nil {
				log.Print("expiration check failed; will retry")
			}
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() { cancelWorker(); <-workerDone }()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
