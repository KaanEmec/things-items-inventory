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

## HTTPS and a reverse proxy

The camera and "install app" on a phone only work on HTTPS. Put a reverse proxy (Caddy, Traefik, nginx) in front of port 8080, serve it on your domain, and set:

- `INVENTORY_PUBLIC_URL=https://your.domain` (cookies become Secure, links are complete)
- `INVENTORY_TRUST_PROXY=1`, only if the proxy sets `X-Forwarded-For` and nothing else can reach port 8080.

## Upgrades

Pull the new image and restart (`docker compose pull && docker compose up -d`). Migrations run by themselves at start. Back up first.

## Backups

Stop the container, copy the volume, start it again:

    docker stop inventory
    docker run --rm -v inventory-data:/data -v "$PWD":/backup alpine tar czf /backup/inventory-data.tgz -C /data .
    docker start inventory

Restore by extracting into an empty volume. Keep `secret.key` with the database.

## Coolify

Create a Docker image or Dockerfile application, add a persistent storage mounted at `/data`, set `INVENTORY_PUBLIC_URL` to your domain, port `8080`, and a health check on `/api/health`. Coolify's proxy terminates HTTPS; also set `INVENTORY_TRUST_PROXY=1`.
