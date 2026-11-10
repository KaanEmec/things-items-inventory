// Package config reads runtime configuration from the environment.
package config

import "os"

// Config holds the runtime settings.
type Config struct {
	Addr    string
	DataDir string
}

// Load reads INVENTORY_ADDR and INVENTORY_DATA, applying defaults.
func Load() Config {
	return Config{
		Addr:    env("INVENTORY_ADDR", ":8080"),
		DataDir: env("INVENTORY_DATA", "./data"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
