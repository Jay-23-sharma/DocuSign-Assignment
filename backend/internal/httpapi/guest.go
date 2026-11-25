package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"signing-app/backend/internal/signing"
)

const guestCookie = "localsign_guest"

type guestAPI struct {
	service signing.Service
	secure  bool
	mail    accountMailer
}

func (a guestAPI) routes(mux *http.ServeMux) {
	handle := func(route string, h func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			if err := h(w, r); err != nil {
				var p signing.Problem
				if errors.As(err, &p) {
					registrationError(w, p.Status, p.Message)
				} else {
					registrationError(w, 500, "Signing is temporarily unavailable.")
				}
			}
		})
	}
	handle("POST /api/signing/sessions", a.exchange)
	handle("GET /api/signing/session", func(w http.ResponseWriter, r *http.Request) error {
		state, e := a.service.State(r.Context(), credentials(r))
		if e != nil {
			return e
		}
		return json.NewEncoder(w).Encode(map[string]string{"state": state})
	})
	handle("POST /api/signing/session/email-code", func(w http.ResponseWriter, r *http.Request) error {
		email, code, e := a.service.IssueCode(r.Context(), credentials(r))
		if e != nil {
			return e
		}
		if e = a.mail.SendCode(email, code); e != nil {
			return signing.Problem{Status: 503, Message: "Code delivery could not be confirmed. Try again after 60 seconds."}
		}
		w.WriteHeader(204)
		return nil
	})
	handle("POST /api/signing/session/verify-code", func(w http.ResponseWriter, r *http.Request) error {
		var in struct{ Code string }
		if !decode(w, r, &in) {
			return nil
		}
		if e := a.service.VerifyCode(r.Context(), credentials(r), in.Code); e != nil {
			return e
		}
		w.WriteHeader(204)
		return nil
	})
	handle("GET /api/signing/document", func(w http.ResponseWriter, r *http.Request) error {
		doc, e := a.service.Document(r.Context(), credentials(r))
		if e != nil {
			return e
		}
		return json.NewEncoder(w).Encode(doc)
	})
	handle("GET /api/signing/files/original", func(w http.ResponseWriter, r *http.Request) error {
		f, e := a.service.Original(r.Context(), credentials(r), r.Method == "GET")
		if e != nil {
			return e
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "original.pdf", time.Time{}, f)
		return nil
	})
	handle("POST /api/signing/signatures", func(w http.ResponseWriter, r *http.Request) error {
		c := credentials(r)
		state, e := a.service.State(r.Context(), c)
		if e != nil {
			return e
		}
		if state != "verified" {
			return signing.Problem{Status: 403, Message: "A verified open signing session is required."}
		}
		data, e := io.ReadAll(http.MaxBytesReader(w, r.Body, signing.MaxImageBytes))
		if e != nil {
			return signing.Problem{Status: 413, Message: "Signature image exceeds the 2 MiB limit or could not be read."}
		}
		ref, e := a.service.SaveImage(r.Context(), c, r.URL.Query().Get("method"), data)
		if e != nil {
			return e
		}
		w.WriteHeader(201)
		return json.NewEncoder(w).Encode(map[string]string{"reference": ref})
	})

	handle("POST /api/signing/submit", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Answers []signing.Answer `json:"answers"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || in.Answers == nil || dec.Decode(&struct{}{}) != io.EOF {
			return signing.Problem{Status: 400, Message: "Provide an array of assigned answers."}
		}
		if e := a.service.Submit(r.Context(), credentials(r), in.Answers); e != nil {
			return e
		}
		return json.NewEncoder(w).Encode(map[string]string{"status": "signed"})
	})
	handle("POST /api/signing/decline", func(w http.ResponseWriter, r *http.Request) error {
		var in struct{ Confirm bool }
		if !decode(w, r, &in) {
			return nil
		}
		if !in.Confirm {
			return signing.Problem{Status: 400, Message: "Confirm the decision to decline."}
		}
		if e := a.service.Decline(r.Context(), credentials(r)); e != nil {
			return e
		}
		return json.NewEncoder(w).Encode(map[string]string{"status": "declined"})
	})
}
func credentials(r *http.Request) signing.Credentials {
	var c signing.Credentials
	if cookie, e := r.Cookie(guestCookie); e == nil {
		c.Cookie = cookie.Value
	}
	c.Expected, _ = strconv.ParseInt(r.Header.Get("X-Signing-Session"), 10, 64)
	return c
}
func (a guestAPI) exchange(w http.ResponseWriter, r *http.Request) error {
	var in struct{ Token string }
	if !decode(w, r, &in) {
		return nil
	}
	t, e := a.service.Exchange(r.Context(), in.Token)
	if e != nil {
		return e
	}
	http.SetCookie(w, &http.Cookie{Name: guestCookie, Value: t.Secret, Path: "/api/signing", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, Expires: t.Expiry})
	return json.NewEncoder(w).Encode(map[string]int64{"session_id": t.ID})
}
