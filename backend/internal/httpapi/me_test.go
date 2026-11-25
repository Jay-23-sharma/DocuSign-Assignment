package httpapi

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/securecookie"
	"signing-app/backend/internal/auth"
)

func TestMeRejectsInvalidSessionsBeforeLookup(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	cookies, err := auth.NewSenderCookies(base64.StdEncoding.EncodeToString(key), false)
	if err != nil {
		t.Fatal(err)
	}
	fixture := securecookie.New(key, nil).SetSerializer(securecookie.JSONEncoder{})
	expired, err := fixture.Encode(auth.SenderCookieName, map[string]int64{"id": 1, "exp": time.Now().Add(-time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	valid, err := cookies.Issue(1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.URLEncoding.DecodeString(valid.Value)
	if err != nil {
		t.Fatal("unexpected cookie encoding")
	}
	raw[len(raw)-1] ^= 1
	for name, value := range map[string]string{"missing": "", "malformed": "invalid", "tampered": base64.URLEncoding.EncodeToString(raw), "expired": expired} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/auth/me?id=1", nil)
			if value != "" {
				req.AddCookie(&http.Cookie{Name: auth.SenderCookieName, Value: value})
			}
			w := httptest.NewRecorder()
			me(nil, cookies)(w, req)
			if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" || w.Body.String() != "{\"error\":\"unauthorized\"}\n" {
				t.Fatal("unexpected session rejection")
			}
		})
	}
}
