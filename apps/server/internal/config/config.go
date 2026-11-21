package config

import (
	"fmt"
	"os"
	"strconv"

	"discordo/server/internal/blob"
	"discordo/server/internal/media/livekit"
	"discordo/server/internal/media/pion"
)

type Config struct {
	Addr                string
	DatabaseURL         string
	PublicURL           string // how browsers reach this server; used for the OAuth redirect
	DiscordClientID     string
	DiscordClientSecret string
	S3                  blob.Config    // uploaded files
	MediaProvider       string         // "pion" or "livekit"
	Media               pion.Config    // built-in SFU
	LiveKit             livekit.Config // MediaProvider "livekit"
}

func Load() (Config, error) {
	provider := env("MEDIA_PROVIDER", "pion")
	if provider != "pion" && provider != "livekit" {
		return Config{}, fmt.Errorf("MEDIA_PROVIDER %q: want pion or livekit", provider)
	}
	port, err := strconv.Atoi(env("MEDIA_UDP_PORT", "7882"))
	if err != nil {
		return Config{}, fmt.Errorf("MEDIA_UDP_PORT: %w", err)
	}
	c := Config{
		Addr:                env("HTTP_ADDR", ":8080"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		PublicURL:           env("PUBLIC_URL", "http://localhost:8080"),
		DiscordClientID:     os.Getenv("DISCORD_CLIENT_ID"),
		DiscordClientSecret: os.Getenv("DISCORD_CLIENT_SECRET"),
		MediaProvider:       provider,
		LiveKit: livekit.Config{
			URL:       os.Getenv("LIVEKIT_URL"),
			APIURL:    os.Getenv("LIVEKIT_API_URL"),
			APIKey:    os.Getenv("LIVEKIT_API_KEY"),
			APISecret: os.Getenv("LIVEKIT_API_SECRET"),
		},
		Media: pion.Config{
			UDPPort:  port,
			PublicIP: os.Getenv("MEDIA_PUBLIC_IP"),
		},
		S3: blob.Config{
			Endpoint:  env("S3_ENDPOINT", "localhost:9000"),
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
			Bucket:    env("S3_BUCKET", "discordo"),
			Secure:    os.Getenv("S3_SECURE") == "true",
		},
	}
	required := map[string]string{
		"DATABASE_URL": c.DatabaseURL, "DISCORD_CLIENT_ID": c.DiscordClientID, "DISCORD_CLIENT_SECRET": c.DiscordClientSecret,
		"S3_ACCESS_KEY": c.S3.AccessKey, "S3_SECRET_KEY": c.S3.SecretKey,
	}
	if provider == "livekit" {
		required["LIVEKIT_URL"], required["LIVEKIT_API_KEY"], required["LIVEKIT_API_SECRET"] = c.LiveKit.URL, c.LiveKit.APIKey, c.LiveKit.APISecret
	}
	for name, v := range required {
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
