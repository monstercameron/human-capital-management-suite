package chatui

import (
	"regexp"
	"strings"
	"sync"
	"testing"
)

// These tests compute what the shipped stylesheet gives an element at a given
// width (s11Winner: which rule applies at that width, then specificity, then
// order) instead of looking for a string in the stylesheet. A rule that is in
// the sheet but loses, or that the browser drops as invalid, fails here.

var (
	s11SheetOnce  sync.Once
	s11SheetRules []s11Rule
)

func s11Rules() []s11Rule {
	s11SheetOnce.Do(func() { s11SheetRules = s11ParseRules(Stylesheet) })
	return s11SheetRules
}

func s11Value(t *testing.T, env s11Env, prop string, match func(string) bool) string {
	t.Helper()
	value, _ := s11Winner(s11Rules(), env, prop, match)
	return value
}

func s11Without(sel string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(sel, part) {
			return false
		}
	}
	return true
}

// s11ComposerState is a composer's focus and content: whether the field is
// unfocused and whether it is empty. A rule written for a state the composer is
// not in does not apply.
type s11ComposerState struct{ unfocused, empty bool }

// s11InState reports whether selector, which may carry :not(:focus-within) and
// :has(.composer-input:placeholder-shown), applies in the state. A selector with
// any other state condition is left out (it does not describe a plain composer).
func s11InState(sel string, state s11ComposerState) bool {
	const notFocused, isEmpty = ":not(:focus-within)", ":has(.composer-input:placeholder-shown)"
	core := strings.ReplaceAll(strings.ReplaceAll(sel, notFocused, ""), isEmpty, "")
	if strings.Contains(core, ":focus") || strings.Contains(core, "placeholder-shown") || strings.Contains(core, ":has(") || strings.Contains(core, ":hover") {
		return false
	}
	if strings.Contains(sel, notFocused) && !state.unfocused {
		return false
	}
	if strings.Contains(sel, isEmpty) && !state.empty {
		return false
	}
	return true
}

// TestTodo_CHATUX_018_Cascade: every rule in the sheet is one a browser keeps. A
// comma after a closing brace made the filter panel's control sizes a rule with
// an empty selector, which is dropped whole, so selects and buttons kept their
// own larger type. The type scale is then read from the cascade.
func TestTodo_CHATUX_018_Cascade(t *testing.T) {
	for _, rule := range s11Rules() {
		if strings.TrimSpace(rule.selector) == "" {
			t.Fatalf("a rule with an empty selector is dropped by the browser: {%s}", rule.body)
		}
	}
	if strings.Contains(Stylesheet, "},") {
		t.Fatal(`the stylesheet has "}," : a selector list that starts with a comma is invalid and the rule is dropped`)
	}
	phone, desktop := s11Env{width: 390}, s11Env{width: 1440}
	for _, env := range []s11Env{phone, desktop} {
		classes := []string{"chatmod-intro", "chatmod-hint", "chatmod-state", "chatmod-desc", "chatmod-name", "chatmod-label", "chatmod-switch-text", "chatmod-status", "chatmod-error", "chatmod-outcome"}
		for _, class := range classes {
			class := class
			got := s11Value(t, env, "font-size", func(sel string) bool {
				return strings.HasSuffix(sel, "."+class) || strings.HasPrefix(sel, ".chat-workspace .chat-details .chatmod :is(") && strings.Contains(sel, "."+class)
			})
			if got != ".75rem" {
				t.Errorf("%dpx: .%s in the details panel is %q, want the panel's .75rem", env.width, class, got)
			}
		}
		for tag, want := range map[string]string{"h4": ".8125rem", "h5": ".75rem"} {
			tag := tag
			if got := s11Value(t, env, "font-size", func(sel string) bool { return strings.HasSuffix(sel, " "+tag) && strings.Contains(sel, "chatmod") }); got != want {
				t.Errorf("%dpx: %s is %q, want %q", env.width, tag, got, want)
			}
		}
		for _, control := range []string{"select", "button", "input", "textarea"} {
			control := control
			got := s11Value(t, env, "font-size", func(sel string) bool {
				return sel == control || strings.HasSuffix(sel, " "+control) || strings.HasSuffix(sel, ".chatmod-"+control) || strings.HasSuffix(sel, ".chatmod-primary") || strings.HasSuffix(sel, ".chatmod-link") ||
					strings.Contains(sel, ":where(input,select,textarea,button)") && strings.HasPrefix(sel, ".chat-workspace .chat-details .chatmod")
			})
			if got != ".75rem" {
				t.Errorf("%dpx: %s in the filter panel is %q, want .75rem", env.width, control, got)
			}
		}
	}
	// The refusal line is in the warning colour, and nothing later turns it grey.
	got := s11Value(t, phone, "color", func(sel string) bool { return strings.HasSuffix(sel, ".chatmod002-blocked") })
	if got != "var(--hcm-color-warning)" {
		t.Errorf("the blocked line's colour is %q", got)
	}
	if got := s11Value(t, phone, "text-decoration", func(sel string) bool { return strings.HasSuffix(sel, ".chatmod002-word") }); !strings.Contains(got, "underline") {
		t.Errorf("the word in the draft is not underlined: %q", got)
	}
}

