// Package catalog is HomeView's built-in list of well-known self-hosted
// applications: their icon, the port and scheme their web UI usually listens
// on, and the words that identify them in VM, container and image names.
// It only offers suggestions; nothing here is applied without an admin's OK.
package catalog

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// IconPrefix marks a service icon that refers to a catalog application, for
// example "app:home-assistant".
const IconPrefix = "app:"

// App describes one application.
type App struct {
	ID   string // stable slug, also used in "app:<id>" icons
	Name string
	File string // icon file name under /static/icons/
	// Web is false for things without a web interface (a broker, an OS); the
	// port is then informational and no URL is suggested.
	Web    bool
	Scheme string // "http" or "https" (Web apps)
	Port   int    // usual port; 0 when there is none
	Alt    []int  // other ports commonly used
	Path   string // path to append, e.g. "/admin"
	// Aliases are lower-case words that identify the app in a guest or
	// container name. Keep them specific: they are matched as substrings of
	// the name with punctuation removed.
	Aliases []string
}

// IconURL is where the embedded icon is served.
func (a App) IconURL() string { return "/static/icons/" + a.File }

// IconRef is the value stored in a service's Icon field.
func (a App) IconRef() string { return IconPrefix + a.ID }

// SuggestedURL builds the usual URL for host (an IP or name). It returns ""
// for apps without a web interface.
func (a App) SuggestedURL(host string) string {
	if !a.Web || host == "" {
		return ""
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") { // bare IPv6
		host = "[" + host + "]"
	}
	u := a.Scheme + "://" + host
	if !((a.Scheme == "http" && a.Port == 80) || (a.Scheme == "https" && a.Port == 443)) && a.Port > 0 {
		u += fmt.Sprintf(":%d", a.Port)
	}
	return u + a.Path
}

// PortLabel is a short human hint such as "8123" or "8123 (alt: 80, 8080)".
func (a App) PortLabel() string {
	if a.Port == 0 {
		return ""
	}
	s := fmt.Sprintf("%d", a.Port)
	if len(a.Alt) > 0 {
		alts := make([]string, len(a.Alt))
		for i, p := range a.Alt {
			alts[i] = fmt.Sprintf("%d", p)
		}
		s += " (alt: " + strings.Join(alts, ", ") + ")"
	}
	return s
}

func web(id, name, file, scheme string, port int, alt []int, path string, aliases ...string) App {
	return App{ID: id, Name: name, File: file, Web: true, Scheme: scheme, Port: port, Alt: alt, Path: path, Aliases: aliases}
}

func noweb(id, name, file string, port int, aliases ...string) App {
	return App{ID: id, Name: name, File: file, Port: port, Aliases: aliases}
}

