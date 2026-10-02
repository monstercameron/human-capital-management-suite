package application

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

// PersonaReplyOutputPolicy contains trusted, server-configured link origins.
// An empty tenant origin uses same-workspace relative document links.
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

// renderPersonaReplyWithAgentDocuments appends server-owned hub citations and
// omission notices before applying the ordinary persona output sanitizer. A
// blank omission label is deliberately collapsed into a generic count so an
// unreadable or missing document cannot be identified to the invoker.
func renderPersonaReplyWithAgentDocuments(body, tenantID, conversationID string, policy PersonaReplyOutputPolicy, documents []agentdocref.ResolvedDocument, omissions []agentdocref.Omission, citationDetails ...PersonaReplyCitationDetail) string {
	var sources, additions []string
	for _, document := range documents {
		if strings.TrimSpace(document.Title) == "" {
			continue
		}
		section := strings.TrimSpace(document.Reference.SectionAnchor)
		detail := personaReplyCitationDetail(document, citationDetails)
		// A document found by search is cited by title; only a pinned or
		// resolved reference knows its version number.
		label := agentDocumentMarkdownText(document.Title)
		var details []string
		if section != "" {
			sectionTitle := detail.SectionTitle
			if sectionTitle == "" {
				sectionTitle = section
			}
			details = append(details, agentDocumentMarkdownText(sectionTitle))
		}
		if document.Version != 0 {
			details = append(details, displayVersion(document.Version))
		}
		if len(details) > 0 {
			label += " · " + strings.Join(details, " · ")
		}
		if strings.TrimSpace(document.Reference.DocumentID) == "" {
			sources = append(sources, "- "+label)
			continue
		}
		origin := strings.TrimRight(policy.TenantOrigin, "/")
		target := origin + "/workspace/app/docs?document=" + url.QueryEscape(document.Reference.DocumentID)
		if detail.VersionID != "" {
			target += "&version=" + url.QueryEscape(detail.VersionID)
		}
		if section != "" {
			target += "#" + url.PathEscape(section)
		}
		sources = append(sources, "- ["+label+"]("+target+")")
		index := detail.Index
		if index == 0 {
			index = len(sources)
		}
		body = strings.ReplaceAll(body, "[["+strconv.Itoa(index)+"]]", "["+label+"]("+target+")")
		if section != "" {
			body = strings.ReplaceAll(body, "[["+strconv.Itoa(index)+":"+section+"]]", "["+label+"]("+target+")")
		}
		body = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), document.Title+":"))
		// Replace older title/version parentheticals only using a server-owned
		// citation. Stored answer text is never rewritten.
		legacy := regexp.MustCompile(`\(` + regexp.QuoteMeta(document.Title) + `, (?:version [0-9]+|v[0-9]+\.[0-9]+\.[0-9]+)(?:, [^)]*)?\)`)
		body = legacy.ReplaceAllStringFunc(body, func(string) string { return "[" + label + "](" + target + ")" })
	}
	// CHATBUG-018: once the citations are links, an answer that is nothing
	// but a title or a link to the document is not delivered.
	titles := make([]string, 0, 2*len(documents))
	for _, document := range documents {
		titles = append(titles, document.Title)
	}
	for _, detail := range citationDetails {
		titles = append(titles, detail.SectionTitle)
	}
	if !personaReplyHasStatement(body, titles...) {
		return ""
	}
	unknown := 0
	for _, omission := range omissions {
		if strings.TrimSpace(omission.Label) == "" {
			unknown++
			continue
		}
		reason := "was not used"
		switch omission.Reason {
		case agentdocref.NotPublished:
			reason = "has no published version"
		case agentdocref.OverBudget:
			reason = "was not fully used because the reference limit was reached"
		case agentdocref.NotReadable, agentdocref.NotFound:
			reason = "could not be read"
		}
		additions = append(additions, "Reference document \""+agentDocumentNoticeText(omission.Label)+"\" "+reason+".")
	}
	if unknown > 0 {
		additions = append(additions, strconv.Itoa(unknown)+" reference document(s) could not be read and were not used.")
	}
	if len(sources) > 0 {
		additions = append([]string{"Sources\n" + strings.Join(sources, "\n")}, additions...)
	}
	if len(additions) > 0 {
		body = strings.TrimSpace(body) + "\n\n" + strings.Join(additions, "\n")
	}
	return renderPersonaReplyText(body, tenantID, conversationID, policy)
}

type PersonaReplyCitationDetail struct {
	DocumentID, VersionID, SectionTitle, SectionAnchor string
	Index                                              int
}

func personaReplyCitationDetail(document agentdocref.ResolvedDocument, details []PersonaReplyCitationDetail) PersonaReplyCitationDetail {
	for _, detail := range details {
		if detail.DocumentID == document.Reference.DocumentID && (detail.SectionAnchor == "" || detail.SectionAnchor == document.Reference.SectionAnchor) {
			return detail
		}
	}
	return PersonaReplyCitationDetail{}
}

func agentDocumentMarkdownText(value string) string {
	value = agentDocumentNoticeText(value)
	replacer := strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`)
	return replacer.Replace(value)
}

func agentDocumentNoticeText(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, `"`, `'`)
	return strings.TrimSpace(value)
}

func personaReplyLinkAllowed(conversation agentdeliver.Conversation, raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && !parsed.IsAbs() && parsed.Host == "" && parsed.User == nil && parsed.Path == "/workspace/app/docs" && parsed.Query().Get("document") != "" && !strings.HasPrefix(raw, "//") {
		return true
	}
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
