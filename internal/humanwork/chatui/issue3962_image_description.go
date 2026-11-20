package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// MaxImageDescriptionChars is the longest description the composer takes. The
// server enforces the same limit (chat.MaxImageDescriptionRunes); the field
// states it so a long description is stopped while it is typed, not refused
// after Send.
const MaxImageDescriptionChars = 1000

// Issue3962Text is the copy of the image description editor and of the places
// that show the description.
func Issue3962Text(locale, key string) string {
	words := map[string][3]string{
		"label":       {"Image description", "Bildbeschreibung", "وصف الصورة"},
		"placeholder": {"Describe what the image shows", "Beschreiben Sie, was das Bild zeigt", "صف ما تعرضه الصورة"},
		"decorative":  {"Decorative image, no description needed", "Dekoratives Bild, keine Beschreibung nötig", "صورة زخرفية، لا حاجة إلى وصف"},
		"hint":        {"Helps people who cannot see the image. For a chart or diagram, give the main point here and the details in your message.", "Hilft Personen, die das Bild nicht sehen können. Bei Diagrammen nennen Sie hier die Kernaussage und die Details in Ihrer Nachricht.", "يساعد من لا يستطيعون رؤية الصورة. في المخططات والرسوم البيانية اذكر الفكرة الرئيسية هنا والتفاصيل في رسالتك."},
		"open":        {"Open image", "Bild öffnen", "افتح الصورة"},
		"description": {"Description", "Beschreibung", "الوصف"},
		"file":        {"File name", "Dateiname", "اسم الملف"},
	}
	i := chatbug039LocaleIndex(locale)
	return chatbug039Text(key, words[key][i], words[key][0])
}

// issue3962Editor is the description editor under an attached image: a labelled
// field, the decorative choice, and the hint. There is deliberately no
// suggestion action: the workspace has no authorized image-capable service, so
// offering one would be a control that cannot work.
func issue3962Editor(m Model, s *Chatattach001Composer, f Chatattach001Draft) ui.Node {
	if !f.IsImage() {
		return nil
	}
	inputID := "chatattach001-alt-" + f.Key
	hintID := inputID + "-hint"
	field := html.Props{ID: inputID, Class: "chatattach001-alt", Type: "text", Value: f.AltText, Dir: "auto", Disabled: s.Sending || f.Decorative, MaxLength: MaxImageDescriptionChars,
		Placeholder: Issue3962Text(m.Locale, "placeholder"), AutoComplete: "off",
		Data: map[string]string{"chatattach001-alt": f.Key},
		Aria: map[string]string{"describedby": hintID}}
	box := html.Props{Class: "chatattach001-decorative", Type: "checkbox", Checked: f.Decorative, Disabled: s.Sending,
		Data: map[string]string{"chatattach001-decorative": f.Key}}
	return html.Div(html.Props{Class: "chatattach001-describe"},
		html.Label(html.Props{For: inputID, Class: "chatattach001-alt-label", Text: Issue3962Text(m.Locale, "label")}),
		html.Input(field),
		html.Label(html.Props{Class: "chatattach001-decorative-label"}, html.Input(box), html.Span(html.Props{Text: Issue3962Text(m.Locale, "decorative")})),
		html.P(html.Props{ID: hintID, Class: "chatattach001-alt-hint", Text: Issue3962Text(m.Locale, "hint")}))
}

// issue3962Alt is the text alternative of an image in the conversation: what
// the author wrote, nothing for a decorative image, and the file name only when
// the author said nothing. described reports whether the alternative is the
// author's own.
func issue3962Alt(a Attachment, name string) (alt string, described bool) {
	if a.Decorative {
		return "", false
	}
	if text := strings.TrimSpace(a.AltText); text != "" {
		return text, true
	}
	return name, false
}

// issue3962Data adds what the image viewer needs to show the description.
func issue3962Data(data map[string]string, a Attachment) {
	if a.Decorative {
		data["media-decorative"] = "true"
		return
	}
	if text := strings.TrimSpace(a.AltText); text != "" {
		data["media-alt"] = text
	}
}

const issue3962Styles = `.chat-image-viewer-caption>.chat-image-viewer-description{white-space:normal;max-block-size:6em;overflow:auto;overflow-wrap:anywhere;font-size:.8125rem}.chat-workspace .chatattach001-chip:has(.chatattach001-describe){flex-wrap:wrap}.chat-workspace .chatattach001-describe{flex:1 1 100%;min-width:0;display:flex;flex-direction:column;gap:var(--hcm-space-1)}.chat-workspace .chatattach001-alt-label{font-weight:600}.chat-workspace .chatattach001-alt{font:inherit;color:inherit;min-height:44px;padding:0 var(--hcm-space-2);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);inline-size:100%;box-sizing:border-box}.chat-workspace .chatattach001-decorative-label{display:flex;align-items:center;gap:var(--hcm-space-2);min-height:44px}.chat-workspace .chatattach001-decorative{inline-size:20px;block-size:20px;flex:none}.chat-workspace .chatattach001-alt-hint{margin:0;font-size:.85em;opacity:.8;overflow-wrap:anywhere}`
