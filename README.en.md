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

### What works today (v0.3)

- E-mail code sign-in + per-device cookie, with a "My devices" page to revoke access.
- Administrator created from the console, **nothing written to `docker-compose.yml`**.
- Users, groups and services (name, description, icon, internal URL, external URL, position).
- One-time sign-in link from the console, as a recovery path if e-mail is down.
- **34-application catalog** with icon and suggested port, **open in a new tab**, and **light/dark theme, accent colour and background image per user** (v0.3).
- **Proxmox discovery** (v0.2): reads the VMs and LXCs of your server or cluster, you approve what goes on the dashboard, and a green/grey dot shows whether they are running.

### What comes next

| Version | Delivery |
|---|---|
| v0.4 | Lightweight agent that lists each machine's Docker containers (optional `homeview.*` labels) |
| v0.5 | YAML import/export, views by hierarchy/type, group permissions |

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
| `HOMEVIEW_DISCOVERY_INTERVAL` | `10m` | How often Proxmox connections are re-read (`30m`, `1h`…, minimum `1m`; `0` turns it off and leaves only the "Sync now" button). |
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

- Clicking the card opens the **external** URL **in a new tab** when there is one, otherwise the internal one.
- If the service has both, the card also shows an **Internal** tag that opens the internal URL (also in a new tab).
- The address you used to open HomeView makes no difference.

> Up to v0.2 the main button depended on the address you were browsing from (local network or domain) and a `HOMEVIEW_INTERNAL_HOSTS` variable existed. It was removed; if it is still in your `.env` it is simply ignored.

### Application catalog

HomeView knows **Home Assistant, Zigbee2MQTT, ESPHome, EMQX, MQTT, MQTT Explorer, Proxmox, Portainer, Omada Controller, Code Server, Node-RED, Grafana, OpenMediaVault, Frigate, Matter, Immich, Jellyfin, Music Assistant, qBittorrent, OpenSpeedTest, FileBrowser, OpenWrt, Debian, Ubuntu, Docker, Podman, Cloudflare DDNS, Cloudflared, UniFi OS, WireGuard, Tailscale, AdGuard Home, Pi-hole and go2rtc**: an icon bundled in the image (no CDN, works offline) and the usual access port.

- In the **Services** form, **Known application** fills in the icon (`app:home-assistant`) and, if you already typed only the IP in the internal URL (e.g. `http://192.168.1.10`), completes it with the default port (`:8123`).
- In **Discovery**, the VM or LXC name is matched against the catalog (`HomeAssistant`, `pihole`, `frigate-nvr`…). When it is recognised, the URL and icon come pre-filled and the item shows the application's icon.
- Ports and paths are **suggestions** (installations differ); check them before saving. Apps without a web UI (MQTT broker, Docker, Debian…) only bring the icon.
- Icons come from [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) (Apache-2.0); the marks belong to their owners. See the `NOTICE` file.

### Appearance (per user)

Under **Appearance**, each person chooses, for their own account and on any device:

- **Theme**: automatic (follow the device), light or dark.
- **Accent colour**: eight presets or any other colour (the text on top switches between white and dark to keep contrast).
- **Background image**: PNG, JPEG or WebP up to 3 MB, with a fade control to keep text readable. The image lives in the database (the `/data` volume), is only served to its owner, and SVG is not accepted.

### Proxmox discovery

HomeView lists the VMs (qemu) and LXCs of Proxmox and lets you choose what goes on the dashboard. It only **reads** Proxmox and **never adds anything by itself**: discovered items wait in a "New" list until an administrator clicks **Add to dashboard**. Proxmox does not report service ports, so you complete the URL (the IP is pre-filled when Proxmox knows it).

**1. Create a read-only token in Proxmox** (web UI):

1. *Datacenter → Permissions → Users*: create `homeview@pve` (realm "Proxmox VE authentication server").
2. *Datacenter → Permissions → API Tokens*: create the token with ID `dashboard`, keeping **Privilege Separation** checked. Copy the secret, which is shown only once.
3. *Datacenter → Permissions → Add*: grant the **PVEAuditor** role on path `/` to the **token** `homeview@pve!dashboard` and also to the **user** `homeview@pve`.

