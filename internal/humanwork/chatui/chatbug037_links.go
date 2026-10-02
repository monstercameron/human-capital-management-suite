package chatui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-037. A message that carries a share link used to print the whole
// address in the middle of the sentence, a long opaque token, with the preview
// card below it. The address is now a short link: "a message in #design" when
// the card below is ready for this reader, and a shortened address with the
// whole address as its title when it is not.

// ChatBug037Styles is joined into the workspace stylesheet in styles.go.
const ChatBug037Styles = `
.chat-workspace a.chat-share-link{overflow-wrap:anywhere}
.chat-workspace a.chat-share-link[data-share-link="unresolved"]{word-break:break-all}
.chat-embed-attachments{display:inline-flex;align-items:center;gap:4px;font-size:.75rem;color:var(--muted)}
.chat-embed-attachments .chat-icon{width:14px;height:14px;flex:none}
`

// chatbug037Text answers one of this fix's own strings from its own table, so
// a catalog that does not hold the key never prints a bracketed key.
func chatbug037Text(locale, key string) string {
	copy := map[string][3]string{
		"in":            {"a message in {where}", "eine Nachricht in {where}", "رسالة في {where}"},
		"bare":          {"a message", "eine Nachricht", "رسالة"},
		"attachment.1":  {"1 attachment", "1 Anhang", "مرفق واحد"},
		"attachment.2":  {"2 attachments", "2 Anhänge", "مرفقان"},
		"attachment.n":  {"{n} attachments", "{n} Anhänge", "{n} مرفقات"},
		"attachment.n2": {"{n} attachments", "{n} Anhänge", "{n} مرفقاً"},
	}
	values, ok := copy[key]
	if !ok {
		return ""
	}
	index := 0
	switch {
	case strings.HasPrefix(locale, "de"):
		index = 1
	case strings.HasPrefix(locale, "ar"):
		index = 2
	}
	return chatbug039Text(key, values[index], values[0])
}

// chatbug037AttachmentLabel is "1 attachment" or "3 attachments" in the
// reader's language; Arabic has its own forms for one, two, three to ten and
// eleven or more.
func chatbug037AttachmentLabel(m Model, count int) string {
	key := "attachment.n"
	switch {
	case count == 1:
		key = "attachment.1"
	case count == 2 && strings.HasPrefix(m.Locale, "ar"):
		key = "attachment.2"
	case count >= 11 && strings.HasPrefix(m.Locale, "ar"):
		key = "attachment.n2"
	}
	return strings.ReplaceAll(chatbug037Text(m.Locale, key), "{n}", m.n(count))
}

// chatbug037AttachmentBadge is the attachment icon with the count, named
// "1 attachment" for assistive technology instead of a bare "Attachments: 1".
func chatbug037AttachmentBadge(m Model, count int) ui.Node {
	label := chatbug037AttachmentLabel(m, count)
	return html.Span(html.Props{Class: "chat-embed-attachments", Role: "img", Title: label, Aria: map[string]string{"label": label}},
		icon("attach"), html.Span(html.Props{Class: "chat-embed-attachments-count", Aria: map[string]string{"hidden": "true"}, Text: m.n(count)}))
}

type chatbug037Span struct {
	Start, End int
	Address    string
	Locator    ShareLocator
}

// chatbug037ShareSpans finds the share addresses of body in the same way
// ShareLocators does, with their positions.
func chatbug037ShareSpans(origin, body string) []chatbug037Span {
	if origin == "" || !(strings.Contains(body, "share=") || strings.Contains(body, "/chat/share/") || strings.Contains(body, "#msg=")) {
		return nil
	}
	if len(body) > 32*1024 {
		body = body[:32*1024]
	}
	var out []chatbug037Span
	for _, span := range shareURLPattern.FindAllStringIndex(body, 32) {
		address := strings.TrimRight(body[span[0]:span[1]], ".,!?;:)]}>")
		locators := ShareLocators(address, origin)
		if len(locators) != 1 {
			continue
		}
		out = append(out, chatbug037Span{Start: span[0], End: span[0] + len(address), Address: address, Locator: locators[0]})
	}
	return out
}

