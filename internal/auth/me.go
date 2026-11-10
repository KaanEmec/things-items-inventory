package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

func itoa(n int) string { return strconv.Itoa(n) }

type householdRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type meResponse struct {
	User struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
	Households      []householdRef `json:"households"`
	ActiveHousehold householdRef   `json:"active_household"`
	Role            string         `json:"role"`
	PackingBox      any            `json:"packing_box"` // filled in when places exist (J3.x)
}

// writeMe answers with the caller's profile for the given user and active
// household, reading everything fresh from the database.
func (s *Service) writeMe(w http.ResponseWriter, r *http.Request, status int, userID, householdID string) {
	ctx := r.Context()
	q := sqlc.New(s.db.Write) // just written: read from the same connection
	user, err := q.GetUser(ctx, userID)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	rows, err := q.ListUserHouseholds(ctx, userID)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	var me meResponse
	me.User.ID, me.User.Name, me.User.Email = user.ID, user.Name, user.Email
	me.Households = []householdRef{}
	for _, h := range rows {
		ref := householdRef{ID: h.HouseholdID, Name: h.Name, Role: h.Role}
		me.Households = append(me.Households, ref)
		if h.HouseholdID == householdID {
			me.ActiveHousehold, me.Role = ref, h.Role
		}
	}
	httpapi.WriteJSON(w, status, me)
}

func (s *Service) getMe(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	s.writeMe(w, r, http.StatusOK, ident.UserID, ident.HouseholdID)
}

func (s *Service) patchMe(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	var in struct {
		Name  *string `json:"name"`
		Email *string `json:"email"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	ctx := r.Context()
	name, email := ident.UserName, ident.Email
	fields := map[string]string{}
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if m := checkName(name, "Name"); m != "" {
			fields["name"] = m
		}
	}
	if in.Email != nil {
		email = normEmail(*in.Email)
		if m := checkEmail(email); m != "" {
			fields["email"] = m
		}
	}
	if len(fields) > 0 {
		httpapi.WriteError(w, httpapi.Invalid(fields))
		return
	}
	_, err := sqlc.New(s.db.Write).UpdateUserProfile(ctx, sqlc.UpdateUserProfileParams{
		Name: name, Email: email, UpdatedAt: s.ts(s.now()), ID: ident.UserID})
	if isUnique(err) {
		httpapi.WriteError(w, invalid("email", "That email address is already in use."))
		return
	} else if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	s.writeMe(w, r, http.StatusOK, ident.UserID, ident.HouseholdID)
}

// changePassword needs the current password and ends the user's other
// sessions. Wrong guesses count against the login limiter.
func (s *Service) changePassword(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	ctx := r.Context()
	client := s.clientAddr(r)
	// Own limiter, keyed by the signed-in user: nobody else can use it up.
	if wait := s.pwLimit.begin(ident.UserID, client); wait > 0 {
		w.Header().Set("Retry-After", itoa(int(wait.Seconds())+1))
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeRateLimited, "Too many attempts. Wait a few minutes and try again."))
		return
	}
	if m := CheckPasswordRules(in.New); m != "" {
		httpapi.WriteError(w, invalid("new", m))
		return
	}
	user, err := sqlc.New(s.db.Read).GetUser(ctx, ident.UserID)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	ok, verr := VerifyPasswordCtx(ctx, in.Current, user.PasswordHash)
	if verr != nil && ctx.Err() != nil {
		httpapi.WriteError(w, verr)
		return
	}
	if verr != nil || !ok {
		httpapi.WriteError(w, invalid("current", "That is not your current password."))
		return
	}
	s.pwLimit.undo(ident.UserID, client)
	hash, err := HashPasswordCtx(ctx, in.New)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		if err := q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{PasswordHash: hash, UpdatedAt: s.ts(s.now()), ID: ident.UserID}); err != nil {
			return err
		}
		return q.DeleteOtherUserSessions(ctx, sqlc.DeleteOtherUserSessionsParams{UserID: ident.UserID, ID: ident.SessionID})
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// switchHousehold changes the session's active household. A household the
// user does not belong to looks like one that does not exist.
func (s *Service) switchHousehold(w http.ResponseWriter, r *http.Request) {
	ident := MustIdentity(r.Context())
	var in struct {
		HouseholdID string `json:"household_id"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	ctx := r.Context()
	_, err := sqlc.New(s.db.Read).GetMembership(ctx, sqlc.GetMembershipParams{HouseholdID: in.HouseholdID, UserID: ident.UserID})
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.WriteError(w, httpapi.NotFound("No such household."))
		return
	} else if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if err := sqlc.New(s.db.Write).SetSessionHousehold(ctx, sqlc.SetSessionHouseholdParams{
		HouseholdID: in.HouseholdID, ID: ident.SessionID, UserID: ident.UserID}); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	s.writeMe(w, r, http.StatusOK, ident.UserID, in.HouseholdID)
}

