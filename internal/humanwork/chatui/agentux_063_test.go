package chatui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// agentux063Surfaces renders the places Chat draws controls in: a channel with
// its thread, details, to-do list, poll and message menu open, an agent's
// conversation, and the layers that open from the composer. Each entry is the
// markup of one surface.
func agentux063Surfaces(t *testing.T, locale string, width int) map[string]string {
	t.Helper()
	surfaces := map[string]string{}
	channel := chat4Fixture(locale, "answered", false)
	channel.ShowDetails = true
	surfaces["channel and details"] = renderAgentUXChat3Node(t, Build(channel), width)
	thread := chat4Fixture(locale, "answered", false)
	thread.ShowThread, thread.ThreadParentID, thread.PickerID = true, "question", "question"
	surfaces["thread and reaction picker"] = renderAgentUXChat3Node(t, Build(thread), width)
	menu := chat4Fixture(locale, "answered", false)
	menu.MenuID = "question"
	surfaces["message menu"] = renderAgentUXChat3Node(t, Build(menu), width)
	direct := chat4Fixture(locale, "answered", true)
	surfaces["agent conversation"] = renderAgentUXChat3Node(t, Build(direct), width)
	working := chat4Fixture(locale, "working15", false)
	surfaces["working card"] = renderAgentUXChat3Node(t, Build(working), width)
	failed := chat4Fixture(locale, "failed", false)
	surfaces["failed card"] = renderAgentUXChat3Node(t, Build(failed), width)
	for _, kind := range []string{"todo", "poll"} {
		surfaces["tray "+kind] = renderAgentUXChat3Node(t, channelTray(channel, handlers{}, kind), width)
	}
	surfaces["search layer"] = renderAgentUXChat3Node(t, chatSearchLayer(channel, handlers{local: localUI{searchOpen: true}}), width)
	surfaces["active row"] = renderAgentUXChat3Node(t, message(channel, handlers{local: localUI{focusRow: "question"}}, channel.Messages[0], false), width)
	return surfaces
}

// agentux063Name is the accessible name of a control as a person using a screen
// reader would get it: its aria-label, the text it holds, the label bound to it
// or, last, its title.
func agentux063Name(n *xhtml.Node, labels map[string]string) string {
	name := chat5Attr(n, "aria-label")
	if name == "" && n.Data != "input" && n.Data != "textarea" && n.Data != "select" {
		name = chat5NodeText(n)
	}
	if name == "" {
		name = labels[chat5Attr(n, "id")]
	}
	if name == "" {
		name = chat5Attr(n, "title")
	}
	return strings.TrimSpace(name)
}

// agentux063Controls walks a rendered surface and calls visit for every
// control that is drawn (not hidden, not a hidden input).
func agentux063Controls(t *testing.T, markup string, visit func(n *xhtml.Node, name string)) {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	walkChat5HTML(root, func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "label" {
			labels[chat5Attr(n, "for")] = chat5NodeText(n)
		}
	})
	walkChat5HTML(root, func(n *xhtml.Node) {
		if n.Type != xhtml.ElementNode {
			return
		}
		role := chat5Attr(n, "role")
		switch n.Data {
		case "button", "input", "textarea", "select", "summary":
		case "a":
			if chat5Attr(n, "href") == "" && role == "" {
				return
			}
		default:
			if role != "button" && role != "switch" && role != "tab" && role != "menuitem" && role != "checkbox" {
				return
			}
		}
		if chat5Attr(n, "type") == "hidden" {
			return
		}
		visit(n, agentux063Name(n, labels))
	})
}

