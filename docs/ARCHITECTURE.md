# Architecture

A self-hosted household inventory: where things are, their state, warranties
and moving boxes. One Go binary, one SQLite file, one folder of photos.
"Stash" is a placeholder name.

Details live elsewhere; this page is the map.
- Data model and API: `files/spec.md` (outside this repo, in the ProjectHub project)
- Roadmap and job status: `files/roadmap/v1/README.md`
- Decisions, dated: `AGENTS.md` → Decisions

## System

```
 Phone / desktop browser (Svelte 5 PWA)        Agents (MCP clients)
        │  HTTPS: /api/v1, SSE, photos                │  /mcp + access token
        ▼                                             ▼
 ┌──────────────────────── Go binary ────────────────────────┐
 │ httpapi ── auth ── inventory ── search ── photos ── mcp    │
 │                 └── jobs (worker pool) ── ai (client)      │
 └───────┬───────────────────┬──────────────────────┬─────────┘
         ▼                   ▼                      ▼
   /data/app.db         /data/photos/…      AI endpoints (optional)
   SQLite, WAL, FTS5    by content hash     "photo understanding" and
                                            "everything else"
```

## Components

| Package | Does | Epic |
|---|---|---|
| `cmd/inventory` | Flags and env, start, graceful shutdown | E1 |
| `internal/config` | Listen address, data directory, server-wide AI defaults | E1 |
| `internal/db` | Open SQLite (one writer, many readers), embedded migrations, sqlc queries | E1 |
| `internal/server` | Lists the features, wires them together (queue, photo hooks), builds the handler | E1 |
| `internal/secret` | Instance key and encryption of stored API tokens | E5 |
| `internal/httpapi` | Routes under `/api/v1`, JSON errors, middleware, serves the embedded app | E1 |
| `internal/auth` | Users, sessions, households, roles, invites, access tokens | E2, E9 |
| `internal/inventory` | Places tree, items, contents lines, boxes, events | E3, E8 |
| `internal/photos` | Upload, content-addressed files, derivatives | E3 |
| `internal/search` | FTS5 trigram index and ranking | E4 |
| `internal/ai` | Two profiles, OpenAI-compatible client, prompts and schemas | E5–E7 |
| `internal/jobs` | Jobs table and worker pool | E5 |
| `internal/mcp` | MCP endpoint for agents | E9 |
| `web/` | Svelte 5 + Vite + Tailwind v4 app, embedded into the binary | all |

Packages are created by the job that first needs them, not in advance.

**Adding a feature:** give the package a `Routes(mux *httpapi.Mux)` (and
optionally a `Seed` that creates rows for one household) and append a
`Feature` to `internal/server/server.go`. Its routes then exist in the app and
in the cross-household test (`crosshousehold_test.go`), which tries every
`{param}` with another household's IDs and expects 404.

## Configuration

| Variable | Meaning |
|---|---|
| `INVENTORY_ADDR` | Listen address, default `:8080` |
| `INVENTORY_DATA` | Data directory (`app.db`, `photos/`, `secret.key`), default `./data`; `/data` in Docker |
| `INVENTORY_PUBLIC_URL` | Address people use; `https://` makes cookies Secure and prefixes printed links |
| `INVENTORY_SETUP_TOKEN` | If set, first-run setup needs this token (403 otherwise); `inventory setup-token` makes one. A startup warning says setup is open when it is unset and `INVENTORY_PUBLIC_URL` is https |
| `INVENTORY_TRUST_PROXY` | `1` takes the client address from `X-Forwarded-For` (only behind a proxy) |
| `INVENTORY_SECRET_KEY` | Base64 key for stored tokens; empty uses `<data>/secret.key` |
| `INVENTORY_AI_BLOCK_PRIVATE` | `1` refuses AI endpoints on private addresses |
| `INVENTORY_AI_{VISION,TEXT}_{BASE_URL,TOKEN,MODEL}` | Server-wide AI defaults per role |

`inventory healthcheck` probes `/api/health` (Docker `HEALTHCHECK`).

## Rules that shape the code

1. **Every row belongs to a household.** Every query on a household table
   takes `household_id`; a test fails when one doesn't.
2. **AI is optional and never blocks a save.** Each entry path saves a row
   first; a job fills it in later. The AI never overwrites a field a person
   edited, and AI-filled fields stay marked until confirmed.
3. **Two AI roles, one client.** `ai.For(household, role)` returns the
   endpoint for `vision` or `text`; `text` can follow `vision`. Tokens are
   encrypted on disk and never leave the server.
4. **One binary, no cgo.** `modernc.org/sqlite`, standard-library `net/http`,
   frontend embedded with `embed`. Slow work goes through the jobs table.
5. **One writer.** All writes go through a single SQLite connection; events
   are written in the same transaction as the change.
6. **Lean by default.** No framework, no abstraction before its second use,
   and no dependency a few lines of code can replace.

## Main flows

- **Capture:** photo → upload (queued on the phone) → item saved as a draft →
  `draft_item` job → vision endpoint → validated JSON → AI fields filled →
  person reviews and confirms.
- **Quick add:** typed row saved as a draft → text endpoint suggests the rest
  → Tab accepts.
- **Moving:** pick the "packing now" box → capture or type what goes in →
  close box → print labels → unpack into places in the new home.

## Key decisions

| Decision | Why |
|---|---|
| Go + SQLite single binary with an embedded PWA | Easiest thing to self-host and back up |
| Built-in auth, households with roles | No identity provider needed; the app is for households |
| OpenAI-compatible endpoints set in Settings, two roles | Any provider or local model; a cheap text model next to a vision model |
| Drafts saved first, AI afterwards | Entry stays fast and works offline or without AI |
| Boxes are places of kind "box" | Items, photos and search work without special cases |
| MIT licence | Decided 2026-11-10 |
## Roadmap

Ten epics, 50 jobs, from foundations to PWA and release; order and status
are in the roadmap README.
