package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// #3996. A thread opened from its reply count showed the whole root message --
// several paragraphs and an image -- and pushed every reply below the fold. A
// tall root now opens as a short, clipped summary under its author and date,
// with a visible control to show all of it. The stored message is untouched, the
// full text stays in the page for screen readers, and attachments are named.

const (
	issue3996TallRunes = 280
	issue3996TallLines = 4
	issue3996KeyPrefix = "thread-root:"
)

var issue3996Copy = map[string][3]string{
	"expand":      {"Show full message", "Ganze Nachricht anzeigen", "عرض الرسالة كاملة"},
	"collapse":    {"Show less", "Weniger anzeigen", "عرض أقل"},
	"attachments": {"This message has attachments. Show the full message to see them.", "Diese Nachricht enthält Anhänge. Zeigen Sie die ganze Nachricht an, um sie zu sehen.", "تحتوي هذه الرسالة على مرفقات. اعرض الرسالة كاملة لرؤيتها."},
}

func issue3996Text(m Model, key string) string {
	row := issue3996Copy[key]
	return chatbug039Text("issue3996."+key, row[chatbug039LocaleIndex(m.Locale)], row[0])
}

// issue3996RootIsTall is whether the root is long enough to crowd out the
// replies: a lot of text, several lines or paragraphs, or any attachment.
func issue3996RootIsTall(root Message, body string) bool {
	if len(root.Attachments) > 0 {
		return true
	}
	body = strings.TrimSpace(body)
	return len([]rune(body)) > issue3996TallRunes || strings.Count(body, "\n") >= issue3996TallLines-1
}

// issue3996Collapsed is whether the root shows as a summary now: it is tall,
// replies are there to read, and the reader has not opened it.
func issue3996Collapsed(m Model, h handlers, root Message, body string) bool {
	if !issue3996RootIsTall(root, body) {
		return false
	}
	if len(m.ThreadMessages) == 0 && root.Replies == 0 {
		return false
	}
	return !h.local.detailGroups[issue3996KeyPrefix+root.ID]
}

// issue3996WrapRoot puts the root's body and extras (everything after the
// author line) in one region that can be clipped, and adds the control that
// shows or hides the rest. A short root, or one with no replies, is unchanged.
func issue3996WrapRoot(m Model, h handlers, root Message, body string, children []ui.Node) []ui.Node {
	if len(children) < 2 || !issue3996RootIsTall(root, body) || (len(m.ThreadMessages) == 0 && root.Replies == 0) {
		return children
	}
	key := issue3996KeyPrefix + root.ID
	collapsed := issue3996Collapsed(m, h, root, body)
	class := "thread-root-content"
	label := issue3996Text(m, "collapse")
	if collapsed {
		class += " collapsed"
		label = issue3996Text(m, "expand")
	}
	out := []ui.Node{children[0]}
	out = append(out, html.Div(html.Props{ID: "thread-root-content", Class: class, Data: map[string]string{"collapsed": boolString(collapsed)}}, children[1:]...))
	if collapsed && len(root.Attachments) > 0 {
		out = append(out, html.P(html.Props{Class: "thread-root-note", Dir: "auto", Text: issue3996Text(m, "attachments")}))
	}
	out = append(out, html.Button(html.Props{Class: "thread-root-toggle", ID: "thread-root-toggle", Type: "button", Text: label,
		Data: map[string]string{"action": "details-group", "id": key},
		Aria: map[string]string{"expanded": boolString(!collapsed), "controls": "thread-root-content"}}))
	return out
}

// Issue3996Styles clips a collapsed root to a few lines with a fade, and gives
// the toggle a 44px target on touch screens.
const Issue3996Styles = `.thread-root-content.collapsed{max-height:min(8.5em,24vh);overflow:hidden;position:relative;-webkit-mask-image:linear-gradient(to bottom,#000 60%,transparent);mask-image:linear-gradient(to bottom,#000 60%,transparent)}` +
	`.thread-root-content.collapsed img,.thread-root-content.collapsed video{max-height:6em;width:auto}` +
	`.thread-root-note{margin:4px 0 0;color:var(--muted);font-size:.75rem}` +
	`.thread-root-toggle{border:0;background:none;padding:2px 0;margin-block-start:4px;color:var(--accent);font:inherit;font-size:.8125rem;font-weight:600;cursor:pointer;text-align:start}` +
	`@media (hover:none),(max-width:760px){.thread-root-toggle{min-height:44px;padding-inline:4px}}`