// TestTodo_CHATBUG_016_Cascade: at phone width the open panel is laid over the
// whole conversation above the composer, and from 761 px it is a column of the
// grid.
func TestTodo_CHATBUG_016_Cascade(t *testing.T) {
	side := func(sel string) bool { return sel == ".chat-side" }
	composer := func(sel string) bool { return sel == ".chat-composer" }
	for _, width := range []int{320, 390, 600, 760} {
		env := s11Env{width: width}
		if got := s11Value(t, env, "position", side); got != "absolute" {
			t.Errorf("%dpx: the panel is %q, want it laid over the conversation", width, got)
		}
		if got := s11Value(t, env, "inset", side); got != "0 0 0 auto" {
			t.Errorf("%dpx: the panel's inset is %q, so it does not cover the header and composer", width, got)
		}
		if got := s11Value(t, env, "width", side); got != "100%" {
			t.Errorf("%dpx: the panel is %q wide, want the full width on a phone", width, got)
		}
		over, under := s11Value(t, env, "z-index", side), s11Value(t, env, "z-index", composer)
		if over == "" || under == "" || s11Atoi(over) <= s11Atoi(under) {
			t.Errorf("%dpx: the panel's layer %q is not above the composer's %q", width, over, under)
		}
		if s11Atoi(over) >= 1199 {
			t.Errorf("%dpx: the panel's layer %q covers the drawers", width, over)
		}
	}
	if got := s11Value(t, s11Env{width: 761}, "position", side); got != "relative" {
		t.Errorf("761px: the panel is %q, want a column of the grid", got)
	}
}

func s11Atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// TestTodo_CHATBUG_017_Cascade: the actions bar of the one message being touched
// or focused is drawn inside that row on a phone (the markup test shows no
// other row has one), at any pointer.
func TestTodo_CHATBUG_017_Cascade(t *testing.T) {
	bar := func(sel string) bool {
		return strings.HasSuffix(sel, ".message-actions") && s11Without(sel, "continued", "first-of-type", "thread", "virtual-spacer", ":has(", ":hover", ":focus", ":not(", "chatmod005")
	}
	row := func(sel string) bool { return sel == ".message" }
	for _, env := range []s11Env{{width: 390}, {width: 390, coarse: true}, {width: 600}, {width: 767}, {width: 900, coarse: true}} {
		if got := s11Value(t, env, "position", bar); got != "absolute" {
			t.Errorf("%+v: the bar is %q", env, got)
		}
		if got := s11Value(t, env, "top", bar); got != "4px" {
			t.Errorf("%+v: the bar's top is %q, want inside the row (4px)", env, got)
		}
		if got := s11Value(t, env, "transform", bar); got != "none" {
			t.Errorf("%+v: the bar is moved by %q, so it straddles the row above", env, got)
		}
		if got := s11Value(t, env, "position", row); got != "relative" {
			t.Errorf("%+v: the row is %q, so the bar is placed against something else", env, got)
		}
	}
}

