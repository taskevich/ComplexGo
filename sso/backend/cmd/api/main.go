package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sso-go/internal/auth"
	"sso-go/internal/config"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	pc, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		cancel()
		return err
	}
	pc.MaxConns = 25
	pc.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	pool, err := pgxpool.NewWithConfig(startup, pc)
	if err != nil {
		cancel()
		return err
	}
	defer pool.Close()
	err = pool.Ping(startup)
	cancel()
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr: c.Addr,
		Handler: auth.NewHandler(&auth.Postgres{Pool: pool},
			auth.Tokens{Config: c}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	done := make(chan error, 1)
	go func() { slog.Info("listening", "address", c.Addr); done <- srv.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdown, finish := context.WithTimeout(context.Background(), 10*time.Second)
	defer finish()
	if err := srv.Shutdown(shutdown); err != nil {
		_ = srv.Close()
		return err
	}
	return nil
}
