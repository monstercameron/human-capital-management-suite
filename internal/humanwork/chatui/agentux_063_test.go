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
