package photos

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
)

type env struct {
	t    *testing.T
	d    *db.DB
	svc  *Service
	srv  *httptest.Server
	dir  string
	hidA string
	hidB string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d := dbtest.New(t)
	dir := t.TempDir()
	svc := New(d, config.Config{DataDir: dir})
	svc.SetTargetHook(func(ctx context.Context, h, u string, tg Target, role string) (bool, error) { return true, nil })
	e := &env{t: t, d: d, svc: svc, dir: dir}
	for _, p := range []*string{&e.hidA, &e.hidB} {
		*p = id.New()
		if _, err := sqlc.New(d.Write).CreateHousehold(context.Background(), sqlc.CreateHouseholdParams{
			ID: *p, Name: "H", Language: "en", Currency: "EUR", CreatedAt: svc.stamp(), UpdatedAt: svc.stamp()}); err != nil {
			t.Fatal(err)
		}
	}
	e.srv = httpapitest.Server(t, func(mux *httpapi.Mux) {
		mux.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if h := r.Header.Get("X-H"); h != "" {
					r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: "u1", HouseholdID: h, Role: auth.RoleMember, Source: auth.SourceSession}))
				}
				next.ServeHTTP(w, r)
			})
		})
	}, svc.Routes)
	return e
}

func (e *env) do(hid, method, path string, ctype string, body io.Reader) (int, http.Header, []byte) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if hid != "" {
		req.Header.Set("X-H", hid)
	}
	req.Header.Set("Accept-Encoding", "identity")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, b
}

func (e *env) upload(hid string, file []byte, fields map[string]string) (int, map[string]any) {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", "x.jpg")
	fw.Write(file)
	mw.Close()
	status, _, b := e.do(hid, "POST", "/api/v1/photos", mw.FormDataContentType(), &buf)
	var out map[string]any
	json.Unmarshal(b, &out)
	return status, out
}

func (e *env) files() (photos, tmp []string) {
	walk := func(root string) (out []string) {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				out = append(out, p)
			}
			return nil
		})
		return
	}
	return walk(filepath.Join(e.dir, "photos")), walk(filepath.Join(e.dir, "tmp"))
}

// exifJPEG makes a wide JPEG whose left half is red and right half blue,
// carrying EXIF orientation o and a fake GPS marker.
func exifJPEG(t *testing.T, w, h, o int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if x >= w/2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, byte(o), 0, 0, 0, 0, 0, 0, 0}
	seg := append(append([]byte("Exif\x00\x00"), tiff...), "GPSMARKER-52.5N"...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	b := buf.Bytes()
	return append(append(append([]byte{}, b[:2]...), append(app1, seg...)...), b[2:]...)
}

func plainJPEG(t *testing.T, w, h int, seed uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = uint8(i) + seed
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, nil)
	return buf.Bytes()
}

