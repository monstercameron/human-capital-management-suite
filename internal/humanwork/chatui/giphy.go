package chatui

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
)

const (
	GiphyPageSize = 20
	GiphyRating   = "g"
	GiphyMaxQuery = 50
)

var giphyIDPattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)
var giphyLinkPattern = regexp.MustCompile(`https://(?:www\.)?giphy\.com/gifs/[^\s<>"']+`)

// GiphyResult is the small, transient projection needed by the GIF picker.
// PreviewURL is a rendition URL returned by GIPHY and must never be persisted.
type GiphyResult struct {
	ID, URL, EmbedURL, PreviewURL, ThumbnailURL, Alt, Rating string
}

// GiphyLink is a canonical page link and its independently validated ID.
type GiphyLink struct {
	ID, URL string
}

// ValidGiphyID accepts only the opaque alphanumeric IDs used by GIPHY.
func ValidGiphyID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && giphyIDPattern.MatchString(id)
}

// GiphyConfigured reports whether browser requests have an explicit key.
func GiphyConfigured(config GiphyPickerConfig) bool {
	return strings.TrimSpace(config.APIKey) != ""
}

// LimitGiphyQuery preserves Unicode text while respecting GIPHY's 50-rune
// search query bound.
func LimitGiphyQuery(query string) string {
	runes := []rune(query)
	if len(runes) > GiphyMaxQuery {
		return string(runes[:GiphyMaxQuery])
	}
	return query
}

// UniqueGiphyIDs keeps valid IDs in caller order and within one API page.
func UniqueGiphyIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	result := make([]string, 0, min(len(ids), GiphyPageSize))
	for _, id := range ids {
		if !ValidGiphyID(id) || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
		if len(result) == GiphyPageSize {
			break
		}
	}
	return result
}

// GiphyLinks extracts distinct canonical GIPHY GIF page links in message
// order. It never treats a CDN URL or an arbitrary slug as a display grant.
func GiphyLinks(body string) []GiphyLink {
	if len(body) > 32*1024 {
		body = body[:32*1024]
	}
	seen := make(map[string]bool)
	links := make([]GiphyLink, 0, 2)
	for _, match := range giphyLinkPattern.FindAllString(body, GiphyPageSize) {
		match = strings.TrimRight(match, ".,!?:;)]}>")
		canonical, id, ok := CanonicalGiphyURL(match)
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		links = append(links, GiphyLink{ID: id, URL: canonical})
		if len(links) == GiphyPageSize {
			break
		}
	}
	return links
}

// InsertGiphyLinkAtUTF16 inserts a provider link at browser textarea offsets
// while keeping it separated from neighboring words.
func InsertGiphyLinkAtUTF16(text, link string, start, end int) (string, int) {
	units := utf16.Encode([]rune(text))
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(units) {
		start = len(units)
	}
	if end > len(units) {
		end = len(units)
	}
	insert := link
	if start > 0 && !unicode.IsSpace(rune(units[start-1])) {
		insert = " " + insert
	}
	if end < len(units) && !unicode.IsSpace(rune(units[end])) {
		insert += " "
	}
	return insertEmojiAtUTF16(text, insert, start, end)
}

// CanonicalGiphyURL validates a GIPHY GIF page URL and returns its stable ID.
// It preserves the provider URL for draft insertion rather than synthesizing
// a URL that could differ from the one returned by the API.
func CanonicalGiphyURL(raw string) (string, string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", "", false
	}
	host := strings.ToLower(u.Hostname())
	if host != "giphy.com" && host != "www.giphy.com" {
		return "", "", false
	}
	if !strings.HasPrefix(u.Path, "/gifs/") {
		return "", "", false
	}
	pathPart := strings.Trim(strings.TrimPrefix(u.Path, "/gifs/"), "/")
	if pathPart == "" || strings.Contains(pathPart, "/") {
		return "", "", false
	}
	parts := strings.Split(pathPart, "-")
	id := parts[len(parts)-1]
	if !ValidGiphyID(id) {
		return "", "", false
	}
	return u.String(), id, true
}

// validGiphyMediaURL limits transient image loads to HTTPS GIPHY media hosts.
func validGiphyMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "media.giphy.com" {
		return true
	}
	if !strings.HasPrefix(host, "media") || !strings.HasSuffix(host, ".giphy.com") {
		return false
	}
	shard := strings.TrimSuffix(strings.TrimPrefix(host, "media"), ".giphy.com")
	if shard == "" {
		return false
	}
	for _, digit := range shard {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
