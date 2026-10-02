package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The file pickers of the two composers. The reply box has its own, so a
// chosen file lands under the box it was chosen from.
const (
	Chatattach001PickerID       = "chatattach001-picker"
	Chatattach001ThreadPickerID = "chatattach001-thread-picker"
)

func chatattach001Drafts(m Model) ui.Node {
	return chatattach001DraftsOf(m, m.Chatattach001, Chatattach001PickerID, "chatattach001-drafts")
}

// chatattach001ThreadDrafts is the reply box's files: a paperclip that opens
// the picker (the reply box has no Add menu to hold it), then the chosen files
// as the conversation composer shows them. It is not drawn where files cannot
// be attached, which is also where the Add menu has no "Attach a file".
func chatattach001ThreadDrafts(m Model, disabled bool) ui.Node {
	s := m.Chatattach001.thread()
	if s == nil || s.Choose == nil {
		return nil
	}
	label := Chatattach001Text(m.Locale, "attach")
	return html.Div(html.Props{Class: "chatattach001-thread"},
		html.Button(html.Props{Class: "tool-button chatattach001-thread-attach", Type: "button", Disabled: disabled || s.Sending || len(s.Files) >= 10, Title: Chatattach001Text(m.Locale, "note"),
			Data: map[string]string{"chatattach001-choose": "thread"}, Aria: map[string]string{"label": label}}, icon("attach")),
		chatattach001DraftsOf(m, s, Chatattach001ThreadPickerID, "chatattach001-drafts chatattach001-thread-drafts"))
}

// chatattach001ReplyWithFiles sends the reply box's files with whatever text
// is in it, also none, and reports whether it did. A reply with no files is
// left to the ordinary reply path.
func chatattach001ReplyWithFiles(m Model, mention mentionStore) bool {
	s := m.Chatattach001.thread()
	if s == nil || len(s.Files) == 0 || m.ThreadParentID == "" {
		return false
	}
	// While a file is uploading or failed the reply waits, as a message does.
	if !s.Ready() || s.Send == nil {
		return true
	}
	raw := domValue("thread-composer")
	body := emojiShortcodesInText(strings.TrimSpace(raw))
	s.Send(body, mention.PersonaReferences("thread-composer", m.SelectedID, raw))
	mention.RemovePersonas("thread-composer", m.SelectedID)
	pinAgentChatAfterSend("thread-composer")
	setDOMValue("thread-composer", "")
	if m.Callbacks.ThreadDraftChanged != nil {
		m.Callbacks.ThreadDraftChanged(m.ThreadParentID, "")
	}
	return true
}

// chatattach001SendFilesOnly sends the composer's files as a message with no
// text and reports whether it did. raw is what the text box holds; a message
// with text goes the ordinary way, with its files.
func chatattach001SendFilesOnly(m Model, raw string) bool {
	s := m.Chatattach001
	if strings.TrimSpace(raw) != "" || strings.TrimSpace(m.Draft) != "" || !s.Sendable() || s.Send == nil || m.SelectedID == "" {
		return false
	}
	s.Send("", nil)
	return true
}

func chatattach001DraftsOf(m Model, s *Chatattach001Composer, pickerID, class string) ui.Node {
	if s == nil {
		return nil
	}
	children := []ui.Node{html.Input(html.Props{ID: pickerID, Type: "file", Hidden: true, Raw: map[string]any{"multiple": true, "accept": "image/png,image/jpeg,image/gif,image/bmp,application/pdf,text/plain,.txt,.csv,.md,.docx,.xlsx,.pptx"}, Data: map[string]string{"chatattach001": "picker"}})}
	for _, f := range s.Files {
		var thumb ui.Node
		if f.IsImage() && strings.HasPrefix(f.URL, "blob:") {
			thumb = html.Img(html.Props{Class: "chatattach001-thumb", Src: f.URL, Alt: ""})
		}
		parts := []ui.Node{thumb, html.Span(html.Props{Class: "chatattach001-name", Dir: "auto", Text: f.Name}), html.Span(html.Props{Text: humanBytes(f.Bytes)})}
		if f.Uploading {
			parts = append(parts, html.Span(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Text: Chatattach001Text(m.Locale, "uploading") + " " + chatCount(m.Locale, f.Progress) + "%"}))
		}
		if f.Failed {
			// The file is still here: one press sends it again.
			parts = append(parts,
				html.Span(html.Props{Class: "chatattach001-failed", Role: "alert", Text: Chatattach001Text(m.Locale, "not_sent")}),
				html.Button(html.Props{Type: "button", Class: "chatattach001-retry", Disabled: s.Sending, Data: map[string]string{"chatattach001-retry": f.Key}, Aria: map[string]string{"label": Chatattach001Text(m.Locale, "retry") + ": " + f.Name}, Text: Chatattach001Text(m.Locale, "retry")}))
		}
		parts = append(parts, html.Button(html.Props{Type: "button", Class: "chatattach001-remove", Disabled: s.Sending, Data: map[string]string{"chatattach001-remove": f.Key}, Aria: map[string]string{"label": Chatattach001Text(m.Locale, "remove") + ": " + f.Name}, Text: Chatattach001Text(m.Locale, "remove")}))
		children = append(children, html.WithKey(html.Div(html.Props{Class: "chatattach001-chip"}, parts...), "chatattach001:"+f.Key))
	}
	if s.Error != "" {
		children = append(children, html.P(html.Props{Class: "chatattach001-error", Role: "status", Aria: map[string]string{"live": "polite"}, Text: Chatattach001Text(m.Locale, s.Error)}))
	}
	return html.Div(html.Props{Class: class, Aria: map[string]string{"label": Chatattach001Text(m.Locale, "files")}}, children...)
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

