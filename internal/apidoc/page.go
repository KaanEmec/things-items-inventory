package apidoc

import (
	"bytes"
	"html/template"
	"strings"
)

// Example is a curl recipe for a common job. The route test in
// internal/server runs every one against a test server, replacing $PLACE,
// $PLACE2, $ITEM and $BOX with real IDs.
type Example struct {
	Title  string
	Method string
	Path   string // after /api/v1
	JSON   string // request body, empty for none
	Photo  string // multipart: extra fields for POST /photos (file is added)
}

// Examples are the four common jobs.
var Examples = []Example{
	{Title: "Find an item", Method: "GET", Path: "/search?q=drill"},
	{Title: "Add an item", Method: "POST", Path: "/items", JSON: `{"name":"Cordless drill","category":"tools","place_id":"$PLACE"}`},
	{Title: "Add a photo to it", Method: "POST", Path: "/photos", Photo: "item_id=$ITEM role=item"},
	{Title: "Move an item", Method: "POST", Path: "/items/$ITEM/move", JSON: `{"place_id":"$PLACE2"}`},
	{Title: "List a box's contents", Method: "GET", Path: "/boxes/number/1"},
}

// Curl renders e as a shell command.
func (e Example) Curl() string {
	var b strings.Builder
	b.WriteString(`curl -s -H "Authorization: Bearer $TOKEN"`)
	if e.Method != "GET" && e.Photo == "" {
		b.WriteString(" -X " + e.Method)
	}
	if e.JSON != "" {
		b.WriteString(` -H "Content-Type: application/json" -d '` + e.JSON + `'`)
	}
	if e.Photo != "" {
		b.WriteString(` -F file=@photo.jpg`)
		for _, f := range strings.Fields(e.Photo) {
			b.WriteString(` -F ` + f)
		}
	}
	return b.String() + ` "$BASE/api/v1` + e.Path + `"`
}

var pageTmpl = template.Must(template.New("docs").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>API reference</title>
<style>
body{font:16px/1.5 system-ui,sans-serif;max-width:60rem;margin:0 auto;padding:1rem;color:#1d2327;background:#fff}
@media (prefers-color-scheme:dark){body{color:#e6e8ea;background:#14171a}code,pre,th{background:#22272b}}
code,pre{background:#f1f3f4;border-radius:4px;font:14px ui-monospace,monospace}
code{padding:.1em .3em}pre{padding:.7rem;overflow-x:auto}
table{border-collapse:collapse;width:100%;font-size:14px}td,th{text-align:left;padding:.3rem .5rem;border-bottom:1px solid #8884;vertical-align:top}
.i{opacity:.6}h2{margin-top:2rem}
</style></head><body>
<h1>API reference</h1>
<p>JSON over HTTPS under <code>/api/v1</code>. The machine-readable description is <a href="/api/v1/openapi.json">/api/v1/openapi.json</a> (OpenAPI 3).</p>
<h2>Authentication</h2>
<p>Create an access token in Settings, Access tokens, and send it as <code>Authorization: Bearer stash_pat_...</code>. A token works inside its own household only, acts with its owner's role, and a <em>read</em> token may only use GET. Tokens cannot use the internal endpoints below. Limit: 600 requests per minute per token (429).</p>
<h2>Conventions</h2>
<ul>
<li>Lists return <code>{"data":[...],"next_cursor":"..."|null}</code>; pass <code>cursor</code> and <code>limit</code> (default 50, max 200) to page.</li>
<li>Every change returns the changed row.</li>
<li>Errors: <code>{"error":{"code","message","fields"}}</code>; the message says what to fix. Rows of another household answer 404.</li>
<li><strong>Versioning:</strong> <code>/api/v1</code> only gains fields. Removing or changing one needs <code>/api/v2</code>. Endpoints marked internal are used by the app itself and may change without notice.</li>
<li>AI agents: the same data is available over MCP at <code>/mcp</code> with the same tokens.</li>
</ul>
<h2>Examples</h2>
<p>Set <code>BASE</code> (for example <code>https://inventory.example.com</code>) and <code>TOKEN</code>; use IDs from earlier answers.</p>
{{range .Examples}}<h3>{{.Title}}</h3><pre>{{.Curl}}</pre>{{end}}
{{range .Groups}}<h2>{{.Name}}</h2>
<table><tr><th>Route</th><th>What it does</th></tr>
{{range .Ops}}<tr class="{{if not .Stable}}i{{end}}"><td><code>{{.Method}} /api/v1{{.Path}}</code>{{if not .Stable}} (internal){{end}}</td><td>{{.Summary}}</td></tr>
{{end}}</table>{{end}}
</body></html>`))

type group struct {
	Name string
	Ops  []Op
}

func renderPage() []byte {
	var groups []group
	idx := map[string]int{}
	for _, o := range Ops() {
		i, ok := idx[o.Group]
		if !ok {
			i = len(groups)
			idx[o.Group] = i
			groups = append(groups, group{Name: o.Group})
		}
		groups[i].Ops = append(groups[i].Ops, o)
	}
	var buf bytes.Buffer
	if err := pageTmpl.Execute(&buf, map[string]any{"Examples": Examples, "Groups": groups}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
