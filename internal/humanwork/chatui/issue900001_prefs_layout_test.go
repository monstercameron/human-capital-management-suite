package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

func issue900001Panel(t *testing.T, locale string) *xhtml.Node {
	t.Helper()
	m := issue3789Model(locale)
	m.Preferences.QuietHours = true
	m.QuietSchedule = QuietScheduleView{Loaded: true, Revision: 2, QuietNow: true, PauseKind: QuietPausePause, PauseUntil: time.Now().Add(time.Hour)}
	m.Callbacks.SaveQuietSchedule = func(QuietScheduleEdit) {}
	m.Away = AwayView{Loaded: true, Revision: 3, Active: true, Until: issue3789Soon(), CoverID: "priya", Hidden: true, Note: "failed"}
	doc, err := xhtml.Parse(strings.NewReader(issue3789Render(t, m)))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func issue900001Walk(n *xhtml.Node, visit func(*xhtml.Node)) {
	visit(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		issue900001Walk(c, visit)
	}
}

func issue900001Attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func issue900001Classes(n *xhtml.Node) map[string]bool {
	out := map[string]bool{}
	for _, c := range strings.Fields(issue900001Attr(n, "class")) {
		out[c] = true
	}
	return out
}

// TestIssue900001_EverySectionBodyIsPaddedLikeQuietHours: the pieces under the
// Schedule and Away headings are direct children of their section (so the one
// section rule reaches them), and that rule gives them the same 12px inline
// inset the section heading has. Before the fix nothing padded them and they
// sat flush against the popover's edge.
func TestIssue900001_EverySectionBodyIsPaddedLikeQuietHours(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		doc := issue900001Panel(t, locale)
		seen := map[string]int{}
		issue900001Walk(doc, func(n *xhtml.Node) {
			id := issue900001Attr(n, "data-prefs-section")
			if n.Type != xhtml.ElementNode || (id != "quiet-schedule" && id != "away") {
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != xhtml.ElementNode || issue900001Classes(c)["chat-prefs-head"] {
					continue
				}
				seen[id]++
				switch c.Data {
				case "p", "div", "form", "button":
				default:
					t.Errorf("%s: %s: unexpected <%s> under the section", locale, id, c.Data)
				}
			}
		})
		if seen["quiet-schedule"] < 4 || seen["away"] < 5 {
			t.Fatalf("%s: the sections hold fewer pieces than expected: %v", locale, seen)
		}
	}
	const section = `.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] :is([data-prefs-section="quiet-schedule"],[data-prefs-section="away"],[data-prefs-section="composer-enter"])>`
	for _, want := range []string{
		section + `:not(.chat-prefs-head):not(button){box-sizing:border-box;margin:0;padding-inline:12px;padding-block-end:8px}`,
		section + `button.away-toggle{box-sizing:border-box;inline-size:calc(100% - 24px);margin-inline:12px;margin-block:0 8px;text-align:start}`,
	} {
		if !strings.Contains(Issue900001Styles, want) {
			t.Errorf("missing rule %q", want)
		}
	}
	if !strings.Contains(Stylesheet, Issue900001Styles) {
		t.Error("the preferences layout styles are not part of the page stylesheet")
	}
}

// TestIssue900001_OptionGroupsLayOutAsPills: choice groups lay out as one row
// of equal pills (an auto-fit grid) instead of one button per line, and the
// pause title spans the whole row.
func TestIssue900001_OptionGroupsLayOutAsPills(t *testing.T) {
	for _, want := range []string{
		`:is(.chat-prefs-choices,.quiet-pause){display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,96px),1fr));gap:6px`,
		`.quiet-pause{grid-template-columns:repeat(auto-fit,minmax(min(100%,72px),1fr))}`,
		`:is(.chat-prefs-choices,.quiet-pause)>:is(.chat-prefs-choice,.button){justify-content:center;text-align:center`,
		`min-block-size:44px`,
		`.quiet-pause-title{grid-column:1/-1`,
	} {
		if !strings.Contains(Issue900001Styles, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// TestIssue900001_RulesStayInsideTheSectionsTheyFix: the inset and spacing rules
// name the Schedule, Away and Composer sections. A bare ".chat-prefs-section>"
// rule also reached the Reading language form and the other sections, whose own
// rows already carry the 12px inset, and indented their labels twice.
func TestIssue900001_RulesStayInsideTheSectionsTheyFix(t *testing.T) {
	if strings.Contains(Issue900001Styles, `.chat-prefs-section`) {
		t.Error("a rule reaches every preferences section; scope it to data-prefs-section")
	}
	for _, id := range []string{"reading-languages", "voice", "emoji-tone", "heated-messages", "quiet-hours"} {
		if strings.Contains(Issue900001Styles, `"`+id+`"`) {
			t.Errorf("the layout rules reach the %s section, which already has its own padding", id)
		}
	}
}

// TestIssue900001_PopoverNeverRunsPastTheViewport: the panel is bounded by the
// viewport in CSS itself (not only by the placement script) and scrolls inside.
func TestIssue900001_PopoverNeverRunsPastTheViewport(t *testing.T) {
	for _, want := range []string{
		`[data-chat-layer="chat-prefs"]{box-sizing:border-box;max-block-size:calc(100vh - 16px);max-block-size:calc(100dvh - 16px);overflow-x:hidden;overflow-y:auto`,
		`overscroll-behavior:contain`,
	} {
		if !strings.Contains(Issue900001Styles, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, bad := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right", " left:", " right:", "min-inline-size:340px"} {
		if strings.Contains(Issue900001Styles, bad) {
			t.Errorf("styles use %q", bad)
		}
	}
}