// ---- household settings -------------------------------------------------

type householdJSON struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Language              string `json:"language"`
	Currency              string `json:"currency"`
	DefaultWarrantyMonths int64  `json:"default_warranty_months"`
	AIPaused              bool   `json:"ai_paused"`
	CreatedAt             string `json:"created_at"`
	UpdatedAt             string `json:"updated_at"`
}

func toHouseholdJSON(h sqlc.Household) householdJSON {
	return householdJSON{h.ID, h.Name, h.Language, h.Currency, h.DefaultWarrantyMonths, h.AiPaused != 0, h.CreatedAt, h.UpdatedAt}
}

func (s *Service) getHousehold(w http.ResponseWriter, r *http.Request) {
	h, err := sqlc.New(s.db.Read).GetHousehold(r.Context(), HouseholdID(r.Context()))
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, toHouseholdJSON(h))
}

func (s *Service) patchHousehold(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name                  *string `json:"name"`
		Language              *string `json:"language"`
		Currency              *string `json:"currency"`
		DefaultWarrantyMonths *int64  `json:"default_warranty_months"`
		AIPaused              *bool   `json:"ai_paused"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	ctx := r.Context()
	hid := HouseholdID(ctx)
	var out householdJSON
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		h, err := q.GetHousehold(ctx, hid)
		if err != nil {
			return err
		}
		fields := map[string]string{}
		if in.Name != nil {
			h.Name = strings.TrimSpace(*in.Name)
			if m := checkName(h.Name, "Name"); m != "" {
				fields["name"] = m
			}
		}
		if in.Language != nil {
			h.Language = *in.Language
			if !languageRe.MatchString(h.Language) || len(h.Language) > 35 {
				fields["language"] = "Use a language code such as en or tr."
			}
		}
		if in.Currency != nil {
			h.Currency = *in.Currency
			if !currencyRe.MatchString(h.Currency) {
				fields["currency"] = "Use a three-letter currency code such as EUR."
			}
		}
		if in.DefaultWarrantyMonths != nil {
			h.DefaultWarrantyMonths = *in.DefaultWarrantyMonths
			if h.DefaultWarrantyMonths < 0 || h.DefaultWarrantyMonths > 240 {
				fields["default_warranty_months"] = "Use a number of months from 0 to 240."
			}
		}
		if in.AIPaused != nil {
			h.AiPaused = 0
			if *in.AIPaused {
				h.AiPaused = 1
			}
		}
		if len(fields) > 0 {
			return httpapi.Invalid(fields)
		}
		h, err = q.UpdateHousehold(ctx, sqlc.UpdateHouseholdParams{Name: h.Name, Language: h.Language, Currency: h.Currency,
			DefaultWarrantyMonths: h.DefaultWarrantyMonths, AiPaused: h.AiPaused, UpdatedAt: s.ts(s.now()), ID: hid})
		out = toHouseholdJSON(h)
		return err
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}