const Chatattach001Styles = `.chat-workspace .chatattach001-drafts{display:flex;flex-wrap:wrap;gap:var(--hcm-space-2);min-width:0}.chat-workspace .chatattach001-chip,.chat-workspace [data-chatattach001-file]{display:flex;align-items:center;gap:var(--hcm-space-2);max-width:100%;min-width:0;padding:var(--hcm-space-2);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text)}.chat-workspace .chatattach001-name{min-width:0;overflow-wrap:anywhere;flex:1}.chat-workspace .chatattach001-thumb{width:40px;height:40px;object-fit:cover;border-radius:var(--hcm-radius-control)}.chat-workspace .chatattach001-error{width:100%;margin:0;color:var(--hcm-color-danger);overflow-wrap:anywhere}.chat-workspace .chatattach001-remove,.chat-workspace .chatattach001-retry{font:inherit;color:inherit;background:transparent;border:0;cursor:pointer;min-height:44px}.chat-workspace .chatattach001-retry{color:var(--hcm-color-brand-primary);font-weight:600}.chat-workspace .chatattach001-failed{color:var(--hcm-color-danger)}.chat-workspace [data-chatattach001-file] .attachment-download{min-height:44px}.chat-workspace [data-chatattach001-file] .chat-icon{flex-shrink:0}@media(max-width:800px){.chat-workspace .chatattach001-chip,.chat-workspace [data-chatattach001-file]{flex-wrap:wrap;width:100%}}` + chatattach001FilesOnlyStyles

// chatattach001FilesOnlyStyles: files alone are a message, and a reply takes
// files. Send is not dimmed for an empty text box that has files under it. A
// conversation composer that is not in use folds to one row; with files it
// wraps, so they keep a row of their own under the text. The reply box's
// paperclip folds away with its other tools, and stays while it holds files;
// "Also send to the conversation" is for a reply's words, so it is not offered
// while the reply carries files.
const chatattach001FilesOnlyStyles = `.chat-workspace .chat-composer:has(.chatattach001-chip) .send-button:not(:disabled),.chat-workspace .thread-composer:has(.chatattach001-chip) .send-button:not(:disabled){opacity:1}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown):has(.chatattach001-chip){flex-wrap:wrap}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .chatattach001-drafts:has(.chatattach001-chip){flex:1 1 100%;order:3}` +
	`.chat-workspace .chatattach001-thread{display:flex;align-items:flex-start;gap:var(--hcm-space-2);min-width:0;padding:2px 6px 0}` +
	`.chat-workspace .chatattach001-thread .chatattach001-thread-drafts{flex:1 1 0%}` +
	`.chat-workspace .chatattach001-thread-attach{flex:none}` +
	`@container chat (max-width:1100px){.chat-workspace .thread-composer:not(:focus-within) .chatattach001-thread:not(:has(.chatattach001-chip)){display:none}}` +
	`.chat-workspace .thread-composer:has(.chatattach001-chip) .thread-also{display:none}` +
	`@media(pointer:coarse){.chat-workspace .chatattach001-thread-attach{min-inline-size:44px;min-block-size:44px}}`