func (e *env) waitDerivatives(hid, photoID string) {
	e.t.Helper()
	for i := 0; i < 400; i++ {
		p, err := sqlc.New(e.d.Read).GetPhoto(context.Background(), sqlc.GetPhotoParams{HouseholdID: hid, ID: photoID})
		if err == nil && p.Derivatives == 1 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	e.t.Fatal("derivatives never appeared")
}

func photoID(out map[string]any) string { return out["photo"].(map[string]any)["id"].(string) }
func linkID(out map[string]any) string  { return out["link"].(map[string]any)["id"].(string) }

func itemFields() map[string]string {
	return map[string]string{"role": "item", "item_id": id.New()}
}

func TestOrientedDerivativesViaJob(t *testing.T) {
	e := newEnv(t)
	q := jobs.New(e.d, jobs.Options{})
	e.svc.UseQueue(q)
	orig := exifJPEG(t, 3000, 2000, 6)
	status, out := e.upload(e.hidA, orig, itemFields())
	if status != 201 {
		t.Fatalf("upload: %d %v", status, out)
	}
	pid := photoID(out)
	if w, h := out["photo"].(map[string]any)["width"], out["photo"].(map[string]any)["height"]; w != 2000.0 || h != 3000.0 {
		t.Fatalf("stored size %vx%v, want 2000x3000 (upright)", w, h)
	}

	// Before the job runs, every size falls back to the original.
	status, hdr, body := e.do(e.hidA, "GET", "/api/v1/photos/"+pid+"/thumb", "", nil)
	if status != 200 || !bytes.Equal(body, orig) || hdr.Get("Content-Type") != "image/jpeg" || hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("fallback: %d %v", status, hdr)
	}
	if strings.Contains(hdr.Get("Cache-Control"), "immutable") {
		t.Fatal("a stand-in must not be cached as immutable")
	}

	if err := q.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer q.Stop(context.Background())
	e.waitDerivatives(e.hidA, pid)

	for size, side := range map[string]int{"thumb": 320, "screen": 1600, "ai": 1568} {
		status, hdr, body := e.do(e.hidA, "GET", "/api/v1/photos/"+pid+"/"+size, "", nil)
		if status != 200 || hdr.Get("Content-Type") != "image/jpeg" || !strings.Contains(hdr.Get("Cache-Control"), "immutable") {
			t.Fatalf("%s: %d %v", size, status, hdr)
		}
		img, err := jpeg.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		if b.Dy() != side || b.Dx() >= b.Dy() {
			t.Fatalf("%s is %dx%d, want portrait with height %d", size, b.Dx(), b.Dy(), side)
		}
		if r, _, bl, _ := img.At(b.Dx()/2, 3*b.Dy()/100+2).RGBA(); r>>8 < 180 || bl>>8 > 90 {
			t.Fatalf("%s: top is not red, so it is not upright", size)
		}
		if r, _, bl, _ := img.At(b.Dx()/2, b.Dy()-3*b.Dy()/100-2).RGBA(); bl>>8 < 180 || r>>8 > 90 {
			t.Fatalf("%s: bottom is not blue", size)
		}
		if bytes.Contains(body, []byte("Exif")) || bytes.Contains(body, []byte("GPSMARKER")) {
			t.Fatalf("%s carries EXIF/GPS", size)
		}
		if orientationOf(bytes.NewReader(body), "image/jpeg") != 1 {
			t.Fatalf("%s has an orientation tag", size)
		}
	}
	// The original is served unchanged under orig.
	if _, _, body := e.do(e.hidA, "GET", "/api/v1/photos/"+pid+"/orig", "", nil); !bytes.Equal(body, orig) {
		t.Fatal("orig differs")
	}
	ai, err := e.svc.AIBytes(context.Background(), e.hidA, pid)
	if err != nil || len(ai) == 0 {
		t.Fatalf("AIBytes: %v", err)
	}
	if _, err := e.svc.AIBytes(context.Background(), e.hidB, pid); err != ErrNotFound {
		t.Fatalf("AIBytes for another household: %v", err)
	}
	// And by sha256.
	sha := out["photo"].(map[string]any)["sha256"].(string)
	if status, _, _ := e.do(e.hidA, "GET", "/api/v1/photos/"+sha+"/screen", "", nil); status != 200 {
		t.Fatalf("by sha: %d", status)
	}
}

func TestDerivativesInlineWithoutQueue(t *testing.T) {
	e := newEnv(t)
	status, out := e.upload(e.hidA, exifJPEG(t, 800, 600, 8), itemFields())
	if status != 201 || out["photo"].(map[string]any)["derivatives"] != true {
		t.Fatalf("%d %v", status, out)
	}
	if files, _ := e.files(); len(files) != 4 {
		t.Fatalf("want original + 3 derivatives, got %v", files)
	}
}

