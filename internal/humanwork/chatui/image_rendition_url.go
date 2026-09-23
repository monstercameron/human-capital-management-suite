package chatui

import (
	"net/url"
	"strings"
)

// validChatRenditionURL accepts only the protected chat-media endpoint and
// the requested rendition. It prevents malformed DOM attributes from
// turning the image loader into a cross-origin fetch surface.
func validChatRenditionURL(raw, variant string) bool {
	if variant != "thumbnail" && variant != "display" {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.IsAbs() || u.Host != "" || u.Scheme != "" {
		return false
	}
	if !strings.HasPrefix(u.Path, "/v1/chat/media/") || strings.Contains(u.Path, "..") {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(u.Path, "/v1/chat/media/"), "/")
	if len(segments) != 1 || segments[0] == "" {
		return false
	}
	q := u.Query()
	return q.Get("grant") != "" && len(q["grant"]) == 1 && q.Get("variant") == variant && len(q["variant"]) == 1
}

// chatMediaVariantURL preserves the protected artifact URL and adds only the
// requested rendition. The original URL remains reserved for download flows.
func chatMediaVariantURL(raw, variant string) (string, bool) {
	if variant != "thumbnail" && variant != "display" {
		return "", false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.IsAbs() || u.Host != "" || u.Scheme != "" {
		return "", false
	}
	if !strings.HasPrefix(u.Path, "/v1/chat/media/") || strings.Contains(u.Path, "..") {
		return "", false
	}
	segments := strings.Split(strings.TrimPrefix(u.Path, "/v1/chat/media/"), "/")
	if len(segments) != 1 || segments[0] == "" {
		return "", false
	}
	q := u.Query()
	if q.Get("grant") == "" || len(q["grant"]) != 1 {
		return "", false
	}
	q.Set("variant", variant)
	u.RawQuery = q.Encode()
	return u.String(), true
}

// chatMediaOriginalURL removes any rendition selector while preserving the
// protected same-origin path and its single access grant.
func chatMediaOriginalURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.IsAbs() || u.Host != "" || u.Scheme != "" {
		return "", false
	}
	if !strings.HasPrefix(u.Path, "/v1/chat/media/") || strings.Contains(u.Path, "..") {
		return "", false
	}
	segments := strings.Split(strings.TrimPrefix(u.Path, "/v1/chat/media/"), "/")
	if len(segments) != 1 || segments[0] == "" {
		return "", false
	}
	q := u.Query()
	if q.Get("grant") == "" || len(q["grant"]) != 1 || len(q["variant"]) > 1 {
		return "", false
	}
	if variant := q.Get("variant"); variant != "" && variant != "thumbnail" && variant != "display" {
		return "", false
	}
	q.Del("variant")
	u.RawQuery = q.Encode()
	return u.String(), true
}

const chatImageViewerMemoryBudget int64 = 64 << 20

// chatImageOriginalFitsViewerBounds includes the source bytes and two decoded
// surfaces (the visible display rendition plus the original being decoded).
// Unknown metadata fails closed to the protected download link.
func chatImageOriginalFitsViewerBounds(width, height int, sourceBytes int64) bool {
	if width <= 0 || height <= 0 || sourceBytes <= 0 || sourceBytes > chatImageViewerMemoryBudget {
		return false
	}
	pixels := int64(width) * int64(height)
	if pixels > chatImageViewerMemoryBudget/8 {
		return false
	}
	estimated := sourceBytes + pixels*8 + (8 << 20) // reserve thumbnail/display Blob and browser overhead
	return estimated <= chatImageViewerMemoryBudget
}
