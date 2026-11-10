package config

import (
	"fmt"
	"os"

	"discordo/server/internal/blob"
)

type Config struct {
	Addr                string
	DatabaseURL         string
	PublicURL           string // how browsers reach this server; used for the OAuth redirect
	DiscordClientID     string
	DiscordClientSecret string
	S3                  blob.Config // uploaded files
}

func Load() (Config, error) {
	c := Config{
		Addr:                env("HTTP_ADDR", ":8080"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		PublicURL:           env("PUBLIC_URL", "http://localhost:8080"),
		DiscordClientID:     os.Getenv("DISCORD_CLIENT_ID"),
		DiscordClientSecret: os.Getenv("DISCORD_CLIENT_SECRET"),
		S3: blob.Config{
			Endpoint:  env("S3_ENDPOINT", "localhost:9000"),
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
			Bucket:    env("S3_BUCKET", "discordo"),
			Secure:    os.Getenv("S3_SECURE") == "true",
		},
	}
	for name, v := range map[string]string{
		"DATABASE_URL": c.DatabaseURL, "DISCORD_CLIENT_ID": c.DiscordClientID, "DISCORD_CLIENT_SECRET": c.DiscordClientSecret,
		"S3_ACCESS_KEY": c.S3.AccessKey, "S3_SECRET_KEY": c.S3.SecretKey,
	} {
		if v == "" {
			return c, fmt.Errorf("missing env %s", name)
		}
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
