# Deploy (VPS + Docker Compose + Caddy)

## 1) DNS + firewall

- Create an `A` record for your domain (e.g. `reader.ru`) pointing to the VPS public IP.
- Open inbound ports: `80/tcp` and `443/tcp`.

## 2) Configure environment

- Copy `.env.example` → `.env`.
- Edit `.env`:
  - Set `TELEGRAM_TOKEN` (use a fresh token from BotFather).
  - Set `MINIAPP_URL` to your domain root, e.g. `https://reader.ru/`.
  - Set `DOMAIN` to the same domain, e.g. `reader.ru`.
  - Set `LETSENCRYPT_EMAIL` (recommended for Let's Encrypt notifications).

If you keep an `.onion` `FLIBUSTA_URL`, you must provide a SOCKS5 proxy via `TOR_PROXY`:

- Option A (recommended): run Tor on the VPS host, keep `TOR_PROXY=127.0.0.1:9050`.
- Option B: enable the `tor` service in `docker-compose.yml` and set `TOR_PROXY=tor:9050`.

## 3) Start

```bash
docker compose up -d --build
```

## 4) Verify

- Check logs:
  - `docker compose logs -f app`
  - `docker compose logs -f caddy`
- Health endpoint (via the domain after HTTPS is issued):
  - `https://reader.ru/api/health`