func TestDedupe(t *testing.T) {
	e := newEnv(t)
	img := plainJPEG(t, 64, 64, 1)
	f := itemFields()
	s1, o1 := e.upload(e.hidA, img, f)
	s2, o2 := e.upload(e.hidA, img, f) // same bytes, same link: no-op
	if s1 != 201 || s2 != 200 || photoID(o1) != photoID(o2) || linkID(o1) != linkID(o2) {
		t.Fatalf("%d %d %v %v", s1, s2, o1, o2)
	}
	// Same bytes on another item: one photo row, one file, two links.
	s3, o3 := e.upload(e.hidA, img, itemFields())
	if s3 != 201 || photoID(o3) != photoID(o1) || linkID(o3) == linkID(o1) {
		t.Fatalf("%d %v", s3, o3)
	}
	files, _ := e.files()
	if len(files) != 4 {
		t.Fatalf("stored more than once: %v", files)
	}
	var n int
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM photos`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d photo rows", n)
	}
	// Another household uploading the same bytes gets its own row, same files.
	s4, o4 := e.upload(e.hidB, img, itemFields())
	if s4 != 201 || photoID(o4) == photoID(o1) {
		t.Fatalf("%d %v", s4, o4)
	}
	if files, _ := e.files(); len(files) != 4 {
		t.Fatalf("files duplicated across households: %v", files)
	}
}

func pngWithSize(t *testing.T, w, h uint32) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1)))
	b := buf.Bytes()
	binary.BigEndian.PutUint32(b[16:], w)
	binary.BigEndian.PutUint32(b[20:], h)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

func TestRejectsLeaveNothingOnDisk(t *testing.T) {
	e := newEnv(t)
	good := plainJPEG(t, 200, 200, 3)
	cases := map[string]struct {
		body   []byte
		status int
	}{
		"text":      {[]byte("hello, this is not an image at all"), 415},
		"gif":       {[]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), 415},
		"svg":       {[]byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), 415},
		"html":      {[]byte("<html><script>alert(1)</script></html>"), 415},
		"empty":     {nil, 415},
		"truncated": {good[:len(good)/2], 400},
		"garbage":   {append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte("garbage"), 500)...), 400},
		"bomb":      {pngWithSize(t, 10000, 10000), 400},
	}
	for name, c := range cases {
		status, out := e.upload(e.hidA, c.body, itemFields())
		if status != c.status {
			t.Errorf("%s: status %d, want %d (%v)", name, status, c.status, out)
		}
	}
	if status, out := e.upload(e.hidA, []byte("x"), itemFields()); status != 415 {
		t.Errorf("%d %v", status, out)
	}
	if p, tmp := e.files(); len(p)+len(tmp) != 0 {
		t.Fatalf("left on disk: %v %v", p, tmp)
	}
	var n int
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM photos`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d rows", n)
	}
	// A valid photo for a target that does not exist leaves nothing either.
	e.svc.SetTargetHook(func(context.Context, string, string, Target, string) (bool, error) { return false, nil })
	if status, _ := e.upload(e.hidA, good, itemFields()); status != 404 {
		t.Fatalf("missing target: %d", status)
	}
	if p, tmp := e.files(); len(p)+len(tmp) != 0 {
		t.Fatalf("left on disk: %v %v", p, tmp)
	}
	// Bad form values.
	e.svc.SetTargetHook(nil)
	for _, f := range []map[string]string{{}, {"role": "item"}, {"role": "nonsense", "item_id": "x"}, {"role": "place", "item_id": "x"}, {"role": "item", "item_id": "../x"}} {
		if status, _ := e.upload(e.hidA, good, f); status != 400 {
			t.Errorf("%v: %d", f, status)
		}
	}
	// Without a hook and without items/places tables, linking is a 404.
	if status, _ := e.upload(e.hidA, good, itemFields()); status != 404 {
		t.Fatalf("no hook: %d", status)
	}
	if p, tmp := e.files(); len(p)+len(tmp) != 0 {
		t.Fatalf("left on disk: %v %v", p, tmp)
	}
	// Not multipart at all.
	if status, _, _ := e.do(e.hidA, "POST", "/api/v1/photos", "application/json", strings.NewReader("{}")); status != 400 {
		t.Fatalf("json upload: %d", status)
	}
}

func TestOversize(t *testing.T) {
	e := newEnv(t)
	e.svc.maxUpload = 2000
	big := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0}, 5000)...)
	if status, out := e.upload(e.hidA, big, itemFields()); status != 413 {
		t.Fatalf("%d %v", status, out)
	}
	if p, tmp := e.files(); len(p)+len(tmp) != 0 {
		t.Fatalf("left on disk: %v %v", p, tmp)
	}
	ok := plainJPEG(t, 32, 32, 0)
	if len(ok) > 2000 {
		t.Skip("fixture too big")
	}
	if status, _ := e.upload(e.hidA, ok, itemFields()); status != 201 {
		t.Fatalf("small file: %d", status)
	}
}