// TestTodo_AGENTUX_063 asserts over the stylesheet and the markup that every
// Chat control answers hover, pressed, focus and busy the same way from the
// motion tokens, and that reduced motion removes all of it.
func TestTodo_AGENTUX_063(t *testing.T) {
	rules := agentux058Rules(Stylesheet)

	// No duration is written as a literal: every transition and animation in
	// Chat's styles takes its time from the motion tokens. Keyframe selectors
	// and the zero of "none" are not durations.
	duration := regexp.MustCompile(`(?:^|[\s,:(])(\d*\.?\d+)(ms|s)\b`)
	for _, rule := range rules {
		for _, declaration := range strings.Split(rule[1], ";") {
			name, value, ok := strings.Cut(declaration, ":")
			name = strings.TrimSpace(name)
			if !ok || !(strings.HasPrefix(name, "transition") || strings.HasPrefix(name, "animation")) {
				continue
			}
			stripped := regexp.MustCompile(`var\([^)]*\)`).ReplaceAllString(value, "")
			// The one allowed literal: CHATUX-037's slow line stays hidden for
			// eight seconds and is then flipped visible in a millisecond. That
			// is a wait and a visibility step, not a transition, so no motion
			// token applies; any other literal still fails.
			stripped = strings.ReplaceAll(stripped, chatux037RevealWait, "")
			if duration.MatchString(stripped) {
				t.Errorf("%s{%s:%s} writes a literal duration, use --hcm-motion-fast or --hcm-motion-base", strings.TrimSpace(rule[0]), name, value)
			}
		}
	}

	// One transition for every control, in one rule, from the motion tokens: the
	// element selectors cover every button, link, summary, input and select, and
	// the classes that are not controls (rows, chips, cards) are named beside them.
	var covered string
	for _, match := range regexp.MustCompile(`([^{}]+)\{transition:background-color var\(--hcm-motion-fast\) var\(--hcm-motion-easing\)`).FindAllStringSubmatch(Stylesheet, -1) {
		covered += match[1] + ","
	}
	if covered == "" {
		t.Fatal("the shared transition is not built from the motion tokens")
	}
	transition := []string{"", covered}
	for _, want := range []string{".chat-workspace button", ".chat-workspace a", ".chat-workspace summary", ".chat-workspace input", ".chat-workspace select", ".agent-reply-row", ".mention-chip", ".chat-row", ".reaction"} {
		if !strings.Contains(transition[1], want) {
			t.Errorf("the shared transition does not cover %s", want)
		}
	}

	// Pressed and focus-visible come from the same two rules for every control.
	for _, want := range []string{
		".chat-workspace button:active,.chat-workspace summary:active{filter:brightness(.96)}",
		"@media(prefers-reduced-motion:reduce){.chat-workspace *,.chat-workspace *::before,.chat-workspace *::after{transition:none!important;animation:none!important}}",
		`:root[data-hcm-motion-preference="reduce"] .chat-workspace *`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("shared state rule missing: %s", want)
		}
	}
	// The focus ring is one colour on every control in the workspace, drawn last
	// so that the features that drew their own in the brand colour agree.
	if !strings.Contains(Stylesheet, agentux063StateStyles) {
		t.Error("the shared state styles are not part of the stylesheet")
	}
	if strings.Index(Stylesheet, agentux063StateStyles) > strings.Index(Stylesheet, ChatLane2Styles) {
		t.Error("the shared state styles moved the browse rules from after them")
	}
	if strings.Index(Stylesheet, agentux063StateStyles) < strings.Index(Stylesheet, ChatLane3Styles) {
		t.Error("the shared state styles come before the rules they override")
	}
	if !strings.Contains(agentux063StateStyles, "outline-color:var(--hcm-color-focus)") {
		t.Error("the focus ring is not drawn in the focus token")
	}
	// A disabled or busy control looks the same everywhere.
	for _, want := range []string{".chat-workspace button:disabled,.chat-workspace input:disabled", `[aria-busy="true"]{cursor:progress;opacity:.7}`} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("no shared rule: %s", want)
		}
	}
}