// TestTodo_CHATUX_011_Cascade: the composer and the header at phone width, in
// each state of the composer.
func TestTodo_CHATUX_011_Cascade(t *testing.T) {
	field := func(state s11ComposerState) func(string) bool {
		return func(sel string) bool {
			return strings.HasSuffix(sel, ".composer-input") && s11Without(sel, "thread-composer", "agent-token") && s11InState(sel, state)
		}
	}
	box := func(state s11ComposerState) func(string) bool {
		return func(sel string) bool {
			core := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(sel, ":not(:focus-within)", ""), ":has(.composer-input:placeholder-shown)", ""))
			return (core == ".chat-composer" || core == ".chat-workspace .chat-composer") && s11InState(sel, state)
		}
	}
	tools := func(state s11ComposerState) func(string) bool {
		return func(sel string) bool {
			isTools := strings.HasSuffix(sel, ".composer-tools") || strings.Contains(sel, ":is(.composer-tools,")
			return isTools && s11Without(sel, "thread-composer") && s11InState(sel, state)
		}
	}
	idle := s11ComposerState{unfocused: true, empty: true}
	focused := s11ComposerState{unfocused: false, empty: true}
	typed := s11ComposerState{unfocused: true, empty: false}
	for _, env := range []s11Env{{width: 320}, {width: 390}, {width: 390, coarse: true}, {width: 599}} {
		for name, state := range map[string]s11ComposerState{"idle": idle, "focused": focused, "typed": typed} {
			if got := s11Value(t, env, "min-height|min-block-size", field(state)); got != "40px" {
				t.Errorf("%+v %s: the field's minimum is %q, want one line (40px)", env, name, got)
			}
			if got := s11Value(t, env, "max-height|max-block-size", field(state)); got != "calc(6*1.45*.9375rem + 18px)" {
				t.Errorf("%+v %s: the field's maximum is %q, want six lines", env, name, got)
			}
		}
		if got := s11Value(t, env, "flex-direction", box(idle)); got != "row" {
			t.Errorf("%+v: the idle composer is a %q, want one row", env, got)
		}
		if got := s11Value(t, env, "flex-direction", box(focused)); got != "column" {
			t.Errorf("%+v: the focused composer is a %q, want the field over its tool row", env, got)
		}
		// CHATUX-033 supersedes the hidden tool row: the idle composer keeps the add
		// menu, mention and emoji in its one row (display:contents).
		if got := s11Value(t, env, "display", tools(idle)); got != "contents" {
			t.Errorf("%+v: the idle composer's tool row is %q, want it in the row (contents)", env, got)
		}
		for name, state := range map[string]s11ComposerState{"focused": focused, "typed": typed} {
			if got := s11Value(t, env, "display", tools(state)); got == "none" {
				t.Errorf("%+v: the %s composer's tool row is hidden", env, name)
			}
		}
		head := func(sel string) bool {
			return sel == ".conversation-header" || sel == ".chat-workspace .conversation-header"
		}
		if got := s11Value(t, env, "height", head); got != "56px" {
			t.Errorf("%+v: the channel header is %q tall, want one row (56px)", env, got)
		}
		if got := s11Value(t, env, "flex-wrap", head); got != "nowrap" {
			t.Errorf("%+v: the channel header is %q, want one row", env, got)
		}
	}
	// A wider window keeps the roomier field.
	if got := s11Value(t, s11Env{width: 1440}, "min-height|min-block-size", field(idle)); got != "60px" {
		t.Errorf("1440px: the field's minimum is %q", got)
	}
}

// TestTodo_CHATBUG_065_Cascade: at 1440, 800 and 390 px, with and without a
// touch screen, the voice and location openers are a point and every other
// control of the composer is in the row's flow, so no two share a rectangle; the
// Add menu is where voice and location are chosen.
func TestTodo_CHATBUG_065_Cascade(t *testing.T) {
	opener := func(kind string) func(string) bool {
		return func(sel string) bool {
			return strings.HasSuffix(sel, ".tool-button") && s11Without(sel, "thread-composer", "composer-format-row", "conversation-header", "format-", "chatvoice-panel") &&
				(strings.Contains(sel, kind) || !strings.Contains(sel, ">") && !strings.Contains(sel, "chatvoice-tool") && !strings.Contains(sel, "chatmap-control"))
		}
	}
	flow := func(sel string) bool {
		return strings.HasSuffix(sel, ".tool-button") && s11Without(sel, "thread-composer", "composer-format-row", "conversation-header", "format-", "chatvoice-panel", "chatvoice-tool", "chatmap-control", ">")
	}
	for _, env := range []s11Env{{width: 1440}, {width: 800}, {width: 390}, {width: 800, coarse: true}, {width: 390, coarse: true}} {
		for _, kind := range []string{"chatvoice-tool", "chatmap-control"} {
			if got := s11Value(t, env, "position", opener(kind)); got != "absolute" {
				t.Errorf("%+v %s: the opener is %q, want out of the row", env, kind, got)
			}
			for _, prop := range []string{"width|inline-size", "height|block-size", "min-width|min-inline-size", "min-height|min-block-size"} {
				if got := s11Value(t, env, prop, opener(kind)); got != "0" {
					t.Errorf("%+v %s: %s is %q, want a point", env, kind, prop, got)
				}
			}
		}
		if got := s11Value(t, env, "position", flow); got == "absolute" || got == "fixed" {
			t.Errorf("%+v: the composer's own buttons are %q, so they could overlap", env, got)
		}
		if got := s11Value(t, env, "width|inline-size", flow); got == "" || got == "0" {
			t.Errorf("%+v: the composer's own buttons are %q wide", env, got)
		}
	}
	for _, kind := range []struct {
		kind  ConversationKind
		name  string
		voice bool
	}{{DirectMessage, "Loretta Haynes", true}, {GroupChat, "Q4 hiring huddle", true}, {PublicChannel, "general", false}, {PrivateChannel, "payroll-close", false}} {
		m := chatux014Model(kind.kind, kind.name)
		m.ShowThread = false
		items := map[string]bool{}
		for _, item := range composerAddItems(m) {
			items[item.kind] = true
		}
		if items["voice"] != kind.voice {
			t.Errorf("%s: the Add menu offers voice = %v, want %v", kind.name, items["voice"], kind.voice)
		}
		if !items["location"] {
			t.Errorf("%s: the Add menu does not offer a location", kind.name)
		}
	}
}

