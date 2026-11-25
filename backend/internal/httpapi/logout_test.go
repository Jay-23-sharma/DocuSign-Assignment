package httpapi

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"signing-app/backend/internal/auth"
	"signing-app/backend/internal/documents"
)

func TestLogout(t *testing.T) {
	for _, secure := range []bool{false, true} {
		cookies, err := auth.NewSenderCookies(base64.StdEncoding.EncodeToString(make([]byte, 32)), secure)
		if err != nil {
			t.Fatal(err)
		}
		issued, err := cookies.Issue(1)
		if err != nil {
			t.Fatal(err)
		}
		router := Handler(nil, nil, cookies, documents.Store{})
		for _, value := range []string{issued.Value, "invalid", "", ""} {
			req := httptest.NewRequest("POST", "/api/auth/logout", nil)
			if value != "" {
				req.AddCookie(&http.Cookie{Name: issued.Name, Value: value})
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != 204 || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("unexpected logout response")
			}
			set := w.Result().Cookies()
			if len(set) != 1 {
				t.Fatal("expected one deletion cookie")
			}
			c := set[0]
			if c.Name != issued.Name || c.Value != "" || c.Path != "/" || c.Domain != "" || !c.HttpOnly || c.Secure != secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge != -1 || c.Expires.IsZero() || !c.Expires.Before(time.Now()) {
				t.Fatal("incorrect deletion cookie")
			}
		}
		req := httptest.NewRequest("GET", "/api/auth/logout", nil)
		req.AddCookie(issued)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 405 || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("GET must not clear the cookie")
		}
	}
}
