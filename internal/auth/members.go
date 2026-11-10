package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

const (
	defaultInviteDays = 7
	maxInviteDays     = 30
)

var (
	errLastOwner = httpapi.Conflict("A household needs at least one owner. Make someone else an owner first.")
	errNotYours  = httpapi.NewError(httpapi.CodeForbidden, "Only an owner can do that for an admin or owner.")
)

// mayManage reports whether an actor may change or remove someone who holds
// target, or hand out role: owners may do anything; admins only deal with
// members and viewers.
func mayManage(actor Role, roles ...Role) bool {
	if actor == RoleOwner {
		return true
	}
	for _, r := range roles {
		if r >= actor {
			return false
		}
	}
	return true
}

// GET /members: everyone in the household (viewers and up) and, for admins
// only, the pending invites.
func (s *Service) listMembers(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	ctx := r.Context()
	q := sqlc.New(s.db.Read)
	rows, err := q.ListMembers(ctx, ident.HouseholdID)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	type member struct {
		UserID       string  `json:"user_id"`
		Name         string  `json:"name"`
		Email        string  `json:"email"`
		Role         string  `json:"role"`
		LastActiveAt *string `json:"last_active_at"`
	}
	type invite struct {
		ID        string `json:"id"`
		Role      string `json:"role"`
		ExpiresAt string `json:"expires_at"`
	}
	members, invites := []member{}, []invite{}
	for _, m := range rows {
		members = append(members, member{m.UserID, m.Name, m.Email, m.Role, nullp(m.LastActiveAt)})
	}
	if Can(r, ActionMembersManage) {
		inv, err := q.ListPendingInvites(ctx, sqlc.ListPendingInvitesParams{HouseholdID: ident.HouseholdID, ExpiresAt: s.ts(s.now())})
		if err != nil {
			httpapi.WriteError(w, err)
			return
		}
		for _, i := range inv {
			invites = append(invites, invite{i.ID, i.Role.String, i.ExpiresAt})
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"members": members, "invites": invites})
}

func (s *Service) memberRole(r *http.Request, q *sqlc.Queries, userID string) (Role, error) {
	m, err := q.GetMembership(r.Context(), sqlc.GetMembershipParams{HouseholdID: HouseholdID(r.Context()), UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, httpapi.NotFound("No such member.")
	} else if err != nil {
		return 0, err
	}
	role, _ := ParseRole(m.Role)
	return role, nil
}

// PATCH /members/{user_id} {role}. The last owner can not be demoted.
func (s *Service) patchMember(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	var in struct {
		Role string `json:"role"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	newRole, ok := ParseRole(in.Role)
	if !ok {
		httpapi.WriteError(w, invalid("role", "Choose owner, admin, member or viewer."))
		return
	}
	ctx, target := r.Context(), r.PathValue("user_id")
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		cur, err := s.memberRole(r, q, target)
		if err != nil {
			return err
		}
		if !mayManage(ident.Role, cur, newRole) {
			return errNotYours
		}
		if cur == RoleOwner && newRole != RoleOwner {
			if n, err := q.CountOwners(ctx, ident.HouseholdID); err != nil {
				return err
			} else if n <= 1 {
				return errLastOwner
			}
		}
		_, err = q.UpdateMemberRole(ctx, sqlc.UpdateMemberRoleParams{Role: newRole.String(), HouseholdID: ident.HouseholdID, UserID: target})
		return err
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"user_id": target, "role": newRole.String()})
}

// DELETE /members/{user_id}: removes the membership, the person's sessions
// that point at this household and their access tokens here. The last owner
// can not be removed.
func (s *Service) deleteMember(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	ctx, target := r.Context(), r.PathValue("user_id")
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		cur, err := s.memberRole(r, q, target)
		if err != nil {
			return err
		}
		if target != ident.UserID && !mayManage(ident.Role, cur) {
			return errNotYours
		}
		if cur == RoleOwner {
			if n, err := q.CountOwners(ctx, ident.HouseholdID); err != nil {
				return err
			} else if n <= 1 {
				return errLastOwner
			}
		}
		if _, err := q.DeleteMembership(ctx, sqlc.DeleteMembershipParams{HouseholdID: ident.HouseholdID, UserID: target}); err != nil {
			return err
		}
		if err := q.DeleteMemberSessions(ctx, sqlc.DeleteMemberSessionsParams{HouseholdID: ident.HouseholdID, UserID: target}); err != nil {
			return err
		}
		return q.DeleteMemberAccessTokens(ctx, sqlc.DeleteMemberAccessTokensParams{HouseholdID: ident.HouseholdID, UserID: target})
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /household: owner only. Foreign keys cascade to everything the
// household owns; sessions pointing at it (no foreign key) are removed here.
// Photo files on disk are cleaned up later by the photos garbage collector.
func (s *Service) deleteHousehold(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), HouseholdID(r.Context())
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		if err := q.DeleteHouseholdSessions(ctx, hid); err != nil {
			return err
		}
		_, err := q.DeleteHousehold(ctx, hid)
		return err
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- invites ------------------------------------------------------------

// POST /invites {role, expires_in_days=7}: a single-use link. Admins may
// invite members and viewers; owners any role.
func (s *Service) createInvite(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	var in struct {
		Role          string `json:"role"`
		ExpiresInDays int    `json:"expires_in_days"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	role, ok := ParseRole(in.Role)
	if !ok {
		httpapi.WriteError(w, invalid("role", "Choose owner, admin, member or viewer."))
		return
	}
	if in.ExpiresInDays == 0 {
		in.ExpiresInDays = defaultInviteDays
	}
	if in.ExpiresInDays < 1 || in.ExpiresInDays > maxInviteDays {
		httpapi.WriteError(w, invalid("expires_in_days", "Use 1 to 30 days."))
		return
	}
	if !mayManage(ident.Role, role) {
		httpapi.WriteError(w, errNotYours)
		return
	}
	token, hash := newToken()
	now := s.now()
	expires := now.Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour)
	iid := id.New()
	err := sqlc.New(s.db.Write).CreateInvite(r.Context(), sqlc.CreateInviteParams{
		ID: iid, HouseholdID: ident.HouseholdID, CreatedAt: s.ts(now), UpdatedAt: s.ts(now),
		Role: sql.NullString{String: role.String(), Valid: true}, TokenHash: hash, ExpiresAt: s.ts(expires), CreatedBy: ident.UserID})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, map[string]string{
		"id": iid, "url": s.cfg.PublicURL + "/invite#" + token, "role": role.String(), "expires_at": s.ts(expires)})
}

