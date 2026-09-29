package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

type API struct {
	Store  Store
	Tokens Tokens
	dummy  []byte
}

func NewHandler(store Store, tokens Tokens) http.Handler {
	dummy, err := bcrypt.GenerateFromPassword([]byte(uuid.NewString()), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	a := &API{store, tokens, dummy}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Ping(r.Context()); err != nil {
			fail(w, 503, "database unavailable")
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	limit := limiter()
	mux.Handle("POST /v1/auth/register", limit(http.HandlerFunc(a.register)))
	mux.Handle("POST /v1/auth/login", limit(http.HandlerFunc(a.login)))
	mux.Handle("POST /v1/auth/refresh", limit(http.HandlerFunc(a.refresh)))
	mux.Handle("POST /v1/auth/logout", limit(http.HandlerFunc(a.logout)))
	mux.HandleFunc("GET /v1/me", a.me)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		defer func() {
			if v := recover(); v != nil {
				slog.Error("request panic", "panic", v)
				fail(w, 500, "internal error")
			}
		}()
		mux.ServeHTTP(w, r)
	})
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type refreshRequest struct {
	Token string `json:"refresh_token"`
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		fail(w, 415, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "invalid JSON body")
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		fail(w, 400, "expected one JSON object")
		return false
	}
	return true
}

func normalize(email string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	a, err := mail.ParseAddress(email)
	return email, err == nil && a.Address == email && len(email) <= 254
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	email, ok := normalize(c.Email)
	if !ok || len(c.Password) < 12 || len(c.Password) > 72 {
		fail(w, 400, "valid email and password of 12 to 72 bytes required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		a.failure(w, err)
		return
	}
	u, err := a.Store.CreateUser(r.Context(), User{ID: uuid.NewString(), Email: email, PasswordHash: string(hash)})
	if errors.Is(err, ErrConflict) {
		fail(w, 409, "email already registered")
		return
	}
	if err != nil {
		a.failure(w, err)
		return
	}
	respond(w, 201, u)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	email, ok := normalize(c.Email)
	if !ok || len(c.Password) > 72 || len(c.Password) == 0 {
		fail(w, 401, "invalid credentials")
		return
	}
	u, err := a.Store.UserByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, ErrCredentials) {
		a.failure(w, err)
		return
	}
	hash := []byte(u.PasswordHash)
	if errors.Is(err, ErrCredentials) {
		hash = a.dummy
	}
	mismatch := bcrypt.CompareHashAndPassword(hash, []byte(c.Password))
	if err != nil || mismatch != nil {
		fail(w, 401, "invalid credentials")
		return
	}
	raw, h := newRefresh()
	s := Session{ID: uuid.NewString(), UserID: u.ID, Hash: h, ExpiresAt: time.Now().Add(a.Tokens.Config.RefreshTTL)}
	pair, err := a.Tokens.Issue(s, raw)
	if err != nil {
		a.failure(w, err)
		return
	}
	if err := a.Store.CreateSession(r.Context(), s); err != nil {
		a.failure(w, err)
		return
	}
	respond(w, 200, pair)
}

func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
	var c refreshRequest
	if !decode(w, r, &c) {
		return
	}
	if !validRefresh(c.Token) {
		fail(w, 401, "invalid refresh token")
		return
	}
	raw, h := newRefresh()
	s, err := a.Store.Rotate(r.Context(), hashToken(c.Token), h)
	if errors.Is(err, ErrCredentials) {
		fail(w, 401, "invalid refresh token")
		return
	}
	if err != nil {
		a.failure(w, err)
		return
	}
	pair, err := a.Tokens.Issue(s, raw)
	if err != nil {
		a.failure(w, err)
		return
	}
	respond(w, 200, pair)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	var c refreshRequest
	if !decode(w, r, &c) {
		return
	}
	if !validRefresh(c.Token) {
		fail(w, 400, "invalid refresh token format")
		return
	}
	if err := a.Store.Revoke(r.Context(), hashToken(c.Token)); err != nil {
		a.failure(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		fail(w, 401, "bearer token required")
		return
	}
	c, err := a.Tokens.Parse(parts[1])
	if err != nil {
		fail(w, 401, "invalid access token")
		return
	}
	u, err := a.Store.SessionUser(r.Context(), c.SessionID, c.Subject)
	if errors.Is(err, ErrCredentials) {
		fail(w, 401, "session expired or revoked")
		return
	}
	if err != nil {
		a.failure(w, err)
		return
	}
	respond(w, 200, u)
}

func (a *API) failure(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	fail(w, 500, "internal error")
}

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}

func limiter() func(http.Handler) http.Handler {
	type entry struct {
		l    *rate.Limiter
		seen time.Time
	}
	var mu sync.Mutex
	clients := map[string]entry{}
	lastCleanup := time.Now()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			now := time.Now()
			mu.Lock()
			if now.Sub(lastCleanup) > time.Minute {
				for k, e := range clients {
					if now.Sub(e.seen) > 5*time.Minute {
						delete(clients, k)
					}
				}
				lastCleanup = now
			}
			e, ok := clients[ip]
			if !ok {
				if len(clients) >= 10000 {
					mu.Unlock()
					fail(w, 429, "too many requests")
					return
				}
				e.l = rate.NewLimiter(rate.Every(6*time.Second), 10)
			}
			e.seen = now
			clients[ip] = e
			allowed := e.l.Allow()
			mu.Unlock()
			if !allowed {
				w.Header().Set("Retry-After", "6")
				fail(w, 429, "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
