// Package mcp serves the inventory to AI agents over the Model Context
// Protocol (J9.3), using the official go-sdk with the streamable HTTP
// transport at POST /mcp.
//
// Auth: only access tokens (J9.1) are accepted; the httpapi middleware has
// already turned the bearer token into an Identity. A token sees only its
// household. A read-only token (or a viewer) is given the read tools only;
// a write token also gets the write tools. No tool deletes anything or
// touches settings, members, AI profiles or tokens. Changes are written
// through inventory.InTx with the token in the context, so history shows
// "via <token name>"; items an agent adds are drafts, so they land in the
// Drafts queue for a person to confirm.
package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// Service is the MCP endpoint.
type Service struct {
	db      *db.DB
	version string
}

// New returns the MCP service.
func New(d *db.DB, version string) *Service { return &Service{db: d, version: version} }

// Routes registers POST /mcp. The stateless JSON mode needs no sessions:
// every request carries its own token and is answered at once.
func (s *Service) Routes(mux *httpapi.Mux) {
	h := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		id, ok := auth.FromContext(r.Context())
		if !ok {
			return nil
		}
		return s.server(id)
	}, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	// Stateless: no server-initiated stream and no sessions to end.
	for _, m := range []string{"GET", "DELETE"} {
		mux.HandleFunc(m+" /mcp", func(w http.ResponseWriter, r *http.Request) {
			if _, ok := auth.FromContext(r.Context()); !ok {
				httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnauthorized, "Send an access token: Authorization: Bearer <token>."))
				return
			}
			w.Header().Set("Allow", "POST")
			httpapi.WriteError(w, httpapi.NewError(httpapi.CodeBadRequest, "Send MCP requests with POST."))
		})
	}
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		id, ok := auth.FromContext(r.Context())
		switch {
		case !ok:
			httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnauthorized, "Send an access token: Authorization: Bearer <token>."))
		case id.Source != auth.SourceToken:
			httpapi.WriteError(w, httpapi.NewError(httpapi.CodeForbidden, "The MCP endpoint takes access tokens only."))
		default:
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			h.ServeHTTP(w, r)
		}
	})
}

// caller is who a tool call acts for.
type caller struct {
	ctx context.Context
	hid string
	uid string
}

// server builds the tool set this identity may use.
func (s *Service) server(id auth.Identity) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "stash-inventory", Version: s.version}, &sdk.ServerOptions{
		Instructions: "Household inventory: where things are. To find something, call search first, then get_item. " +
			"Places and boxes are named by ID in arguments; paths are for reading to the user. " +
			"Never invent IDs: take them from search, list_places or list_items. Lists are paged: pass next_cursor back as cursor.",
	})
	call := func(ctx context.Context) caller {
		return caller{ctx: auth.WithIdentity(ctx, id), hid: id.HouseholdID, uid: id.UserID}
	}
	s.readTools(srv, call)
	if !id.ReadOnly && id.Role >= auth.RoleMember {
		s.writeTools(srv, call)
	}
	return srv
}

// result is what a tool returns: compact text for the model plus the
// structured data.
type result struct {
	text string
	data any
}

// add registers a tool whose handler returns compact text and structured data.
func add[In any](srv *sdk.Server, call func(context.Context) caller, name, desc string, fn func(c caller, in In) (result, error)) {
	sdk.AddTool(srv, &sdk.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
			r, err := fn(call(ctx), in)
			if err != nil {
				return nil, nil, friendly(err)
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: r.text}}}, r.data, nil
		})
}

// friendly turns an API error into a message that says what to fix.
func friendly(err error) error {
	var he *httpapi.Error
	if errors.As(err, &he) {
		msg := he.Message
		for f, m := range he.Fields {
			msg += " " + f + ": " + m
		}
		return errors.New(msg)
	}
	slog.Error("mcp: tool failed", "err", err)
	return errors.New("Something went wrong on the server. Try again later.")
}

type callFn = func(context.Context) caller
