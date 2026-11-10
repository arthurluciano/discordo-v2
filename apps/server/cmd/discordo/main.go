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

	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	discord := auth.NewDiscord(cfg.DiscordClientID, cfg.DiscordClientSecret, cfg.PublicURL+"/auth/discord/callback")
	blobs, err := blob.NewS3(ctx, cfg.S3)
	if err != nil {
		return fmt.Errorf("object storage: %w", err)
	}
	handler, hub := app.New(store, discord, app.Options{PublicURL: cfg.PublicURL, Blobs: blobs})
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
