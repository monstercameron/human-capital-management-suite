package chatui

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-053. An address typed into a message was printed as text: it could
// not be pressed, a long one ran over three or four lines, and the line broke
// in the middle of it. Every http(s) address in a message is now a link. One
// longer than 60 characters shows its site and the end of its path with an
// ellipsis for the middle; the middle stays in the link's text, drawn as the
// ellipsis, so the link's title, its target and a copy of the selected text
// all give the whole address. A link is one unbreakable piece: in a column too
// narrow for it, it is cut at its end with an ellipsis and never broken inside.
//
// The addresses this page gives a name of their own (a shared message, a
// document, a task, a workflow, a channel) are taken by their renderers first;
// this one has what they left. A share address of another server has no card
// and no name here, and reads "a shared message" with its site.

// ChatBug053Styles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatBug053Styles = `
.chat-workspace a.chat-link{display:inline-block;max-inline-size:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;vertical-align:bottom;direction:ltr;unicode-bidi:isolate}
.chat-workspace a.chat-link .chat-link-cut{display:inline-block;max-inline-size:1.1em;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;vertical-align:bottom}
`

// chatbug053Long is the length, in characters, past which an address is
// shortened for the eye.
const chatbug053Long = 60

var chatbug053Address = chatperf2Literals(regexp.MustCompile(`https?://[^\s<>"']+`), "http://", "https://")

func chatbug053Copy(locale string) string {
	values := [3]string{"a shared message", "eine geteilte Nachricht", "رسالة تمت مشاركتها"}
	return chatbug039Text("", values[chatbug039LocaleIndex(locale)], values[0])
}

// chatbug053Trim takes sentence punctuation off the end of a matched address:
// the full stop that ends the sentence, and a closing bracket the address did
// not open.
func chatbug053Trim(address string) string {
	for {
		trimmed := strings.TrimRight(address, ".,!?;:")
		for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}, {"{", "}"}} {
			if strings.HasSuffix(trimmed, pair[1]) && strings.Count(trimmed, pair[0]) < strings.Count(trimmed, pair[1]) {
				trimmed = strings.TrimSuffix(trimmed, pair[1])
			}
		}
		trimmed = strings.TrimSuffix(trimmed, ">")
		if trimmed == address {
			return address
		}
		address = trimmed
	}
}

// chatbug053Parts splits a long address into the part shown first (its scheme
// and site), the middle that is drawn as an ellipsis, and the end. The end
// begins at a separator of the address where there is one, so that no word of
// it is cut. An address of 60 characters or fewer has no middle.
func chatbug053Parts(address string) (site, middle, end string) {
	if utf8.RuneCountInString(address) <= chatbug053Long {
		return address, "", ""
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		return address, "", ""
	}
	site = parsed.Scheme + "://" + parsed.Host
	if !strings.HasPrefix(address, site) {
		return address, "", ""
	}
	rest := []rune(address[len(site):])
	// What is left of 60 characters after the site and the ellipsis, and never
	// less than 16, so that a long site name still shows where the path ends.
	keep := max(16, chatbug053Long-utf8.RuneCountInString(site)-1)
	if len(rest) <= keep+1 {
		return address, "", ""
	}
	start := len(rest) - keep
	// Move the cut forward to the next separator, within the first half of what
	// is kept; an end with no separator there is one long word and is cut as is.
	for i := start; i < start+keep/2; i++ {
		if strings.ContainsRune("/?&#=-_.", rest[i]) {
			start = i
			break
		}
	}
	return site, string(rest[:start]), string(rest[start:])
}

// chatbug053ShareSite reports the site of an address that is a share link of
// some server: the two forms this product issues.
func chatbug053ShareSite(address string) (string, bool) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	share := parsed.Path == "/workspace/app/chat" && strings.HasPrefix(parsed.Fragment, "share=") && validShareToken(strings.TrimPrefix(parsed.Fragment, "share="))
	share = share || (strings.HasPrefix(parsed.Path, "/chat/share/") && validShareToken(strings.TrimPrefix(parsed.Path, "/chat/share/")))
	return parsed.Host, share
}

// chatbug053Link is one address as a link.
func chatbug053Link(m Model, address string) ui.Node {
	if site, ok := chatbug053ShareSite(address); ok {
		label := chatbug053Copy(m.Locale) + " (" + site + ")"
		return html.A(html.Props{Class: "chat-share-link", Href: address, Title: address, Dir: "auto", Data: map[string]string{"share-link": "foreign"}}, ui.Text(label))
	}
	site, middle, end := chatbug053Parts(address)
	if middle == "" {
		return html.A(html.Props{Class: "chat-link", Href: address}, ui.Text(address))
	}
	return html.A(html.Props{Class: "chat-link", Href: address, Title: address, Data: map[string]string{"link": "shortened"}},
		html.Span(html.Props{Class: "chat-link-site", Text: site}),
		html.Span(html.Props{Class: "chat-link-cut", Text: middle}),
		html.Span(html.Props{Class: "chat-link-end", Text: end}))
}

// chatbug053Text is a run of message text with its addresses as links. Text
// that is already the label of a link is left as text.
func chatbug053Text(m Model, text string) []ui.Node {
	if m.renderInLink || !strings.Contains(text, "://") {
		return []ui.Node{ui.Text(text)}
	}
	var nodes []ui.Node
	last := 0
	for _, span := range chatbug053Address.FindAllStringIndex(text, 64) {
		if span[0] < last {
			continue
		}
		if span[0] > 0 {
			if before, _ := utf8.DecodeLastRuneInString(text[:span[0]]); unicode.IsLetter(before) || unicode.IsDigit(before) {
				continue
			}
		}
		address := chatbug053Trim(text[span[0]:span[1]])
		if href, ok := safeMarkdownHref(address); !ok || href != address {
			continue
		}
		if span[0] > last {
			nodes = append(nodes, ui.Text(text[last:span[0]]))
		}
		nodes = append(nodes, chatbug053Link(m, address))
		last = span[0] + len(address)
	}
	if last == 0 {
		return []ui.Node{ui.Text(text)}
	}
	if last < len(text) {
		nodes = append(nodes, ui.Text(text[last:]))
	}
	return nodes
}
