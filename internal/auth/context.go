package auth

import (
	"context"
	"net/http"
)

// Source says how a request proved who it is.
type Source string

const (
	SourceSession Source = "session" // the session cookie
	SourceToken   Source = "token"   // a bearer access token (J9.1)
)

// Identity is who is calling and in which household. One middleware sets it
// for every /api/ request that authenticates; handlers read it through the
// getters below and never look at cookies or headers themselves.
type Identity struct {
	UserID      string
	UserName    string
	Email       string
	HouseholdID string // the active household
	Role        Role   // the user's role in it
	Source      Source
	SessionID   string   // SourceSession only
	TokenID     string   // SourceToken only
	ReadOnly    bool     // SourceToken with scope read: only GET and HEAD
	TokenScope  []string // reserved for J9.1
}

type ctxKey struct{}

// WithIdentity returns ctx carrying id. Only the auth middleware (and tests)
// should call it.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the caller's identity; ok is false for a request
// without a valid session or token.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// MustIdentity is FromContext for handlers wrapped in Require, where the
// identity is guaranteed. It panics (the server turns that into a 500) when
// called outside one.
func MustIdentity(ctx context.Context) Identity {
	id, ok := FromContext(ctx)
	if !ok {
		panic("auth: no identity in context; wrap the handler in auth.Require")
	}
	return id
}

// UserID returns the caller's user ID (empty when anonymous).
func UserID(ctx context.Context) string { id, _ := FromContext(ctx); return id.UserID }

// HouseholdID returns the caller's active household ID (empty when
// anonymous). Every household-owned query takes it.
func HouseholdID(ctx context.Context) string { id, _ := FromContext(ctx); return id.HouseholdID }

// RoleOf returns the caller's role in the active household (0 when anonymous).
func RoleOf(ctx context.Context) Role { id, _ := FromContext(ctx); return id.Role }

// Authenticator is the seam for other ways of signing in. J9.1 registers
// one that reads "Authorization: Bearer <token>", looks the token up and
// returns an Identity with Source SourceToken, the token owner's current
// role and ReadOnly set for scope read. Return ok=false when the request
// carries no credential of this kind (the next authenticator is tried);
// return an error for a credential that is present but wrong (the request
// then proceeds anonymously and gets 401 where a session is needed).
type Authenticator func(r *http.Request) (id Identity, ok bool, err error)