func (s *Service) revokeInvite(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	now := s.ts(s.now())
	n, err := sqlc.New(s.db.Write).RevokeInvite(r.Context(), sqlc.RevokeInviteParams{
		RevokedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, HouseholdID: ident.HouseholdID, ID: r.PathValue("id")})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if n == 0 {
		httpapi.WriteError(w, httpapi.NotFound("No such invite."))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type inviteRow = sqlc.GetInviteByTokenHashRow

// openInvite finds a usable invite for a token: not used, revoked or expired.
func (s *Service) openInvite(r *http.Request, q *sqlc.Queries, token string) (inviteRow, bool, error) {
	if token == "" || len(token) > 128 {
		return inviteRow{}, false, nil
	}
	inv, err := q.GetInviteByTokenHash(r.Context(), hashToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		return inviteRow{}, false, nil
	} else if err != nil {
		return inviteRow{}, false, err
	}
	exp, perr := time.Parse(timeLayout, inv.ExpiresAt)
	if perr != nil || !s.now().Before(exp) || inv.UsedAt.Valid || inv.RevokedAt.Valid {
		return inviteRow{}, false, nil
	}
	return inv, true, nil
}

// GET /invites/{token}: what the link is for, or {valid:false}.
func (s *Service) getInvite(w http.ResponseWriter, r *http.Request) {
	inv, ok, err := s.openInvite(r, sqlc.New(s.db.Read), r.PathValue("token"))
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if !ok {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"valid": false})
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"valid": true, "household_name": inv.HouseholdName, "role": inv.Role.String})
}

var errBadInvite = httpapi.BadRequest("This invite link is not valid any more. Ask for a new one.")

// POST /invites/{token}/accept. A signed-in user (cookie) just joins and the
// session switches to the new household; otherwise name, email and password
// create the account, and the person is signed in.
func (s *Service) acceptInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ident, signedIn := FromContext(ctx)
	if signedIn && ident.Source != SourceSession {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeForbidden, "Access tokens cannot accept invites."))
		return
	}
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	// Validate the link before spending a hash on it (re-checked in the transaction).
	if !s.linkLimit.allow(s.clientAddr(r)) {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeRateLimited, "Too many attempts. Wait a few minutes and try again."))
		return
	}
	if _, ok, err := s.openInvite(r, sqlc.New(s.db.Read), r.PathValue("token")); err != nil {
		httpapi.WriteError(w, err)
		return
	} else if !ok {
		httpapi.WriteError(w, errBadInvite)
		return
	}
	var hash string
	if !signedIn {
		in.Name, in.Email = strings.TrimSpace(in.Name), normEmail(in.Email)
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
		if len(fields) > 0 {
			httpapi.WriteError(w, httpapi.Invalid(fields))
			return
		}
		var err error
		if hash, err = HashPasswordCtx(ctx, in.Password); err != nil {
			httpapi.WriteError(w, err)
			return
		}
	}
	now := s.ts(s.now())
	userID := ident.UserID
	var householdID string
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		inv, ok, err := s.openInvite(r, q, r.PathValue("token"))
		if err != nil {
			return err
		}
		if !ok {
			return errBadInvite
		}
		householdID = inv.HouseholdID
		n, err := q.UseResetInvite(ctx, sqlc.UseResetInviteParams{ // marks any invite row used, once
			UsedAt: sql.NullString{String: now, Valid: true}, UsedBy: sql.NullString{String: "", Valid: false}, UpdatedAt: now, ID: inv.ID})
		if err != nil {
			return err
		}
		if n != 1 {
			return errBadInvite
		}
		if signedIn {
			if _, err := q.GetMembership(ctx, sqlc.GetMembershipParams{HouseholdID: householdID, UserID: userID}); err == nil {
				return httpapi.Conflict("You are already a member of this household.")
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		} else {
			userID = id.New()
			if _, err := q.CreateUser(ctx, sqlc.CreateUserParams{ID: userID, Name: in.Name, Email: in.Email,
				PasswordHash: hash, CreatedAt: now, UpdatedAt: now}); err != nil {
				if isUnique(err) {
					return invalid("email", "That email already has an account. Sign in first, then open the link again.")
				}
				return err
			}
		}
		if err := q.CreateMembership(ctx, sqlc.CreateMembershipParams{HouseholdID: householdID, UserID: userID, Role: inv.Role.String, CreatedAt: now}); err != nil {
			return err
		}
		if signedIn {
			return q.SetSessionHousehold(ctx, sqlc.SetSessionHouseholdParams{HouseholdID: householdID, ID: ident.SessionID, UserID: userID})
		}
		return s.startSession(ctx, q, w, r, userID, householdID)
	})
	if err != nil {
		w.Header().Del("Set-Cookie")
		httpapi.WriteError(w, err)
		return
	}
	s.writeMe(w, r, http.StatusOK, userID, householdID)
}
