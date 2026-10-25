# HomeView

> A lightweight, self-hosted dashboard for your homelab: e-mail code sign-in, services organised in groups, and separate links for internal and external access.

**English** · [Português (Brasil)](README.md)

![MIT license](https://img.shields.io/badge/license-MIT-blue) ![status](https://img.shields.io/badge/status-v0.1%20in%20development-orange)

## What it is

HomeView is a simple alternative to dashboards such as Heimdall and Homarr, built for people with several servers, VMs and containers who want to **control who sees what**.

- **One small container**, no Redis or Postgres. Data lives in a single SQLite file.
- **Passwordless sign-in**: only people registered by the administrator can get in, using a code sent by e-mail. The browser/device is then authorised by a cookie and can be revoked at any time.
- **Admin-only changes**: only administrators add users, groups and services.
- **Two addresses per service** (internal and external). The dashboard highlights the right one depending on whether you are at home or away, and shows the other as a shortcut.
- **Portuguese and English UI**, light and dark themes, instant search (press `/`).

### What works today (v0.1)

- E-mail code sign-in + per-device cookie, with a "My devices" page to revoke access.
- Administrator created from the console, **nothing written to `docker-compose.yml`**.
- Users, groups and services (name, description, icon, internal URL, external URL, position).
- One-time sign-in link from the console, as a recovery path if e-mail is down.

### What comes next

| Version | Delivery |
|---|---|
| v0.2 | Automatic discovery of VMs and LXCs through the Proxmox API, approved by the administrator |
| v0.3 | Lightweight agent that lists each machine's Docker containers (optional `homeview.*` labels) |
| v0.4 | YAML import/export, views by hierarchy/type, group permissions |

## Install (3 steps)

You need Docker with Compose and an SMTP account to send the codes (see [Setting up e-mail](#setting-up-e-mail)).

**1. Download the files** into a folder, e.g. `/opt/homeview`:

```bash
mkdir -p /opt/homeview && cd /opt/homeview
curl -fsSLO https://raw.githubusercontent.com/andregoncalvespires/homeview-dash/main/docker-compose.yml
curl -fsSL  https://raw.githubusercontent.com/andregoncalvespires/homeview-dash/main/.env.example -o .env
```

**2. Edit `.env`** with the dashboard address and your SMTP details, then start it:

```bash
nano .env
docker compose up -d
```

**3. Create the administrator** (interactive; nothing is stored in the compose file):

```bash
docker compose exec -it homeview homeview admin create
```

Open the configured address, enter the administrator's e-mail and type the code you receive. Then use **Groups**, **Services** and **Users** in the menu to build your dashboard.

> The image is published at `ghcr.io/andregoncalvespires/homeview-dash`. If `docker compose up` is denied, the package is still private: on GitHub open *Packages → homeview-dash → Package settings* and set the visibility to **Public**. You can also build locally by uncommenting `build: .` in the compose file.

## Configuration

Everything is set through environment variables, usually in the `.env` file.

| Variable | Default | Description |
|---|---|---|
| `HOMEVIEW_BASE_URL` | `http://localhost:8080` | Public address of the dashboard. With `https://` the cookie gets the `Secure` flag. |
| `HOMEVIEW_PORT` | `8080` | Host port published by the compose file. The container always listens on 8080. |
| `HOMEVIEW_LANG` | `pt-BR` | Default language (`pt-BR` or `en`). Each user can switch it in the footer. |
| `HOMEVIEW_INTERNAL_HOSTS` | *(empty)* | Extra host names meaning "I am on the local network", comma separated. |
| `HOMEVIEW_SMTP_HOST` | *(required)* | SMTP server. |
| `HOMEVIEW_SMTP_PORT` | `587` | SMTP port. |
| `HOMEVIEW_SMTP_SECURITY` | `starttls` | `starttls` (587), `tls` (465) or `none` (trusted local relay only). |
| `HOMEVIEW_SMTP_USER` / `HOMEVIEW_SMTP_PASS` | *(empty)* | Credentials, if the server requires them. |
| `HOMEVIEW_SMTP_FROM` | SMTP user | Sender, e.g. `HomeView <homeview@example.com>`. |
| `HOMEVIEW_COOKIE_SECURE` | automatic | Force `true`/`false` if needed (defaults to `true` when the base URL is https). |
| `HOMEVIEW_ADDR` / `HOMEVIEW_DATA` | `:8080` / `/data` | Listen address and data folder (already set in the image). |
| `TZ` | `UTC` | Time zone used in the pages, e.g. `America/Sao_Paulo`. |

### Internal vs external links

Each service can have an **internal URL** (e.g. `http://192.168.1.10:8123`) and an **external URL** (e.g. `https://ha.example.com`).

- If you open HomeView through a private IP, a single-label name, `*.lan`, `*.local` or a name listed in `HOMEVIEW_INTERNAL_HOSTS`, the main button is the **internal** one and the **external** link appears as a shortcut.
- Through any other address (e.g. `https://dash.example.com`) it is the other way round.
- A service with only one URL always uses it as the main button.

If you use split DNS with a "public-looking" name (e.g. `dash.home.example.com` pointing at a local IP), list it in `HOMEVIEW_INTERNAL_HOSTS`.

### Setting up e-mail

The sign-in code is sent over SMTP, so a dedicated account is a good idea.

- **Gmail**: enable 2-step verification and create an *app password*. Use `smtp.gmail.com`, port `587`, `starttls`, user = your address, password = the app password.
- **Other providers** (Brevo, Mailgun, Office 365, your own server): use the SMTP details they give you. For port 465 set `HOMEVIEW_SMTP_SECURITY=tls`.
- **Unencrypted local relay** (e.g. Postfix on your LAN): `HOMEVIEW_SMTP_SECURITY=none`. Do not use this on networks you do not control.

HomeView **refuses** to send credentials over an unencrypted connection.

### Behind a reverse proxy or Cloudflare Tunnel

1. Publish the container (port 8080) through the proxy/tunnel and set `HOMEVIEW_BASE_URL=https://dash.example.com`.
2. The proxy must **preserve the `Host` header** (Nginx: `proxy_set_header Host $host;`). Cloudflare Tunnel already does. The `Host` decides between internal and external links.
3. Always use HTTPS away from home.

## Day to day

- **Update**: `docker compose pull && docker compose up -d`.
- **Logs**: `docker compose logs -f homeview`. If the code does not arrive, the reason (for example an SMTP authentication failure) shows up here.
- **Locked out** (e-mail is down): create a one-time link from the console, valid for 15 minutes:

  ```bash
  docker compose exec -it homeview homeview admin login-link --email you@example.com
  ```

- **List users**: `docker compose exec homeview homeview admin list`.
- **Backup**: all data is in the `homeview-data` volume (`homeview.db`). For example:

  ```bash
  docker compose stop homeview
  docker run --rm -v "$(docker volume ls -q | grep homeview-data)":/data -v "$PWD":/backup alpine \
    tar czf /backup/homeview-data.tgz -C /data .
  docker compose start homeview
  ```

- If you prefer a host folder over a named volume, it must be owned by UID `10001` (`chown 10001:10001 /path`).

## Security

- Only **e-mails registered by the administrator** can receive a code; the sign-in page answers identically for unknown addresses.
- 6-digit codes last 10 minutes, allow at most 5 attempts and are limited to 3 e-mails per 15 minutes per user. They are stored only as an HMAC hash.
- The device cookie is a random 256-bit value; only its hash is stored. Each device can be revoked under **My devices**, and devices idle for 180 days expire.
- Forms are CSRF-protected, responses carry strict security headers (CSP, `X-Frame-Options`, ...) and only `http://` / `https://` URLs are accepted.
- Sign-in security depends on each user's e-mail account security. Do not expose the dashboard to the internet without HTTPS.

Found a vulnerability? Please open an issue without exploitable details or message the maintainer privately.

## Development

Requires Go 1.24+.

```bash
go mod tidy
go vet ./... && go test ./...
HOMEVIEW_SMTP_HOST=localhost HOMEVIEW_SMTP_SECURITY=none HOMEVIEW_SMTP_FROM=dev@localhost HOMEVIEW_DATA=./data go run ./cmd/homeview
```

Layout: `cmd/homeview` (entry point), `internal/auth` (codes and devices), `internal/web` (pages, `templates/` and `static/`), `internal/store` (interface and SQLite), `internal/i18n` (pt-BR/EN strings). To add a language, copy one of the blocks in `internal/i18n/i18n.go`; the tests fail if a key is missing.

## License

[MIT](LICENSE)