**2. Register the connection** under **Discovery → Connections** (admin menu): a name, the URL (`https://IP:8006`), the token ID (`homeview@pve!dashboard`) and the secret. HomeView tests access before saving and stores nothing if it fails.

**3. Approve what you want** under **Discovery**. Each item becomes a service, in the group you choose (by default the Proxmox node name, created if missing).

Good to know:

- **Certificate**: Proxmox uses a self-signed certificate by default. The safe way is to paste the certificate's **SHA-256 fingerprint** (*Node → System → Certificates*) into the dedicated field: HomeView then trusts only that certificate. A "do not verify the certificate" option exists, but it is a last resort.
- **The token secret is stored encrypted** (AES-256-GCM, key derived from the server secret that is generated on first run and kept in the `/data` volume). It never appears on screen or in logs. If you lose the volume or change the server secret, edit the connection and enter the token again.
- **Network**: the container must reach `https://IP:8006` on Proxmox (usually works on a local network without changes).
- **Guest IP** (best effort): a running LXC reports its IP through the API; for a VM it needs the *QEMU Guest Agent* installed and enabled. Without an IP, type the URL yourself.
- **Syncing**: automatic every `HOMEVIEW_DISCOVERY_INTERVAL` and manual with the button. Items that vanish from Proxmox are flagged and you decide whether to forget them; an item comes back by itself if it reappears. Deleting a service returns its item to the "New" list. "Ignored" items can be restored.
- **Status**: services that came from Proxmox show a dot on the card (green = running, hollow = stopped), based on the last sync.

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
- **Fix a user's name** (for example, accents stored wrongly):
  `docker compose exec homeview homeview admin rename --email you@example.com --name "André Pires"`.
  Tip: if your terminal is not UTF-8, typing accents at the interactive prompt can corrupt the text; pass the name with `--name` (also accepted by `admin create`) or switch the terminal to UTF-8. HomeView rejects names with invalid encoding.
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

## Versions

Versions are published automatically; no manual tagging is needed. The source of truth is the [`VERSION`](VERSION) file at the repository root.

**To publish a new version:**

1. Change the number in `VERSION` (for example `0.1.1`), in the same commit as your changes or in a separate one. For the first release, create the file containing just `0.1.0`.
2. Push to `main`.

The [`release.yml`](.github/workflows/release.yml) workflow then:

- reads `VERSION` and, if the release `v<version>` does not exist yet, creates the **tag** and the GitHub **release**, with notes generated from the commits;
- builds and pushes the **Docker image** (amd64 and arm64) to `ghcr.io/andregoncalvespires/homeview-dash` with the tags `<version>`, `<major>.<minor>` and `latest`. Versions with a suffix, such as `0.2.0-rc.1`, become *pre-releases* and do not move `latest`.

If the release already exists nothing is recreated, so pushing again is safe. To rebuild only the image (for example after a failed build), use **Actions → Release → Run workflow**.

> **One-time setup:** under *Settings → Actions → General → Workflow permissions*, choose **Read and write permissions**. After the first image is published, make the package public under *Packages → homeview-dash → Package settings*.
>
> The image build lives in the same workflow as the release because tags created with `GITHUB_TOKEN` do not trigger other workflows.

## Development

Requires the Go version declared in `go.mod` (currently 1.26).

```bash
go mod tidy
go vet ./... && go test ./...
HOMEVIEW_SMTP_HOST=localhost HOMEVIEW_SMTP_SECURITY=none HOMEVIEW_SMTP_FROM=dev@localhost HOMEVIEW_DATA=./data go run ./cmd/homeview
```

Layout: `cmd/homeview` (entry point), `internal/auth` (codes and devices), `internal/web` (pages, `templates/` and `static/`), `internal/store` (interface and SQLite), `internal/i18n` (pt-BR/EN strings). To add a language, copy one of the blocks in `internal/i18n/i18n.go`; the tests fail if a key is missing.

## License

[MIT](LICENSE)
