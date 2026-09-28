package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
	t.Setenv("ACCESS_TTL", "15m")
	t.Setenv("REFRESH_TTL", "720h")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{
		{"DATABASE_URL", ""}, {"JWT_SECRET", "short"}, {"JWT_SECRET", "replace-with-a-random-secret-of-at-least-32-bytes"},
		{"ACCESS_TTL", "0s"}, {"ACCESS_TTL", "2h"}, {"ACCESS_TTL", "bad"}, {"REFRESH_TTL", "1m"}, {"REFRESH_TTL", "3000h"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}
