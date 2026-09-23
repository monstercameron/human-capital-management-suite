package chatui

import (
	"net/url"
	"regexp"
	"strings"
)

// LinkEmbed is a viewer-authorized preview. Body is held in client memory
// only; the destination post persists just the locator the person pasted.
type LinkEmbed struct {
	Token, SourceRoom, SourcePost    string
	Channel, Author, Body, TimeLabel string
	AttachmentCount                  int
	State                            string // loading, ready, unavailable
}

type ShareLocator struct{ Token, LegacyPost string }

var shareURLPattern = regexp.MustCompile(`(?:https?://[^\s<>"']+|/(?:workspace/app/chat#share=|chat/share/)[A-Za-z0-9_-]+|#msg=[A-Za-z0-9_-]+)`)
var shareTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validShareToken(token string) bool {
	return len(token) > 0 && len(token) <= 2048 && shareTokenPattern.MatchString(token)
}

// ShareLocators recognizes only same-origin app links and server-issued
// locator paths. It never interprets a foreign URL or an arbitrary fragment.
func ShareLocators(body, origin string) []ShareLocator {
	if len(body) > 32*1024 {
		body = body[:32*1024]
	}
	base, err := url.Parse(origin)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil
	}
	seen := map[string]bool{}
	out := make([]ShareLocator, 0, 3)
	for _, match := range shareURLPattern.FindAllString(body, -1) {
		match = strings.TrimRight(match, ".,!?;:)]}>")
		if strings.HasPrefix(match, "#msg=") {
			id := strings.TrimPrefix(match, "#msg=")
			if validShareToken(id) && !seen["legacy:"+id] {
				out = append(out, ShareLocator{LegacyPost: id})
				seen["legacy:"+id] = true
			}
		} else {
			parsed, e := url.Parse(match)
			if e != nil {
				continue
			}
			if parsed.IsAbs() && (parsed.Scheme != base.Scheme || !strings.EqualFold(parsed.Host, base.Host)) {
				continue
			}
			token := ""
			switch {
			case parsed.Path == "/workspace/app/chat" && strings.HasPrefix(parsed.Fragment, "share="):
				token = strings.TrimPrefix(parsed.Fragment, "share=")
			case parsed.Path == "/workspace/app/chat" && strings.HasPrefix(parsed.Fragment, "msg="):
				id := strings.TrimPrefix(parsed.Fragment, "msg=")
				if validShareToken(id) && !seen["legacy:"+id] {
					out = append(out, ShareLocator{LegacyPost: id})
					seen["legacy:"+id] = true
				}
			case strings.HasPrefix(parsed.Path, "/chat/share/") && parsed.Fragment == "" && parsed.RawQuery == "":
				token = strings.TrimPrefix(parsed.Path, "/chat/share/")
			}
			if validShareToken(token) && !seen[token] {
				out = append(out, ShareLocator{Token: token})
				seen[token] = true
			}
		}
		if len(out) == 3 {
			break
		}
	}
	return out
}
