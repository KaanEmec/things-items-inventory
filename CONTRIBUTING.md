# Contributing

Thanks for looking. Stash is MIT licensed; by contributing you agree your change
is released under the same licence.

## Build and run

Prerequisites: Go 1.27, Node 26 with npm 11. Code and docs live in `code/`.

| Command | What it does |
|---|---|
| `make dev` | Go server on :8080 plus the Vite dev server (proxies `/api`) |
| `make build` | Builds the frontend, then `bin/inventory` |
| `make test` | Go tests and Vitest |
| `make lint` | `go vet`, staticcheck and `svelte-check` |
| `make generate` | Regenerates the sqlc code (install `sqlc` first) |
| `make ci` | Lint, test, build: what CI runs |

CI also fails when generated code is out of date (`make generate-check`).

Start with [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the map of packages
and the rules the code follows.

## AI evaluation (manual)

`make ai-eval` runs a set of test photos and texts against a real AI endpoint
and scores the answers. Run it by hand when a prompt or a model changes. It is
not part of CI, and it costs money on a paid endpoint.

    export INVENTORY_AI_VISION_TOKEN=...      # your key
    export INVENTORY_AI_VISION_MODEL=...      # a model that accepts images
    make ai-eval CASES=~/inventory-ai-cases

The test set holds photos of real homes. **Keep it outside this repository**
and never commit photos, receipts or expected answers that show a real home.
The full guide, case format and scoring are in
[cmd/ai-eval/README.md](cmd/ai-eval/README.md).

## Pull requests

- Keep a change to one thing. Say what changed and why.
- Run `make ci` before you push.
- Do not add tokens, keys or personal data to code, tests or fixtures.
- Roadmap jobs are in `files/roadmap/`; update a job's Status line when you
  start or finish it, if you have access to that folder.

## Security

Do not open a public issue for a security problem. See [SECURITY.md](SECURITY.md).