// TestTodo_AGENTUX_063_Accessibility walks the rendered tree of every Chat
// surface in three languages and four widths: every control has a name, an
// icon-only control also has a title tooltip, and no target is declared
// smaller than 24 by 24.
func TestTodo_AGENTUX_063_Accessibility(t *testing.T) {
	seen := map[string]bool{}
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		for surface, markup := range agentux063Surfaces(t, locale, width) {
			agentux063Controls(t, markup, func(n *xhtml.Node, name string) {
				where := fmt.Sprintf("%s/%d/%s: <%s class=%q data-action=%q id=%q>", locale, width, surface, n.Data, chat5Attr(n, "class"), chat5Attr(n, "data-action"), chat5Attr(n, "id"))
				if name == "" {
					if !seen[where] {
						seen[where] = true
						t.Errorf("%s has no accessible name", where)
					}
					return
				}
				if n.Data != "button" {
					return
				}
				// An icon-only button shows no words, so the title is its tooltip.
				visible := strings.TrimSpace(chat5NodeText(n))
				if visible == "" && chat5Attr(n, "title") == "" && chat5Attr(n, "aria-describedby") == "" {
					if !seen[where] {
						seen[where] = true
						t.Errorf("%s is icon-only and has no title tooltip", where)
					}
				}
			})
		}
	})
	// The target-size floor is a rule for every control, not a list.
	for _, want := range []string{".chat-workspace button,.chat-workspace input,.chat-workspace select,.chat-workspace summary{min-height:24px}", ".chat-workspace button{min-width:24px}"} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("target floor missing: %s", want)
		}
	}
	// A control that is declared smaller than the floor says so in a min-size
	// declaration; the floor rule above sets 24 and a feature can lower it again.
	// Each such declaration must have a later rule that restores 24: the switches
	// and the Saved list's row actions did, and carry theirs in
	// agentux063StateStyles.
	small := regexp.MustCompile(`(?:^|;)\s*(?:min-height|min-width|min-block-size|min-inline-size)\s*:\s*(\d+)px`)
	subjectOf := func(selector string) string {
		fields := strings.Fields(strings.TrimSpace(selector))
		if len(fields) == 0 {
			return ""
		}
		return fields[len(fields)-1]
	}
	for _, rule := range agentux058Rules(Stylesheet) {
		for _, found := range small.FindAllStringSubmatch(rule[1], -1) {
			var px int
			fmt.Sscan(found[1], &px)
			if px == 0 || px >= 24 {
				continue
			}
			subject := subjectOf(strings.Split(rule[0], ",")[0])
			if strings.Contains(subject, ".chat-icon") || strings.Contains(rule[0], "::") || strings.Contains(rule[1], "clip") {
				continue
			}
			// A count, a badge and a waveform bar are labels inside a control, not
			// targets of their own.
			if regexp.MustCompile(`count|badge|waveform`).MatchString(rule[0]) {
				continue
			}
			// These three were declared 22 high and are restored below.
			if strings.Contains(rule[0], ".switch") || strings.Contains(rule[0], ".chatmod-switch-track") || strings.Contains(rule[0], ".chatsave-actions .message-action") {
				continue
			}
			t.Errorf("a control is given a minimum size of %dpx: %s{%s}", px, strings.TrimSpace(rule[0]), rule[1])
		}
	}
	for _, want := range []string{
		":root .chat-workspace :is(.switch,.chatmod-switch-track){block-size:24px;min-block-size:24px}",
		".chat-workspace .chatsave-actions .message-action{height:24px;min-height:24px}",
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("the 24px floor is not restored: %s", want)
		}
	}
}

