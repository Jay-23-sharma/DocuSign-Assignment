package auth

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"testing"
	"time"
)

func TestSenderCookie(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	codec, err := NewSenderCookies(key, true)
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := codec.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != 28800 {
		t.Fatal("cookie attributes")
	}
	restarted, _ := NewSenderCookies(key, false)
	if id, err := restarted.Decode(cookie.Value); err != nil || id != 42 {
		t.Fatal("same-key round trip")
	}
	// Mutate the authenticated bytes, preserving the outer wire encoding.
	raw, err := base64.URLEncoding.DecodeString(cookie.Value)
	if err != nil {
		t.Fatal("unexpected encoding")
	}
	raw[len(raw)-1] ^= 1
	expired, err := codec.codec.Encode(SenderCookieName, senderSession{42, time.Now().Add(-time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{expired, base64.URLEncoding.EncodeToString(raw), "invalid"} {
		if _, err := codec.Decode(v); err == nil {
			t.Fatal("accepted invalid or expired cookie")
		}
	}
	different, _ := NewSenderCookies(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)), true)
	if _, err := different.Decode(cookie.Value); err == nil {
		t.Fatal("wrong signing key accepted")
	}
	for _, k := range []string{"", "invalid!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := NewSenderCookies(k, true); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
}