// TestTodo_CHATBUG_060_Cascade: right-to-left, from the cascade. The badge is
// written left-to-right, so its logical inset resolves to the wrong side and the
// physical one is used; the field reserves its inline end, which is that side.
func TestTodo_CHATBUG_060_Cascade(t *testing.T) {
	// Selectors that name a direction apply only to that direction.
	forDirection := func(dir string) func(string) bool {
		other := map[string]string{"rtl": "ltr", "ltr": "rtl"}[dir]
		return func(sel string) bool {
			return !strings.Contains(sel, `[dir="`+other+`"]`) && (dir == "rtl" || !strings.Contains(sel, `[dir="rtl"]`))
		}
	}
	both := func(dir string, base func(string) bool) func(string) bool {
		in := forDirection(dir)
		return func(sel string) bool { return base(sel) && in(sel) }
	}
	env := s11Env{width: 1440}
	badge := func(sel string) bool { return strings.HasSuffix(sel, ".chat-search-shortcut") }
	search := func(sel string) bool {
		return strings.HasSuffix(sel, ".chat-search") && s11Without(sel, "member-filter", "chat-search-layer")
	}
	if got := s11Value(t, env, "left", both("rtl", badge)); got != "22px" {
		t.Errorf("rtl: the badge's left is %q, want 22px", got)
	}
	if got := s11Value(t, env, "right|inset-inline-end", both("rtl", badge)); got != "auto" {
		t.Errorf("rtl: the badge's right edge is %q, want auto", got)
	}
	if got := s11Value(t, env, "inset-inline-end|right", both("ltr", badge)); got != "22px" {
		t.Errorf("ltr: the badge's end inset is %q, want 22px", got)
	}
	// The field reserves its inline end in both directions: the right in LTR and
	// the left in RTL, which is where the badge is drawn in each.
	for _, dir := range []string{"ltr", "rtl"} {
		if got := s11Value(t, env, "padding-inline-end|padding-right", both(dir, search)); got != "64px" {
			t.Errorf("%s: the field reserves %q at its end, want 64px (the badge is 22px in and about 45px wide)", dir, got)
		}
	}
	// Directional icons mirror in right-to-left and only there.
	for _, name := range []string{"send", "reply", "chevron-right", "arrow-left", "panel-left", "chevron-left"} {
		match := func(sel string) bool {
			return strings.Contains(sel, ".icon-"+name) && !strings.Contains(sel, " ."+name)
		}
		if got := s11Value(t, env, "transform", both("rtl", match)); got != "scaleX(-1)" {
			t.Errorf("rtl: icon-%s is %q, want it mirrored", name, got)
		}
		if got := s11Value(t, env, "transform", both("ltr", match)); got == "scaleX(-1)" {
			t.Errorf("ltr: icon-%s is mirrored", name)
		}
	}
	// A message body starts where its author line does and takes its own direction.
	body := func(sel string) bool { return sel == ".message-body" || sel == ".message-body,.agent-reply-answer" }
	if got := s11Value(t, env, "text-align", body); got != "match-parent" {
		t.Errorf("the message body's alignment is %q, want match-parent", got)
	}
	m := chat4Fixture("ar", "sent", false)
	m.Direction = "rtl"
	markup := render(t, m)
	opening := regexp.MustCompile(`<[^>]*class="message-body[^"]*"[^>]*>`).FindString(markup)
	if opening == "" || !strings.Contains(opening, `dir="auto"`) {
		t.Errorf("an Arabic page's message body does not take its own direction: %q", opening)
	}
}
