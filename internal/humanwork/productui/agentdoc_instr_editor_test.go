package productui

import (
	"strings"
	"testing"
)

func TestTodo_AGENTDOC_008_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		resolved := ResolveProductLocale(locale)
		snapshot := personaAdminEditorReadySnapshot()
		snapshot.Starters[0].Instructions = "Use only approved policy sources."
		create := personaAdminRender(t, PersonaAdminEditor(resolved, &personaAdminTestClient{}, snapshot))
		for _, want := range []string{`name="instructions"`, `maxlength="8000"`, `data-agentdoc-instructions="true"`, `data-agentdoc-built-in="true"`, "Use only approved policy sources.", agentDocInstructionText(resolved, "label"), agentDocInstructionText(resolved, "built_in_label"), agentDocInstructionText(resolved, "review_help"), agentDocInstructionCounter(resolved, 0)} {
			if !strings.Contains(create, want) {
				t.Fatalf("%s create editor missing %q: %s", locale, want, create)
			}
		}
		if strings.Contains(create, `instructions_bound`) || strings.Contains(create, `data-starter-instructions="server-owned"`) {
			t.Fatalf("%s create editor kept read-only instructions: %s", locale, create)
		}
		persona := PersonaAdminPersona{ID: "policy-helper", StarterID: "starter", StarterVersion: 1, Handle: "policy-helper", Name: "Policy Helper", Purpose: "Answer policies.", ChannelClasses: []string{"PRIVATE"}, Instructions: "Use approved documents.", Guidance: "Answer in the reader's language."}
		version := personaAdminRender(t, personaAdminVersionEditor(resolved, &personaAdminTestClient{}, persona, true))
		if !strings.Contains(version, `name="instructions"`) || !strings.Contains(version, "Use approved documents.") || !strings.Contains(version, "Answer in the reader&#39;s language.") || !strings.Contains(version, agentDocInstructionText(resolved, "review_help")) {
			t.Fatalf("%s version editor missing editable instructions: %s", locale, version)
		}
	}
}

func TestAgentDocInstr_VersionEditorIsInNormalFlow(t *testing.T) {
	css := personaAdminStylesheet()
	if strings.Contains(css, `.persona-admin-version-editor{position:absolute`) || strings.Contains(css, `inset-inline-end:0;width:min(42rem`) {
		t.Fatalf("version editor is still positioned as a popover: %s", css)
	}
}

func TestAgentDocInstr_ReadModeRendersInstructionDocumentChips(t *testing.T) {
	persona := PersonaAdminPersona{
		ID: "policy-helper", Name: "Policy Helper", Version: "2", Handle: "policy-helper", Purpose: "Answer policies.", Lifecycle: PersonaDraft,
		Instructions:       "Use only approved policy sources.",
		Guidance:           `Read {{doc:doc-policy}} first.`,
		DocumentReferences: []PersonaAdminDocumentReference{{DocumentID: "doc-policy", Title: "Leave policy", Label: "Leave policy", VersionMode: "PINNED", PinnedVersion: 3, Readable: true}},
	}
	markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, PersonaAdminSnapshot{}))
	for _, want := range []string{`data-agentdoc-instructions-readmode="true"`, ">Your instructions</summary>", "Leave policy", `href="/workspace/app/docs?document=doc-policy"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("read-mode instructions missing %q: %s", want, markup)
		}
	}
	empty := persona
	empty.Guidance = ""
	markup = personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, empty, PersonaAdminSnapshot{}))
	if strings.Contains(markup, `data-agentdoc-instructions-readmode="true"`) {
		t.Fatalf("empty guidance rendered a read-mode section: %s", markup)
	}
}
