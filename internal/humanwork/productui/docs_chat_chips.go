package productui

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Chat references in a document. The Markdown syntax (resolved server-side
// on GetDocument, internal/application/document_chat_refs.go):
//
//	#channel-name               a channel the reader can see, by name
//	[#label](channel:<id>)      a channel by conversation ID
//	@handle or @<subject-id>    a person (handle: "rafael.torres")
//	[@Name](person:<id>)        a person by subject ID
//	/workspace/app/chat#share=<token>  a chat message permalink
//
// All of it is plain text in the source, so it round-trips through both
// panes of the split editor. Only what the server resolved for this reader
// becomes a chip; anything else renders exactly as written.

func (r DocumentChatRefs) empty() bool {
	return len(r.Channels)+len(r.People)+len(r.Messages) == 0
}

func (r DocumentChatRefs) channel(key string) (DocumentChatChannelReference, bool) {
	for _, c := range r.Channels {
		if c.Key == key {
			return c, true
		}
	}
	return DocumentChatChannelReference{}, false
}

func (r DocumentChatRefs) person(key string) (DocumentChatPersonReference, bool) {
	key = strings.ToLower(key)
	for _, p := range r.People {
		if strings.ToLower(p.Key) == key {
			return p, true
		}
	}
	return DocumentChatPersonReference{}, false
}

func (r DocumentChatRefs) message(token string) (DocumentChatMessageReference, bool) {
	for _, m := range r.Messages {
		if m.Token == token {
			return m, true
		}
	}
	return DocumentChatMessageReference{}, false
}

// encodeDocsChatRefs keeps the reader body's props comparable strings.
func encodeDocsChatRefs(refs DocumentChatRefs) string {
	if refs.empty() {
		return ""
	}
	data, err := json.Marshal(refs)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeDocsChatRefs(value string) DocumentChatRefs {
	var refs DocumentChatRefs
	if value != "" {
		_ = json.Unmarshal([]byte(value), &refs)
	}
	return refs
}

func docsViewChatRefs(view View) DocumentChatRefs {
	if view.Document == nil {
		return DocumentChatRefs{}
	}
	return view.Document.Chat
}

// DocsChannelName normalizes "#name" for matching, as the server does:
// lowercase, runs of spaces, "_" and "-" as one "-".
func DocsChannelName(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsSpace(r) || r == '_' || r == '-' {
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
			continue
		}
		b.WriteRune(r)
		dash = false
	}
	return strings.TrimRight(b.String(), "-")
}

// DocsPersonHandle is the "@handle" for a display name.
func DocsPersonHandle(name string) string {
	var b strings.Builder
	dot := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dot = false
			continue
		}
		if !dot && b.Len() > 0 {
			b.WriteByte('.')
			dot = true
		}
	}
	return strings.TrimRight(b.String(), ".")
}

// Chat addresses a chip navigates to; the chat page opens each from its
// fragment (a channel, a person's details, a shared message).
func docsChatChannelHref(id string) string {
	return "/workspace/app/chat#channel=" + url.QueryEscape(id)
}

func docsChatPersonHref(id string) string {
	return "/workspace/app/chat#person=" + url.QueryEscape(id)
}

func docsChatShareHref(token string) string { return "/workspace/app/chat#share=" + token }

var (
	docsChatTextPattern  = regexp.MustCompile(`#([\p{L}\p{N}][\p{L}\p{N}_-]{0,79})|@([\p{L}\p{N}](?:[\p{L}\p{N}._-]{0,126}[\p{L}\p{N}])?)|(?:https?://[^\s/<>"']+)?/(?:workspace/app/chat#share=|chat/share/)([A-Za-z0-9_-]+)`)
	docsChatSharePattern = regexp.MustCompile(`^(?:https?://[^\s/<>"']+)?/(?:workspace/app/chat#share=|chat/share/)([A-Za-z0-9_-]+)$`)
	docsChatChannelToken = regexp.MustCompile(`^channel:([A-Za-z0-9._~:-]{1,256})$`)
	docsChatPersonToken  = regexp.MustCompile(`^person:([A-Za-z0-9._~:@-]{1,256})$`)
)

