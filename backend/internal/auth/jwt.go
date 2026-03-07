package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Signer struct {
	Secret []byte
	Issuer string
	TTL    time.Duration
	Now    func() time.Time
}

type Claims struct {
	Subject  string
	Username string
	Issuer   string
	IssuedAt int64
	Expires  int64
}

func NewSigner(secret, issuer string, ttl time.Duration) *Signer {
	return &Signer{
		Secret: []byte(secret),
		Issuer: issuer,
		TTL:    ttl,
		Now:    time.Now,
	}
}

func (s *Signer) Sign(userID, username string) (string, error) {
	if len(s.Secret) == 0 {
		return "", errors.New("jwt secret is required")
	}
	if userID == "" {
		return "", errors.New("user id is required")
	}

	now := s.Now().UTC()
	exp := now.Add(s.TTL)

	header, err := json.Marshal(map[string]any{
		"alg": "HS256",
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(map[string]any{
		"sub":      userID,
		"username": username,
		"iss":      s.Issuer,
		"iat":      now.Unix(),
		"exp":      exp.Unix(),
	})
	if err != nil {
		return "", err
	}

	h := base64.RawURLEncoding.EncodeToString(header)
	p := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := h + "." + p

	mac := hmac.New(sha256.New, s.Secret)
	if _, err := mac.Write([]byte(unsigned)); err != nil {
		return "", err
	}
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s.%s", unsigned, sig), nil
}

func (s *Signer) Parse(token string) (Claims, error) {
	return s.parseInternal(token, false)
}

func (s *Signer) ParseAllowExpired(token string) (Claims, error) {
	return s.parseInternal(token, true)
}

func (s *Signer) parseInternal(token string, allowExpired bool) (Claims, error) {
	if len(s.Secret) == 0 {
		return Claims{}, errors.New("jwt secret is required")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("invalid token format")
	}

	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, s.Secret)
	if _, err := mac.Write([]byte(unsigned)); err != nil {
		return Claims{}, err
	}
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expectedSig), []byte(parts[2])) {
		return Claims{}, errors.New("invalid token signature")
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, errors.New("invalid token payload")
	}

	var payload struct {
		Subject  string `json:"sub"`
		Username string `json:"username"`
		Issuer   string `json:"iss"`
		IssuedAt int64  `json:"iat"`
		Expires  int64  `json:"exp"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return Claims{}, errors.New("invalid token payload")
	}

	if payload.Subject == "" {
		return Claims{}, errors.New("token subject missing")
	}
	if !allowExpired && payload.Expires > 0 && s.Now().UTC().Unix() >= payload.Expires {
		return Claims{}, errors.New("token expired")
	}
	if s.Issuer != "" && payload.Issuer != s.Issuer {
		return Claims{}, errors.New("invalid token issuer")
	}

	return Claims{
		Subject:  payload.Subject,
		Username: payload.Username,
		Issuer:   payload.Issuer,
		IssuedAt: payload.IssuedAt,
		Expires:  payload.Expires,
	}, nil
}
