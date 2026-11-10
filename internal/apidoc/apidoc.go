// Package apidoc describes /api/v1 for people who write their own tools
// (J9.2): an OpenAPI document at /api/v1/openapi.json and a readable page at
// /api/docs, both built from routeTable and served from the binary with no
// outside assets.
package apidoc

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// Op is one documented route.
type Op struct {
	Method, Path string // Path relative to /api/v1, e.g. "/items/{id}"
	Group        string
	Stable       bool
	Paged        bool
	Summary      string
}

// Pattern is the route pattern as registered, e.g. "GET /api/v1/items".
func (o Op) Pattern() string { return o.Method + " /api/v1" + o.Path }

// Ops returns the documented routes in table order.
func Ops() []Op {
	var out []Op
	for _, line := range strings.Split(strings.TrimSpace(routeTable), "\n") {
		f := strings.Split(line, " | ")
		if len(f) != 5 {
			panic("apidoc: bad route line: " + line)
		}
		mp := strings.SplitN(f[0], " ", 2)
		out = append(out, Op{Method: mp[0], Path: mp[1], Group: f[1], Stable: f[2] == "S", Paged: f[3] == "P", Summary: f[4]})
	}
	return out
}

// Service serves the description and the reference page. Both are public.
type Service struct {
	spec []byte
	page []byte
}

// New builds the documents once.
func New(version string) *Service {
	spec, err := json.MarshalIndent(openAPI(version), "", " ")
	if err != nil {
		panic(err)
	}
	return &Service{spec: spec, page: renderPage()}
}

// Routes registers GET /api/v1/openapi.json and GET /api/docs.
func (s *Service) Routes(mux *httpapi.Mux) {
	mux.HandleFunc("GET /api/v1/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(s.spec)
	})
	mux.HandleFunc("GET /api/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(s.page)
	})
}

func openAPI(version string) map[string]any {
	type m = map[string]any
	paths := m{}
	for _, o := range Ops() {
		op := m{
			"summary":     o.Summary,
			"tags":        []string{o.Group},
			"operationId": opID(o),
			"x-stable":    o.Stable,
			"responses": m{
				"200":     m{"description": "The result. Changes return the changed row; lists return {data, next_cursor}."},
				"default": m{"description": "An error.", "content": m{"application/json": m{"schema": m{"$ref": "#/components/schemas/Error"}}}},
			},
		}
		if o.Stable {
			op["security"] = []m{{"bearer": []string{}}, {"session": []string{}}}
		} else {
			op["security"] = []m{{"session": []string{}}}
			op["x-internal"] = true
			op["description"] = "Internal to the app: session cookie only, not available to access tokens, may change without notice."
		}
		var params []m
		for _, p := range pathParams(o.Path) {
			params = append(params, m{"name": p, "in": "path", "required": true, "schema": m{"type": "string"}})
		}
		if o.Paged {
			params = append(params,
				m{"name": "cursor", "in": "query", "schema": m{"type": "string"}, "description": "next_cursor of the previous page."},
				m{"name": "limit", "in": "query", "schema": m{"type": "integer", "minimum": 1, "maximum": 200, "default": 50}})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		switch o.Method {
		case "POST", "PUT", "PATCH":
			ct := "application/json"
			if o.Path == "/photos" {
				ct = "multipart/form-data"
			}
			op["requestBody"] = m{"content": m{ct: m{"schema": m{"type": "object"}}}}
		}
		item, ok := paths[o.Path].(m)
		if !ok {
			item = m{}
			paths[o.Path] = item
		}
		item[strings.ToLower(o.Method)] = op
	}
	return m{
		"openapi": "3.0.3",
		"info": m{"title": "Stash inventory API", "version": version,
			"description": "/api/v1 only gains fields; removing or changing one needs /api/v2. Routes marked x-internal are used by the app itself and may change."},
		"servers": []m{{"url": "/api/v1"}},
		"paths":   paths,
		"components": m{
			"securitySchemes": m{
				"bearer":  m{"type": "http", "scheme": "bearer", "description": "An access token (Settings > Access tokens), stash_pat_..."},
				"session": m{"type": "apiKey", "in": "cookie", "name": "session"},
			},
			"schemas": m{
				"Error": m{"type": "object", "properties": m{"error": m{"type": "object", "properties": m{
					"code":    m{"type": "string"},
					"message": m{"type": "string"},
					"fields":  m{"type": "object", "additionalProperties": m{"type": "string"}},
				}}}},
			},
		},
	}
}

func pathParams(path string) []string {
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "{") {
			out = append(out, strings.Trim(seg, "{}"))
		}
	}
	return out
}

func opID(o Op) string {
	r := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_")
	return strings.ToLower(o.Method) + r.Replace(o.Path)
}
