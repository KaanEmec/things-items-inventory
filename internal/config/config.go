// Package config reads runtime configuration from the environment.
package config

import (
	"log/slog"
	"os"
	"strings"
)

// Config holds the runtime settings.
type Config struct {
	Addr    string
	DataDir string
	// PublicURL is the address people use to reach the app, for example
	// "https://stuff.example.com" (INVENTORY_PUBLIC_URL). An https:// value
	// makes session cookies Secure and prefixes printed reset links. Empty
	// is fine for local use.
	PublicURL string
	// TrustProxy makes the login rate limiter take the client address from
	// the last X-Forwarded-For entry (INVENTORY_TRUST_PROXY=1). Turn it on
	// only behind a reverse proxy that sets the header; otherwise anyone
	// could forge it.
	TrustProxy bool
	// SecretKey is the base64 instance key for encrypting stored API tokens
	// (INVENTORY_SECRET_KEY). Empty means <data>/secret.key is used.
	SecretKey Secret
	// AI holds the server-wide defaults a household uses for a role until
	// it saves its own profile (INVENTORY_AI_{VISION,TEXT}_{BASE_URL,TOKEN,MODEL}).
	AI AIDefaults
	// AIBlockPrivate refuses AI endpoints on loopback, private and
	// unique-local addresses (INVENTORY_AI_BLOCK_PRIVATE=1). Link-local and
	// cloud-metadata addresses are always refused.
	AIBlockPrivate bool
}

// Secret is a string that prints as "***" in logs and formatted output.
// Convert with string(s) where the real value is needed.
type Secret string

func (Secret) String() string   { return "***" }
func (Secret) GoString() string { return `"***"` }

// LogValue keeps the value out of slog output.
func (Secret) LogValue() slog.Value { return slog.StringValue("***") }

// AIEndpoint is one server-wide default endpoint.
type AIEndpoint struct {
	BaseURL string
	Token   Secret
	Model   string
}

// AIDefaults are the server-wide defaults per AI role.
type AIDefaults struct{ Vision, Text AIEndpoint }

// Load reads INVENTORY_ADDR, INVENTORY_DATA, INVENTORY_PUBLIC_URL and
// INVENTORY_TRUST_PROXY, applying defaults.
func Load() Config {
	return Config{
		Addr:           env("INVENTORY_ADDR", ":8080"),
		DataDir:        env("INVENTORY_DATA", "./data"),
		PublicURL:      strings.TrimRight(os.Getenv("INVENTORY_PUBLIC_URL"), "/"),
		TrustProxy:     os.Getenv("INVENTORY_TRUST_PROXY") == "1",
		SecretKey:      Secret(os.Getenv("INVENTORY_SECRET_KEY")),
		AIBlockPrivate: boolEnv("INVENTORY_AI_BLOCK_PRIVATE"),
		AI: AIDefaults{
			Vision: aiEndpoint("VISION"),
			Text:   aiEndpoint("TEXT"),
		},
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func aiEndpoint(role string) AIEndpoint {
	p := "INVENTORY_AI_" + role + "_"
	return AIEndpoint{
		BaseURL: strings.TrimSpace(os.Getenv(p + "BASE_URL")),
		Token:   Secret(strings.TrimSpace(os.Getenv(p + "TOKEN"))),
		Model:   strings.TrimSpace(os.Getenv(p + "MODEL")),
	}
}

func boolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
