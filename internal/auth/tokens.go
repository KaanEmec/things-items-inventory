package auth

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// Access tokens (J9.1): "stash_pat_" + 32 random base62 characters. The
// prefix lets secret scanners find leaked tokens; the app name is still a
// placeholder (spec section 8) and changes in this one constant.
const (
	tokenPrefix       = "stash_pat_"
	base62            = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	tokenTouchEvery   = time.Minute
	tokenMaxPerMinute = 600 // requests per token per fixed one-minute window
)

func newAccessToken() (token, hint string) {
	b := make([]byte, 32)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(base62))))
		if err != nil {
			panic("auth: crypto/rand: " + err.Error())
		}
		b[i] = base62[n.Int64()]
	}
	return tokenPrefix + string(b), string(b[len(b)-4:])
}

// bearer is the Authenticator for "Authorization: Bearer stash_pat_...".
// The token acts with its owner's current role in the token's household
// (the lookup joins memberships, so removing the member kills it at once),
// limited by scope: read means GET and HEAD only.
func (s *Service) bearer(r *http.Request) (Identity, bool, error) {
	h := r.Header.Get("Authorization")
	tok, found := strings.CutPrefix(h, "Bearer ")
	if !found || !strings.HasPrefix(tok, tokenPrefix) {
		return Identity{}, false, nil // not ours; another authenticator or the cookie
	}
	if len(tok) > 100 {
		return Identity{}, false, errors.New("bad token")
	}
	ctx := r.Context()
	row, err := sqlc.New(s.db.Read).GetAccessTokenByHash(ctx, hashToken(tok))
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("auth: look up access token", "err", err)
		}
		return Identity{}, false, errors.New("unknown token")
	}
	now := s.now()
	if row.RevokedAt.Valid {
		return Identity{}, false, errors.New("revoked token")
	}
	if row.ExpiresAt.Valid {
		if exp, err := time.Parse(timeLayout, row.ExpiresAt.String); err != nil || !now.Before(exp) {
			return Identity{}, false, errors.New("expired token")
		}
	}
	role, ok := ParseRole(row.Role)
	if !ok {
		return Identity{}, false, errors.New("bad role")
	}
	if !s.tokenLimit.allow(row.TokenID) {
		return Identity{}, false, httpapi.NewError(httpapi.CodeRateLimited, "This access token is making too many requests. Slow down.")
	}
	last, perr := time.Parse(timeLayout, row.LastUsedAt.String)
	if !row.LastUsedAt.Valid || perr != nil || now.Sub(last) >= tokenTouchEvery {
		if err := sqlc.New(s.db.Write).TouchAccessToken(ctx, sqlc.TouchAccessTokenParams{
			LastUsedAt: sql.NullString{String: s.ts(now), Valid: true}, ID: row.TokenID}); err != nil {
			slog.Error("auth: touch access token", "err", err)
		}
	}
	return Identity{
		UserID: row.UserID, UserName: row.UserName, Email: row.UserEmail,
		HouseholdID: row.HouseholdID, Role: role, Source: SourceToken,
		TokenID: row.TokenID, TokenName: row.TokenName, ReadOnly: row.Scope == "read",
	}, true, nil
}

type tokenJSON struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Scope      string  `json:"scope"`
	Hint       string  `json:"hint"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
	CreatedAt  string  `json:"created_at"`
}

func nullp(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

// GET /tokens: the caller's own live tokens in the active household.
func (s *Service) listTokens(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	rows, err := sqlc.New(s.db.Read).ListAccessTokens(r.Context(), sqlc.ListAccessTokensParams{HouseholdID: ident.HouseholdID, UserID: ident.UserID})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	out := []tokenJSON{}
	for _, t := range rows {
		out = append(out, tokenJSON{t.ID, t.Name, t.Scope, t.Hint, nullp(t.LastUsedAt), nullp(t.ExpiresAt), t.CreatedAt})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

// POST /tokens: the token value is in this response and nowhere else, ever.
func (s *Service) createToken(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	var in struct {
		Name      string  `json:"name"`
		Scope     string  `json:"scope"`
		ExpiresAt *string `json:"expires_at"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	fields := map[string]string{}
	if n := len([]rune(in.Name)); n == 0 || n > 60 {
		fields["name"] = "Give the token a name of up to 60 characters."
	}
	if in.Scope != "read" && in.Scope != "write" {
		fields["scope"] = "Choose read or write."
	}
	expires := sql.NullString{}
	if in.ExpiresAt != nil && *in.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *in.ExpiresAt)
		if err != nil || !t.After(s.now()) {
			fields["expires_at"] = "Use a date and time in the future (RFC 3339)."
		} else {
			expires = sql.NullString{String: s.ts(t), Valid: true}
		}
	}
	if len(fields) > 0 {
		httpapi.WriteError(w, httpapi.Invalid(fields))
		return
	}
	token, hint := newAccessToken()
	now := s.ts(s.now())
	tid := id.New()
	err := sqlc.New(s.db.Write).CreateAccessToken(r.Context(), sqlc.CreateAccessTokenParams{
		ID: tid, HouseholdID: ident.HouseholdID, CreatedAt: now, UpdatedAt: now, UserID: ident.UserID,
		Name: in.Name, Scope: in.Scope, TokenHash: hashToken(token), Hint: hint, ExpiresAt: expires})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{
		"id": tid, "name": in.Name, "scope": in.Scope, "hint": hint,
		"expires_at": nullp(expires), "created_at": now, "token": token,
	})
}

func (s *Service) revokeToken(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	now := s.ts(s.now())
	n, err := sqlc.New(s.db.Write).RevokeAccessToken(r.Context(), sqlc.RevokeAccessTokenParams{
		RevokedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now,
		HouseholdID: ident.HouseholdID, UserID: ident.UserID, ID: r.PathValue("id")})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if n == 0 {
		httpapi.WriteError(w, httpapi.NotFound("No such token."))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
