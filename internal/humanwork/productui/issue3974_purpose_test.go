package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// #3974: the General agent's description told people to add documents "below"
// while the composer offered no way to add one. The plain description must not
// point at a control that is not there; the pointing wording belongs only to a
// composer that draws the picker.
func TestIssue3974_GeneralAgentPurposeOnlyPointsAtThePickerWhenOneIsDrawn(t *testing.T) {
	pointers := map[string][]string{"en-US": {"below"}, "de-DE": {"unten"}, "ar": {"أدناه"}}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		t.Run(localeID, func(t *testing.T) {
			locale := ResolveProductLocale(localeID)
			plain := locale.Text("agents.general_agent_purpose")
			withDocs := locale.Text("agents.general_agent_purpose_documents")
			if withDocs == "agents.general_agent_purpose_documents" || withDocs == plain {
				t.Fatalf("missing picker wording for %s: %q", localeID, withDocs)
			}
			for _, word := range pointers[localeID] {
				if strings.Contains(plain, word) {
					t.Fatalf("plain description still points at a control (%q): %q", word, plain)
				}
				if !strings.Contains(withDocs, word) {
					t.Fatalf("picker description lost its pointer (%q): %q", word, withDocs)
				}
			}

			without, err := ui.RenderToString(tagAgentComposer(locale, AgentSnapshot{StartAvailable: true}))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(without, "data-agent-request-documents") {
				t.Fatalf("composer without a hub drew the picker: %s", without)
			}
			if !strings.Contains(without, "<bdi>"+plain+"</bdi>") || strings.Contains(without, "<bdi>"+withDocs+"</bdi>") {
				t.Fatalf("composer without a picker must show the plain description only: %s", without)
			}
			// The client swaps the wording when it mounts the picker, so the plain
			// node carries the picker wording.
			if !strings.Contains(without, `data-agent-general-purpose="true"`) || !strings.Contains(without, `data-purpose-documents="`) {
				t.Fatalf("description lacks the swap hooks: %s", without)
			}

			with, err := ui.RenderToString(tagAgentComposer(locale, AgentSnapshot{StartAvailable: true, DocumentHubAvailable: true}))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(with, `data-agent-request-documents="true"`) || !strings.Contains(with, "<bdi>"+withDocs+"</bdi>") {
				t.Fatalf("composer with a picker must show the picker wording: %s", with)
			}
		})
	}
}
