package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
)

func issue3907Model() Model {
	m := chatbug071Model()
	m.Members = append(m.Members, Member{ID: "maya", HomeTenantID: "tenant", Name: "Maya Patel"})
	m.Draft = "Please ask @Maya Patel and @Priya Shah about it"
	m.Callbacks.SendMessage = func(string, string) {}
	return m
}

// Issue 3907: Preview drew a picked mention as plain words. A resolved mention
// is now the same person chip a sent message has; a name that only resembles a
// mention stays text.
func TestIssue3907_PreviewDrawsAResolvedMentionAsAPersonChip(t *testing.T) {
	m := issue3907Model()
	refs := []ChatReference{{Kind: "PERSON_MENTION", TenantID: "tenant", ID: "maya", Display: "Maya Patel", ConversationID: "room"}}
	markup := stdhtml.UnescapeString(renderNode(t, composer(m, handlers{local: localUI{composerPreview: true}, composerRefs: refs})))
	for _, want := range []string{`class="mention-chip"`, `data-action="open-person"`, `data-id="maya"`, ">@Maya Patel<"} {
		if !strings.Contains(markup, want) {
			t.Errorf("preview lacks %q: %s", want, markup)
		}
	}
	if !strings.Contains(markup, "@Priya Shah") || strings.Contains(markup, `data-id="priya"`) {
		t.Errorf("text that only resembles a mention was promoted: %s", markup)
	}
	if strings.Count(markup, "mention-chip") != 1 {
		t.Errorf("want exactly one chip: %s", markup)
	}
}

func TestIssue3907_PreviewChipHasAnAccessibleNameInEveryLanguage(t *testing.T) {
	refs := []ChatReference{{Kind: "PERSON_MENTION", TenantID: "tenant", ID: "maya", Display: "Maya Patel", ConversationID: "room"}}
	// The chip name comes from the shell catalog (productui holds de-DE and ar); a
	// stub catalog proves the preview chip resolves it in the viewer's language.
	catalog := map[string]string{"en-US": "View {name}'s details", "de-DE": "Details zu {name} anzeigen", "ar": "عرض تفاصيل {name}"}
	labels := map[string]string{}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3907Model()
		m.Locale = locale
		m.Text = func(key string) string {
			if key == KeyViewPerson {
				return catalog[locale]
			}
			return ""
		}
		markup := stdhtml.UnescapeString(renderNode(t, composer(m, handlers{local: localUI{composerPreview: true}, composerRefs: refs})))
		at := strings.Index(markup, `data-action="open-person"`)
		if at < 0 {
			t.Fatalf("%s: no person chip: %s", locale, markup)
		}
		open := strings.LastIndex(markup[:at], "<button")
		end := at + strings.Index(markup[at:], ">")
		tag := markup[open:end]
		label := ""
		if i := strings.Index(tag, `aria-label="`); i >= 0 {
			label = tag[i+len(`aria-label="`):]
			label = label[:strings.Index(label, `"`)]
		}
		if !strings.Contains(label, "Maya Patel") {
			t.Errorf("%s: the chip itself has no accessible name naming the person: %s", locale, tag)
		}
		labels[locale] = label
	}
	if labels["de-DE"] == labels["en-US"] || labels["ar"] == labels["en-US"] {
		t.Errorf("chip name is not localized: %v", labels)
	}
}

func TestIssue3907_PreviewWithoutResolvedMentionsStaysPlain(t *testing.T) {
	m := issue3907Model()
	markup := renderNode(t, composer(m, handlers{local: localUI{composerPreview: true}}))
	if strings.Contains(markup, "mention-chip") {
		t.Errorf("no mention was resolved, yet the preview drew a chip: %s", markup)
	}
}
