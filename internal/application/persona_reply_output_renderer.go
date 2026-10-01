package application

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
)

// PersonaReplyOutputPolicy contains trusted, server-configured link origins.
// An empty tenant origin disables links while leaving plain text available.
type PersonaReplyOutputPolicy struct {
	TenantOrigin        string
	AdminAllowedOrigins []string
}

// renderPersonaReplyText keeps plain text and links approved by the existing
// agentdeliver origin policy. Images, remote embeds, raw URLs, and unsafe link
// destinations are removed before the chat Markdown and GIPHY renderers see it.
func renderPersonaReplyText(body, tenantID, conversationID string, policy PersonaReplyOutputPolicy) string {
	body = strings.TrimSpace(body)
	if body == "" || strings.ContainsRune(body, '\x00') {
		return ""
	}
	conversation := agentdeliver.Conversation{
		TenantID: tenantID, ConversationID: conversationID,
		TenantOrigin:        policy.TenantOrigin,
		AdminAllowedOrigins: append([]string(nil), policy.AdminAllowedOrigins...),
	}
	imagePattern := regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	body = imagePattern.ReplaceAllString(body, "")
	linkPattern := regexp.MustCompile(`\[([^\]]+)\]\(([^)]*)\)`)
	protected := make([]string, 0)
	markerBase := "DFPERSONALINK"
	for strings.Contains(body, markerBase) {
		markerBase += "X"
	}
	body = linkPattern.ReplaceAllStringFunc(body, func(markup string) string {
		parts := linkPattern.FindStringSubmatch(markup)
		if len(parts) != 3 || !personaReplyLinkAllowed(conversation, parts[2]) || isGiphyOrigin(parts[2]) {
			if len(parts) > 1 {
				return parts[1]
			}
			return ""
		}
		marker := markerBase + strconv.Itoa(len(protected)) + "TOKEN"
		protected = append(protected, markup)
		return marker
	})
	unsafeURL := regexp.MustCompile(`(?i)(?:https?://|www\.|//)[^\s<>()]+`)
	body = unsafeURL.ReplaceAllStringFunc(body, func(candidate string) string {
		if isGiphyOrigin(candidate) {
			return ""
		}
		return ""
	})
	for i, markup := range protected {
		body = strings.ReplaceAll(body, markerBase+strconv.Itoa(i)+"TOKEN", markup)
	}
	return strings.TrimSpace(body)
}

func personaReplyLinkAllowed(conversation agentdeliver.Conversation, raw string) bool {
	result, err := agentdeliver.Render(conversation, agentdeliver.Result{Items: []agentdeliver.ResultItem{{
		ID: "link", Links: []agentdeliver.Link{{URL: strings.TrimSpace(raw), Label: "link"}},
	}}})
	return err == nil && result.Body != ""
}

func isGiphyOrigin(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	return host == "giphy.com" || strings.HasSuffix(host, ".giphy.com")
}
