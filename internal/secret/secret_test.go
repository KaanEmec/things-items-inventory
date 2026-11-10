package secret

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSealOpenAndBinding(t *testing.T) {
	dir := t.TempDir()
	b, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	ct1, _ := b.Seal([]byte("tok"), []byte("h1/vision"))
	ct2, _ := b.Seal([]byte("tok"), []byte("h1/vision"))
	if bytes.Equal(ct1, ct2) {
		t.Fatal("nonce reused")
	}
	if got, err := b.Open(ct1, []byte("h1/vision")); err != nil || string(got) != "tok" {
		t.Fatalf("open: %q %v", got, err)
	}
	for _, aad := range []string{"h2/vision", "h1/text", ""} {
		if _, err := b.Open(ct1, []byte(aad)); err == nil {
			t.Fatalf("opened with aad %q", aad)
		}
	}
	ct1[len(ct1)-1] ^= 1
	if _, err := b.Open(ct1, []byte("h1/vision")); err == nil {
		t.Fatal("tampered value opened")
	}
}

func TestKeyFileCreatedOnceWithMode0600(t *testing.T) {
	dir := t.TempDir()
	b1, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil || fi.Size() != 32 {
		t.Fatalf("key file: %v %v", fi, err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	ct, _ := b1.Seal([]byte("x"), nil)
	b2, _ := Load(dir, "")
	if _, err := b2.Open(ct, nil); err != nil {
		t.Fatal("second load uses a different key")
	}
}

func TestEnvKeyWins(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	b, err := Load("", base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	ct, _ := b.Seal([]byte("x"), nil)
	b2, _ := New(key)
	if _, err := b2.Open(ct, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("", "short"); err == nil {
		t.Fatal("bad env key accepted")
	}
	if _, err := Load("", ""); err == nil {
		t.Fatal("no key source accepted")
	}
}
