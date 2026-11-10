package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestCheckAddr(t *testing.T) {
	always := []string{"169.254.169.254", "169.254.0.1", "fe80::1", "0.0.0.0", "::", "::ffff:169.254.169.254", "fd00:ec2::254"}
	private := []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1", "100.64.0.1", "100.127.255.255", "fd12::1", "fc00::1"}
	public := []string{"8.8.8.8", "1.1.1.1", "172.32.0.1", "100.128.0.1", "2606:4700::1111"}
	for _, s := range always {
		for _, bp := range []bool{false, true} {
			if checkAddr(netip.MustParseAddr(s), bp) == nil {
				t.Errorf("%s allowed (blockPrivate=%v)", s, bp)
			}
		}
	}
	for _, s := range private {
		ip := netip.MustParseAddr(s)
		if checkAddr(ip, false) != nil {
			t.Errorf("%s blocked with blockPrivate=false", s)
		}
		if checkAddr(ip, true) == nil {
			t.Errorf("%s allowed with blockPrivate=true", s)
		}
	}
	for _, s := range public {
		if checkAddr(netip.MustParseAddr(s), true) != nil {
			t.Errorf("%s blocked", s)
		}
	}
}

func TestHTTPClientBlocksLoopbackWhenAsked(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { reply(w, `{"name":"a"}`) })
	c := New(NewHTTPClient(true))
	_, err := c.Complete(context.Background(), f.cfg(ModePrompt), req, validName)
	assertKind(t, err, KindUnreachable)
	if !strings.Contains(err.Error(), "not allowed") || len(f.reqs) != 0 {
		t.Fatalf("%v reqs %d", err, len(f.reqs))
	}
	_, err = c.Models(context.Background(), f.cfg(""))
	assertKind(t, err, KindUnreachable)
}

func TestHTTPClientAllowsLoopbackByDefault(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { reply(w, `{"name":"a"}`) })
	c := New(NewHTTPClient(false))
	if _, err := c.Complete(context.Background(), f.cfg(ModePrompt), req, validName); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPClientBlocksMetadataAndRedirect(t *testing.T) {
	c := New(NewHTTPClient(false))
	cfg := Config{BaseURL: "http://169.254.169.254/v1", Token: secret, Model: "m", Mode: ModePrompt}
	_, err := c.Complete(context.Background(), cfg, req, validName)
	assertKind(t, err, KindUnreachable)
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatal(err)
	}
	// An allowed server redirecting to a blocked address.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	_, err = c.Complete(context.Background(), Config{BaseURL: srv.URL, Token: secret, Model: "m", Mode: ModePrompt}, req, validName)
	assertKind(t, err, KindUnreachable)
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatal(err)
	}
}

func TestResponseBodyCapped(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		w.Write([]byte(`{"choices":[{"message":{"content":"`))
		w.Write([]byte(strings.Repeat("a", maxBody+10)))
	})
	c, _ := testClient()
	_, err := c.Complete(context.Background(), f.cfg(ModePrompt), req, validName)
	assertKind(t, err, KindOther)
}

func TestTokenSplitAcrossSnippetBoundary(t *testing.T) {
	for pad := 280; pad <= 300; pad++ {
		body := strings.Repeat("x", pad) + secret + " tail"
		e := classify(500, body, secret)
		if strings.Contains(e.Message, "sk-") || strings.Contains(e.Message, "SECRET") {
			t.Fatalf("pad %d leaked: %s", pad, e.Message)
		}
	}
}
