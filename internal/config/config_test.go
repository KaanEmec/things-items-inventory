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

func TestTrustedProxyHeaders(t *testing.T) {
	t.Setenv("INVENTORY_TRUSTED_PROXIES", "10.0.0.1, 172.18.0.0/16, bogus")
	c := Load()
	if len(c.TrustedProxies) != 2 {
		t.Fatalf("proxies: %v", c.TrustedProxies)
	}
	// A forged header from an untrusted peer is ignored.
	if got := c.ClientIP("203.0.113.9:5555", "1.2.3.4"); got != "203.0.113.9" {
		t.Fatalf("untrusted peer: %s", got)
	}
	// A trusted peer: the rightmost entry that is not a proxy wins.
	if got := c.ClientIP("172.18.0.2:1", "6.6.6.6, 198.51.100.7, 172.18.0.5"); got != "198.51.100.7" {
		t.Fatalf("trusted peer: %s", got)
	}
	if got := c.ClientIP("10.0.0.1:1", "not-an-ip"); got != "10.0.0.1" {
		t.Fatalf("garbage header: %s", got)
	}
	if c.IsTrustedProxy("203.0.113.9") || !c.IsTrustedProxy("10.0.0.1") {
		t.Fatal("IsTrustedProxy")
	}
	// Legacy switch trusts every peer; nothing configured trusts none.
	if !(Config{TrustProxy: true}).IsTrustedProxy("1.1.1.1") || (Config{}).IsTrustedProxy("1.1.1.1") {
		t.Fatal("legacy switch")
	}
}
