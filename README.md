# Stash

Stash is a self-hosted inventory of what your household owns: which place or
moving box each thing is in, its state, its warranty, and its photos. One Go
binary serves the API and the app; the data is one SQLite file and a folder of
photos. The app works on a phone as an installable web app.

AI is optional. With it, photos of items, labels and receipts fill in the
details, and a pasted list becomes items. Without it, everything works by hand.

> "Stash" is a placeholder name. It is set in one place: `APP_NAME` in
> `web/src/lib/app.ts`. The binary and image are still called `inventory`.

## Screenshots

_Placeholder: three or four screenshots go here (overview, item with photo and
draft, moving boxes and labels, phone capture)._

## Quick start

With Docker (until a release image is published, build it from this folder):

    docker build -t inventory .
    docker run -d --name inventory --restart unless-stopped \
      -p 8080:8080 -v inventory-data:/data \
      -e INVENTORY_PUBLIC_URL=http://localhost:8080 inventory

Open http://localhost:8080 and create the first account in the setup screen.
Your data is in the `inventory-data` volume (`/data` in the container).

Without Docker (Go 1.27 and Node 26 with npm 11):

    make build
    ./bin/inventory

Data goes to `./data` and the server listens on `:8080`. Change these with
`INVENTORY_DATA` and `INVENTORY_ADDR`.

For use beyond your own machine, run it behind a reverse proxy with HTTPS. The
camera and "install app" need HTTPS. Set `INVENTORY_PUBLIC_URL` to your
address.

## Configuration

Environment variables are listed in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#configuration) and commented in
[docker-compose.yml](docker-compose.yml).

## Guides

- [Self-hosting](docs/SELF-HOSTING.md): Docker and Compose, the `/data` volume,
  HTTPS, upgrades, backups
- [AI setup](docs/AI.md): the two endpoints, OpenRouter or your own, and what is
  sent
- [Phone](docs/PHONE.md): installing on iPhone and Android, uploads
- [Agents](docs/MCP.md): access tokens and connecting an MCP client
- [Architecture](docs/ARCHITECTURE.md): how the code is organised
- [Contributing](CONTRIBUTING.md) and [security policy](SECURITY.md)

## Licence

MIT. See [LICENSE](LICENSE). Copyright (c) 2026 Kaan Emec.

The dependencies use permissive licences (MIT, BSD, Apache-2.0, SIL OFL). A full
third-party licence list will ship with the image.
