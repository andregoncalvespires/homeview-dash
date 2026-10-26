package catalog

import (
	"strings"
	"testing"
)

func TestCatalogIntegrity(t *testing.T) {
	seen := map[string]bool{}
	if len(apps) < 35 {
		t.Errorf("catalog has %d apps, expected at least the 35 expected", len(apps))
	}
	for _, a := range apps {
		if a.ID == "" || a.Name == "" || a.File == "" || seen[a.ID] {
			t.Errorf("bad or duplicate entry: %+v", a)
		}
		seen[a.ID] = true
		if strings.ToLower(a.ID) != a.ID || strings.ContainsAny(a.ID, " :/") {
			t.Errorf("ID %q must be a lower-case slug", a.ID)
		}
		if a.Web && (a.Scheme != "http" && a.Scheme != "https" || a.Port <= 0 || a.Port > 65535) {
			t.Errorf("%s: bad web endpoint %s:%d", a.ID, a.Scheme, a.Port)
		}
		if !a.Web && a.Scheme != "" {
			t.Errorf("%s: non-web app with a scheme", a.ID)
		}
		for _, al := range a.Aliases {
			if al != squash(al) {
				t.Errorf("%s: alias %q must be lower case without punctuation", a.ID, al)
			}
		}
	}
}

func TestMatch(t *testing.T) {
	cases := map[string]string{
		"homeassistant":                    "home-assistant",
		"HAOS":                             "home-assistant",
		"Home Assistant OS 14":             "home-assistant",
		"zigbee2mqtt":                      "zigbee2mqtt", // not plain mqtt
		"eclipse-mosquitto":                "mqtt",
		"emqx/emqx:5":                      "emqx",
		"node-red":                         "node-red",
		"ghcr.io/immich-app/immich-server": "immich",
		"pihole":                           "pi-hole",
		"adguardhome":                      "adguard-home",
		"OMV":                              "openmediavault",
		"cloudflared":                      "cloudflared",
		"frigate-nvr":                      "frigate",
		"docker":                           "docker",
		"influxdb2":                        "influxdb",
		"matter-server":                    "matter",
		"lxc-debian12":                     "debian",
	}
	for in, want := range cases {
		got, ok := Match(in)
		if !ok || got.ID != want {
			t.Errorf("Match(%q) = %q, %v; want %q", in, got.ID, ok, want)
		}
	}
	for _, in := range []string{"", "scratch", "backup", "vm-100", "pi"} {
		if a, ok := Match(in); ok {
			t.Errorf("Match(%q) unexpectedly matched %s", in, a.ID)
		}
	}
}

func TestSuggestedURL(t *testing.T) {
	cases := []struct {
		id, host, want string
	}{
		{"home-assistant", "192.168.1.10", "http://192.168.1.10:8123"},
		{"portainer", "10.0.0.5", "https://10.0.0.5:9443"},
		{"code-server", "10.0.0.5", "http://10.0.0.5:8443"},
		{"matter", "10.0.0.5", "http://10.0.0.5:5580"},
		{"influxdb", "10.0.0.5", "http://10.0.0.5:8086"},
		{"proxmox", "pve.lan", "https://pve.lan:8006"},
		{"pi-hole", "10.0.0.2", "http://10.0.0.2/admin"},
		{"openwrt", "192.168.1.1", "http://192.168.1.1"},
		{"unifi", "10.0.0.3", "https://10.0.0.3"},
		{"home-assistant", "fd00::1", "http://[fd00::1]:8123"},
		{"mqtt", "10.0.0.4", ""},   // broker: no web UI
		{"docker", "10.0.0.4", ""}, // not a web app
		{"home-assistant", "", ""},
	}
	for _, c := range cases {
		a, ok := Get(c.id)
		if !ok {
			t.Fatalf("missing app %s", c.id)
		}
		if got := a.SuggestedURL(c.host); got != c.want {
			t.Errorf("%s @ %q: got %q, want %q", c.id, c.host, got, c.want)
		}
	}
}

func TestIconRefs(t *testing.T) {
	a, _ := Get("grafana")
	if a.IconRef() != "app:grafana" || a.IconURL() != "/static/icons/grafana.svg" {
		t.Errorf("%q %q", a.IconRef(), a.IconURL())
	}
	if got, ok := FromIcon("app:grafana"); !ok || got.ID != "grafana" {
		t.Error("FromIcon failed")
	}
	for _, bad := range []string{"", "🏠", "app:", "app:nope", "grafana", "http://x/y.png"} {
		if _, ok := FromIcon(bad); ok {
			t.Errorf("FromIcon(%q) matched", bad)
		}
	}
	if (App{Port: 8080, Alt: []int{80, 8081}}).PortLabel() != "8080 (alt: 80, 8081)" || (App{}).PortLabel() != "" {
		t.Error("PortLabel")
	}
}

func TestSortedByName(t *testing.T) {
	all := All()
	for i := 1; i < len(all); i++ {
		if strings.ToLower(all[i-1].Name) > strings.ToLower(all[i].Name) {
			t.Errorf("%q comes before %q", all[i-1].Name, all[i].Name)
		}
	}
}
