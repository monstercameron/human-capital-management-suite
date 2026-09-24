package chatui

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// DocReference is a document link recognized in a chat message: either a
// bare "doc:<id>" token (the same address form documents use for
// cross-linking each other) or a canonical same-origin docs URL.
type DocReference struct {
	ID         string
	Start, End int
}

// DocPreview is a viewer-authorized document unfurl. Body is held in client
// memory only; the destination post persists just the reference the author
// typed. An unreadable document's title and snippet are withheld: the
// server never sends them for a document the viewer cannot open.
type DocPreview struct {
	ID, Title, Owner, UpdatedAt, Snippet string
	Readable                             bool
	State                                string // loading, ready, unavailable
}

var docTokenPattern = regexp.MustCompile(`doc:[A-Za-z0-9._~-]+`)
var docURLPattern = regexp.MustCompile(`https?://[^\s<>"']+|/workspace/app/docs\?[^\s<>"']+`)

func validDocReferenceID(id string) bool {
	if id == "" || len(id) > 256 || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._~-", r)) {
			return false
		}
	}
	return true
}

// DocReferenceURL is the canonical in-app address for a document link.
func DocReferenceURL(id string) string {
	return "/workspace/app/docs?document=" + url.QueryEscape(id)
}

// DocReferences recognizes "doc:<id>" tokens and canonical same-origin docs
// URLs, longest match first with overlaps dropped. It never infers a
// document from an untrusted host or a query string it cannot parse.
func DocReferences(body, origin string) []DocReference {
	if len(body) > 32*1024 {
		body = body[:32*1024]
	}
	var out []DocReference
	for _, span := range docTokenPattern.FindAllStringIndex(body, 16) {
		id := strings.TrimPrefix(body[span[0]:span[1]], "doc:")
		if validDocReferenceID(id) {
			out = append(out, DocReference{ID: id, Start: span[0], End: span[1]})
		}
	}
	if base, err := url.Parse(origin); err == nil && base.Scheme != "" && base.Host != "" {
		for _, span := range docURLPattern.FindAllStringIndex(body, 16) {
			full := body[span[0]:span[1]]
			for _, match := range []string{strings.TrimRight(full, ".,!?;:)]}>"), full} {
				parsed, err := url.Parse(match)
				if err != nil || parsed.Path != "/workspace/app/docs" {
					continue
				}
				if parsed.IsAbs() && (parsed.Scheme != base.Scheme || !strings.EqualFold(parsed.Host, base.Host)) {
					continue
				}
				id := parsed.Query().Get("document")
				if !validDocReferenceID(id) || DocReferenceURL(id) != "/workspace/app/docs?document="+url.QueryEscape(id) {
					continue
				}
				out = append(out, DocReference{ID: id, Start: span[0], End: span[0] + len(match)})
				break
			}
		}
	}
	if len(out) < 2 {
		return out
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	merged := out[:1]
	for _, ref := range out[1:] {
		if ref.Start < merged[len(merged)-1].End {
			continue
		}
		merged = append(merged, ref)
	}
	return merged
}

// docLinkReferenceBody turns a "doc:<id>" token or a copied docs URL into a
// single in-app link, then hands the remaining text to the channel/mention
// renderers. A document the preview cache has not resolved yet still links
// (the destination itself is access-checked when it opens); the label never
// claims a title the viewer has not been authorized to see.
func docLinkReferenceBody(m Model, body string) []ui.Node {
	refs := DocReferences(body, m.EmbedOrigin)
	if len(refs) == 0 {
		return channelReferenceBody(m, body)
	}
	var nodes []ui.Node
	last := 0
	for _, ref := range refs {
		if ref.Start < last {
			continue
		}
		if ref.Start > last {
			nodes = append(nodes, channelReferenceBody(m, body[last:ref.Start])...)
		}
		label := "doc:" + ref.ID
		if preview, ok := m.DocPreviews[ref.ID]; ok && preview.State == "ready" {
			if preview.Readable {
				label = preview.Title
			} else {
				label = m.t(KeyDocRestricted)
			}
		}
		nodes = append(nodes, html.A(html.Props{Class: "chat-doc-reference", Href: DocReferenceURL(ref.ID), Data: map[string]string{"action": "open-doc-reference", "id": ref.ID}, Aria: map[string]string{"label": m.tf(KeyOpenDocument, map[string]string{"title": label})}}, ui.Text(label)))
		last = ref.End
	}
	if last < len(body) {
		nodes = append(nodes, channelReferenceBody(m, body[last:])...)
	}
	return nodes
}

// docPreviewEmbeds is the below-message unfurl card for every document
// reference in body: title, owner, last-updated date, a snippet and the
// access state, exactly as the viewer is authorized to see it. A document
// the cache has not resolved yet renders a loading card rather than nothing,
// so the layout does not jump once the read lands.
func docPreviewEmbeds(m Model, body string) []ui.Node {
	refs := DocReferences(body, m.EmbedOrigin)
	if len(refs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]ui.Node, 0, len(refs))
	for _, ref := range refs {
		if seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		preview, ok := m.DocPreviews[ref.ID]
		if !ok {
			preview = DocPreview{ID: ref.ID, State: "loading"}
		}
		out = append(out, docPreviewCard(m, preview))
	}
	return out
}

func docPreviewCard(m Model, preview DocPreview) ui.Node {
	content := []ui.Node{html.Span(html.Props{Class: "chat-embed-label", Text: m.t(KeyDocEmbedTitle)})}
	switch preview.State {
	case "ready":
		if !preview.Readable {
			content = append(content, html.Span(html.Props{Class: "chat-doc-embed-locked", Text: m.t(KeyDocRestricted)}))
			break
		}
		content = append(content, html.Strong(html.Props{Class: "chat-embed-source", Text: preview.Title}))
		byline := preview.Owner
		if preview.UpdatedAt != "" {
			byline = strings.TrimSpace(byline + " · " + preview.UpdatedAt)
		}
		if byline != "" {
			content = append(content, html.Span(html.Props{Class: "chat-embed-byline", Text: byline}))
		}
		if preview.Snippet != "" {
			content = append(content, html.P(html.Props{Class: "chat-embed-body", Dir: "auto", Text: preview.Snippet}))
		}
	case "unavailable":
		content = append(content, html.Span(html.Props{Text: m.t(KeyEmbedUnavailable)}))
	default:
		content = append(content, html.Span(html.Props{Text: m.t(KeyEmbedLoading)}))
	}
	if preview.State == "ready" && preview.Readable {
		return html.A(html.Props{Class: "chat-embed chat-embed-link chat-doc-embed", Href: DocReferenceURL(preview.ID), Data: map[string]string{"action": "open-doc-reference", "id": preview.ID}}, content...)
	}
	return html.Div(html.Props{Class: "chat-embed chat-doc-embed", Data: map[string]string{"embed-state": preview.State}}, content...)
}