func TestCrossHousehold(t *testing.T) {
	e := newEnv(t)
	_, out := e.upload(e.hidB, plainJPEG(t, 50, 50, 9), itemFields())
	pid, lid := photoID(out), linkID(out)
	sha := out["photo"].(map[string]any)["sha256"].(string)
	for _, ref := range []string{pid, sha} {
		for _, size := range []string{"orig", "thumb", "screen", "ai"} {
			if status, _, body := e.do(e.hidA, "GET", "/api/v1/photos/"+ref+"/"+size, "", nil); status != 404 || bytes.Contains(body, []byte(sha)) {
				t.Fatalf("%s/%s: %d", ref, size, status)
			}
		}
		if status, _, _ := e.do(e.hidB, "GET", "/api/v1/photos/"+ref+"/orig", "", nil); status != 200 {
			t.Fatalf("own photo: %d", status)
		}
	}
	if status, _, _ := e.do("", "GET", "/api/v1/photos/"+pid+"/orig", "", nil); status != 401 {
		t.Fatalf("anonymous: %d", status)
	}
	if status, _, _ := e.do(e.hidA, "PATCH", "/api/v1/photo-links/"+lid, "application/json", strings.NewReader(`{"sort":0}`)); status != 404 {
		t.Fatalf("patch: %d", status)
	}
	if status, _, _ := e.do(e.hidA, "DELETE", "/api/v1/photo-links/"+lid, "", nil); status != 404 {
		t.Fatalf("delete: %d", status)
	}
	body := `{"photo_id":"` + pid + `","item_id":"` + id.New() + `","role":"item"}`
	if status, _, _ := e.do(e.hidA, "POST", "/api/v1/photo-links", "application/json", strings.NewReader(body)); status != 404 {
		t.Fatalf("link foreign photo: %d", status)
	}
	if files, _ := e.files(); len(files) == 0 {
		t.Fatal("household B's files vanished")
	}
}

func TestUnlinkRemovesFilesWithLastLink(t *testing.T) {
	e := newEnv(t)
	img := plainJPEG(t, 100, 80, 5)
	_, o1 := e.upload(e.hidA, img, itemFields())
	pid := photoID(o1)
	item2 := id.New()
	body := `{"photo_id":"` + pid + `","item_id":"` + item2 + `","role":"label"}`
	status, _, b := e.do(e.hidA, "POST", "/api/v1/photo-links", "application/json", strings.NewReader(body))
	if status != 201 {
		t.Fatalf("link: %d %s", status, b)
	}
	var l2 map[string]map[string]any
	json.Unmarshal(b, &l2)
	// Same photo in household B too: files must outlive A's last link.
	_, ob := e.upload(e.hidB, img, itemFields())

	if status, _, _ := e.do(e.hidA, "DELETE", "/api/v1/photo-links/"+linkID(o1), "", nil); status != 204 {
		t.Fatalf("delete 1: %d", status)
	}
	if files, _ := e.files(); len(files) != 4 {
		t.Fatalf("files removed with a link left: %v", files)
	}
	if status, _, _ := e.do(e.hidA, "GET", "/api/v1/photos/"+pid+"/orig", "", nil); status != 200 {
		t.Fatalf("photo gone too early: %d", status)
	}
	if status, _, _ := e.do(e.hidA, "DELETE", "/api/v1/photo-links/"+l2["link"]["id"].(string), "", nil); status != 204 {
		t.Fatalf("delete 2: %d", status)
	}
	if status, _, _ := e.do(e.hidA, "GET", "/api/v1/photos/"+pid+"/orig", "", nil); status != 404 {
		t.Fatalf("photo row should be gone: %d", status)
	}
	if files, _ := e.files(); len(files) != 4 {
		t.Fatalf("B still uses the file: %v", files)
	}
	if status, _, _ := e.do(e.hidB, "DELETE", "/api/v1/photo-links/"+linkID(ob), "", nil); status != 204 {
		t.Fatal(status)
	}
	if files, tmp := e.files(); len(files)+len(tmp) != 0 {
		t.Fatalf("files remain after the last link: %v %v", files, tmp)
	}
	if status, _, _ := e.do(e.hidA, "DELETE", "/api/v1/photo-links/"+linkID(o1), "", nil); status != 404 {
		t.Fatalf("second delete: %d", status)
	}
}

