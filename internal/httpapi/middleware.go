package httpapi

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// statusWriter records the status code for the request log.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// requestID sets X-Request-ID, keeping a sane one sent by the caller.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
			r.Header.Set("X-Request-ID", id)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

// logRequests logs method, path, status, duration and request ID. Never
// bodies, queries, Authorization or Cookie.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		slog.Info("request",
			"method", r.Method,
			"path", logPath(r.URL.Path),
			"status", sw.status,
			"duration", time.Since(start),
			"request_id", r.Header.Get("X-Request-ID"),
		)
	})
}

// logPath hides secrets that travel in the path: an invite token in
// /api/v1/invites/{token}[/accept]. (DELETE /invites/{id} is redacted too,
// which costs nothing.)
func logPath(p string) string {
	rest, ok := strings.CutPrefix(p, "/api/v1/invites/")
	if !ok {
		return p
	}
	_, tail, _ := strings.Cut(rest, "/")
	if tail != "" {
		tail = "/" + tail
	}
	return "/api/v1/invites/{token}" + tail
}

// recoverPanics turns a panic in a handler into an internal error.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.Error("panic", "value", v, "request_id", r.Header.Get("X-Request-ID"))
				WriteError(w, NewError(CodeInternal, "Something went wrong. Please try again."))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// 'unsafe-inline' styles: Svelte sets inline style attributes.
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data:; "+
			"style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

// compressible reports whether a Content-Type is worth gzipping: text, JSON,
// XML, SVG and the like. Images, video, fonts and archives already are
// compressed.
func compressible(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch {
	case strings.HasPrefix(ct, "text/"), strings.HasSuffix(ct, "+json"), strings.HasSuffix(ct, "+xml"):
		return true
	}
	switch ct {
	case "application/json", "application/xml", "application/javascript", "image/svg+xml", "application/x-ndjson":
		return true
	}
	return false
}

// decide runs once, when the status is known, and chooses whether to compress.
func (w *gzipWriter) decide(code int, first []byte) {
	if w.decided {
		return
	}
	w.decided = true
	h := w.Header()
	if h.Get("Content-Encoding") != "" || code == http.StatusNoContent || code == http.StatusNotModified ||
		code == http.StatusPartialContent || code < 200 {
		return
	}
	ct := h.Get("Content-Type")
	if ct == "" && len(first) > 0 {
		ct = http.DetectContentType(first)
	}
	if !compressible(ct) {
		return
	}
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	w.gz = gzip.NewWriter(w.ResponseWriter)
}

func (w *gzipWriter) WriteHeader(code int) {
	w.decide(code, nil)
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipWriter) Write(b []byte) (int, error) {
	w.decide(http.StatusOK, b)
	if w.gz == nil {
		return w.ResponseWriter.Write(b)
	}
	return w.gz.Write(b)
}

func (w *gzipWriter) close() {
	if w.gz != nil {
		w.gz.Close()
	}
}

func (w *gzipWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// gzipAPI compresses /api/ responses for clients that accept gzip, except
// those whose Content-Type is already compressed (photos).
func gzipAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}
