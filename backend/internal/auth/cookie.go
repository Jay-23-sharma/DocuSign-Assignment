package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/securecookie"
)

const SenderCookieName = "localsign_sender"
const sessionLifetime = 8 * time.Hour

type SenderCookies struct {
	codec    *securecookie.SecureCookie
	secure   bool
	guestKey []byte
}

type senderSession struct {
	ID      int64 `json:"id"`
	Expires int64 `json:"exp"`
}

func NewSenderCookies(keyBase64 string, secure bool) (*SenderCookies, error) {
	key, err := base64.StdEncoding.Strict().DecodeString(keyBase64)
	if err != nil || len(key) != 32 || base64.StdEncoding.EncodeToString(key) != keyBase64 {
		return nil, errors.New("SENDER_COOKIE_KEY must be standard Base64 encoding of 32 random bytes")
	}
	codec := securecookie.New(key, nil).SetSerializer(securecookie.JSONEncoder{}).MaxAge(int(sessionLifetime.Seconds()))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("LocalSign signing email codes v1"))
	return &SenderCookies{codec, secure, mac.Sum(nil)}, nil
}

func (s *SenderCookies) Issue(id int64) (*http.Cookie, error) {
	expiry := time.Now().UTC().Add(sessionLifetime).Truncate(time.Second)
	value, err := s.codec.Encode(SenderCookieName, senderSession{id, expiry.Unix()})
	if err != nil {
		return nil, err
	}
	cookie := s.scopedCookie()
	cookie.Value = value
	cookie.Expires = expiry
	cookie.MaxAge = int(sessionLifetime.Seconds())
	return cookie, nil
}

func (s *SenderCookies) Expire() *http.Cookie {
	cookie := s.scopedCookie()
	cookie.MaxAge = -1
	cookie.Expires = time.Unix(1, 0).UTC()
	return cookie
}

func (s *SenderCookies) scopedCookie() *http.Cookie {
	return &http.Cookie{Name: SenderCookieName, Path: "/", HttpOnly: true,
		Secure: s.secure, SameSite: http.SameSiteLaxMode}
}

func (s *SenderCookies) Decode(value string) (int64, error) {
	var payload senderSession
	if err := s.codec.Decode(SenderCookieName, value, &payload); err != nil || payload.ID <= 0 || payload.Expires <= time.Now().Unix() {
		return 0, errors.New("invalid sender session")
	}
	return payload.ID, nil
}

func (s *SenderCookies) GuestConfig() ([]byte, bool) { return s.guestKey, s.secure }