// docsChatTextNodes renders resolved "#name", "@handle" and permalinks in
// a run of text as chips; the text between them goes to rest.
func docsChatTextNodes(view View, value string, rest func(View, string) []ui.Node) []ui.Node {
	refs := docsViewChatRefs(view)
	if refs.empty() || !strings.ContainsAny(value, "#@/") {
		return rest(view, value)
	}
	var nodes []ui.Node
	last := 0
	for _, m := range docsChatTextPattern.FindAllStringSubmatchIndex(value, -1) {
		start, end := m[0], m[1]
		if start > 0 && (m[2] >= 0 || m[4] >= 0) {
			prev, _ := utf8.DecodeLastRuneInString(value[:start])
			if unicode.IsLetter(prev) || unicode.IsDigit(prev) || strings.ContainsRune("_&#/.@[", prev) {
				continue
			}
		}
		var chip ui.Node
		switch {
		case m[2] >= 0:
			if c, ok := refs.channel("name:" + DocsChannelName(value[m[2]:m[3]])); ok && !c.Locked {
				chip = docsChannelChip(view, c)
			}
		case m[4] >= 0:
			if p, ok := refs.person(value[m[4]:m[5]]); ok {
				chip = docsPersonChip(view, p)
			}
		case m[6] >= 0:
			if msg, ok := refs.message(value[m[6]:m[7]]); ok {
				chip = docsMessageCard(view, msg)
			}
		}
		if chip == nil {
			continue
		}
		if start > last {
			nodes = append(nodes, rest(view, value[last:start])...)
		}
		nodes = append(nodes, chip)
		last = end
	}
	if last == 0 {
		return rest(view, value)
	}
	if last < len(value) {
		nodes = append(nodes, rest(view, value[last:])...)
	}
	return nodes
}

// docsChatLinkNode renders a Markdown link or autolink whose target is a
// channel:, person: or chat permalink address. ok is false for any other
// target; a channel: or person: target the reader's references do not name
// keeps its label as plain text (never an unsafe link).
func docsChatLinkNode(view View, target string, label []ui.Node) (ui.Node, bool) {
	refs := docsViewChatRefs(view)
	if m := docsChatChannelToken.FindStringSubmatch(target); m != nil {
		c, ok := refs.channel("id:" + m[1])
		switch {
		case !ok:
			return html.Span(html.Props{Class: "docs-chat-unresolved"}, label...), true
		case c.Locked:
			return docsLockedChannelChip(view), true
		}
		return docsChannelChip(view, c), true
	}
	if m := docsChatPersonToken.FindStringSubmatch(target); m != nil {
		if p, ok := refs.person(m[1]); ok {
			return docsPersonChip(view, p), true
		}
		return html.Span(html.Props{Class: "docs-chat-unresolved"}, label...), true
	}
	if m := docsChatSharePattern.FindStringSubmatch(target); m != nil {
		if msg, ok := refs.message(m[1]); ok {
			return docsMessageCard(view, msg), true
		}
	}
	return nil, false
}

func docsChatChipLink(class, href, label string, children ...ui.Node) ui.Node {
	return html.A(html.Props{Class: "docs-chat-chip " + class, Href: href, Aria: map[string]string{"label": label}, Data: map[string]string{"docs-action": "open", "docs-id": href}}, children...)
}

func docsChannelChip(view View, c DocumentChatChannelReference) ui.Node {
	locale := view.Locale.Resolved
	members := strings.ReplaceAll(docsChatText(locale, "members"), "{count}", docsLocaleDigits(locale, strconv.Itoa(c.MemberCount)))
	if c.MemberCount == 1 {
		members = docsChatText(locale, "member_one")
	}
	label := strings.NewReplacer("{name}", c.Name, "{members}", members).Replace(docsChatText(locale, "open_channel"))
	glyph := ui.Node(html.Span(html.Props{Class: "docs-chat-glyph", Aria: map[string]string{"hidden": "true"}}, ui.Text("#")))
	if c.Private {
		glyph = productIcon("privacy", "docs-chat-icon")
	}
	return docsChatChipLink("docs-chat-channel", docsChatChannelHref(c.ConversationID), label,
		glyph, html.Span(html.Props{Class: "docs-chat-name", Dir: "auto"}, ui.Text(c.Name)),
		html.Span(html.Props{Class: "docs-chat-meta", Aria: map[string]string{"hidden": "true"}}, ui.Text(members)))
}

// docsLockedChannelChip names nothing: not the channel, its members or
// whether it exists.
func docsLockedChannelChip(view View) ui.Node {
	locale := view.Locale.Resolved
	return html.Span(html.Props{Class: "docs-chat-chip docs-chat-locked", Raw: map[string]any{"role": "img", "title": docsChatText(locale, "locked_hint")}, Aria: map[string]string{"label": docsChatText(locale, "locked_channel") + ". " + docsChatText(locale, "locked_hint")}},
		productIcon("privacy", "docs-chat-icon"), html.Span(html.Props{Class: "docs-chat-name"}, ui.Text(docsChatText(locale, "locked_channel"))))
}

