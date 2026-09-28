package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"sso-go/migrations"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestPostgresAuthFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(clean, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	goose.SetBaseFS(migrations.Files)
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpContext(ctx, db, "."); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpContext(ctx, db, "."); err != nil {
		t.Fatal("idempotent up:", err)
	}
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := &Postgres{Pool: pool}
	h := NewHandler(store, testTokens())
	counter := 0
	request := func(method, path string, body any, token string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		counter++
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1000", counter)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("want %d got %d: %s", status, w.Code, w.Body.String())
		}
	}
	creds := credentials{Email: " Alice@Example.com ", Password: "long-secret-password"}
	w := request("POST", "/v1/auth/register", creds, "")
	expect(w, 201)
	if strings.Contains(w.Body.String(), "password") {
		t.Fatal("password leaked")
	}
	expect(request("POST", "/v1/auth/register", creds, ""), 409)
	expect(request("POST", "/v1/auth/login", credentials{Email: creds.Email, Password: "incorrect"}, ""), 401)
	expect(request("POST", "/v1/auth/login", credentials{Email: "absent@example.com", Password: creds.Password}, ""), 401)
	expect(request("GET", "/v1/me", nil, ""), 401)
	w = request("POST", "/v1/auth/login", creds, "")
	expect(w, 200)
	var first Pair
	if err = json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	expect(request("GET", "/v1/me", nil, first.AccessToken), 200)
	w = request("POST", "/v1/auth/refresh", refreshRequest{first.RefreshToken}, "")
	expect(w, 200)
	var next Pair
	if err = json.Unmarshal(w.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if next.RefreshToken == first.RefreshToken {
		t.Fatal("refresh not rotated")
	}
	expect(request("POST", "/v1/auth/refresh", refreshRequest{first.RefreshToken}, ""), 401)
	expect(request("POST", "/v1/auth/logout", refreshRequest{next.RefreshToken}, ""), 204)
	expect(request("GET", "/v1/me", nil, next.AccessToken), 401)
	expect(request("GET", "/v1/me", nil, first.AccessToken), 401)
	expect(request("POST", "/v1/auth/refresh", refreshRequest{next.RefreshToken}, ""), 401)
	expect(request("POST", "/v1/auth/logout", refreshRequest{next.RefreshToken}, ""), 204)

	w = request("POST", "/v1/auth/login", creds, "")
	expect(w, 200)
	var concurrent Pair
	_ = json.Unmarshal(w.Body.Bytes(), &concurrent)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, hash := newRefresh()
			_, err := store.Rotate(ctx, hashToken(concurrent.RefreshToken), hash)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if err != ErrCredentials {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatalf("rotation winners: %d", wins)
	}

	w = request("POST", "/v1/auth/login", creds, "")
	expect(w, 200)
	var expired Pair
	_ = json.Unmarshal(w.Body.Bytes(), &expired)
	if _, err = pool.Exec(ctx, `UPDATE sessions SET expires_at=now()-interval '1 second' WHERE refresh_hash=$1`, hashToken(expired.RefreshToken)); err != nil {
		t.Fatal(err)
	}
	expect(request("GET", "/v1/me", nil, expired.AccessToken), 401)
	expect(request("POST", "/v1/auth/refresh", refreshRequest{expired.RefreshToken}, ""), 401)

	expect(request("POST", "/v1/auth/register", map[string]string{"extra": "field"}, ""), 400)
	for i := 0; i < 11; i++ {
		r := httptest.NewRequest("POST", "/v1/auth/login", strings.NewReader("{}"))
		r.RemoteAddr = "203.0.113.1:1111"
		r.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		if i == 10 {
			expect(out, 429)
		}
	}
	if err = goose.DownContext(ctx, db, "."); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=$1 AND table_name IN ('users','sessions')`, schema).Scan(&count); err != nil || count != 0 {
		t.Fatalf("down migration: count=%d err=%v", count, err)
	}
	if err = goose.UpContext(ctx, db, "."); err != nil {
		t.Fatal("up after down:", err)
	}
}