// TestTodo_AGENTUX_063_Performance: a 111-message channel renders under 400
// interactive elements, with a message's action bar present only for the row
// that is hovered or focused, and every other row keeps the same box.
func TestTodo_AGENTUX_063_Performance(t *testing.T) {
	m := chat4Fixture("en-US", "sent", false)
	m.Messages, m.PersonaInvocations = nil, nil
	for i := 0; i < 111; i++ {
		m.Messages = append(m.Messages, Message{ID: fmt.Sprint(i), AuthorID: "person", Author: "Person", Body: "Message", Replies: i % 3})
	}
	idle := render(t, m)
	if n := integrate1VisibleControls(idle); n >= 400 {
		t.Fatalf("an idle 111-message channel renders %d interactive elements", n)
	}
	if strings.Count(idle, `class="message-actions"`) != 0 {
		t.Fatal("an idle channel renders action bars")
	}
	one := renderNode(t, message(m, handlers{local: localUI{pointerRow: "5"}}, m.Messages[5], false))
	other := renderNode(t, message(m, handlers{local: localUI{pointerRow: "5"}}, m.Messages[6], false))
	if !strings.Contains(one, `class="message-actions"`) || strings.Contains(other, `class="message-actions"`) {
		t.Fatalf("the action bar is not drawn for exactly the active row")
	}
	// Keyboard: the row itself takes focus, and focus shows its bar.
	if !strings.Contains(other, `tabindex="0"`) && !strings.Contains(other, `tabindex="-1"`) {
		t.Error("a message row cannot be reached from the keyboard, so its actions could never be asked for")
	}
	if chatRowActionsVisible(localUI{pointerRow: "5"}, "6") || !chatRowActionsVisible(localUI{focusRow: "6"}, "6") {
		t.Error("hover or focus state does not decide which row shows actions")
	}
}

// chatux037RevealWait is the animation timing of the slow loading line, the only
// duration literal Chat's styles may carry.
const chatux037RevealWait = " 1ms linear 8s"

// TestAgentUX063_DraftPreview: the composer's Preview toggle shows the draft
// as formatted text in the draft's own slot, so Bold's ** markers read as
// bold before anything is sent. The toolbar carries no "Markdown" word
// (CHATBUG-003), so the preview is the formatting's explanation.
func TestAgentUX063_DraftPreview(t *testing.T) {
	draft := "**bold** and [guide](https://example.org/guide)"
	previewModel := func() Model {
		m := chat4Fixture("en-US", "sent", false)
		m.Draft = draft
		m.Callbacks.SendMessage = func(string, string) {}
		return m
	}
	// Editing: the field holds the draft and the toggle is off.
	edit := renderNode(t, composer(previewModel(), handlers{}))
	if !strings.Contains(edit, "<textarea") || !strings.Contains(edit, `data-action="composer-preview-toggle"`) || !strings.Contains(edit, `aria-pressed="false"`) {
		t.Fatalf("the composer has no Preview toggle, or it starts on: %s", edit)
	}
	if strings.Contains(edit, "composer-preview-body") {
		t.Fatalf("the preview is drawn while editing: %s", edit)
	}
	// Preview: the same slot shows the draft formatted, with no field and no
	// dangling label for one.
	preview := renderNode(t, composer(previewModel(), handlers{local: localUI{composerPreview: true}}))
	if agentux063HasExactAttr(preview, `id="chat-composer"`) {
		t.Fatalf("the field is still drawn under the preview: %s", preview)
	}
	for _, want := range []string{`class="composer-draft"`, "composer-preview-body", "<strong>bold</strong>", `href="https://example.org/guide"`, `aria-pressed="true"`} {
		if !strings.Contains(preview, want) {
			t.Fatalf("the preview is missing %q: %s", want, preview)
		}
	}
	if strings.Contains(preview, "**bold**") || agentux063HasExactAttr(preview, `for="chat-composer"`) {
		t.Fatalf("raw marks or a label for a missing field: %s", preview)
	}
	// The thread's reply field previews the same way from its own draft.
	thread := previewModel()
	thread.ShowThread, thread.ThreadParentID = true, "root"
	thread.ThreadDrafts = map[string]string{"root": draft}
	thread.Callbacks.ReplyInThread = func(string, string) {}
	threadPreview := renderNode(t, threadPane(thread, handlers{local: localUI{threadComposerPreview: true}}))
	if agentux063HasExactAttr(threadPreview, `id="thread-composer"`) || !strings.Contains(threadPreview, "<strong>bold</strong>") {
		t.Fatalf("the thread reply does not preview: %s", threadPreview)
	}
	// The toggle and the preview are named in every language.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := previewModel()
		m.Locale = locale
		markup := renderNode(t, composer(m, handlers{local: localUI{composerPreview: true}}))
		label, tip := composerText(m, keyComposerPreview), composerText(m, keyComposerPreviewTip)
		if label == "" || label == keyComposerPreview || tip == "" || tip == keyComposerPreviewTip || !strings.Contains(markup, label) {
			t.Fatalf("%s: the Preview toggle is unnamed: %s", locale, markup)
		}
	}
}

