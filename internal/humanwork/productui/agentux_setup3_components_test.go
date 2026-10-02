package productui

import (
	"strings"
	"testing"
)

func TestAgentUXSetup3_F1DirectConversationNames(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	if got := personaAdminConversationOption(locale, PersonaAdminTarget{ID: "direct-policy", Label: "Policy Helper", Kind: "DIRECT_MESSAGE", ViewerDirect: true}); got != "Your direct conversation with Policy Helper" {
		t.Fatalf("viewer direct option=%q", got)
	}
	if got := personaAdminPlacementName(locale, PersonaAdminInstallation{Conversation: "Walt Brennan", Kind: "DIRECT_MESSAGE"}); got != "Direct conversation with Walt Brennan" {
		t.Fatalf("direct placement=%q", got)
	}
	if got := personaAdminPlacementName(locale, PersonaAdminInstallation{Conversation: "general", Kind: "CHANNEL"}); got != "#general" {
		t.Fatalf("channel placement=%q", got)
	}
}

func TestAgentUXSetup3_F2PlacementShowsOnlyAuthorizedOfficialDocuments(t *testing.T) {
	one := 1
	persona := agentUXSetup2Persona()
	persona.Installations = []PersonaAdminInstallation{{InstallationID: "install-general", ConversationID: "general", Conversation: "general", Kind: "CHANNEL", Version: "4", OfficialDocumentCount: &one, OfficialDocumentTitles: []string{"Paid time off policy"}}}
	markup := personaAdminRender(t, personaAdminPlacements(ResolveProductLocale("en-US"), persona, agentUXSetup2Snapshot(persona)))
	for _, want := range []string{`data-installation-id="install-general"`, "#general", "1 document in this conversation", "Paid time off policy"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("placement missing %q: %s", want, markup)
		}
	}
}

func TestAgentUXSetup3_F3PublishedCardShowsCompletedEvidenceOnly(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Lifecycle = PersonaPublished
	persona.ReviewApproved = true
	persona.EvaluationRef = ""
	persona.ReviewApprovedAt = "2026-09-29"
	persona.EvaluationPassedAt = "2026-09-30"
	markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, agentUXSetup2Snapshot(persona)))
	for _, want := range []string{"Reviewed by", "Curtis Bell", "2026-09-29", "Evaluation passed", "2026-09-30"} {
		if !strings.Contains(markup, want) {
			t.Errorf("published evidence missing %q: %s", want, markup)
		}
	}
	for _, absent := range []string{"Evaluation is done by", "Message them", "Run evaluation"} {
		if strings.Contains(markup, absent) {
			t.Errorf("published card retained %q: %s", absent, markup)
		}
	}
}

func TestAgentUXSetup3_F4PeopleUseFullNamesAndInitials(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.OwnerInitials, persona.StewardInitials = "WB", "LH"
	persona.ReviewerInitials = "CB"
	persona.ReviewApproved = true
	persona.Lifecycle = PersonaPublished
	persona.ReviewApprovedAt = "2026-09-29"
	persona.EvaluationPassedAt = "2026-09-30"
	markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, agentUXSetup2Snapshot(persona)))
	for _, want := range []string{"Walt Brennan", "Loretta Haynes", "Curtis Bell", ">WB</", ">LH</"} {
		if !strings.Contains(markup, want) {
			t.Errorf("full identity missing %q: %s", want, markup)
		}
	}
}

func TestAgentUXSetup3_F5RemoveUsesUninstallPermission(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Installations = []PersonaAdminInstallation{{InstallationID: "install-general", ConversationID: "general", Conversation: "general", Kind: "CHANNEL", Version: "4"}}
	snapshot := agentUXSetup2Snapshot(persona)
	snapshot.AllowedCommands = []string{"UNINSTALL"}
	markup := personaAdminRender(t, personaAdminPlacements(ResolveProductLocale("en-US"), persona, snapshot))
	remove := markup[strings.Index(markup, `data-persona-admin-command-form="UNINSTALL"`):]
	remove = remove[:strings.Index(remove, "</form>")]
	if strings.Contains(remove, "disabled") || !strings.Contains(remove, ">Remove</button>") {
		t.Fatalf("authorized uninstall button=%s", remove)
	}
	snapshot.AllowedCommands = []string{"INSTALL"}
	markup = personaAdminRender(t, personaAdminPlacements(ResolveProductLocale("en-US"), persona, snapshot))
	remove = markup[strings.Index(markup, `data-persona-admin-command-form="UNINSTALL"`):]
	remove = remove[:strings.Index(remove, "</form>")]
	if !strings.Contains(remove, "disabled") {
		t.Fatalf("uninstall without permission remained enabled: %s", remove)
	}
}

func TestAgentUXSetup3_F7AccessPreviewUsesConcreteDocumentsAndActions(t *testing.T) {
	one := 1
	persona := agentUXSetup2Persona()
	persona.Lifecycle = PersonaPublished
	snapshot := agentUXSetup2Snapshot(persona)
	snapshot.Preview = PersonaAdminPreview{
		Subject: "Walt Brennan", Conversation: "general", Audience: "employees", ReplyPlacement: "public channel",
		EffectiveSkills: []PersonaAdminSkill{{ID: "hcmnext.skill.knowledge_search_with_citations"}, {ID: "persona.chat_reply"}},
		DerivedData:     []string{"POLICY_DOCUMENT"}, OfficialDocumentCount: &one, OfficialDocumentTitles: []string{"Paid time off policy"},
	}
	markup := personaAdminRender(t, personaAdminPreview(ResolveProductLocale("en-US"), &personaAdminTestClient{}, snapshot))
	want := "Walt Brennan can use Policy Helper in #general. It can read: policy documents placed in #general (1). It can do: search those documents, reply to Walt."
	if !strings.Contains(markup, want) {
		t.Fatalf("truthful preview missing %q: %s", want, markup)
	}
	denied := snapshot
	denied.Preview.EffectiveSkills = nil
	denied.Preview.DerivedData = nil
	denied.Preview.ReplyPlacement = ""
	denied.Preview.OfficialDocumentCount = nil
	denied.Preview.OfficialDocumentTitles = nil
	denied.Preview.Warnings = []string{"This person is outside the agent audience."}
	markup = personaAdminRender(t, personaAdminPreview(ResolveProductLocale("en-US"), &personaAdminTestClient{}, denied))
	if !strings.Contains(markup, "Walt Brennan cannot use Policy Helper in #general.") {
		t.Fatalf("refusal was not explained in words: %s", markup)
	}
}
