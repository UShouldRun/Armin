---
name: self-hosting
description: Deploy and run self-hosted apps — Docker Compose stacks, Cloudflare Tunnel instead of open ports, JWT auth, secrets in .env, backups and restores, health checks. Load when working on deployment, containers or infrastructure.
---
## Docker Compose
- One service per process; pinned image versions, never `latest` in
  production.
- `restart: unless-stopped`, `healthcheck` on every service, and
  `depends_on: condition: service_healthy` for ordering.
- Named volumes for data; bind mounts only for config.
- Secrets from `.env` (never committed; ship `.env.example` with
  placeholder values). Don't bake secrets into images.
- Internal networks for service-to-service traffic; only the tunnel
  or reverse proxy talks to the outside.
- Small images: multi-stage builds, static Go binaries on
  distroless/alpine, `.dockerignore`.

## Exposure
- Cloudflare Tunnel (`cloudflared`) instead of opening ports on the
  host; route hostnames to internal service names.
- If a reverse proxy (nginx) is used, it terminates TLS or sits
  behind the tunnel; set forwarding headers and body-size limits.

## Auth
- JWT: short-lived access tokens, refresh tokens stored server-side
  (revocable), `HttpOnly; Secure; SameSite` cookies, signing key from
  `.env`, algorithm pinned on verify.
- Rate-limit login and refresh endpoints.

## Data
- Backups on a schedule (e.g. `mysqldump` from a cron container),
  compressed, dated, copied off the host; retention policy.
- Test restores — a backup never restored is a guess. Keep the
  restore procedure in a script.
- Migrations versioned and applied in order; never edit an applied
  one.

## Operating
- `docker compose logs -f <service>`, `docker compose ps` for health;
  a `manage.sh` with up / down / logs / backup / restore commands.
- Update by pulling pinned new versions, then `docker compose up -d`;
  keep the previous image tag to roll back.
- Never run destructive commands (`down -v`, dropping databases,
  deleting volumes) without the user confirming.