// apps is sorted by name at start-up (see init), so the entries below can be
// added in any order. When two aliases of equal length match, the first app in
// that sorted order wins.
var apps = []App{
	web("home-assistant", "Home Assistant", "home-assistant.svg", "http", 8123, nil, "", "homeassistant", "haos", "hassio", "hass"),
	web("zigbee2mqtt", "Zigbee2MQTT", "zigbee2mqtt.svg", "http", 8080, nil, "", "zigbee2mqtt", "z2m"),
	web("esphome", "ESPHome", "esphome.svg", "http", 6052, nil, "", "esphome"),
	web("emqx", "EMQX", "emqx.svg", "http", 18083, nil, "", "emqx"),
	noweb("mqtt", "MQTT (broker)", "mqtt.svg", 1883, "mosquitto", "mqttbroker", "mqtt"),
	web("mqtt-explorer", "MQTT Explorer", "mqtt.svg", "http", 4000, nil, "", "mqttexplorer"),
	web("proxmox", "Proxmox", "proxmox.svg", "https", 8006, nil, "", "proxmox", "pve"),
	web("portainer", "Portainer", "portainer.svg", "https", 9443, []int{9000}, "", "portainer"),
	web("omada", "Omada Controller", "omada.svg", "https", 8043, []int{8088}, "", "omada"),
	web("code-server", "Code Server", "code-server.png", "http", 8443, []int{8080}, "", "codeserver", "vscode"),
	web("node-red", "Node-RED", "node-red.svg", "http", 1880, nil, "", "nodered"),
	web("grafana", "Grafana", "grafana.svg", "http", 3000, nil, "", "grafana", "graffana"),
	web("openmediavault", "OpenMediaVault", "openmediavault.svg", "http", 80, nil, "", "openmediavault", "omv"),
	web("frigate", "Frigate", "frigate.svg", "https", 8971, []int{5000}, "", "frigate"),
	web("matter", "Matter Server", "matter.svg", "http", 5580, nil, "", "matterserver", "matter"),
	web("influxdb", "InfluxDB", "influxdb.svg", "http", 8086, nil, "", "influxdb", "influx"),
	web("immich", "Immich", "immich.svg", "http", 2283, nil, "", "immich"),
	web("jellyfin", "Jellyfin", "jellyfin.svg", "http", 8096, nil, "", "jellyfin"),
	web("music-assistant", "Music Assistant", "music-assistant.svg", "http", 8095, nil, "", "musicassistant"),
	web("qbittorrent", "qBittorrent", "qbittorrent.svg", "http", 8080, nil, "", "qbittorrent", "qbit"),
	web("openspeedtest", "OpenSpeedTest", "openspeedtest.svg", "http", 3000, []int{3001}, "", "openspeedtest", "speedtest"),
	web("filebrowser", "FileBrowser", "filebrowser.svg", "http", 8080, []int{80}, "", "filebrowser"),
	web("openwrt", "OpenWrt", "openwrt.svg", "http", 80, nil, "", "openwrt"),
	web("unifi", "UniFi OS", "unifi.svg", "https", 443, []int{8443}, "", "unifi"),
	web("pi-hole", "Pi-hole", "pi-hole.svg", "http", 80, nil, "/admin", "pihole"),
	web("adguard-home", "AdGuard Home", "adguard-home.svg", "http", 3000, []int{80}, "", "adguard"),
	web("go2rtc", "go2rtc", "go2rtc.png", "http", 1984, nil, "", "go2rtc"),
	web("wireguard", "WireGuard (wg-easy)", "wireguard.svg", "http", 51821, nil, "", "wgeasy", "wireguard"),
	noweb("tailscale", "Tailscale", "tailscale.svg", 0, "tailscale"),
	noweb("cloudflared", "Cloudflared", "cloudflared.svg", 0, "cloudflared"),
	noweb("cloudflare-ddns", "Cloudflare DDNS", "cloudflare.svg", 0, "cloudflareddns", "ddnsupdater", "cfddns"),
	noweb("docker", "Docker", "docker.svg", 0, "docker"),
	noweb("podman", "Podman", "podman.svg", 0, "podman"),
	noweb("debian", "Debian", "debian.svg", 0, "debian"),
	noweb("ubuntu", "Ubuntu", "ubuntu.svg", 0, "ubuntu"),
}

func init() {
	sort.SliceStable(apps, func(i, j int) bool {
		return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
	})
}

var byID = func() map[string]App {
	m := make(map[string]App, len(apps))
	for _, a := range apps {
		m[a.ID] = a
	}
	return m
}()

// All returns the catalog sorted by name (a copy).
func All() []App { return append([]App(nil), apps...) }

// Get looks an application up by ID.
func Get(id string) (App, bool) { a, ok := byID[id]; return a, ok }

// FromIcon resolves a service icon value like "app:home-assistant".
func FromIcon(icon string) (App, bool) {
	if !strings.HasPrefix(icon, IconPrefix) {
		return App{}, false
	}
	return Get(strings.TrimPrefix(icon, IconPrefix))
}

func squash(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Match guesses the application from a guest, container or image name. The
// longest matching alias wins, so "zigbee2mqtt" is not mistaken for "mqtt".
func Match(name string) (App, bool) {
	n := squash(name)
	if n == "" {
		return App{}, false
	}
	var best App
	bestLen := 0
	for _, a := range apps {
		for _, al := range a.Aliases {
			if len(al) >= 3 && len(al) > bestLen && strings.Contains(n, al) {
				best, bestLen = a, len(al)
			}
		}
	}
	return best, bestLen > 0
}
