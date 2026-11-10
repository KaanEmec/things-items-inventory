// Package config reads runtime configuration from the environment.
package config

import (
	"log/slog"
	"net"
	"net/netip"
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
	// TrustedProxies lists the proxy addresses (IPs or CIDRs) whose
	// X-Forwarded-For and X-Forwarded-Proto headers are believed
	// (INVENTORY_TRUSTED_PROXIES, comma separated). When set, headers from
	// any other peer are ignored; it makes TrustProxy unnecessary.
	TrustedProxies []netip.Prefix
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
		TrustedProxies: parseProxies(os.Getenv("INVENTORY_TRUSTED_PROXIES")),
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

func parseProxies(v string) []netip.Prefix {
	var out []netip.Prefix
	for _, f := range strings.Split(v, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if p, err := netip.ParsePrefix(f); err == nil {
			out = append(out, p.Masked())
		} else if a, err := netip.ParseAddr(f); err == nil {
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
		} else {
			slog.Warn("ignoring invalid INVENTORY_TRUSTED_PROXIES entry", "entry", f)
		}
	}
	return out
}

// IsTrustedProxy reports whether ip may set forwarding headers. With a
// TrustedProxies list only those addresses qualify; otherwise TrustProxy
// (INVENTORY_TRUST_PROXY=1) trusts every peer.
func (c Config) IsTrustedProxy(ip string) bool {
	if len(c.TrustedProxies) == 0 {
		return c.TrustProxy
	}
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, p := range c.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ClientIP returns the caller's address: the TCP peer, or, when that peer is
// a trusted proxy, the first X-Forwarded-For entry (from the right) that is
// not itself a trusted proxy.
func (c Config) ClientIP(remoteAddr, xff string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	if !c.IsTrustedProxy(host) || xff == "" {
		return host
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if net.ParseIP(ip) == nil {
			return host
		}
		if len(c.TrustedProxies) == 0 || !c.IsTrustedProxy(ip) {
			return ip
		}
	}
	return host
}
