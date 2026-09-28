package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"sso-go/internal/config"
	"strings"
	"testing"
	"time"
)

func testTokens() Tokens {
	return Tokens{Config: config.Config{Secret: strings.Repeat("s", 32), Issuer: "test", Audience: "test-api", AccessTTL: 15 * time.Minute, RefreshTTL: 24 * time.Hour}}
}
func TestTokens(t *testing.T) {
	tokens := testTokens()
	raw, _ := newRefresh()
	s := Session{ID: uuid.NewString(), UserID: uuid.NewString(), ExpiresAt: time.Now().Add(time.Hour)}
	p, err := tokens.Issue(s, raw)
	if err != nil {
		t.Fatal(err)
	}
	c, err := tokens.Parse(p.AccessToken)
	if err != nil || c.Subject != s.UserID {
		t.Fatalf("valid token: %v", err)
	}
	other := tokens
	other.Config.Secret = strings.Repeat("x", 32)
	if _, err := other.Parse(p.AccessToken); err == nil {
		t.Fatal("accepted wrong signature")
	}
	other = tokens
	other.Config.Audience = "wrong"
	if _, err := other.Parse(p.AccessToken); err == nil {
		t.Fatal("accepted wrong audience")
	}
	other = tokens
	other.Config.Issuer = "wrong"
	if _, err := other.Parse(p.AccessToken); err == nil {
		t.Fatal("accepted wrong issuer")
	}
	c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	expired, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(tokens.Config.Secret))
	if _, err := tokens.Parse(expired); err == nil {
		t.Fatal("accepted expired token")
	}
	c.ExpiresAt = nil
	missing, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(tokens.Config.Secret))
	if _, err := tokens.Parse(missing); err == nil {
		t.Fatal("accepted missing expiry")
	}
	c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Minute))
	wrongAlg, _ := jwt.NewWithClaims(jwt.SigningMethodHS384, c).SignedString([]byte(tokens.Config.Secret))
	if _, err := tokens.Parse(wrongAlg); err == nil {
		t.Fatal("accepted wrong algorithm")
	}
	if !validRefresh(raw) || validRefresh("invalid") {
		t.Fatal("refresh validation")
	}
}
