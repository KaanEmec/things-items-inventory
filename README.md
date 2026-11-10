# Stash

Self-hostable inventory of personal belongings for a household: where things
are, their state, warranties and moving boxes. One Go binary serves the API and
an embedded Svelte app. "Stash" is a placeholder name (`web/src/lib/app.ts`).

## Prerequisites

Go 1.27 and Node 26 with npm 11.

## Make targets

- `make dev`: Go server on :8080 plus the Vite dev server (proxies `/api`)
- `make build`: build the frontend, then `bin/inventory` with version and build time
- `make test`: Go tests and Vitest
- `make lint`: `go vet` and `svelte-check`
- `make generate`: no-op for now
- `make ci`: lint, test, build

Run `./bin/inventory` after `make build`. Settings: `INVENTORY_ADDR` (default
`:8080`) and `INVENTORY_DATA` (default `./data`).

Released under the MIT license.
