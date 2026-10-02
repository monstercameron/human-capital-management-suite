package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func chatattach001Drafts(m Model) ui.Node {
	s := m.Chatattach001
	if s == nil {
		return nil
	}
	children := []ui.Node{html.Input(html.Props{ID: "chatattach001-picker", Type: "file", Hidden: true, Raw: map[string]any{"multiple": true, "accept": "image/png,image/jpeg,image/gif,image/bmp,application/pdf,text/plain,.txt,.csv,.md,.docx,.xlsx,.pptx"}, Data: map[string]string{"chatattach001": "picker"}})}
	for _, f := range s.Files {
		var thumb ui.Node
		if f.IsImage() && strings.HasPrefix(f.URL, "blob:") {
			thumb = html.Img(html.Props{Class: "chatattach001-thumb", Src: f.URL, Alt: ""})
		}
		parts := []ui.Node{thumb, html.Span(html.Props{Class: "chatattach001-name", Dir: "auto", Text: f.Name}), html.Span(html.Props{Text: humanBytes(f.Bytes)})}
		if f.Uploading {
			parts = append(parts, html.Span(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Text: Chatattach001Text(m.Locale, "uploading") + " " + strconv.Itoa(f.Progress) + "%"}))
		}
		parts = append(parts, html.Button(html.Props{Type: "button", Class: "chatattach001-remove", Disabled: s.Sending, Data: map[string]string{"chatattach001-remove": f.Key}, Aria: map[string]string{"label": Chatattach001Text(m.Locale, "remove") + ": " + f.Name}, Text: Chatattach001Text(m.Locale, "remove")}))
		children = append(children, html.WithKey(html.Div(html.Props{Class: "chatattach001-chip"}, parts...), "chatattach001:"+f.Key))
	}
	if s.Error != "" {
		children = append(children, html.P(html.Props{Class: "chatattach001-error", Role: "status", Aria: map[string]string{"live": "polite"}, Text: Chatattach001Text(m.Locale, s.Error)}))
	}
	return html.Div(html.Props{Class: "chatattach001-drafts", Aria: map[string]string{"label": Chatattach001Text(m.Locale, "files")}}, children...)
}

func chatattach001File(m Model, msg Message, a Attachment) ui.Node {
	glyph := "document"
	if strings.HasPrefix(a.ContentType, "text/") {
		glyph = "edit"
	}
	if strings.Contains(a.ContentType, "spreadsheet") {
		glyph = "checklist"
	}
	var unavailable ui.Node
	if a.PreviewUnavailable {
		unavailable = html.Span(html.Props{Role: "status", Text: Chatattach001Text(m.Locale, "unavailable")})
	}
	return html.Div(html.Props{Class: "attachment-chip", Data: map[string]string{"attachment-id": a.ID, "chatattach001-file": "true"}},
		icon(glyph), html.Span(html.Props{Class: "chatattach001-name", Dir: "auto", Text: a.Name}),
		html.Span(html.Props{Class: "attachment-size", Text: humanBytes(a.Bytes)}),
		unavailable,
		html.Button(html.Props{Type: "button", Class: "attachment-download", Disabled: m.Callbacks.DownloadAttachment == nil, Data: map[string]string{"action": "download-attachment", "id": msg.ID, "extra": a.ID}, Aria: map[string]string{"label": Chatattach001Text(m.Locale, "download") + ": " + a.Name}, Text: Chatattach001Text(m.Locale, "download")}))
}

const Chatattach001Styles = `.chat-workspace .chatattach001-drafts{display:flex;flex-wrap:wrap;gap:var(--hcm-space-2);min-width:0}.chat-workspace .chatattach001-chip,.chat-workspace [data-chatattach001-file]{display:flex;align-items:center;gap:var(--hcm-space-2);max-width:100%;min-width:0;padding:var(--hcm-space-2);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text)}.chat-workspace .chatattach001-name{min-width:0;overflow-wrap:anywhere;flex:1}.chat-workspace .chatattach001-thumb{width:40px;height:40px;object-fit:cover;border-radius:var(--hcm-radius-control)}.chat-workspace .chatattach001-error{width:100%;margin:0;color:var(--hcm-color-danger);overflow-wrap:anywhere}.chat-workspace .chatattach001-remove{font:inherit;color:inherit;background:transparent;border:0;cursor:pointer;min-height:44px}.chat-workspace [data-chatattach001-file] .attachment-download{min-height:44px}.chat-workspace [data-chatattach001-file] .chat-icon{flex-shrink:0}@media(max-width:800px){.chat-workspace .chatattach001-chip,.chat-workspace [data-chatattach001-file]{flex-wrap:wrap;width:100%}}`