func docsPersonChip(view View, p DocumentChatPersonReference) ui.Node {
	label := strings.ReplaceAll(docsChatText(view.Locale.Resolved, "view_person"), "{name}", p.DisplayName)
	return docsChatChipLink("docs-chat-person", docsChatPersonHref(p.SubjectID), label,
		html.Span(html.Props{Class: "docs-chat-glyph", Aria: map[string]string{"hidden": "true"}}, ui.Text("@")),
		html.Span(html.Props{Class: "docs-chat-name", Dir: "auto"}, ui.Text(p.DisplayName)))
}

// docsMessageCard quotes a chat message the reader may open. One they may
// not either names the reason (DOCS-08): "deleted", the one distinction the
// server can make without leaking anything the reader could not already
// infer from the reference itself, complete with the channel it lived in
// and an "Open channel" link when the server named it — or, for every other
// unreadable case (no access, an unresolvable token), one neutral sentence
// that does not confirm or deny what happened. Spans throughout, so the
// card stays valid inside the paragraph it was written in.
func docsMessageCard(view View, msg DocumentChatMessageReference) ui.Node {
	locale := view.Locale.Resolved
	if !msg.Readable {
		children := []ui.Node{productIcon("privacy", "docs-chat-icon")}
		if msg.ChannelName != "" {
			children = append(children, html.Span(html.Props{}, ui.Text(docsChatText(locale, "message_deleted"))))
			href := docsChatChannelHref(msg.ConversationID)
			children = append(children, html.A(html.Props{Class: "docs-chat-quote-jump", Href: href, Data: map[string]string{"docs-action": "open", "docs-id": href}},
				ui.Text(strings.ReplaceAll(docsChatText(locale, "open_locked_channel"), "{name}", msg.ChannelName))))
		} else {
			children = append(children, html.Span(html.Props{}, ui.Text(docsChatText(locale, "message_unavailable"))))
		}
		return html.Span(html.Props{Class: "docs-chat-quote docs-chat-quote-locked", Raw: map[string]any{"role": "note"}}, children...)
	}
	head := []ui.Node{html.Strong(html.Props{Class: "docs-chat-quote-author", Dir: "auto"}, ui.Text(msg.AuthorName))}
	if msg.ChannelName != "" {
		head = append(head, html.Span(html.Props{Class: "docs-chat-quote-channel", Dir: "auto"}, ui.Text("#"+msg.ChannelName)))
	}
	if !msg.CreatedAt.IsZero() {
		head = append(head, html.Tag("time", html.Props{Class: "docs-chat-quote-time", Raw: map[string]any{"datetime": msg.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")}}, ui.Text(view.Locale.FormatDate(msg.CreatedAt))))
	}
	href := docsChatShareHref(msg.Token)
	label := strings.ReplaceAll(docsChatText(locale, "quoted_message"), "{name}", msg.AuthorName)
	return html.Span(html.Props{Class: "docs-chat-quote", Raw: map[string]any{"role": "figure"}, Aria: map[string]string{"label": label}},
		html.Span(html.Props{Class: "docs-chat-quote-head"}, head...),
		html.Span(html.Props{Class: "docs-chat-quote-body", Dir: "auto"}, docsPlainTextNodes(view, msg.Body)...),
		html.A(html.Props{Class: "docs-chat-quote-jump", Href: href, Data: map[string]string{"docs-action": "open", "docs-id": href}},
			productIcon("chat", "docs-chat-icon"), ui.Text(docsChatText(locale, "jump_to_message"))))
}

var docsChatCopy = map[string]map[string]string{
	"en-US": {
		"members": "{count} members", "member_one": "1 member",
		"open_channel":        "Open channel #{name}, {members}",
		"locked_channel":      "Private channel",
		"locked_hint":         "You are not a member of this channel",
		"view_person":         "View {name}'s details",
		"quoted_message":      "Chat message from {name}",
		"message_unavailable": "This message isn't available to you — it may have been deleted or moved.",
		"message_deleted":     "This message was deleted.",
		"open_locked_channel": "Open #{name}",
		"jump_to_message":     "Jump to message",
		"suggest_people":      "People", "suggest_channels": "Channels", "suggest_docs": "Documents",
		"suggest_empty":   "No matches",
		"suggest_loading": "Searching…",
		"suggest_members": "{count} members",
	},
	"de-DE": {
		"members": "{count} Mitglieder", "member_one": "1 Mitglied",
		"open_channel":        "Kanal #{name} öffnen, {members}",
		"locked_channel":      "Privater Kanal",
		"locked_hint":         "Sie sind kein Mitglied dieses Kanals",
		"view_person":         "Details zu {name} anzeigen",
		"quoted_message":      "Chatnachricht von {name}",
		"message_unavailable": "Diese Nachricht ist für Sie nicht verfügbar – sie wurde möglicherweise gelöscht oder verschoben.",
		"message_deleted":     "Diese Nachricht wurde gelöscht.",
		"open_locked_channel": "#{name} öffnen",
		"jump_to_message":     "Zur Nachricht springen",
		"suggest_people":      "Personen", "suggest_channels": "Kanäle", "suggest_docs": "Dokumente",
		"suggest_empty":   "Keine Treffer",
		"suggest_loading": "Suche läuft…",
		"suggest_members": "{count} Mitglieder",
	},
	"ar": {
		"members": "{count} أعضاء", "member_one": "عضو واحد",
		"open_channel":        "فتح القناة #{name}، {members}",
		"locked_channel":      "قناة خاصة",
		"locked_hint":         "لست عضوًا في هذه القناة",
		"view_person":         "عرض تفاصيل {name}",
		"quoted_message":      "رسالة دردشة من {name}",
		"message_unavailable": "هذه الرسالة غير متاحة لك — ربما تم حذفها أو نقلها.",
		"message_deleted":     "تم حذف هذه الرسالة.",
		"open_locked_channel": "فتح #{name}",
		"jump_to_message":     "الانتقال إلى الرسالة",
		"suggest_people":      "الأشخاص", "suggest_channels": "القنوات", "suggest_docs": "المستندات",
		"suggest_empty":   "لا توجد نتائج",
		"suggest_loading": "جارٍ البحث…",
		"suggest_members": "{count} أعضاء",
	},
}

func docsChatText(locale, key string) string {
	if copy, ok := docsChatCopy[locale]; ok && copy[key] != "" {
		return copy[key]
	}
	return docsChatCopy["en-US"][key]
}

// docsChatRefsStylesheet styles the chips, the quoted message card and the
// editor's reference suggestions.
func docsChatRefsStylesheet() string {
	return `
.docs-markdown .docs-chat-chip{display:inline-flex;align-items:baseline;gap:.25em;max-width:100%;padding:0 .45em;border-radius:999px;background:var(--soft);color:var(--ink);text-decoration:none;border:1px solid var(--line);line-height:1.5;vertical-align:baseline;white-space:nowrap}
.docs-markdown a.docs-chat-chip:hover{border-color:var(--accent);color:var(--accent)}
.docs-markdown a.docs-chat-chip:focus-visible,.docs-markdown .docs-chat-quote-jump:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-chat-glyph{color:var(--muted);font-weight:600}
.docs-chat-name{font-weight:600;max-width:16em;overflow:hidden;text-overflow:ellipsis}
.docs-chat-meta{color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-chat-icon{width:.95em;height:.95em;flex:none;align-self:center}
.docs-markdown .docs-chat-locked{color:var(--muted);border-style:dashed;background:transparent}
.docs-markdown .docs-chat-quote{display:grid;gap:var(--hcm-space-1);margin-block:var(--hcm-space-1);padding:var(--hcm-space-2);border:1px solid var(--line);border-inline-start:3px solid var(--accent);border-radius:var(--hcm-radius-control);background:var(--surface);text-align:start}
.docs-chat-quote-head{display:flex;flex-wrap:wrap;gap:var(--hcm-space-1);align-items:baseline;font-size:var(--hcm-font-size-small);color:var(--muted)}
.docs-chat-quote-author{color:var(--ink)}
.docs-chat-quote-body{white-space:pre-wrap;color:var(--ink);unicode-bidi:plaintext}
.docs-markdown .docs-chat-quote-jump{display:inline-flex;gap:.35em;align-items:center;justify-self:start;font-size:var(--hcm-font-size-small)}
.docs-markdown .docs-chat-quote-locked{display:flex;align-items:center;gap:var(--hcm-space-1);color:var(--muted);border-inline-start-color:var(--line)}
@media (prefers-reduced-motion:no-preference){.docs-markdown a.docs-chat-chip{transition:border-color var(--hcm-motion-fast,.14s),color var(--hcm-motion-fast,.14s)}}
` + docsSuggestStylesheet()
}

// docsChatRefTarget reports a channel: or person: link target, which the
// editor keeps as a link so the reference round-trips through both panes.
func docsChatRefTarget(target string) bool {
	return docsChatChannelToken.MatchString(target) || docsChatPersonToken.MatchString(target)
}

// DocsChatText is the chat-reference copy for locale, for the browser half
// that builds the editor's suggestion rows.
func DocsChatText(locale, key string) string { return docsChatText(locale, key) }
