package main

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_AGENTP_018_StalePreviewClearedOnIncompleteSelection proves the
// browser never shows a previous access answer once the targets stop naming a
// complete selection: any missing persona, person or conversation clears the
// projected preview and names exactly the fields still needed.
func TestTodo_AGENTP_018_StalePreviewClearedOnIncompleteSelection(t *testing.T) {
	complete := productui.PersonaAdminSnapshot{
		Preview: productui.PersonaAdminPreview{
			Subject: "Walt Brennan", Conversation: "Leadership",
			EffectiveSkills: []productui.PersonaAdminSkill{{ID: "persona.chat_reply", Tier: "T2"}},
			DerivedData:     []string{"POLICY_DOCUMENT"},
		},
	}
	kept := personaAdminApplyPreviewSelection(complete, "policy-helper", "walt", "leadership")
	if len(kept.PreviewValidationFields) != 0 || len(kept.Preview.EffectiveSkills) != 1 || kept.PreviewPersonaID != "policy-helper" || kept.PreviewSubjectID != "walt" || kept.PreviewConversationID != "leadership" {
		t.Fatalf("complete selection lost its access answer: %+v", kept)
	}
	for _, targets := range []struct {
		name                   string
		persona, subject, room string
		want                   []string
	}{
		{name: "missing persona", persona: "", subject: "walt", room: "leadership", want: []string{"persona"}},
		{name: "missing subject", persona: "policy-helper", subject: "", room: "leadership", want: []string{"subject"}},
		{name: "missing conversation", persona: "policy-helper", subject: "walt", room: "", want: []string{"conversation"}},
		{name: "missing all", persona: "", subject: "", room: "", want: []string{"persona", "subject", "conversation"}},
	} {
		t.Run(targets.name, func(t *testing.T) {
			got := personaAdminApplyPreviewSelection(complete, targets.persona, targets.subject, targets.room)
			if len(got.PreviewValidationFields) != len(targets.want) {
				t.Fatalf("validation fields = %v, want %v", got.PreviewValidationFields, targets.want)
			}
			for i, field := range targets.want {
				if got.PreviewValidationFields[i] != field {
					t.Fatalf("validation fields = %v, want %v", got.PreviewValidationFields, targets.want)
				}
			}
			if !reflect.DeepEqual(got.Preview, productui.PersonaAdminPreview{}) {
				t.Fatalf("incomplete selection kept a stale access answer: %+v", got.Preview)
			}
		})
	}
}
