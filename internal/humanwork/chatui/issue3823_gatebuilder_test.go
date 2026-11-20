package chatui

import (
	stdhtml "html"
	"regexp"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

func issue3823Owner(locale, mode string) GateView {
	v := chatgateViewFixture()
	v.Locale, v.Administrator, v.ChannelName = locale, true, "sales"
	d := v.Gate.Versions[0]
	d.Mode = mode
	v.Gate.Versions = []chatgate.Definition{d}
	v.Directory = map[string][]GateChoice{"person": {{ID: "p1", Label: "Fatima Khan"}}}
	return v
}

// TestIssue3823_OwnerHeadingNamesTheChannel: the owner's page is "Joining
// questions for #sales" with a way back to the channel, not "Gate".
func TestIssue3823_OwnerHeadingNamesTheChannel(t *testing.T) {
	want := map[string]string{"en-US": "Joining questions for #sales", "de-DE": "Beitrittsfragen für #sales", "ar": "أسئلة الانضمام إلى #sales"}
	for _, locale := range chatgateLocales {
		markup := stdhtml.UnescapeString(gateMarkup(t, issue3823Owner(locale, "review")))
		heads := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "h2" })
		if len(heads) == 0 || renderNodeText(heads[0]) != want[locale] {
			t.Errorf("%s: heading is not %q: %s", locale, want[locale], markup)
		}
		if regexp.MustCompile(`>Gate<`).MatchString(markup) || strings.Contains(markup, `aria-label="Gate"`) {
			t.Errorf("%s: the page is still called Gate", locale)
		}
		crumbs := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "nav" })
		if len(crumbs) != 1 {
			t.Fatalf("%s: %d breadcrumbs", locale, len(crumbs))
		}
		back := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-gate-action") == "close" && strings.Contains(chatPolishAttr(n, "aria-label"), "#sales")
		})
		if len(back) != 1 {
			t.Errorf("%s: no way back to #sales in the breadcrumb", locale)
		}
	}
}

// TestIssue3823_RulesEditorOnlyForRuleMode: the rules editor is shown only when
// people are let in by rule.
func TestIssue3823_RulesEditorOnlyForRuleMode(t *testing.T) {
	for _, locale := range chatgateLocales {
		for _, mode := range []string{"automatic", "review"} {
			if markup := gateMarkup(t, issue3823Owner(locale, mode)); strings.Contains(markup, "gate-rule-field") {
				t.Errorf("%s/%s: the rules editor is shown", locale, mode)
			}
		}
		if markup := gateMarkup(t, issue3823Owner(locale, "rule")); !strings.Contains(markup, "gate-rule-field") {
			t.Errorf("%s: the rules editor is missing in rule mode", locale)
		}
	}
}

// TestIssue3823_PlainFieldNames: no developer terms, and no two controls with
// the same visible label.
func TestIssue3823_PlainFieldNames(t *testing.T) {
	for _, locale := range chatgateLocales {
		v := issue3823Owner(locale, "review")
		v.ConsumerNames = map[string]string{"payroll": "Payroll"}
		markup := stdhtml.UnescapeString(gateMarkup(t, v))
		if locale == "en-US" {
			for _, old := range []string{"Data class", "Named consumers", "Question kind", ">Purpose<", "Sample answers</label>"} {
				if strings.Contains(markup, old) {
					t.Errorf("developer wording %q is still shown", old)
				}
			}
		}
		seen := map[string]int{}
		for _, l := range chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "label" && chatPolishAttr(n, "for") != "" }) {
			id := chatPolishAttr(l, "for")
			if strings.HasPrefix(id, "gate-editor-") {
				id = "q-" + id[strings.LastIndex(id, "-")+1:]
			}
			seen[id+"|"+strings.TrimSpace(chatmodVisible(renderNodeText(l)))]++
		}
		texts := map[string]string{}
		for key := range seen {
			parts := strings.SplitN(key, "|", 2)
			if other, dup := texts[parts[1]]; dup && other != parts[0] && !strings.HasPrefix(parts[0], "q-") {
				t.Errorf("%s: two controls are both labelled %q", locale, parts[1])
			}
			texts[parts[1]] = parts[0]
		}
		sample := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "label" && chatPolishAttr(n, "for") == "gate-sample-person" })
		summary := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-chat-disclosure-toggle") != ""
		})
		if len(sample) != 1 || len(summary) == 0 || renderNodeText(sample[0]) == renderNodeText(summary[0]) {
			t.Errorf("%s: the preview person select and the sample form share a label", locale)
		}
		if !strings.Contains(markup, "Fatima Khan") {
			t.Errorf("%s: the person list lost its full name", locale)
		}
	}
}

func renderNodeText(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// TestIssue3823_FullNames: the directory gives a full name, not the first name
// a person goes by.
func TestIssue3823_FullNames(t *testing.T) {
	for _, tc := range []struct{ preferred, legal, want string }{
		{"Fatima", "Fatima Khan", "Fatima Khan"},
		{"Linh", "Linh Tran Nguyen", "Linh Tran Nguyen"},
		{"Bob", "Robert Smith", "Bob Smith"},
		{"Fatima", "", "Fatima"},
		{"", "Fatima Khan", "Fatima Khan"},
		{" ", " ", ""},
		{"Madonna", "Madonna", "Madonna"},
	} {
		if got := GateFullName(tc.preferred, tc.legal); got != tc.want {
			t.Errorf("GateFullName(%q, %q) = %q, want %q", tc.preferred, tc.legal, got, tc.want)
		}
	}
}
