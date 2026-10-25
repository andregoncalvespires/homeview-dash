package web

import (
	"net"
	"net/netip"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

// Links is the result of choosing which URL of a service to emphasise.
type Links struct {
	Primary           string
	Secondary         string
	PrimaryIsExternal bool
}

// ChooseLinks picks the primary link. When the dashboard is opened from the
// local network the internal URL is primary, otherwise the external one. If a
// service only has one URL, that one is always primary and there is no
// secondary link.
func ChooseLinks(s store.Service, internalContext bool) Links {
	in, ex := s.InternalURL, s.ExternalURL
	switch {
	case in == "" && ex == "":
		return Links{}
	case in == "":
		return Links{Primary: ex, PrimaryIsExternal: true}
	case ex == "":
		return Links{Primary: in}
	case internalContext:
		return Links{Primary: in, Secondary: ex}
	default:
		return Links{Primary: ex, Secondary: in, PrimaryIsExternal: true}
	}
}

// IsInternalHost reports whether a request Host header (with or without port)
// looks like local-network access: loopback/private/CGNAT addresses, single
// label names, common local suffixes, or anything listed in extra.
func IsInternalHost(hostport string, extra []string) bool {
	host := strings.ToLower(strings.TrimSpace(hostport))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	for _, e := range extra {
		if host == e {
			return true
		}
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
	}
	if !strings.Contains(host, ".") { // "homeview", "localhost"
		return true
	}
	for _, suf := range []string{".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain", ".localhost"} {
		if strings.HasSuffix(host, suf) {
			return true
		}
	}
	return false
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10") // Tailscale and carrier-grade NAT

// validHTTPURL accepts only absolute http(s) URLs with a host.
func validHTTPURL(raw string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, " \t\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

// validIcon accepts an empty value, a short emoji/text, or an http(s) URL.
func validIcon(icon string) bool {
	if icon == "" {
		return true
	}
	low := strings.ToLower(icon)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		return validHTTPURL(icon)
	}
	if strings.ContainsAny(icon, "<>&\"'`\\") || utf8.RuneCountInString(icon) > 16 {
		return false
	}
	for _, r := range icon {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return !strings.Contains(icon, "://") && !strings.HasPrefix(icon, "//")
}

// deviceLabel builds a short human label such as "Firefox · Linux".
func deviceLabel(ua string) string {
	browser := "Browser"
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/") || strings.Contains(ua, "Opera"):
		browser = "Opera"
	case strings.Contains(ua, "Firefox/") || strings.Contains(ua, "FxiOS/"):
		browser = "Firefox"
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	osName := ""
	switch {
	case strings.Contains(ua, "Android"):
		osName = "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		osName = "iOS"
	case strings.Contains(ua, "Windows"):
		osName = "Windows"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		osName = "macOS"
	case strings.Contains(ua, "CrOS"):
		osName = "ChromeOS"
	case strings.Contains(ua, "Linux"):
		osName = "Linux"
	}
	if osName == "" {
		return browser
	}
	return browser + " · " + osName
}