// chatbug037Embed is what the card below the message holds for a locator.
func chatbug037Embed(m Model, locator ShareLocator) LinkEmbed {
	if locator.LegacyPost == "" {
		return m.Embeds[locator.Token]
	}
	for _, message := range m.Messages {
		if message.ID == locator.LegacyPost {
			return LinkEmbed{State: "ready", SourceRoom: m.SelectedID, SourcePost: message.ID, Channel: m.selected().Name}
		}
	}
	return LinkEmbed{State: "unavailable"}
}

// chatbug037Where names the conversation a linked message is in, the way the
// reader's own rail names it: "#design" for a channel, the people for a direct
// conversation. Empty when the reader cannot see where it is.
func chatbug037Where(m Model, embed LinkEmbed) string {
	for _, c := range m.Conversations {
		if c.ID != embed.SourceRoom || c.ID == "" {
			continue
		}
		name := displayName(m, c)
		if name == "" {
			break
		}
		if c.Kind == PublicChannel || c.Kind == PrivateChannel {
			return "#" + name
		}
		return name
	}
	if name := strings.TrimSpace(embed.Channel); name != "" {
		return "#" + strings.TrimPrefix(name, "#")
	}
	return ""
}

// chatbug037ShortAddress shortens an address for the eye; the whole address
// stays in the link and in its title.
func chatbug037ShortAddress(address string) string {
	runes := []rune(address)
	if len(runes) <= 48 {
		return address
	}
	return string(runes[:44]) + "…"
}

func chatbug037ShareLink(m Model, span chatbug037Span) ui.Node {
	embed := chatbug037Embed(m, span.Locator)
	if embed.State == "ready" {
		label := chatbug037Text(m.Locale, "bare")
		if where := chatbug037Where(m, embed); where != "" {
			label = strings.ReplaceAll(chatbug037Text(m.Locale, "in"), "{where}", where)
		}
		return html.A(html.Props{Class: "chat-share-link", Href: span.Address, Dir: "auto", Data: map[string]string{"share-link": "resolved"}}, ui.Text(label))
	}
	return html.A(html.Props{Class: "chat-share-link", Href: span.Address, Title: span.Address, Data: map[string]string{"share-link": "unresolved"}}, ui.Text(chatbug037ShortAddress(span.Address)))
}

// chatbug037ShareBody renders body with its share addresses as links and hands
// each run of text between them to rest. It reports false when body holds no
// share address, so the caller keeps its own path.
func chatbug037ShareBody(m Model, body string, rest func(Model, string) []ui.Node) ([]ui.Node, bool) {
	spans := chatbug037ShareSpans(m.EmbedOrigin, body)
	if len(spans) == 0 {
		return nil, false
	}
	var nodes []ui.Node
	last := 0
	for _, span := range spans {
		if span.Start < last {
			continue
		}
		if span.Start > last {
			nodes = append(nodes, rest(m, body[last:span.Start])...)
		}
		nodes = append(nodes, chatbug037ShareLink(m, span))
		last = span.End
	}
	if last < len(body) {
		nodes = append(nodes, rest(m, body[last:])...)
	}
	return nodes, true
}

// chatbug037SameServer reports whether an address names the server the reader
// is on. The share token identifies the message, so a stored address written
// with another name for the same machine (localhost, 127.0.0.1, [::1]) must
// keep working; any other host name has to match exactly.
func chatbug037SameServer(address, base *url.URL) bool {
	if strings.EqualFold(address.Host, base.Host) {
		return true
	}
	loopback := func(host string) bool {
		host = strings.ToLower(host)
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	return loopback(address.Hostname()) && loopback(base.Hostname()) && address.Port() == base.Port()
}
