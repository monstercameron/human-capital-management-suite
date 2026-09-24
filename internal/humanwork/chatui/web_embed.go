package chatui

import (
	"errors"
	"net/url"
	"strings"
)

var ErrWebEmbedDenied = errors.New("chatui: web embed denied")

// WebEmbedDescriptor is presentation input returned only after the caller has
// obtained an origin-bound chatmedia grant. It contains no HCM data or bearer
// credential. The browser still treats every field as untrusted.
type WebEmbedDescriptor struct {
	URL, Origin, Grant, Nonce, Title string
}

// WebEmbedBridgeMessage is the small, versioned message surface an embed may
// request from its parent. Resize is the only supported action; navigation,
// data access and chat commands are deliberately absent.
type WebEmbedBridgeMessage struct {
	Version int
	Grant   string
	Nonce   string
	Action  string
	Height  int
}

// ValidateWebEmbedDescriptor checks that the approved origin exactly matches
// the HTTPS URL. The origin and grant must come from the authorization result,
// not from message text or a caller-selected host.
func ValidateWebEmbedDescriptor(embed WebEmbedDescriptor) error {
	if strings.TrimSpace(embed.Grant) == "" || len(embed.Nonce) < 16 || strings.TrimSpace(embed.Title) == "" {
		return ErrWebEmbedDenied
	}
	u, err := url.Parse(embed.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return ErrWebEmbedDenied
	}
	urlOrigin, ok := canonicalEmbedOrigin(u)
	if !ok || u.Opaque != "" {
		return ErrWebEmbedDenied
	}
	approved, err := url.Parse(embed.Origin)
	if err != nil || approved.Path != "" || approved.RawQuery != "" || approved.Fragment != "" || approved.User != nil {
		return ErrWebEmbedDenied
	}
	approvedOrigin, ok := canonicalEmbedOrigin(approved)
	if !ok || approvedOrigin != urlOrigin {
		return ErrWebEmbedDenied
	}
	return nil
}

func canonicalEmbedOrigin(u *url.URL) (string, bool) {
	if u == nil || u.Scheme != "https" || u.Host == "" {
		return "", false
	}
	host := u.Hostname()
	if host == "" || strings.HasSuffix(host, ".") || strings.ContainsAny(host, "% \t\r\n") {
		return "", false
	}
	for i := 0; i < len(host); i++ {
		if host[i] >= 0x80 {
			return "", false
		}
	}
	port := u.Port()
	if port != "" && port != "443" {
		return "", false
	}
	if strings.Contains(host, ":") {
		host = "[" + strings.ToLower(host) + "]"
	} else {
		host = strings.ToLower(host)
	}
	return "https://" + host, true
}

// ValidateWebEmbedBridge accepts only the bounded, typed resize message for
// the currently authorized frame. The WASM listener additionally checks the
// browser-supplied source window and exact event origin before calling this.
func ValidateWebEmbedBridge(embed WebEmbedDescriptor, message WebEmbedBridgeMessage) (int, error) {
	if err := ValidateWebEmbedDescriptor(embed); err != nil {
		return 0, err
	}
	if message.Version != 1 || message.Grant != embed.Grant || message.Nonce != embed.Nonce || message.Action != "resize" || message.Height < 80 || message.Height > 600 {
		return 0, ErrWebEmbedDenied
	}
	return message.Height, nil
}
