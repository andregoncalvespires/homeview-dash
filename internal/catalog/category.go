package catalog

// Category ids, in the order they are shown.
const (
	CatAutomation = "automation"
	CatNetwork    = "network"
	CatMedia      = "media"
	CatStorage    = "storage"
	CatMonitoring = "monitoring"
	CatInfra      = "infra"
	CatOther      = "other"
)

// Categories lists every category id in display order.
var Categories = []string{CatAutomation, CatNetwork, CatMedia, CatStorage, CatMonitoring, CatInfra, CatOther}

var categoryOf = map[string]string{
	"home-assistant": CatAutomation, "zigbee2mqtt": CatAutomation, "esphome": CatAutomation, "node-red": CatAutomation,
	"matter": CatAutomation, "mqtt": CatAutomation, "emqx": CatAutomation, "mqtt-explorer": CatAutomation,
	"frigate": CatAutomation, "go2rtc": CatAutomation,
	"omada": CatNetwork, "openwrt": CatNetwork, "unifi": CatNetwork, "pi-hole": CatNetwork, "adguard-home": CatNetwork,
	"wireguard": CatNetwork, "tailscale": CatNetwork, "cloudflared": CatNetwork, "cloudflare-ddns": CatNetwork, "openspeedtest": CatNetwork,
	"immich": CatMedia, "jellyfin": CatMedia, "music-assistant": CatMedia, "qbittorrent": CatMedia,
	"openmediavault": CatStorage, "filebrowser": CatStorage,
	"grafana": CatMonitoring, "influxdb": CatMonitoring,
	"proxmox": CatInfra, "portainer": CatInfra, "docker": CatInfra, "podman": CatInfra, "debian": CatInfra, "ubuntu": CatInfra, "code-server": CatInfra,
}

// Category returns the category of a catalog application id ("other" when unknown).
func Category(id string) string {
	if c, ok := categoryOf[id]; ok {
		return c
	}
	return CatOther
}
