package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sso-go/internal/config"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}
type Tokens struct{ Config config.Config }
type Pair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

func newRefresh() (string, []byte) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw)
}

func hashToken(raw string) []byte { h := sha256.Sum256([]byte(raw)); return h[:] }

func validRefresh(raw string) bool {
	b, e := base64.RawURLEncoding.DecodeString(raw)
	return e == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == raw
}

func (t Tokens) Issue(s Session, refresh string) (Pair, error) {
	now := time.Now()
	deadline := now.Add(t.Config.AccessTTL)
	if s.ExpiresAt.Before(deadline) {
		deadline = s.ExpiresAt
	}
	c := Claims{
		SessionID: s.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    t.Config.Issuer,
			Subject:   s.UserID,
			Audience:  jwt.ClaimStrings{t.Config.Audience},
			ExpiresAt: jwt.NewNumericDate(deadline),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(t.Config.Secret))
	return Pair{AccessToken: raw, RefreshToken: refresh, TokenType: "Bearer", ExpiresIn: deadline.Unix() - now.Unix()}, err
}

func (t Tokens) Parse(raw string) (*Claims, error) {
	c := &Claims{}
	_, err := jwt.ParseWithClaims(
		raw,
		c,
		func(_ *jwt.Token) (any, error) { return []byte(t.Config.Secret), nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(t.Config.Issuer),
		jwt.WithAudience(t.Config.Audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return nil, ErrCredentials
	}
	if _, err := uuid.Parse(c.Subject); err != nil {
		return nil, ErrCredentials
	}
	if _, err := uuid.Parse(c.SessionID); err != nil {
		return nil, ErrCredentials
	}
	if c.IssuedAt == nil || c.NotBefore == nil {
		return nil, errors.New("missing token timestamps")
	}
	return c, nil
}
