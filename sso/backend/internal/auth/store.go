package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrCredentials = errors.New("invalid credentials")
var ErrConflict = errors.New("email already registered")

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}
type Session struct {
	ID, UserID string
	Hash       []byte
	ExpiresAt  time.Time
}
type Store interface {
	CreateUser(context.Context, User) (User, error)
	UserByEmail(context.Context, string) (User, error)
	CreateSession(context.Context, Session) error
	Rotate(context.Context, []byte, []byte) (Session, error)
	Revoke(context.Context, []byte) error
	SessionUser(context.Context, string, string) (User, error)
	Ping(context.Context) error
}
type Postgres struct{ Pool *pgxpool.Pool }

func (p *Postgres) Ping(ctx context.Context) error { return p.Pool.Ping(ctx) }

func (p *Postgres) CreateUser(ctx context.Context, u User) (User, error) {
	err := p.Pool.QueryRow(
		ctx,
		`INSERT INTO users(id,email,password_hash) VALUES($1,$2,$3) RETURNING created_at`,
		u.ID,
		u.Email,
		u.PasswordHash,
	).Scan(&u.CreatedAt)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return User{}, ErrConflict
	}
	return u, err
}

func (p *Postgres) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := p.Pool.QueryRow(
		ctx,
		`SELECT id,email,password_hash,created_at FROM users WHERE email=$1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrCredentials
	}
	return u, err
}

func (p *Postgres) CreateSession(ctx context.Context, s Session) error {
	_, err := p.Pool.Exec(
		ctx,
		`INSERT INTO sessions(id,user_id,refresh_hash,expires_at) VALUES($1,$2,$3,$4)`,
		s.ID,
		s.UserID,
		s.Hash,
		s.ExpiresAt,
	)
	return err
}

func (p *Postgres) Rotate(ctx context.Context, old, next []byte) (Session, error) {
	var s Session
	err := p.Pool.QueryRow(
		ctx,
		`UPDATE sessions SET refresh_hash=$2 WHERE refresh_hash=$1 AND revoked_at IS NULL AND expires_at>now() RETURNING id,user_id,expires_at`,
		old,
		next,
	).Scan(&s.ID, &s.UserID, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrCredentials
	}
	return s, err
}

func (p *Postgres) Revoke(ctx context.Context, hash []byte) error {
	_, err := p.Pool.Exec(
		ctx,
		`UPDATE sessions SET revoked_at=now() WHERE refresh_hash=$1 AND revoked_at IS NULL`,
		hash,
	)
	return err
}

func (p *Postgres) SessionUser(ctx context.Context, sid, uid string) (User, error) {
	var u User
	err := p.Pool.QueryRow(
		ctx,
		`SELECT u.id,u.email,u.created_at FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.id=$1 AND u.id=$2 AND s.revoked_at IS NULL AND s.expires_at>now()`,
		sid,
		uid,
	).Scan(&u.ID, &u.Email, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrCredentials
	}
	return u, err
}
