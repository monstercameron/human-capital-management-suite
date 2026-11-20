package chatui

import (
	"regexp"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue #3935: the message renderer only makes a link of an http(s) address or
// an in-app path, so "[plan](example.com)" and the inserted "[plan](url)" show
// as plain text. The preview names each such link while the draft can still be
// edited; the draft itself is never rewritten and nothing unsafe is linked.

// composerLinkProblem is one link in a draft whose destination will not render.
type composerLinkProblem struct {
	Label string // the visible text, kept as typed
	Kind  string // "empty" (placeholder or blank), "web" (a bare address), "blocked"
	Fix   string // the explicit https:// form for a bare address, else empty
}

const (
	issue3935KeyEmpty   = "chat.composer.link_empty"
	issue3935KeyWeb     = "chat.composer.link_web"
	issue3935KeyBlocked = "chat.composer.link_blocked"
	issue3935KeyEdit    = "chat.composer.link_edit"
)

var (
	composerLinkPattern = regexp.MustCompile(`\[([^\[\]\n]*)\]\(([^()\s]*(?:\([^()\s]*\)[^()\s]*)*)\)`)
	composerCodePattern = regexp.MustCompile("(?s)```.*?```|`[^`\n]*`")
	composerHostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\.)+[A-Za-z]{2,}(?::\d+)?(?:[/?#]\S*)?$`)
)

// composerLinkProblems lists the links in draft that will not render as links.
// Code spans are skipped: link syntax in them is meant as text.
func composerLinkProblems(draft string) []composerLinkProblem {
	if !strings.Contains(draft, "](") {
		return nil
	}
	draft = composerCodePattern.ReplaceAllString(draft, " ")
	var out []composerLinkProblem
	for _, match := range composerLinkPattern.FindAllStringSubmatch(draft, -1) {
		label, dest := strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
		if label == "" {
			label = dest
		}
		if _, ok := safeMarkdownHref(dest); ok {
			continue
		}
		problem := composerLinkProblem{Label: label, Kind: "blocked"}
		switch {
		case dest == "" || dest == "url":
			problem.Kind = "empty"
		case composerHostPattern.MatchString(dest):
			problem.Kind, problem.Fix = "web", "https://"+dest
		}
		out = append(out, problem)
	}
	return out
}

var issue3935Copy = map[string]map[string]string{
	"en-US": {
		issue3935KeyEmpty:   "The link \"{label}\" has no address yet, so it will send as plain text. Edit the message and replace the placeholder with a web address.",
		issue3935KeyWeb:     "The link \"{label}\" needs a full address to work. Edit the message and use {fix}.",
		issue3935KeyBlocked: "The link \"{label}\" points somewhere that cannot be opened from a message, so it will send as plain text. Edit the message and use a web address starting with https://.",
		issue3935KeyEdit:    "Edit message",
	},
	"de-DE": {
		issue3935KeyEmpty:   "Der Link „{label}“ hat noch keine Adresse und wird als einfacher Text gesendet. Bearbeite die Nachricht und ersetze den Platzhalter durch eine Webadresse.",
		issue3935KeyWeb:     "Der Link „{label}“ braucht eine vollständige Adresse. Bearbeite die Nachricht und verwende {fix}.",
		issue3935KeyBlocked: "Der Link „{label}“ führt zu einem Ziel, das sich aus einer Nachricht nicht öffnen lässt, und wird als einfacher Text gesendet. Bearbeite die Nachricht und verwende eine Webadresse, die mit https:// beginnt.",
		issue3935KeyEdit:    "Nachricht bearbeiten",
	},
	"ar": {
		issue3935KeyEmpty:   "الرابط «{label}» ليس له عنوان بعد، لذلك سيُرسل كنص عادي. عدّل الرسالة واستبدل العنصر النائب بعنوان ويب.",
		issue3935KeyWeb:     "يحتاج الرابط «{label}» إلى عنوان كامل ليعمل. عدّل الرسالة واستخدم {fix}.",
		issue3935KeyBlocked: "الرابط «{label}» يشير إلى وجهة لا يمكن فتحها من رسالة، لذلك سيُرسل كنص عادي. عدّل الرسالة واستخدم عنوان ويب يبدأ بـ https://.",
		issue3935KeyEdit:    "تعديل الرسالة",
	},
}

func issue3935Text(m Model, key string) string {
	return chatbug039Text(key, issue3935Copy[chatEmojiLocale(m.Locale)][key], issue3935Copy["en-US"][key])
}

// composerLinkNote is the preview's warning: one line per link that will not
// render, announced politely, with a button back to the editable field.
func composerLinkNote(m Model, draft, target string) ui.Node {
	problems := composerLinkProblems(draft)
	if len(problems) == 0 {
		return nil
	}
	lines := make([]ui.Node, 0, len(problems))
	for _, p := range problems {
		key := issue3935KeyBlocked
		switch p.Kind {
		case "empty":
			key = issue3935KeyEmpty
		case "web":
			key = issue3935KeyWeb
		}
		text := strings.NewReplacer("{label}", p.Label, "{fix}", p.Fix).Replace(issue3935Text(m, key))
		lines = append(lines, html.P(html.Props{Class: "composer-link-note-line", Dir: "auto", Text: text}))
	}
	lines = append(lines, html.Button(html.Props{Class: "composer-link-note-edit", Type: "button",
		Data: map[string]string{"action": "composer-link-edit", "id": target}, Text: issue3935Text(m, issue3935KeyEdit)}))
	return html.Div(html.Props{Class: "composer-link-note", Role: "status", Dir: agentReplyDirection(m.Locale),
		Aria: map[string]string{"live": "polite"}}, lines...)
}

// issue3935Styles sets the preview warning apart from the message text. The
// button is at least 44 px tall on a touch screen; the layout uses logical
// properties so it holds in right-to-left locales.
const issue3935Styles = `.chat-workspace .composer-link-note{margin-block-end:8px;padding:8px 10px;border-inline-start:3px solid var(--warning);background:color-mix(in srgb,var(--warning) 10%,transparent);border-radius:6px;font-size:.8125rem;color:var(--ink)}` +
	`.chat-workspace .composer-link-note-line{margin:0 0 4px}` +
	`.chat-workspace .composer-link-note-edit{margin-block-start:4px;min-block-size:32px;padding-inline:10px;border:1px solid var(--line,currentColor);border-radius:6px;background:transparent;color:inherit;font:inherit;cursor:pointer}` +
	`@media(pointer:coarse){.chat-workspace .composer-link-note-edit{min-block-size:44px;min-inline-size:44px}}`

// issue3935EditMessage leaves the preview for the editable field and puts the
// caret back in it, so a keyboard user can fix the destination at once. The
// draft is untouched.
func issue3935EditMessage(local localStore, target string) {
	if target == threadComposerID {
		local.update(func(u *localUI) { u.threadComposerPreview = false })
	} else {
		local.update(func(u *localUI) { u.composerPreview = false })
	}
	focusField(target)
}
