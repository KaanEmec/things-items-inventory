package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

var (
	languageRe = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
)

func invalid(field, msg string) error { return httpapi.Invalid(map[string]string{field: msg}) }

func normEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func checkEmail(s string) string {
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || len(s) > 254 {
		return "Enter a valid email address."
	}
	return ""
}

func checkName(s string, what string) string {
	if n := utf8.RuneCountInString(s); n == 0 || n > 100 {
		return what + " is required (up to 100 characters)."
	}
	return ""
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// ---- setup -------------------------------------------------------------

func (s *Service) getSetup(w http.ResponseWriter, r *http.Request) {
	n, err := sqlc.New(s.db.Read).CountUsers(r.Context())
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]bool{"needed": n == 0, "token_required": n == 0 && s.cfg.SetupToken != ""})
}

var errSetupDone = httpapi.Conflict("Setup has already been completed.")

// postSetup creates the first user, household and owner membership, and
// signs the person in. The whole thing runs in one transaction on the single
// write connection, so two racing requests run one after the other and the
// second sees the first one's user and gets 409.
func (s *Service) postSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name          string `json:"name"`
		Email         string `json:"email"`
		Password      string `json:"password"`
		HouseholdName string `json:"household_name"`
		Language      string `json:"language"`
		SetupToken    string `json:"setup_token"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	in.Name, in.Email, in.HouseholdName = strings.TrimSpace(in.Name), normEmail(in.Email), strings.TrimSpace(in.HouseholdName)
	if in.Language == "" {
		in.Language = "en"
	}
	fields := map[string]string{}
	if m := checkName(in.Name, "Name"); m != "" {
		fields["name"] = m
	}
	if m := checkEmail(in.Email); m != "" {
		fields["email"] = m
	}
	if m := CheckPasswordRules(in.Password); m != "" {
		fields["password"] = m
	}
	if m := checkName(in.HouseholdName, "Household name"); m != "" {
		fields["household_name"] = m
	}
	if !languageRe.MatchString(in.Language) || len(in.Language) > 35 {
		fields["language"] = "Use a language code such as en or tr."
	}
	if len(fields) > 0 {
		httpapi.WriteError(w, httpapi.Invalid(fields))
		return
	}
	// Cheap check first: once set up, a request must not cost a 64 MiB hash.
	if n, err := sqlc.New(s.db.Read).CountUsers(r.Context()); err != nil {
		httpapi.WriteError(w, err)
		return
	} else if n > 0 {
		httpapi.WriteError(w, errSetupDone)
		return
	}
	if want := string(s.cfg.SetupToken); want != "" &&
		subtle.ConstantTimeCompare([]byte(strings.TrimSpace(in.SetupToken)), []byte(want)) != 1 {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeForbidden,
			"This server needs its setup token. Find it in your server's environment (INVENTORY_SETUP_TOKEN)."))
		return
	}
	hash, err := HashPasswordCtx(r.Context(), in.Password) // slow: done before taking the write connection
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}

	ctx := r.Context()
	now := s.ts(s.now())
	userID, householdID := id.New(), id.New()
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		if n, err := q.CountUsers(ctx); err != nil {
			return err
		} else if n > 0 {
			return errSetupDone
		}
		if _, err := q.CreateUser(ctx, sqlc.CreateUserParams{ID: userID, Name: in.Name, Email: in.Email,
			PasswordHash: hash, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if _, err := q.CreateHousehold(ctx, sqlc.CreateHouseholdParams{ID: householdID, Name: in.HouseholdName,
			Language: in.Language, Currency: "EUR", CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if err := q.CreateMembership(ctx, sqlc.CreateMembershipParams{HouseholdID: householdID, UserID: userID,
			Role: RoleOwner.String(), CreatedAt: now}); err != nil {
			return err
		}
		for _, hook := range s.setupHooks {
			if err := hook(ctx, tx, householdID); err != nil {
				return err
			}
		}
		return s.startSession(ctx, q, w, r, userID, householdID)
	})
	if err != nil {
		// The cookie header was set inside the failed transaction; drop it.
		w.Header().Del("Set-Cookie")
		httpapi.WriteError(w, err)
		return
	}
	s.writeMe(w, r, http.StatusCreated, userID, householdID)
}

// ---- login, logout ------------------------------------------------------

var errBadLogin = httpapi.NewError(httpapi.CodeUnauthorized, "Wrong email or password.")

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	email, client := normEmail(in.Email), s.clientAddr(r)
	if email == "" || in.Password == "" || len(in.Password) > maxPasswordBytes {
		httpapi.WriteError(w, errBadLogin)
		return
	}
	if wait := s.limit.begin(email, client); wait > 0 { // counts this attempt; undone on success
		w.Header().Set("Retry-After", itoa(int(wait.Seconds())+1))
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeRateLimited, "Too many attempts. Wait a few minutes and try again."))
		return
	}
	ctx := r.Context()
	q := sqlc.New(s.db.Read)
	user, err := q.GetUserByEmail(ctx, email)
	hash := dummyHash
	switch {
	case err == nil:
		hash = user.PasswordHash
	case !errors.Is(err, sql.ErrNoRows):
		httpapi.WriteError(w, err)
		return
	}
	ok, verr := VerifyPasswordCtx(ctx, in.Password, hash)
	if verr != nil && ctx.Err() != nil {
		httpapi.WriteError(w, verr)
		return
	}
	if verr != nil || !ok || err != nil { // err != nil: unknown email, the dummy verify only spent the time
		httpapi.WriteError(w, errBadLogin)
		return
	}
	s.limit.undo(email, client)

	households, err := q.ListUserHouseholds(ctx, user.ID)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if len(households) == 0 {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeForbidden, "This account is not a member of any household."))
		return
	}
	wq := sqlc.New(s.db.Write)
	_ = wq.DeleteExpiredSessions(ctx, s.ts(s.now()))
	if err := s.startSession(ctx, wq, w, r, user.ID, households[0].HouseholdID); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	s.writeMe(w, r, http.StatusOK, user.ID, households[0].HouseholdID)
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	if err := sqlc.New(s.db.Write).DeleteSession(r.Context(), ident.SessionID); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	s.clearCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) logoutAll(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	if err := sqlc.New(s.db.Write).DeleteUserSessions(r.Context(), ident.UserID); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	s.clearCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// ---- password reset -----------------------------------------------------

var errBadReset = httpapi.BadRequest("This reset link is not valid any more. Ask for a new one.")

func (s *Service) reset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if m := CheckPasswordRules(in.Password); m != "" {
		httpapi.WriteError(w, invalid("password", m))
		return
	}
	if in.Token == "" || len(in.Token) > 128 {
		httpapi.WriteError(w, errBadReset)
		return
	}
	ctx := r.Context()
	if !s.linkLimit.allow(s.clientAddr(r)) {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeRateLimited, "Too many attempts. Wait a few minutes and try again."))
		return
	}
	now := s.now()
	// Validate the token before spending a hash on it (re-checked in the transaction).
	if inv, err := sqlc.New(s.db.Read).GetResetInviteByTokenHash(ctx, hashToken(in.Token)); err != nil || !resetUsable(inv, now) {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			httpapi.WriteError(w, err)
			return
		}
		httpapi.WriteError(w, errBadReset)
		return
	}
	hash, err := HashPasswordCtx(ctx, in.Password)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		inv, err := q.GetResetInviteByTokenHash(ctx, hashToken(in.Token))
		if errors.Is(err, sql.ErrNoRows) {
			return errBadReset
		} else if err != nil {
			return err
		}
		if !resetUsable(inv, now) {
			return errBadReset
		}
		n, err := q.UseResetInvite(ctx, sqlc.UseResetInviteParams{
			UsedAt: sql.NullString{String: s.ts(now), Valid: true}, UsedBy: sql.NullString{String: inv.UserID.String, Valid: true},
			UpdatedAt: s.ts(now), ID: inv.ID})
		if err != nil {
			return err
		}
		if n != 1 {
			return errBadReset // someone used it a moment ago
		}
		if err := q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{PasswordHash: hash, UpdatedAt: s.ts(now), ID: inv.UserID.String}); err != nil {
			return err
		}
		return q.DeleteUserSessions(ctx, inv.UserID.String)
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func resetUsable(inv sqlc.GetResetInviteByTokenHashRow, now time.Time) bool {
	exp, err := time.Parse(timeLayout, inv.ExpiresAt)
	return err == nil && now.Before(exp) && !inv.UsedAt.Valid && !inv.RevokedAt.Valid && inv.UserID.Valid
}

// createResetLink lets an owner or admin make a one-time reset link for a
// member of the active household, but only when that household is the
// person's only one (their password is global). An admin may only reset
// members and viewers; an owner may reset anyone.
func (s *Service) createResetLink(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	ctx := r.Context()
	target := r.PathValue("user_id")
	m, err := sqlc.New(s.db.Read).GetMembership(ctx, sqlc.GetMembershipParams{HouseholdID: ident.HouseholdID, UserID: target})
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.WriteError(w, httpapi.NotFound("No such member."))
		return
	} else if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if tr, _ := ParseRole(m.Role); ident.Role != RoleOwner && tr >= ident.Role {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeForbidden, "Only an owner can reset the password of an admin or owner."))
		return
	}
	// A password is global: if this person also belongs to another household,
	// an admin here must not be able to take the whole account over.
	if hs, err := sqlc.New(s.db.Read).ListUserHouseholds(ctx, target); err != nil {
		httpapi.WriteError(w, err)
		return
	} else if len(hs) > 1 {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeForbidden,
			"This person belongs to more than one household, so only the server administrator can reset their password: run `inventory reset-password <email>` on the server."))
		return
	}
	link, expires, err := s.issueResetLink(ctx, sqlc.New(s.db.Write), ident.HouseholdID, target, ident.UserID)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, map[string]string{"url": link, "expires_at": s.ts(expires)})
}

// issueResetLink revokes the user's open reset links in the household and
// stores a new one. The returned link carries the token in the URL fragment
// (never sent to the server, so it stays out of request logs); it has the
// public URL in front when one is configured.
func (s *Service) issueResetLink(ctx context.Context, q *sqlc.Queries, householdID, userID, createdBy string) (string, time.Time, error) {
	now := s.now()
	expires := now.Add(resetLifetime)
	token, hash := newToken()
	if err := q.RevokeResetInvites(ctx, sqlc.RevokeResetInvitesParams{
		RevokedAt: sql.NullString{String: s.ts(now), Valid: true}, UpdatedAt: s.ts(now),
		HouseholdID: householdID, UserID: sql.NullString{String: userID, Valid: true}}); err != nil {
		return "", time.Time{}, err
	}
	err := q.CreateResetInvite(ctx, sqlc.CreateResetInviteParams{
		ID: id.New(), HouseholdID: householdID, CreatedAt: s.ts(now), UpdatedAt: s.ts(now),
		UserID: sql.NullString{String: userID, Valid: true}, TokenHash: hash, ExpiresAt: s.ts(expires), CreatedBy: createdBy})
	if err != nil {
		return "", time.Time{}, err
	}
	return s.cfg.PublicURL + "/reset#" + token, expires, nil
}
