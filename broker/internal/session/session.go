// Package session provides HMAC-signed cookies for the broker's own login
// session and signed `state` values for the Google connect flow (CSRF + return).
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const cookieName = "connect_session"

type Signer struct {
	key []byte
}

func NewSigner(secret string) *Signer { return &Signer{key: []byte(secret)} }

type User struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Exp   int64  `json:"exp"`
}

// sign returns base64(payload).base64(hmac).
func (s *Signer) sign(payload []byte) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
}

func (s *Signer) verify(token string) ([]byte, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("malformed token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errors.New("bad signature")
	}
	return payload, nil
}

func (s *Signer) SetSession(w http.ResponseWriter, u User, ttl time.Duration) {
	u.Exp = time.Now().Add(ttl).Unix()
	b, _ := json.Marshal(u)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    s.sign(b),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(u.Exp, 0),
	})
}

func (s *Signer) Session(r *http.Request) (*User, error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil, err
	}
	payload, err := s.verify(c.Value)
	if err != nil {
		return nil, err
	}
	var u User
	if err := json.Unmarshal(payload, &u); err != nil {
		return nil, err
	}
	if time.Now().Unix() > u.Exp {
		return nil, errors.New("session expired")
	}
	return &u, nil
}

// State is the signed value carried through the Google connect redirect.
type State struct {
	AppID     string `json:"app_id"`
	UserSub   string `json:"sub"`
	ReturnURL string `json:"return"`
	Nonce     string `json:"nonce"`
	Exp       int64  `json:"exp"`
}

func (s *Signer) SignState(st State, ttl time.Duration) string {
	st.Exp = time.Now().Add(ttl).Unix()
	b, _ := json.Marshal(st)
	return s.sign(b)
}

func (s *Signer) VerifyState(token string) (*State, error) {
	payload, err := s.verify(token)
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(payload, &st); err != nil {
		return nil, err
	}
	if time.Now().Unix() > st.Exp {
		return nil, errors.New("state expired")
	}
	return &st, nil
}
