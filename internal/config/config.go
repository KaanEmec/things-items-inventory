// Package config reads runtime configuration from the environment.
package config

import (
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
}

// Load reads INVENTORY_ADDR, INVENTORY_DATA, INVENTORY_PUBLIC_URL and
// INVENTORY_TRUST_PROXY, applying defaults.
func Load() Config {
	return Config{
		Addr:       env("INVENTORY_ADDR", ":8080"),
		DataDir:    env("INVENTORY_DATA", "./data"),
		PublicURL:  strings.TrimRight(os.Getenv("INVENTORY_PUBLIC_URL"), "/"),
		TrustProxy: os.Getenv("INVENTORY_TRUST_PROXY") == "1",
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