// agentux063HasExactAttr is whether the markup carries the attribute with
// exactly that value. The attribute name is matched on a word boundary
// (data-id="chat-composer" is not id="chat-composer") and the value must not
// continue into a longer id ("chat-composer-commands").
func agentux063HasExactAttr(markup, attr string) bool {
	needle := " " + attr
	for rest := markup; ; {
		index := strings.Index(rest, needle)
		if index < 0 {
			return false
		}
		rest = rest[index+len(needle):]
		if rest == "" || rest[0] != '-' {
			return true
		}
	}
}

// TestAgentUX063_HoverTint: a message row takes one shared hover tint in every
// conversation kind — the rule names the row, never the kind of room it sits
// in — and no kind-scoped rule repaints it.
func TestAgentUX063_HoverTint(t *testing.T) {
	const shared = ".message:hover,.message:focus-visible,.message:has(:focus-visible),.chat-ephemeral:hover{background:color-mix(in srgb,var(--hcm-color-text) 4%,transparent)}"
	if !strings.Contains(Stylesheet, shared) {
		t.Fatal("the shared message-row hover tint is gone")
	}
	// The shared rule wins the cascade wherever it applies, so the tint a row
	// takes is the shared one and nothing else paints over it.
	for _, selector := range []string{".message:hover", ".message:focus-visible", ".message:has(:focus-visible)", ".chat-ephemeral:hover"} {
		if got := chatbugCascadeValue(Stylesheet, selector, "background"); got != "color-mix(in srgb,var(--hcm-color-text) 4%,transparent)" {
			t.Errorf("%s paints %q, want the shared tint", selector, got)
		}
	}
	// And no other rule paints the row itself on hover or focus — not from a
	// conversation-kind ancestor, not from anywhere: the subject of every
	// row-tint rule is the bare row. Rules that paint the row's children (the
	// action bar) or its siblings (action buttons) are not the row's tint.
	for _, rule := range agentux058Rules(Stylesheet) {
		selector, body := rule[0], rule[1]
		if (!strings.Contains(selector, ":hover") && !strings.Contains(selector, ":focus-visible")) || !strings.Contains(body, "background") {
			continue
		}
		for _, part := range strings.Split(selector, ",") {
			subject := strings.TrimSpace(part)
			for _, combinator := range []string{" ", ">", "+", "~"} {
				if index := strings.LastIndex(subject, combinator); index >= 0 {
					subject = strings.TrimSpace(subject[index+1:])
				}
			}
			row, _, _ := strings.Cut(subject, ":")
			if row != ".message" && row != ".chat-ephemeral" {
				continue
			}
			switch strings.TrimSpace(part) {
			case ".message:hover", ".message:focus-visible", ".message:has(:focus-visible)", ".chat-ephemeral:hover":
			default:
				t.Errorf("a second hover tint repaints message rows: %s{%s}", strings.TrimSpace(part), body)
			}
		}
	}
	// A channel row and an agent-conversation row are the same element for the
	// tint: the rule reaches both through the shared class.
	channel := renderNode(t, message(chat4Fixture("en-US", "answered", false), handlers{}, Message{ID: "m", AuthorID: "walt", Body: "hi"}, false))
	direct := renderNode(t, message(chat4Fixture("en-US", "answered", true), handlers{}, Message{ID: "m", AuthorID: "walt", Body: "hi"}, false))
	for name, markup := range map[string]string{"channel": channel, "agent conversation": direct} {
		if !strings.Contains(markup, `class="message`) && !strings.Contains(markup, ` message `) && !strings.Contains(markup, `message `) {
			t.Errorf("%s: the row does not carry the shared message class: %s", name, markup)
		}
	}
}