func TestReorderAndMain(t *testing.T) {
	e := newEnv(t)
	item := id.New()
	var links []string
	for i := 0; i < 3; i++ {
		_, o := e.upload(e.hidA, plainJPEG(t, 40, 40, uint8(i*7)), map[string]string{"role": "item", "item_id": item})
		links = append(links, linkID(o))
	}
	status, _, b := e.do(e.hidA, "PATCH", "/api/v1/photo-links/"+links[2], "application/json", strings.NewReader(`{"sort":0}`))
	if status != 200 {
		t.Fatalf("%d %s", status, b)
	}
	got, err := e.svc.LinksOf(context.Background(), e.hidA, true, []string{item})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, l := range got[item] {
		order = append(order, l.LinkID)
	}
	if want := []string{links[2], links[0], links[1]}; strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order %v, want %v", order, want)
	}
	// Role change: item -> receipt is fine; item -> contents is not.
	if status, _, _ := e.do(e.hidA, "PATCH", "/api/v1/photo-links/"+links[1], "application/json", strings.NewReader(`{"role":"receipt"}`)); status != 200 {
		t.Fatalf("role: %d", status)
	}
	if status, _, _ := e.do(e.hidA, "PATCH", "/api/v1/photo-links/"+links[1], "application/json", strings.NewReader(`{"role":"contents"}`)); status != 400 {
		t.Fatalf("bad role: %d", status)
	}
	// Removing a target's links through UnlinkTarget deletes the files.
	if err := e.svc.UnlinkTarget(context.Background(), e.hidA, Target{ItemID: item}); err != nil {
		t.Fatal(err)
	}
	if files, _ := e.files(); len(files) != 0 {
		t.Fatalf("%v", files)
	}
}

func TestPathTraversal(t *testing.T) {
	e := newEnv(t)
	_, o := e.upload(e.hidA, plainJPEG(t, 40, 40, 0), itemFields())
	pid := photoID(o)
	secret := filepath.Join(e.dir, "secret.key")
	os.WriteFile(secret, []byte("TOPSECRET"), 0o600)
	for _, p := range []string{
		"/api/v1/photos/..%2fsecret.key/orig",
		"/api/v1/photos/..%2f..%2fsecret.key/thumb",
		"/api/v1/photos/" + pid + "/..%2f..%2fsecret.key",
		"/api/v1/photos/" + pid + "/orig%00",
		"/api/v1/photos/%2e%2e/%2e%2e/secret.key",
		"/api/v1/photos/" + pid + "/ORIG",
		"/api/v1/photos/" + pid + "/",
	} {
		status, _, body := e.do(e.hidA, "GET", p, "", nil)
		if status == 200 || bytes.Contains(body, []byte("TOPSECRET")) {
			t.Errorf("%s: %d", p, status)
		}
	}
	// The path helpers refuse anything that is not a hash.
	for _, bad := range []string{"../../etc/passwd", strings.Repeat("A", 64), strings.Repeat("a", 63), strings.Repeat("a", 62) + "/."} {
		if _, err := e.svc.origPath(bad, "jpg"); err == nil {
			t.Errorf("origPath accepted %q", bad)
		}
		if _, err := e.svc.derivPath(bad, "thumb"); err == nil {
			t.Errorf("derivPath accepted %q", bad)
		}
	}
	if _, err := e.svc.origPath(strings.Repeat("a", 64), "../x"); err == nil {
		t.Error("origPath accepted a bad extension")
	}
	if _, err := e.svc.derivPath(strings.Repeat("a", 64), "../x"); err == nil {
		t.Error("derivPath accepted a bad size")
	}
}

func TestOrientationParsing(t *testing.T) {
	for o := 1; o <= 8; o++ {
		if got := orientationOf(bytes.NewReader(exifJPEG(t, 20, 10, o)), "image/jpeg"); got != o {
			t.Errorf("orientation %d parsed as %d", o, got)
		}
	}
	if got := orientationOf(bytes.NewReader(plainJPEG(t, 20, 10, 0)), "image/jpeg"); got != 1 {
		t.Errorf("no EXIF: %d", got)
	}
	if got := orientationOf(bytes.NewReader([]byte("junk")), "image/webp"); got != 1 {
		t.Errorf("junk: %d", got)
	}
	// All eight orientations turn a 2x1 (left red, right blue) correctly.
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})
	src.Set(1, 0, color.RGBA{0, 0, 255, 255})
	wantDims := map[int][2]int{1: {2, 1}, 2: {2, 1}, 3: {2, 1}, 4: {2, 1}, 5: {1, 2}, 6: {1, 2}, 7: {1, 2}, 8: {1, 2}}
	for o, d := range wantDims {
		out := orientRGBA(src, o)
		if out.Bounds().Dx() != d[0] || out.Bounds().Dy() != d[1] {
			t.Errorf("o=%d dims %v", o, out.Bounds())
		}
	}
	if c := orientRGBA(src, 6).RGBAAt(0, 0); c.R != 255 { // 6: red goes to the top
		t.Errorf("o=6 top is %v", c)
	}
	if c := orientRGBA(src, 8).RGBAAt(0, 0); c.B != 255 { // 8: blue goes to the top
		t.Errorf("o=8 top is %v", c)
	}
}
