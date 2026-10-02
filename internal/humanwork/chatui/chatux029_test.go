package chatui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_029: the stylesheet the product serves gives the details
// panel one row shape, one group label and one list shape. Each rule below is
// the fix for a defect seen on the page (second refinement round, CHATUX-029).
func TestTodo_CHATUX_029(t *testing.T) {
	sheet := chatui.ScopedStylesheet()
	for _, rule := range []struct{ why, css string }{
		{"1 notifications: one compact row per choice, radio and label on one line",
			"label.details-notify-option{display:flex;align-items:center;gap:8px;margin:0;min-block-size:32px"},
		{"1 notifications: the radio is a 16 px square with no margin of its own",
			".details-notify-radio{flex:none;inline-size:16px;block-size:16px"},
		{"2 an open row is not tinted, hovered or not",
			".manage-sec-row[aria-expanded=true],.manage-sec-row[aria-expanded=true]:hover{background:transparent}"},
		{"2 an open agent row is not tinted either",
			".chat-details .chat-disclosure-button[aria-expanded=true],.chat-details .chat-disclosure-button[aria-expanded=true]:hover{background:transparent}"},
		{"3 a group label is the quiet small label with 16 px above and 6 px below",
			".details-manage .manage-caption{display:block;margin:16px 0 6px;padding:0 8px;font-size:.75rem;font-weight:500"},
		{"4 the joining row has the row shape and an arrow at the end",
			"a[data-gate-open]{display:flex;align-items:center;gap:8px;min-block-size:36px"},
		{"4 the joining arrow turns over in right-to-left",
			"[dir=rtl] .chat-details .details-section>a[data-gate-open]::after"},
		{"5 Manage channel is a header in the panel's section-title style",
			".details-manage>.details-group>.manage-sec-row{min-block-size:40px;padding:0;font-weight:600"},
		{"5 a row is 36 px",
			".chat-details .manage-sec-row{min-block-size:36px"},
		{"6 role labels: the label field is 140 px and 32 px high, the name takes the rest",
			".manage-role-field{flex:0 0 140px;inline-size:140px"},
		{"6 role labels: a 32 px field", ".manage-role-field input:not([type=checkbox]):not([type=radio]){min-block-size:32px;block-size:32px"},
		{"6 role labels: a 36 px row", ".manage-role-row{display:flex;align-items:center;gap:8px;min-block-size:36px}"},
		{"6 role labels: the name wraps to two lines before it is cut", "-webkit-line-clamp:2;line-clamp:2;line-height:1.25"},
		{"7 word lists: one 36 px row each, name left and switch right",
			".chatmod-row-main{flex-wrap:nowrap;justify-content:space-between;gap:8px;min-block-size:36px"},
		{"7 word lists: the state is said once, by the switch",
			".chatmod-row>.chatmod-state{position:absolute"},
		{"7 word lists: language headings are the quiet group label",
			".chatmod-group>h5{margin:12px 0 2px;padding:0 8px;font-size:.75rem;font-weight:500"},
	} {
		if !strings.Contains(sheet, rule.css) {
			t.Errorf("%s: the served stylesheet lacks %s", rule.why, rule.css)
		}
	}
	// The stylesheet gives every button a 24 px minimum height at a higher
	// specificity than one class, so a row that must be taller names the panel.
	if !regexp.MustCompile(`\.chat-details \.manage-sec-row\{min-block-size:36px`).MatchString(sheet) {
		t.Error("the row height does not name .chat-details, so a 24 px button rule wins")
	}
	// No rule that tints a row while it is open survives in the panel's styles.
	for _, rule := range regexp.MustCompile(`[^{}]*\[aria-expanded=true\][^{}]*\{[^}]*background:var\(--soft\)[^}]*\}`).FindAllString(sheet, -1) {
		if strings.Contains(rule, "manage-sec") || strings.Contains(rule, "chatstate-section") {
			t.Errorf("a rule tints an open row: %s", rule)
		}
	}
}

// TestTodo_CHATUX_029_Browser: the page as served in English, German and
// Arabic. Notifications are three labelled radios in one group, the group
// labels and rows carry the classes the sheet styles, and the joining row is a
// row of its own that names what it opens.
func TestTodo_CHATUX_029_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		page := chatux027Page(t, locale, false)
		lane3NoLeaks(t, locale+" details", page)
		// 1: three radios, each inside its label, in one group.
		options := regexp.MustCompile(`<label class="details-notify-option[^"]*">\s*<input[^>]*type="radio"[^>]*>\s*<span class="details-notify-text">[^<]+</span>`).FindAllString(page, -1)
		if len(options) != 3 {
			t.Errorf("%s: %d notification choices with the radio inside its label, want 3", locale, len(options))
		}
		if !strings.Contains(page, `role="radiogroup"`) {
			t.Errorf("%s: the notification choices are not one radio group", locale)
		}
		// 3 and 5: Manage channel is one header, its groups carry a caption each.
		block := chatux027Block(t, page)
		if strings.Count(block, `class="manage-caption"`) < 2 {
			t.Errorf("%s: Manage channel has fewer than two group labels", locale)
		}
		if got := strings.Count(page, `data-details-group="manage"`); got != 1 {
			t.Errorf("%s: %d Manage channel headers, want 1", locale, got)
		}
	}
}
