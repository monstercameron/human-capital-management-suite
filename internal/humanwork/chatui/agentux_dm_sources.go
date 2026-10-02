package chatui

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type agentReplySource struct {
	Title    string
	Href     string
	Readable bool
}

type agentReplyEnvelope struct {
	Body          string
	Backlink      string
	QuestionLabel string
	QuestionText  string
	QuestionAt    time.Time
	Sources       []agentReplySource
}

type agentQuestionContext struct {
	Label string    `json:"label"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
}

func parseAgentReplyEnvelope(body string) agentReplyEnvelope {
	envelope := agentReplyEnvelope{Body: strings.TrimSpace(body)}
	const contextMarker = "\n\n[chat-agent-question-context:"
	if start := strings.LastIndex(envelope.Body, contextMarker); start >= 0 && strings.HasSuffix(envelope.Body, ")") {
		close := strings.Index(envelope.Body[start+len(contextMarker):], "](")
		if close >= 0 {
			encodedStart := start + len(contextMarker)
			encodedEnd := encodedStart + close
			href := envelope.Body[encodedEnd+2 : len(envelope.Body)-1]
			var question agentQuestionContext
			decoded, decodeErr := base64.RawURLEncoding.DecodeString(envelope.Body[encodedStart:encodedEnd])
			if decodeErr == nil && json.Unmarshal(decoded, &question) == nil && validAgentReplyBacklink(href) && strings.TrimSpace(question.Text) != "" && !question.At.IsZero() {
				envelope.QuestionLabel = strings.TrimSpace(question.Label)
				envelope.QuestionText = strings.TrimSpace(question.Text)
				envelope.QuestionAt = question.At
				envelope.Backlink = href
				envelope.Body = strings.TrimSpace(envelope.Body[:start])
			}
		}
	}
	const questionMarker = "\n\n[chat-agent-question:"
	if envelope.Backlink == "" {
		if start := strings.LastIndex(envelope.Body, questionMarker); start >= 0 && strings.HasSuffix(envelope.Body, ")") {
			close := strings.Index(envelope.Body[start+len(questionMarker):], "](")
			if close >= 0 {
				labelStart := start + len(questionMarker)
				labelEnd := labelStart + close
				href := envelope.Body[labelEnd+2 : len(envelope.Body)-1]
				if validAgentReplyBacklink(href) {
					envelope.QuestionLabel = strings.TrimSpace(envelope.Body[labelStart:labelEnd])
					envelope.Backlink = href
					envelope.Body = strings.TrimSpace(envelope.Body[:start])
				}
			}
		}
	}
	const backlinkPrefix = "\n\n[Open the original message]("
	if envelope.Backlink == "" {
		if start := strings.LastIndex(envelope.Body, backlinkPrefix); start >= 0 && strings.HasSuffix(envelope.Body, ")") {
			href := envelope.Body[start+len(backlinkPrefix) : len(envelope.Body)-1]
			if validAgentReplyBacklink(href) {
				envelope.Backlink = href
				envelope.Body = strings.TrimSpace(envelope.Body[:start])
			}
		}
	}
	// Older durable copies placed this signed locator in the text. The actor
	// receipt still proves the author, so render a single labelled control and
	// keep the locator itself out of the answer body.
	if envelope.Backlink == "" {
		const legacyBacklinkPrefix = "\n\nOpen the source conversation: "
		if start := strings.LastIndex(envelope.Body, legacyBacklinkPrefix); start >= 0 {
			href := strings.TrimSpace(envelope.Body[start+len(legacyBacklinkPrefix):])
			if validAgentReplyBacklink(href) {
				envelope.Backlink = href
				envelope.Body = strings.TrimSpace(envelope.Body[:start])
			}
		}
	}
	const sourcesPrefix = "\n\nSources\n"
	start := strings.LastIndex(envelope.Body, sourcesPrefix)
	if start < 0 {
		envelope.Body = agentAnswerDisplayVersions(envelope.Body)
		return envelope
	}
	for _, line := range strings.Split(envelope.Body[start+len(sourcesPrefix):], "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if line == "" {
			continue
		}
		source := agentReplySource{Title: line}
		denied := strings.HasSuffix(line, " <!--chat.agent.source.readable:false-->")
		line = strings.TrimSpace(strings.SplitN(line, " <!--chat.agent.source.readable:", 2)[0])
		source.Title = line
		if strings.HasPrefix(line, "[") {
			if split := strings.LastIndex(line, "]("); split > 1 && strings.HasSuffix(line, ")") {
				title, href := line[1:split], line[split+2:len(line)-1]
				if validAgentDocumentHref(href) {
					source.Title, source.Href = title, href
				}
			}
		}
		source.Title = strings.TrimSpace(source.Title)
		source.Title = agentAnswerDisplayVersions(source.Title)
		source.Readable = source.Href != "" && !denied
		if denied {
			source.Href = ""
		}
		if source.Title != "" {
			envelope.Sources = append(envelope.Sources, source)
		}
	}
	if len(envelope.Sources) > 0 {
		envelope.Body = strings.TrimSpace(envelope.Body[:start])
	}
	envelope.Body = agentAnswerPresentBody(envelope.Body, envelope.Sources)
	return envelope
}

// bindAgentQuestionThreadLink projects the recipient-scoped context carried
// beside an ephemeral answer. The signed source locator remains the backlink;
// the fragment is local presentation data and is never sent to the server.
func bindAgentQuestionThreadLink(envelope agentReplyEnvelope, href string) agentReplyEnvelope {
	if envelope.QuestionText != "" || envelope.Backlink != "" {
		return envelope
	}
	parsed, err := url.Parse(strings.TrimSpace(href))
	if err != nil || !strings.HasPrefix(parsed.Fragment, "hcm-question=") {
		return envelope
	}
	encoded := strings.TrimPrefix(parsed.Fragment, "hcm-question=")
	parsed.Fragment = ""
	backlink := parsed.String()
	var question agentQuestionContext
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
	if decodeErr != nil || json.Unmarshal(decoded, &question) != nil || !validAgentReplyBacklink(backlink) || strings.TrimSpace(question.Text) == "" || question.At.IsZero() {
		return envelope
	}
	envelope.QuestionLabel = strings.TrimSpace(question.Label)
	envelope.QuestionText = strings.TrimSpace(question.Text)
	envelope.QuestionAt = question.At
	envelope.Backlink = backlink
	return envelope
}

func validAgentReplyBacklink(href string) bool {
	parsed, err := url.Parse(strings.TrimSpace(href))
	return err == nil && !parsed.IsAbs() && parsed.Host == "" && strings.HasPrefix(parsed.Path, "/chat/share/")
}

func validAgentDocumentHref(href string) bool {
	parsed, err := url.Parse(strings.TrimSpace(href))
	return err == nil && parsed.User == nil && parsed.Path == "/workspace/app/docs" && parsed.Query().Get("document") != "" && (!parsed.IsAbs() || parsed.Scheme == "https" || parsed.Scheme == "http")
}

func renderAgentReplySources(model Model, envelope agentReplyEnvelope) []ui.Node {
	nodes := make([]ui.Node, 0, 2)
	if len(envelope.Sources) > 0 {
		items := make([]ui.Node, 0, len(envelope.Sources))
		for _, source := range envelope.Sources {
			content := ui.Node(html.Tag("bdi", html.Props{Dir: "auto", Text: source.Title}))
			if source.Href != "" {
				href := source.Href
				open := ui.UseEvent(func(event ui.Event) {
					if eventPlainClick(event) && model.Callbacks.Navigate != nil && validAgentDocumentHref(href) {
						event.PreventDefault()
						model.Callbacks.Navigate(href)
					}
				})
				content = html.A(html.Props{Href: href, Class: "agent-reply-source-link", Dir: "auto", OnClick: open}, html.Tag("bdi", html.Props{Text: source.Title}))
			}
			// CHATUX-003: each source is a compact chip. One the reader may open is a
			// link; one they may not is a muted chip with a lock and a tooltip.
			chip := html.Props{Class: "agent-reply-source"}
			children := []ui.Node{icon("document"), content}
			if source.Href == "" {
				unavailable := agentAnswerSourceUnavailable(model.Locale)
				// The tooltip is the sentence itself, shown on hover and focus, so it is
				// read once and by everyone.
				chip = html.Props{Class: "agent-reply-source agent-reply-source-locked", Raw: map[string]any{"tabindex": "0"}}
				children = []ui.Node{icon("lock"), content, html.Span(html.Props{Class: "agent-reply-source-unavailable", Text: unavailable})}
			}
			items = append(items, html.Li(chip, children...))
		}
		nodes = append(nodes, html.Section(html.Props{Class: "agent-reply-sources", Aria: map[string]string{"label": agentReplyFallback(model.Locale, "chat.agent.sources", "Sources")}},
			html.H4(html.Props{Text: agentReplyFallback(model.Locale, "chat.agent.sources", "Sources")}), html.Ul(html.Props{}, items...)))
	}
	return nodes
}

func renderAgentQuestionContext(model Model, envelope agentReplyEnvelope, timeLabel string) ui.Node {
	if envelope.Backlink == "" {
		return nil
	}
	conversation := strings.TrimSpace(envelope.QuestionLabel)
	if conversation == "" {
		conversation = agentReplyFallback(model.Locale, "chat.agent.conversation", "the conversation")
	}
	prefix := agentReplyFallback(model.Locale, "chat.agent.you_asked_in", "You asked in")
	view := strings.ReplaceAll(agentReplyFallback(model.Locale, "chat.agent.view_in", "View in {conversation}"), "{conversation}", conversation)
	line := []ui.Node{html.Span(html.Props{Text: prefix + " "}), html.Tag("bdi", html.Props{Dir: "ltr", Text: conversation})}
	if strings.TrimSpace(timeLabel) == "" && !envelope.QuestionAt.IsZero() {
		timeLabel = envelope.QuestionAt.Local().Format("3:04 PM")
		if strings.HasPrefix(strings.ToLower(model.Locale), "de") {
			timeLabel = envelope.QuestionAt.Local().Format("15:04")
		}
	}
	if strings.TrimSpace(timeLabel) != "" {
		line = append(line, html.Span(html.Props{Text: " · " + timeLabel}))
	}
	children := []ui.Node{html.Div(html.Props{Class: "agent-question-context-line"}, line...)}
	if envelope.QuestionText != "" {
		children = append(children, html.Blockquote(html.Props{Dir: "auto", Text: envelope.QuestionText}))
	}
	children = append(children, html.A(html.Props{Href: envelope.Backlink, Text: view}))
	return html.Aside(html.Props{Class: "agent-question-context", Aria: map[string]string{"label": prefix + " " + conversation}}, children...)
}

const AgentDMSourcesStyles = `.agent-reply-sources{margin-block:.65rem 0;padding-block-start:.55rem;border-block-start:1px solid var(--line)}.agent-reply-sources h4{margin:0 0 .3rem;font-size:.78rem;color:var(--muted)}.agent-reply-sources ul{margin:0;padding-inline-start:1.2rem}.agent-reply-source-link,.agent-reply-backlink{color:var(--accent);overflow-wrap:anywhere}.agent-reply-backlink{display:inline-flex;margin-block-start:.55rem}.agent-dm-avatar{background:var(--soft);color:var(--accent);border:1px solid var(--line)}.agent-identity-line{display:inline-flex;align-items:center;gap:.45rem;min-width:0}`
