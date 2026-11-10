# Self-hosting

One image, one volume. Replace `IMAGE` with the image you pull (or build with `docker build -t inventory .`).

## Docker

    docker run -d --name inventory --restart unless-stopped \
      -p 8080:8080 -v inventory-data:/data \
      -e INVENTORY_PUBLIC_URL=https://stuff.example.com IMAGE

Open `http://localhost:8080` and create the first account in the setup screen.

## Compose

Copy `docker-compose.yml`, set `INVENTORY_PUBLIC_URL`, then `docker compose up -d`.
AI is optional: the commented `INVENTORY_AI_*` variables set server-wide defaults; households can also enter their own endpoint in Settings.

## The /data volume

Everything lives in `/data`, the only writable path: the SQLite database (with its WAL files), the photos (content-addressed) and `secret.key` (encrypts stored API tokens). The container runs as a non-root user (uid 65532); a bind mount must be writable by it.

## Lock first-run setup

Until the first account exists, anyone who reaches the address can create the household. For any internet-facing instance set `INVENTORY_SETUP_TOKEN` to a secret value (`docker run --rm IMAGE setup-token`, or `openssl rand -hex 24`). The setup screen then asks for it; once a household exists it is ignored. Unset, setup stays open and an https instance logs a warning at startup.

## HTTPS and a reverse proxy

The camera and "install app" on a phone only work on HTTPS. Put a reverse proxy (Caddy, Traefik, nginx) in front of port 8080, serve it on your domain, and set:

- `INVENTORY_PUBLIC_URL=https://your.domain` (cookies become Secure, links are complete)
- `INVENTORY_TRUSTED_PROXIES=172.18.0.0/16,10.0.0.1` (comma-separated IPs or CIDRs of your proxy): `X-Forwarded-For` and `X-Forwarded-Proto` are believed only from these peers. `INVENTORY_TRUST_PROXY=1` trusts every peer; use it only if nothing but the proxy can reach port 8080.

## Upgrades

Pull the new image and restart (`docker compose pull && docker compose up -d`). Migrations run by themselves at start. Back up first.

## Backups

Stop the container, copy the volume, start it again:

    docker stop inventory
    docker run --rm -v inventory-data:/data -v "$PWD":/backup alpine tar czf /backup/inventory-data.tgz -C /data .
    docker start inventory

Restore by extracting into an empty volume. Keep `secret.key` with the database.

**Export** (owners and admins): Settings > Household > "Export everything"
downloads one zip with CSV files, `inventory.json` and the original photos.
It leaves out AI tokens, password hashes, sessions, access tokens and invite
links, so it is for taking your data elsewhere, not a restore point. Use the
`/data` copy above for restores. Continuous backup with Litestream is planned
(J10.5) and not available yet.

## Coolify

Create a Docker image or Dockerfile application, add a persistent storage mounted at `/data`, set `INVENTORY_PUBLIC_URL` to your domain, port `8080`, and a health check on `/api/health`. Coolify's proxy terminates HTTPS; also set `INVENTORY_TRUST_PROXY=1`.
