# HomeView

> A lightweight, self-hosted dashboard for your homelab: e-mail code sign-in, services organised in groups, and separate links for internal and external access.

**English** · [Português (Brasil)](README.md)

![MIT license](https://img.shields.io/badge/license-MIT-blue) ![status](https://img.shields.io/badge/status-v0.9%20in%20development-orange)

## What it is

HomeView is a simple dashboard for people with several servers, VMs and containers who want to **control who sees what**.

- **One small container**, no Redis or Postgres. Data lives in a single SQLite file.
- **Passwordless sign-in**: only people registered by the administrator can get in, using a code sent by e-mail. The browser/device is then authorised by a cookie and can be revoked at any time.
- **Admin-only changes**: only administrators add users, groups and services.
- **Two addresses per service** (internal and external). The dashboard highlights the right one depending on whether you are at home or away, and shows the other as a shortcut.
- **Portuguese and English UI**, light and dark themes, instant search in the middle of the header (press `/`; on phones it becomes a magnifier icon).

### What works today (v0.9)

- **Passwordless sign-in**: e-mail code + per-device cookie, with a "My devices" page to revoke access. A one-time sign-in link from the console is the recovery path if e-mail is down.
- **Administrator created from the console**, nothing written to `docker-compose.yml`.
- **Users, groups and services** (name, description, icon, internal URL, external URL), with a **35-application catalog** (icon and suggested port) or a **custom icon** uploaded from your computer.
- **Proxmox discovery**: reads the VMs and LXCs of your server or cluster; you approve what goes on the dashboard, with **several services per VM/LXC** and the option to point at the **Proxmox host's own IP**. A green/grey dot shows whether they are running.
- **Three dashboard views** (layers icon next to the gear): **Groups** (your own), **Infrastructure** (by Proxmox VM/LXC, with status and IP) and **Type** (Automation, Network, Media…, with an editable type per service). The choice is remembered in the browser.
- **Proxmox figures at the top of the dashboard**: a row with one card per node (CPU, memory and the usage of every storage), up to 4 per row, and a **Show VMs/LXCs** button that opens the node's machines panel (CPU, memory, disk and network) over the cards. Configurable threshold alerts. The administrator decides per user what each person sees.
- **Installable as an app (PWA)** on phones and computers, with its own icon, full-screen and fast start.
- **Organisable dashboard**: rearrange cards and groups by dragging (two columns of groups), **collapse/expand groups** (remembered in the browser) and instant search.
- **Sub-groups and per-group permissions**: a group can be restricted to administrators and the people you choose (applies to its sub-groups and to every view).
- **YAML editor** for groups, sub-groups, permissions and services, with validation before applying and file download.
- **Per-user appearance**: light/dark theme, accent colour and 11 built-in backgrounds (starry sky and dark theme are the default) or your own image.
- **Gear menu** (⚙) in the header with every screen.

### What comes next

| Version | Delivery |
|---|---|
| v1.0 | To be decided (ideas: role-based permissions, change history) |

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

Runtime settings (address, SMTP, language…) are environment variables, usually in the `.env` file. Users, groups, services and connections live in the SQLite database `homeview.db`, inside the Docker volume `homeview-data` (`/data`): there is no configuration file next to the compose file.

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

### Internal vs external links

Each service can have an **internal URL** (e.g. `http://192.168.1.10:8123`) and an **external URL** (e.g. `https://ha.example.com`).

- Clicking the card opens the **external** URL **in a new tab** when there is one, otherwise the internal one.
- If the service has both, the card also shows an **Internal** tag that opens the internal URL (also in a new tab).
- The address you used to open HomeView makes no difference.

> Up to v0.2 the main button depended on the address you were browsing from (local network or domain) and a `HOMEVIEW_INTERNAL_HOSTS` variable existed. It was removed; if it is still in your `.env` it is simply ignored.

### Application catalog

HomeView knows **AdGuard Home, Cloudflare DDNS, Cloudflared, Code Server, Debian, Docker, EMQX, ESPHome, FileBrowser, Frigate, go2rtc, Grafana, Home Assistant, Immich, InfluxDB, Jellyfin, Matter Server, MQTT (broker), MQTT Explorer, Music Assistant, Node-RED, Omada Controller, OpenMediaVault, OpenSpeedTest, OpenWrt, Pi-hole, Podman, Portainer, Proxmox, qBittorrent, Tailscale, Ubuntu, UniFi OS, WireGuard (wg-easy) and Zigbee2MQTT**: an icon bundled in the image (no CDN, works offline) and the usual access port.

- In the **Services** form, **Known application** fills in the icon (`app:home-assistant`) and, if you already typed only the IP in the internal URL (e.g. `http://192.168.1.10`), completes it with the default port (`:8123`).
- In **Discovery**, the VM or LXC name is matched against the catalog (`HomeAssistant`, `pihole`, `frigate-nvr`…). When it is recognised, the URL and icon come pre-filled and the item shows the application's icon.
- Ports and paths are **suggestions** (installations differ); check them before saving. Apps without a web UI (MQTT broker, Docker, Debian…) only bring the icon.
- Icons come from [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) (Apache-2.0); the marks belong to their owners. See the `NOTICE` file.

### Proxmox discovery

HomeView lists the VMs (qemu) and LXCs of Proxmox and lets you choose what goes on the dashboard. It only **reads** Proxmox and **never adds anything by itself**: discovered items wait in a "New" list until an administrator clicks **Add to dashboard**. Proxmox does not report service ports, so you complete the URL (the IP is pre-filled when Proxmox knows it).

**1. Create a read-only token in Proxmox** (web UI):

1. *Datacenter → Permissions → Users*: create `homeview@pve` (realm "Proxmox VE authentication server").
2. *Datacenter → Permissions → API Tokens*: create the token with ID `dashboard`, keeping **Privilege Separation** checked. Copy the secret, which is shown only once.
3. *Datacenter → Permissions → Add*: grant the **PVEAuditor** role on path `/` to the **token** `homeview@pve!dashboard` and also to the **user** `homeview@pve`.

**2. Register the connection** under **Discovery → Connections** (admin menu): a name, the URL (`https://IP:8006`), the token ID (`homeview@pve!dashboard`) and the secret. HomeView tests access before saving and stores nothing if it fails.

**3. Approve what you want** under **Discovery**. Each item becomes a service, in the group you choose (by default the Proxmox node name, created if missing). **A machine can have several services**: after the first approval use **Add another service**, pick the application (its IP and usual port are filled in) and repeat as often as needed. E.g. an LXC with OpenSpeedTest, Portainer and two Zigbee2MQTT. The machine only goes back to "New" when its last service is removed.

Under **Services**, the **Where it runs** field lists the discovered machines and the **Proxmox hosts**. Picking one fills the IP into the internal URL (with the application's port) and, for a machine, shows the running/stopped status on the card.

Good to know:

- **Certificate**: Proxmox uses a self-signed certificate by default. The safe way is to paste the certificate's **SHA-256 fingerprint** (*Node → System → Certificates*) into the dedicated field: HomeView then trusts only that certificate. A "do not verify the certificate" option exists, but it is a last resort.
- **The token secret is stored encrypted** (AES-256-GCM, key derived from the server secret that is generated on first run and kept in the `/data` volume). It never appears on screen or in logs. If you lose the volume or change the server secret, edit the connection and enter the token again.
- **Network**: the container must reach `https://IP:8006` on Proxmox (usually works on a local network without changes).
- **Guest IP** (best effort): a running LXC reports its IP through the API; for a VM it needs the *QEMU Guest Agent* installed and enabled. Without an IP, type the URL yourself.
- **Syncing**: automatic every `HOMEVIEW_DISCOVERY_INTERVAL` and manual with the button. Items that vanish from Proxmox are flagged and you decide whether to forget them; an item comes back by itself if it reappears. Deleting a service returns its item to the "New" list. "Ignored" items can be restored.
- **Status**: services that came from Proxmox show a dot on the card (green = running, hollow = stopped), based on the last sync.

### Proxmox figures

With a connection set up (see above), the top of the dashboard shows a row with **one card per node**: CPU and memory gauges, a bar for every **storage** in two columns (shared ones appear once; those in alert come first and, past 6, the rest sits behind a **+N storages** toggle) and the uptime. The cards share the whole row (1 to 4 per row, wider when there are fewer nodes; 2 on medium screens and 1 on phones). Each card has a **Show VMs/LXCs** button that opens, over the cards, a panel with that node's machines: CPU, memory, disk and network, with *All*, *VMs*, *LXCs* and *Alerts only* filters. It closes with Esc, the ×, a click outside or the same button. The row can be collapsed with the *Proxmox* title.

- **Who sees it**: under **Users** the administrator ticks, per person, what they may see: **Nodes**, **Machines** and **Alerts**. With nothing ticked the row does not even appear and the API returns no data. Administrators always see everything. A machine behind services the person cannot see (a restricted group, say) stays hidden from them.
- **What each person chooses**: under **Appearance → Proxmox information**, within what was allowed: show nodes, show VMs/LXCs, highlight alerts, which columns the panel shows, the **warning** (yellow, default 75%) and **critical** (red, default 90%) thresholds and the refresh interval (default 30 s, 10 to 600).
- **Alerts**: a card gets a yellow or red border when CPU, memory or a storage passes a threshold, and the **Show VMs/LXCs** button shows a badge with the number of machines in alert even while the panel is closed.
- **How the data arrives**: the server reads Proxmox with the same read-only token as discovery (*PVEAuditor* role) and only while someone has the dashboard open; several people share one reading (refreshed at most every 10 s). If a server stops answering, the card keeps the last values with a *Stale data* note. The network rate is computed between two readings. VMs without the *QEMU Guest Agent* report no disk usage, so only the size is shown.

### Install as an app (PWA)

HomeView can be installed as an app, with a home-screen icon and no browser bar.

- **Requirement: HTTPS.** Browsers only offer installation on a secure address (`https://`), which is already the case behind a reverse proxy or a Cloudflare Tunnel (see above). On `http://IP:port` in a local network the install option does not appear, except on `localhost`.
- **Android (Chrome)**: ⚙ menu → **Install app** (or Chrome's menu → *Install app*).
- **iPhone/iPad (Safari)**: *Share* button → **Add to Home Screen**.
- **Computer (Chrome/Edge)**: the install icon in the address bar, or ⚙ menu → **Install app**.

The app keeps only HomeView's own files (styles, scripts and icons) to start faster. **No page, Proxmox figure or uploaded image is stored on the device**: when offline you only get a notice with a *Try again* button. Sign-in still lasts through the device cookie. To regenerate the icons: `python3 tools/icons/gen.py internal/web/static/pwa` (needs Pillow).

### Dashboard views

The layers icon next to the gear switches how the dashboard is shown: **Groups** (your own organisation, the only one you can rearrange by dragging), **Infrastructure** (one section per discovered VM/LXC, ordered by node and ID, with a status dot and IP; services without a machine go under *No machine*) and **Type** (the catalog application's category; anything not from the catalog goes to *Other*). Every section can also be collapsed.

The sign-in page uses the same default look as the dashboard (dark theme and starry sky).

### Sub-groups and permissions (admin)

Under **Groups**, a group can be a **sub-group of** another (one level only; shown as *Home › Kitchen*) and can be marked **Restricted**: then only administrators and the people ticked under **Who can see it** see the group and its services. The rule covers sub-groups (a sub-group only shows when its parent does) and every dashboard view. Services without a group stay visible to everyone. Administrators always see everything.

In the **Services** form, **Type** sets the category used by the *Type* view (it comes from the catalog icon by default), and picking a **Proxmox host** under *Where it runs* places the service in that node's section in the *Infrastructure* view.

### Building the dashboard (admin)

Once users, groups and services exist (⚙ menu), these tools help you shape the dashboard. Only administrators see them.

- **Custom icon**: under **Services** (and when approving a discovered machine) pick **Or upload an image from your computer** (PNG, JPEG, WebP, GIF or SVG, up to 512 KB). The image is stored in HomeView's database, identical images are stored once, and an image is removed when no service uses it any more.
- **Rearranging (admin)**: on the dashboard, **Rearrange** (in the ⚙ menu) lets you drag cards between groups and use the ⠿ handle to move groups; the keyboard works too (Alt + ↑/↓). The order is saved immediately. The dashboard uses two columns of groups on wide screens (one on narrow ones), and each group shows at most two columns of cards.
- **YAML editor (admin)**: ⚙ menu → **Edit YAML**. Groups and services show up as text; **Check** previews the changes and **Apply** saves them (anything missing from the file is removed). You can download `homeview.yml`. Proxmox connections, users and discovery are not included. Sub-groups go under `groups:` inside the parent group, permissions use `restricted: true` and `users:` (e-mails), and each service accepts `type:`. Configuration lives in the SQLite file `homeview.db`, in the Docker volume `homeview-data` (`/data`).

### Appearance and daily use (per user)

Under ⚙ menu → **Appearance**, each person chooses, for their own account and on any device:

- **Theme**: automatic (follow the device), light or dark.
- **Accent colour**: eight presets or any other colour (the text on top switches between white and dark to keep contrast).
- **Gear menu**: Services, Groups, Edit YAML, Discovery, Users, Appearance, My devices and Sign out live in a drop-down under the ⚙ icon in the header (closes with Esc or a click elsewhere).
- **Collapsing groups**: click a group name to hide its cards and save space. The choice is remembered in this browser, and searching expands groups automatically.
- **Built-in backgrounds**: 11 original images (Starry sky is the default, together with the dark theme; "No background" is also an option). Regenerate with `python3 tools/backgrounds/gen.py <dir>` (needs Pillow and numpy).
- **Your own background image**: PNG, JPEG or WebP up to 3 MB, with a fade control to keep text readable. The image lives in the database (the `/data` volume), is only served to its owner, and SVG is not accepted.

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
