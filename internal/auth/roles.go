package auth

import (
	"net/http"

	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// Role is a household role. Roles are ordered: each may do everything the
// lower ones may.
type Role int

const (
	RoleViewer Role = iota + 1
	RoleMember
	RoleAdmin
	RoleOwner
)

var roleNames = map[Role]string{RoleViewer: "viewer", RoleMember: "member", RoleAdmin: "admin", RoleOwner: "owner"}

func (r Role) String() string { return roleNames[r] }

// ParseRole turns "owner", "admin", "member" or "viewer" into a Role.
func ParseRole(s string) (Role, bool) {
	for r, n := range roleNames {
		if n == s {
			return r, true
		}
	}
	return 0, false
}

// Action names something a person may do. Every handler that needs a role
// gets it from the table below, so what each role may do is reviewed in one
// place.
type Action string

const (
	ActionSession Action = "session" // any signed-in user, no household role needed beyond membership

	ActionInventoryRead  Action = "inventory.read"  // lists, details, search, history, photos
	ActionInventoryWrite Action = "inventory.write" // add and change items, places, boxes, lines, photos
	ActionHomesManage    Action = "homes.manage"    // create, rename, archive homes
	ActionTokensManage   Action = "tokens.manage"   // the user's own access tokens
	ActionHouseholdRead  Action = "household.read"
	ActionHouseholdEdit  Action = "household.edit" // name, language, currency, warranty, AI paused
	ActionMembersManage  Action = "members.manage" // members, invites, reset links
	ActionAIRead         Action = "ai.read"        // the AI profiles, without tokens
	ActionAISettings     Action = "ai.settings"    // change or clear them
	ActionExport         Action = "export"
	ActionHouseholdDrop  Action = "household.delete"
)

// minRole is the one table of actions and the lowest role that may do them.
// Adding an action here is how a feature asks for a role; Require panics at
// start-up for an action that is not listed.
var minRole = map[Action]Role{
	ActionSession:        RoleViewer,
	ActionInventoryRead:  RoleViewer,
	ActionInventoryWrite: RoleMember,
	ActionHomesManage:    RoleAdmin,
	ActionTokensManage:   RoleMember,
	ActionHouseholdRead:  RoleViewer,
	ActionHouseholdEdit:  RoleAdmin,
	ActionMembersManage:  RoleAdmin,
	ActionAIRead:         RoleViewer,
	ActionAISettings:     RoleAdmin,
	ActionExport:         RoleAdmin,
	ActionHouseholdDrop:  RoleOwner,
}

// Can reports whether the request's identity may do action.
func Can(r *http.Request, action Action) bool {
	id, ok := FromContext(r.Context())
	return ok && id.Role >= minRole[action] && minRole[action] != 0
}

// Require wraps h so it runs only for a signed-in caller whose role in the
// active household is at least what action needs: 401 without a session or
// token, 403 when the role is too low or a read-only token tries to change
// data. Use it for endpoints marked S (stable, access tokens allowed) in the
// spec. Inside h, read the household with HouseholdID(ctx) and pass it to
// every query.
//
// Rows of another household must look absent: the data layer filters by
// household_id and a miss becomes httpapi.NotFound, never Forbidden.
func Require(action Action, h http.HandlerFunc) http.HandlerFunc {
	return require(action, false, h)
}

// RequireSession is Require for endpoints marked I (internal to the app):
// only the session cookie is accepted, never an access token.
func RequireSession(action Action, h http.HandlerFunc) http.HandlerFunc {
	return require(action, true, h)
}

func require(action Action, sessionOnly bool, h http.HandlerFunc) http.HandlerFunc {
	min, ok := minRole[action]
	if !ok {
		panic("auth.Require: unknown action " + string(action) + "; add it to the minRole table in roles.go")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := FromContext(r.Context())
		if !ok {
			httpapi.WriteError(w, errUnauthenticated())
			return
		}
		forbidden := id.Role < min ||
			(sessionOnly && id.Source != SourceSession) ||
			(id.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead)
		if forbidden {
			httpapi.WriteError(w, httpapi.NewError(httpapi.CodeForbidden, "You are not allowed to do that."))
			return
		}
		h(w, r)
	}
}

func errUnauthenticated() *httpapi.Error {
	return httpapi.NewError(httpapi.CodeUnauthorized, "Please sign in.")
}
