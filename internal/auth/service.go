// Package auth is accounts, sessions, households and roles: first-run setup,
// login, the request identity and the role checks every other feature uses.
//
// A feature package's handlers wrap themselves in auth.Require(action, h) (or
// auth.RequireSession for app-internal endpoints), read auth.HouseholdID(ctx)
// and pass it to every query. See roles.go for the action table.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

const (
	cookieName       = "session"
	sessionLifetime  = 30 * 24 * time.Hour
	sessionRefreshAt = time.Hour // last_seen_at/expires_at are rewritten at most this often
	resetLifetime    = 24 * time.Hour
	timeLayout       = "2006-01-02T15:04:05.000Z"
)

// Service holds what the auth endpoints and middleware need.
type Service struct {
	db            *db.DB
	cfg           config.Config
	now           func() time.Time
	limit         *limiter
	authenticator []Authenticator
}

// New returns the auth service. Register its routes with Routes.
func New(d *db.DB, cfg config.Config) *Service {
	s := &Service{db: d, cfg: cfg, now: func() time.Time { return time.Now().UTC() }}
	s.limit = newLimiter(func() time.Time { return s.now() })
	return s
}

// AddAuthenticator adds another way to sign in (J9.1: bearer access
// tokens). Authenticators run before the session cookie is considered.
// Call it before serving.
func (s *Service) AddAuthenticator(a Authenticator) { s.authenticator = append(s.authenticator, a) }

// Routes registers the middleware and every endpoint of this package.
func (s *Service) Routes(mux *httpapi.Mux) {
	mux.Use(s.csrf)
	mux.Use(s.authenticate)

	mux.HandleFunc("GET /api/v1/setup", s.getSetup)
	mux.HandleFunc("POST /api/v1/setup", s.postSetup)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/logout", RequireSession(ActionSession, s.logout))
	mux.HandleFunc("POST /api/v1/auth/logout-all", RequireSession(ActionSession, s.logoutAll))
	mux.HandleFunc("POST /api/v1/auth/reset", s.reset)

	mux.HandleFunc("GET /api/v1/me", RequireSession(ActionSession, s.getMe))
	mux.HandleFunc("PATCH /api/v1/me", RequireSession(ActionSession, s.patchMe))
	mux.HandleFunc("POST /api/v1/me/password", RequireSession(ActionSession, s.changePassword))
	mux.HandleFunc("PUT /api/v1/me/household", RequireSession(ActionSession, s.switchHousehold))

	mux.HandleFunc("GET /api/v1/household", RequireSession(ActionHouseholdRead, s.getHousehold))
	mux.HandleFunc("PATCH /api/v1/household", RequireSession(ActionHouseholdEdit, s.patchHousehold))
	mux.HandleFunc("POST /api/v1/members/{user_id}/reset-link", RequireSession(ActionMembersManage, s.createResetLink))
}

func (s *Service) ts(t time.Time) string { return t.UTC().Format(timeLayout) }

// newToken returns a random secret (32 bytes, URL-safe base64) and the
// SHA-256 hex digest that is stored instead of it.
func newToken() (token, hash string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("auth: crypto/rand: " + err.Error())
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) secureCookie(r *http.Request) bool {
	return r.TLS != nil || strings.HasPrefix(s.cfg.PublicURL, "https://")
}

func (s *Service) setCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", Expires: expires, MaxAge: int(time.Until(expires).Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureCookie(r),
	})
}

func (s *Service) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureCookie(r),
	})
}

// clientAddr is the address used for rate limiting and recorded on sessions.
func (s *Service) clientAddr(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// csrf rejects requests that change data and came from another site. It
// covers every non-bearer unsafe request, including login, so a hostile page
// can not sign someone in to an attacker's account either.
func (s *Service) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && crossSite(r) {
				httpapi.WriteError(w, httpapi.NewError(httpapi.CodeForbidden, "This request came from another site and was blocked."))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func crossSite(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err != nil || !strings.EqualFold(u.Host, r.Host)
	}
	return false
}

// authenticate puts the caller's Identity in the request context when the
// request carries a valid credential. It never rejects: Require does that.
func (s *Service) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, a := range s.authenticator {
			id, ok, err := a(r)
			if err != nil {
				break
			}
			if ok {
				next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
				return
			}
		}
		if id, ok := s.sessionIdentity(w, r); ok {
			r = r.WithContext(WithIdentity(r.Context(), id))
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) sessionIdentity(w http.ResponseWriter, r *http.Request) (Identity, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" || len(c.Value) > 128 {
		return Identity{}, false
	}
	ctx := r.Context()
	row, err := sqlc.New(s.db.Read).GetSessionByTokenHash(ctx, hashToken(c.Value))
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("auth: look up session", "err", err)
		}
		return Identity{}, false
	}
	now := s.now()
	expires, err1 := time.Parse(timeLayout, row.ExpiresAt)
	seen, err2 := time.Parse(timeLayout, row.LastSeenAt)
	if err1 != nil || err2 != nil || !now.Before(expires) {
		s.clearCookie(w, r)
		return Identity{}, false
	}
	if now.Sub(seen) >= sessionRefreshAt {
		newExpiry := now.Add(sessionLifetime)
		err := sqlc.New(s.db.Write).TouchSession(ctx, sqlc.TouchSessionParams{
			LastSeenAt: s.ts(now), ExpiresAt: s.ts(newExpiry), ID: row.SessionID})
		if err != nil {
			slog.Error("auth: refresh session", "err", err)
		} else {
			s.setCookie(w, r, c.Value, newExpiry)
			_ = sqlc.New(s.db.Write).TouchMembership(ctx, sqlc.TouchMembershipParams{
				LastActiveAt: sql.NullString{String: s.ts(now), Valid: true}, HouseholdID: row.HouseholdID, UserID: row.UserID})
		}
	}
	role, _ := ParseRole(row.Role)
	return Identity{
		UserID: row.UserID, UserName: row.UserName, Email: row.UserEmail,
		HouseholdID: row.HouseholdID, Role: role, Source: SourceSession, SessionID: row.SessionID,
	}, role != 0
}

// startSession stores a new session for the user and sets the cookie.
func (s *Service) startSession(ctx context.Context, q *sqlc.Queries, w http.ResponseWriter, r *http.Request, userID, householdID string) error {
	token, hash := newToken()
	now := s.now()
	expires := now.Add(sessionLifetime)
	ua := r.UserAgent()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	err := q.CreateSession(ctx, sqlc.CreateSessionParams{
		ID: id.New(), TokenHash: hash, UserID: userID, HouseholdID: householdID,
		CreatedAt: s.ts(now), LastSeenAt: s.ts(now), ExpiresAt: s.ts(expires),
		UserAgent: sql.NullString{String: ua, Valid: true}, Ip: sql.NullString{String: s.clientAddr(r), Valid: true},
	})
	if err != nil {
		return err
	}
	s.setCookie(w, r, token, expires)
	return nil
}
