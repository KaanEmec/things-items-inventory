package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("INVENTORY_ADDR", "")
	t.Setenv("INVENTORY_DATA", "")
	got := Load()
	if got.Addr != ":8080" || got.DataDir != "./data" {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("INVENTORY_ADDR", ":9000")
	t.Setenv("INVENTORY_DATA", "/data")
	got := Load()
	if got.Addr != ":9000" || got.DataDir != "/data" {
		t.Fatalf("unexpected config: %+v", got)
	}
}

func TestLoadPublicURLAndProxy(t *testing.T) {
	t.Setenv("INVENTORY_PUBLIC_URL", "https://stuff.example.com/")
	t.Setenv("INVENTORY_TRUST_PROXY", "1")
	got := Load()
	if got.PublicURL != "https://stuff.example.com" || !got.TrustProxy {
		t.Fatalf("unexpected config: %+v", got)
	}
}
