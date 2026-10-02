package chat

import (
	"context"
	"net/url"
	"regexp"
	"strings"
)

// AgentDocumentSource is a read projection, never an authority saved in a post.
type AgentDocumentSource struct {
	Title, DocumentID, VersionID, SectionAnchor, SectionTitle string
	Href                                                      string
	Readable                                                  bool
}

// AgentSourceAccess resolves current document access as the authenticated
// reader. A title-only legacy source must resolve to exactly one placement.
type AgentSourceAccess interface {
	ResolveAgentDocumentSource(context.Context, Principal, string, string, AgentDocumentSource) (AgentDocumentSource, error)
}

func (s *Service) SetAgentSourceAccess(access AgentSourceAccess) { s.agentSourceAccess = access }

func (s *Service) projectAgentSources(ctx context.Context, reader Principal, tenant, conversation, body string) string {
	const prefix = "\n\nSources\n"
	start := strings.LastIndex(body, prefix)
	if start < 0 {
		return body
	}
	lines := strings.Split(body[start+len(prefix):], "\n")
	links := make(map[string]AgentDocumentSource)
	for i, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		source := parseAgentDocumentSource(strings.TrimPrefix(line, "- "))
		if source.SectionAnchor == "" {
			source.SectionTitle = agentLegacyCitationSection(body[:start], source.Title)
		}
		originalHref := source.Href
		// A stored readable flag or URL is never permission. Re-resolve it on
		// every history read, watch event and ephemeral read, failing closed.
		source.Readable = false
		if s.agentSourceAccess != nil {
			resolved, err := s.agentSourceAccess.ResolveAgentDocumentSource(ctx, reader, tenant, conversation, source)
			if err == nil && resolved.Readable && validProjectedAgentSource(resolved.Href) {
				source = resolved
			}
		}
		label := strings.NewReplacer("[", "\\[", "]", "\\]", "\n", " ", "\r", " ").Replace(source.Title)
		if source.Readable {
			lines[i] = "- [" + label + "](" + source.Href + ") <!--chat.agent.source.readable:true-->"
		} else {
			source.Href = ""
			lines[i] = "- " + label + " <!--chat.agent.source.readable:false-->"
		}
		if originalHref != "" {
			links[originalHref] = source
		}
	}
	answer := body[:start]
	linkPattern := regexp.MustCompile(`\[([^\]]+)\]\(([^)]*)\)`)
	answer = linkPattern.ReplaceAllStringFunc(answer, func(markup string) string {
		parts := linkPattern.FindStringSubmatch(markup)
		if source, ok := links[parts[2]]; ok {
			if !source.Readable {
				return parts[1]
			}
			return "[" + parts[1] + "](" + source.Href + ")"
		}
		if parsed, err := url.Parse(parts[2]); err == nil && parsed.Path == "/workspace/app/docs" {
			return parts[1]
		}
		return markup
	})
	return answer + prefix + strings.Join(lines, "\n")
}

func agentLegacyCitationSection(body, title string) string {
	title = strings.TrimSpace(strings.SplitN(title, " · ", 2)[0])
	title = regexp.MustCompile(` \(version [0-9]+(?:,.*)?\)$`).ReplaceAllString(title, "")
	pattern := regexp.MustCompile(`\(` + regexp.QuoteMeta(title) + `, (?:version [0-9]+|v[0-9]+\.[0-9]+\.[0-9]+), ([^)]*)\)`)
	if match := pattern.FindStringSubmatch(body); len(match) == 2 {
		return strings.TrimSpace(strings.TrimSuffix(match[1], " section"))
	}
	return ""
}

func parseAgentDocumentSource(line string) AgentDocumentSource {
	line = strings.TrimSpace(strings.SplitN(line, " <!--chat.agent.source.readable:", 2)[0])
	source := AgentDocumentSource{Title: line}
	if strings.HasPrefix(line, "[") {
		if split := strings.LastIndex(line, "]("); split > 1 && strings.HasSuffix(line, ")") {
			source.Title = line[1:split]
			source.Href = line[split+2 : len(line)-1]
			if parsed, err := url.Parse(source.Href); err == nil && parsed.Path == "/workspace/app/docs" {
				source.DocumentID = parsed.Query().Get("document")
				source.VersionID = parsed.Query().Get("version")
				source.SectionAnchor = parsed.Fragment
			}
		}
	}
	return source
}

func validProjectedAgentSource(href string) bool {
	parsed, err := url.Parse(href)
	return err == nil && parsed.Scheme == "" && parsed.Host == "" && parsed.User == nil && parsed.Path == "/workspace/app/docs" && parsed.Query().Get("document") != ""
}
