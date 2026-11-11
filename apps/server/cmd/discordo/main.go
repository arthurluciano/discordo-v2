package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"discordo/server/internal/app"
	"discordo/server/internal/auth"
	"discordo/server/internal/blob"
	"discordo/server/internal/config"
	"discordo/server/internal/media/pion"
	"discordo/server/internal/postgres"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// In a compose stack the database and the object storage start alongside us and take a moment.
	var store *postgres.Store
	if err := retry(ctx, func() (err error) { store, err = postgres.Open(ctx, cfg.DatabaseURL); return }); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer store.Close()

	discord := auth.NewDiscord(cfg.DiscordClientID, cfg.DiscordClientSecret, cfg.PublicURL+"/auth/discord/callback")
	var blobs *blob.S3
	if err := retry(ctx, func() (err error) { blobs, err = blob.NewS3(ctx, cfg.S3); return }); err != nil {
		return fmt.Errorf("object storage: %w", err)
	}
	sfu, err := pion.New(cfg.Media)
	if err != nil {
		return fmt.Errorf("media: %w", err)
	}
	defer sfu.Close()
	handler, hub := app.New(store, discord, app.Options{PublicURL: cfg.PublicURL, Blobs: blobs, Media: sfu})
	go hub.Run(ctx)

	srv := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	slog.Info("listening", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// retry gives a dependency up to a minute to come up before giving up with its last error.
func retry(ctx context.Context, connect func() error) error {
	deadline := time.Now().Add(time.Minute)
	for {
		err := connect()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		slog.Warn("dependency not ready, retrying", "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
