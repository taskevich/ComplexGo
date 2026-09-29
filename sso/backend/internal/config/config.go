package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL, Addr, Secret, Issuer, Audience string
	AccessTTL, RefreshTTL                       time.Duration
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Addr:        env("HTTP_ADDR", ":8080"),
		Secret:      os.Getenv("JWT_SECRET"),
		Issuer:      env("JWT_ISSUER", "sso"),
		Audience:    env("JWT_AUDIENCE", "sso-api"),
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if len(c.Secret) < 32 || strings.HasPrefix(c.Secret, "replace-with") {
		return c, errors.New("JWT_SECRET must be a random secret of at least 32 bytes")
	}
	var err error
	c.AccessTTL, err = time.ParseDuration(env("ACCESS_TTL", "15m"))
	if err != nil || c.AccessTTL < time.Second || c.AccessTTL > time.Hour {
		return c, fmt.Errorf("ACCESS_TTL must be between 1s and 1h")
	}
	c.RefreshTTL, err = time.ParseDuration(env("REFRESH_TTL", "720h"))
	if err != nil || c.RefreshTTL < c.AccessTTL || c.RefreshTTL > 2160*time.Hour {
		return c, fmt.Errorf("REFRESH_TTL must be >= ACCESS_TTL and <= 2160h")
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
