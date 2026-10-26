package web

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/andregoncalvespires/homeview-dash/internal/catalog"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

// Links is the result of choosing which URL of a service to emphasise.
type Links struct {
	Primary   string // opened by clicking the card
	Secondary string // the internal URL, offered as a small "Internal" tag
}

// ChooseLinks picks what a card links to: the external URL when there is one
// (it works from anywhere), otherwise the internal URL. When both exist the
// internal one stays reachable through the secondary tag.
func ChooseLinks(s store.Service) Links {
	in, ex := s.InternalURL, s.ExternalURL
	switch {
	case ex != "" && in != "":
		return Links{Primary: ex, Secondary: in}
	case ex != "":
		return Links{Primary: ex}
	default:
		return Links{Primary: in}
	}
}

// validHTTPURL accepts only absolute http(s) URLs with a host.
func validHTTPURL(raw string) bool {
	if len(raw) > 2048 || !utf8.ValidString(raw) || strings.ContainsAny(raw, " \t\r\n") {
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
	if !utf8.ValidString(icon) {
		return false
	}
	if strings.HasPrefix(icon, catalog.IconPrefix) {
		_, ok := catalog.FromIcon(icon)
		return ok
	}
	if strings.HasPrefix(icon, uploadIconPrefix) {
		_, ok := uploadIconID(icon) // that the image exists is checked where the store is at hand
		return ok
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
