package agentobo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/threatregister"
	"gopkg.in/yaml.v3"
)

type personaDecision struct {
	SchemaVersion   int      `yaml:"schema_version"`
	DecisionID      string   `yaml:"decision_id"`
	Status          string   `yaml:"status"`
	DecisionDate    string   `yaml:"decision_date"`
	Owner           string   `yaml:"owner"`
	ParentDecisions []string `yaml:"parent_decisions"`
	Authority       struct {
		InvocationMode           string   `yaml:"invocation_mode"`
		EffectiveAuthority       string   `yaml:"effective_authority"`
		ForbiddenAuthorities     []string `yaml:"forbidden_authorities"`
		SponsoredModeFromMention string   `yaml:"sponsored_mode_from_mention"`
		PersonaServiceCredential string   `yaml:"persona_service_credential"`
	} `yaml:"authority"`
	Invocation struct {
		StartsOnlyFrom           []string `yaml:"starts_only_from"`
		NewPostAuthoredByInvoker string   `yaml:"new_post_authored_by_invoker"`
		RejectedSources          []string `yaml:"rejected_sources"`
		AutomaticRouting         string   `yaml:"automatic_routing"`
		PersonaDiscoveryByAgents string   `yaml:"persona_discovery_by_agents"`
	} `yaml:"invocation"`
	Context struct {
		DefaultScope       []string `yaml:"default_scope"`
		MaximumRecentPosts int      `yaml:"maximum_recent_posts"`
		AllNonInvokerPosts string   `yaml:"all_non_invoker_posts"`
		BotAndAgentPosts   string   `yaml:"bot_and_agent_posts"`
		PeerPostTaint      string   `yaml:"peer_post_taint"`
		PeerPostsToPlanner string   `yaml:"peer_posts_to_planner"`
	} `yaml:"context"`
	AudienceFloor struct {
		SharedResultCommitRequires    []string `yaml:"shared_result_commit_requires"`
		PublicChannelAudience         string   `yaml:"public_channel_audience"`
		PrivateChannelAudience        string   `yaml:"private_channel_audience"`
		GroupDMAudience               string   `yaml:"group_dm_audience"`
		GuestsAndExternalMembersCount string   `yaml:"guests_and_external_members_count"`
		FailureReceipt                string   `yaml:"failure_receipt"`
		FailureDelivery               []string `yaml:"failure_delivery"`
		OneToOnePersonaDM             string   `yaml:"one_to_one_persona_dm"`
		ChannelPolicyMayForcePrivate  bool     `yaml:"channel_policy_may_force_private"`
	} `yaml:"audience_floor"`
	Writes struct {
		T0                 string   `yaml:"T0"`
		T1                 string   `yaml:"T1"`
		T2                 string   `yaml:"T2"`
		T3                 string   `yaml:"T3"`
		T4                 string   `yaml:"T4"`
		ApprovalActor      string   `yaml:"approval_actor"`
		ForbiddenApprovers []string `yaml:"forbidden_approvers"`
		CardBindings       []string `yaml:"card_bindings"`
		ChatCardExpiry     string   `yaml:"chat_card_expiry"`
	} `yaml:"writes"`
	Ownership struct {
		RequiredRoles            []string `yaml:"required_roles"`
		Lifecycle                []string `yaml:"lifecycle"`
		PublicationRequires      []string `yaml:"publication_requires"`
		OwnerLossSuspensionAfter string   `yaml:"owner_loss_suspension_after"`
	} `yaml:"ownership"`
	Placement struct {
		ExternalAndCrossCompany        string `yaml:"external_and_cross_company"`
		MaximumPersonasPerConversation int    `yaml:"maximum_personas_per_conversation"`
	} `yaml:"placement"`
	Limits struct {
		PerInvokerPerPersonaPerHour      int `yaml:"per_invoker_per_persona_per_hour"`
		ConcurrentTasksPerInvokerPersona int `yaml:"concurrent_tasks_per_invoker_persona"`
		PerConversationPerHour           int `yaml:"per_conversation_per_hour"`
	} `yaml:"limits"`
	Stop struct {
		RevocationEpochOwner string `yaml:"revocation_epoch_owner"`
		StopBefore           string `yaml:"stop_before"`
		SuspendedMention     string `yaml:"suspended_mention"`
	} `yaml:"stop"`
	ReleaseGate struct {
		ID                          string   `yaml:"id"`
		Requires                    []string `yaml:"requires"`
		FirstReleaseTiersInChannels []string `yaml:"first_release_tiers_in_channels"`
		FirstReleaseT3              string   `yaml:"first_release_t3"`
		FirstReleaseT4              string   `yaml:"first_release_t4"`
	} `yaml:"release_gate"`
	DecisionOwners []struct {
		Decision  string `yaml:"decision"`
		OwnerTodo string `yaml:"owner_todo"`
	} `yaml:"decision_owners"`
}

func loadPersonaYAML[T any](t *testing.T, name string, out *T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "definitions", "architecture", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := yaml.Unmarshal(b, out); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return b
}

func loadPersonaThreatRegister(t *testing.T) *threatregister.AgentExtension {
	t.Helper()
	r, err := threatregister.LoadAgentExtension(filepath.Join(repoRoot, "definitions", "architecture", "agent-threat-model.yaml"))
	if err != nil {
		t.Fatalf("load canonical agent threat register: %v", err)
	}
	return r
}

func personaCanonicalDigest(t *testing.T, b []byte) string {
	t.Helper()
	var value any
	if err := yaml.Unmarshal(b, &value); err != nil {
		t.Fatalf("parse persona YAML: %v", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("canonical persona JSON: %v", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func personaSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func TestTodo_AGENTP_001(t *testing.T) {
	var record personaDecision
	loadPersonaYAML(t, "agent-persona-decisions.yaml", &record)
	if record.SchemaVersion != 1 || record.DecisionID != "AGENTP-001" || record.Status != "DECIDED" || record.DecisionDate != "2026-09-28" || record.Owner == "" {
		t.Fatalf("persona decision identity = %+v", record)
	}
	if !reflect.DeepEqual(record.ParentDecisions, []string{"AGENT2-001", "AGENT2-002", "AGENT-006"}) {
		t.Fatalf("parent decisions = %v", record.ParentDecisions)
	}
	if record.Authority.InvocationMode != "ON_BEHALF_OF" || !strings.Contains(record.Authority.EffectiveAuthority, "invoker_current_authority") {
		t.Fatalf("authority is not invoker-scoped: %+v", record.Authority)
	}
	if record.Authority.SponsoredModeFromMention != "FORBIDDEN" || record.Authority.PersonaServiceCredential != "NONE" {
		t.Fatalf("mention can acquire standing/sponsored authority: %+v", record.Authority)
	}
	for _, forbidden := range []string{"AUTHOR", "BUSINESS_OWNER", "TECHNICAL_STEWARD", "INSTALLER", "ADMINISTRATOR"} {
		if !personaSet(record.Authority.ForbiddenAuthorities)[forbidden] {
			t.Errorf("missing forbidden authority %s", forbidden)
		}
	}
	if !reflect.DeepEqual(record.Invocation.StartsOnlyFrom, []string{"SERVER_RESOLVED_HANDLE", "SERVER_RESOLVED_SLASH_COMMAND"}) || record.Invocation.NewPostAuthoredByInvoker != "REQUIRED" {
		t.Fatalf("invocation source = %+v", record.Invocation)
	}
	for _, rejected := range []string{"QUOTED_TEXT", "FORWARDED_TEXT", "EDITED_TEXT", "BOT_POST", "AGENT_POST", "PERSONA_MENTION"} {
		if !personaSet(record.Invocation.RejectedSources)[rejected] {
			t.Errorf("missing rejected source %s", rejected)
		}
	}
	if record.Context.MaximumRecentPosts != 50 || record.Context.AllNonInvokerPosts != "UNTRUSTED_PEER" || record.Context.BotAndAgentPosts != "UNTRUSTED_PEER" || record.Context.PeerPostTaint != "UNTRUSTED_PEER" || !strings.Contains(record.Context.PeerPostsToPlanner, "QUARANTINED_EXTRACTION") {
		t.Fatalf("context boundary = %+v", record.Context)
	}
	if len(record.AudienceFloor.SharedResultCommitRequires) != 3 || record.AudienceFloor.OneToOnePersonaDM != "ALWAYS_PRIVATE" || !record.AudienceFloor.ChannelPolicyMayForcePrivate {
		t.Fatalf("audience floor = %+v", record.AudienceFloor)
	}
	if record.Writes.ApprovalActor != "INVOKER_ONLY" || record.Writes.ChatCardExpiry != "15m" || len(record.Writes.CardBindings) != 3 {
		t.Fatalf("approval binding = %+v", record.Writes)
	}
	if !reflect.DeepEqual(record.Ownership.RequiredRoles, []string{"BUSINESS_OWNER", "TECHNICAL_STEWARD"}) || record.Ownership.OwnerLossSuspensionAfter != "14d" {
		t.Fatalf("ownership = %+v", record.Ownership)
	}
	if record.Placement.ExternalAndCrossCompany != "OFF_BY_DEFAULT" || record.Placement.MaximumPersonasPerConversation != 5 {
		t.Fatalf("placement = %+v", record.Placement)
	}
	if record.Limits.PerInvokerPerPersonaPerHour != 30 || record.Limits.ConcurrentTasksPerInvokerPersona != 3 || record.Limits.PerConversationPerHour != 120 {
		t.Fatalf("limits = %+v", record.Limits)
	}
	if record.Stop.RevocationEpochOwner != "AGENT2-020" || record.Stop.StopBefore != "NEXT_STEP" || record.Stop.SuspendedMention != "UNAVAILABLE_NO_RUN" {
		t.Fatalf("stop = %+v", record.Stop)
	}
	if record.ReleaseGate.ID != "G-AGENT-PERSONA" || !reflect.DeepEqual(record.ReleaseGate.Requires, []string{"G-AGENT-OBO", "AGENTP-021", "AGENTP-022", "AGENTP-024"}) || record.ReleaseGate.FirstReleaseT4 != "DISABLED" {
		t.Fatalf("release gate = %+v", record.ReleaseGate)
	}
	if len(record.DecisionOwners) < 10 {
		t.Fatalf("decision owner map too small: %d", len(record.DecisionOwners))
	}
	for _, owner := range record.DecisionOwners {
		if !strings.HasPrefix(owner.OwnerTodo, "AGENTP-") {
			t.Errorf("decision %s owner = %s, want AGENTP todo", owner.Decision, owner.OwnerTodo)
		}
	}
}

func TestTodo_AGENTP_001_Golden(t *testing.T) {
	var record personaDecision
	b := loadPersonaYAML(t, "agent-persona-decisions.yaml", &record)
	goldenBytes, err := os.ReadFile("testdata/uxblind_p1_persona_decision_golden.json")
	if err != nil {
		t.Fatalf("read persona decision golden: %v", err)
	}
	var golden struct {
		CanonicalDigest string   `json:"canonical_digest"`
		Gate            string   `json:"gate"`
		Required        []string `json:"required"`
		OwnerCount      int      `json:"owner_count"`
	}
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatalf("parse persona decision golden: %v", err)
	}
	if got := personaCanonicalDigest(t, b); got != golden.CanonicalDigest {
		t.Errorf("persona decision digest = %s, want %s", got, golden.CanonicalDigest)
	}
	if record.ReleaseGate.ID != golden.Gate || !reflect.DeepEqual(record.ReleaseGate.Requires, golden.Required) || len(record.DecisionOwners) != golden.OwnerCount {
		t.Fatalf("persona decision golden shape changed: gate=%s required=%v owners=%d", record.ReleaseGate.ID, record.ReleaseGate.Requires, len(record.DecisionOwners))
	}
}

func TestTodo_AGENTP_001_Security(t *testing.T) {
	var record personaDecision
	loadPersonaYAML(t, "agent-persona-decisions.yaml", &record)
	effective := strings.ToUpper(record.Authority.EffectiveAuthority)
	effectiveTokens := personaSet(strings.Fields(effective))
	for _, forbidden := range []string{"AUTHOR", "BUSINESS_OWNER", "TECHNICAL_STEWARD", "INSTALLER", "ADMINISTRATOR"} {
		if effectiveTokens[forbidden] {
			t.Errorf("effective authority names forbidden principal %s: %q", forbidden, record.Authority.EffectiveAuthority)
		}
	}
	for _, forbidden := range []string{"AUTHOR", "BUSINESS_OWNER", "TECHNICAL_STEWARD", "INSTALLER", "ADMINISTRATOR"} {
		if !personaSet(record.Authority.ForbiddenAuthorities)[forbidden] {
			t.Errorf("authority guard no longer names %s", forbidden)
		}
	}
	if record.Authority.InvocationMode != "ON_BEHALF_OF" || record.Writes.ApprovalActor != "INVOKER_ONLY" {
		t.Fatal("persona authority or approval escaped the invoker")
	}
	if record.AudienceFloor.FailureReceipt == "" || len(record.AudienceFloor.FailureDelivery) != 2 {
		t.Fatal("audience-floor failure has no private fallback")
	}
	if record.ReleaseGate.FirstReleaseT3 != "ONE_TO_ONE_PERSONA_DM_ONLY" || record.ReleaseGate.FirstReleaseT4 != "DISABLED" {
		t.Fatal("persona release gate permits unsafe first-release effects")
	}
}

func TestTodo_AGENTP_002(t *testing.T) {
	record := loadPersonaThreatRegister(t)
	if violations := record.Validate(); len(violations) != 0 {
		t.Fatalf("canonical threat register has %d violations: %v", len(violations), violations)
	}
	if record.SchemaVersion != 1 || record.RegisterID != "AGENT2-002" || record.Status != "THREAT_MODELLED" || record.DecisionRef != "AGENT2-001" || record.PersonaDecisionRef != "AGENTP-001" {
		t.Fatalf("canonical register identity or decision references = %+v", record)
	}
	if record.Tooling.Family != "tools/planning/threatregister" || record.Tooling.Representation != "REGISTER_EXTENSION" {
		t.Fatalf("canonical register is not owned by threatregister tooling: %+v", record.Tooling)
	}
	classes := threatregister.AllPersonaAttackClasses()
	threats := record.ThreatsForClasses(classes)
	if len(threats) != len(classes) {
		t.Fatalf("persona threat count = %d, want %d", len(threats), len(classes))
	}
	seen := map[string]bool{}
	for _, threat := range threats {
		if threat.ID == "" || seen[threat.ID] {
			t.Fatalf("duplicate or empty threat id %q", threat.ID)
		}
		seen[threat.ID] = true
		if threat.Asset == "" || threat.TrustBoundary == "" || threat.Scenario == "" || threat.Precedent == "" || threat.Severity == "" || threat.Detection == "" || threat.Recovery == "" || threat.Owner == "" || len(threat.ControlTodos) == 0 || len(threat.Mitigations) == 0 || len(threat.Tests) == 0 {
			t.Fatalf("threat %s is incomplete", threat.ID)
		}
		if len(threat.Tests) != 1 || threat.Tests[0].Kind != "SECURITY" || threat.Tests[0].Name != "TestTodo_AGENTP_022_Security" {
			t.Errorf("threat %s test evidence = %+v", threat.ID, threat.Tests)
		}
		if threat.RedTeamCase != "AGENTP-022" || threat.RedTeamCaseID == "" {
			t.Errorf("threat %s red-team mapping = %s/%s", threat.ID, threat.RedTeamCase, threat.RedTeamCaseID)
		}
	}
	gotClasses := make([]string, 0, len(threats))
	for _, threat := range threats {
		gotClasses = append(gotClasses, threat.AttackClass)
	}
	if !reflect.DeepEqual(gotClasses, classes) {
		t.Fatalf("persona attack class order = %v, want %v", gotClasses, classes)
	}
}

func TestTodo_AGENTP_002_Golden(t *testing.T) {
	record := loadPersonaThreatRegister(t)
	goldenBytes, err := os.ReadFile("testdata/uxblind_p1_persona_threat_golden.json")
	if err != nil {
		t.Fatalf("read persona threat golden: %v", err)
	}
	var golden struct {
		CanonicalDigest string            `json:"canonical_digest"`
		AttackClasses   []string          `json:"attack_classes"`
		ThreatCount     int               `json:"threat_count"`
		ControlCount    int               `json:"control_count"`
		RedTeamCase     string            `json:"red_team_case"`
		CaseIDs         map[string]string `json:"case_ids"`
	}
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatalf("parse persona threat golden: %v", err)
	}
	digest, err := record.CanonicalDigestForClasses(threatregister.AllPersonaAttackClasses())
	if err != nil {
		t.Fatalf("canonical persona digest: %v", err)
	}
	if got := "sha256:" + digest; got != golden.CanonicalDigest {
		t.Errorf("persona threat digest = %s, want %s", got, golden.CanonicalDigest)
	}
	threats := record.ThreatsForClasses(threatregister.AllPersonaAttackClasses())
	classes := make([]string, 0, len(threats))
	controls := map[string]bool{}
	for _, threat := range threats {
		classes = append(classes, threat.AttackClass)
		for _, control := range threat.ControlTodos {
			controls[control] = true
		}
		if threat.RedTeamCase != golden.RedTeamCase || threat.RedTeamCaseID != golden.CaseIDs[threat.AttackClass] {
			t.Errorf("threat %s case = %s/%s, want %s/%s", threat.ID, threat.RedTeamCase, threat.RedTeamCaseID, golden.RedTeamCase, golden.CaseIDs[threat.AttackClass])
		}
	}
	if !reflect.DeepEqual(classes, golden.AttackClasses) || len(threats) != golden.ThreatCount || len(controls) != golden.ControlCount {
		t.Fatalf("persona threat golden shape changed: classes=%v threats=%d controls=%d", classes, len(threats), len(controls))
	}
}

func TestTodo_AGENTP_002_Security(t *testing.T) {
	record := loadPersonaThreatRegister(t)
	if violations := record.Validate(); len(violations) != 0 {
		t.Fatalf("canonical threat register has %d violations: %v", len(violations), violations)
	}
	classes := threatregister.AllPersonaAttackClasses()
	threats := record.ThreatsForClasses(classes)
	requiredControls := map[string]bool{}
	seenControls := map[string]bool{}
	controlByClass := map[string]bool{}
	caseByClass := map[string]bool{}
	allowedCases := personaSet(record.RedTeamCases)
	for _, threat := range threats {
		if threat.RedTeamCase != "AGENTP-022" || !allowedCases[threat.RedTeamCase] || threat.RedTeamCaseID != personaCaseID(threat.AttackClass) {
			t.Errorf("threat %s has invalid AGENTP-022 case mapping %q/%q", threat.ID, threat.RedTeamCase, threat.RedTeamCaseID)
		}
		for _, control := range threat.ControlTodos {
			if !strings.HasPrefix(control, "AGENTP-") || !personaSet(record.RequiredControlTodos)[control] {
				t.Errorf("threat %s names non-persona or undeclared control %s", threat.ID, control)
			}
			requiredControls[control] = true
			seenControls[control] = true
			controlByClass[threat.AttackClass] = true
		}
		caseByClass[threat.AttackClass] = threat.RedTeamCase == "AGENTP-022" && threat.RedTeamCaseID == personaCaseID(threat.AttackClass)
	}
	for _, class := range classes {
		if !controlByClass[class] {
			t.Errorf("attack class %s has no control todo", class)
		}
		if !caseByClass[class] {
			t.Errorf("attack class %s has no AGENTP-022 case", class)
		}
	}
	if len(requiredControls) != 13 {
		t.Errorf("persona control set size = %d, want 13", len(requiredControls))
	}
	missing := make([]string, 0)
	for _, control := range record.RequiredControlTodos {
		if strings.HasPrefix(control, "AGENTP-") && !seenControls[control] {
			missing = append(missing, control)
		}
	}
	// The canonical validator rejects mutations to any class's control or case.
	if len(threats) > 0 {
		mutated := *record
		mutated.Threats = append([]threatregister.AgentThreat(nil), record.Threats...)
		personaIndex := -1
		for i, threat := range mutated.Threats {
			if threat.AttackClass == classes[0] {
				personaIndex = i
				break
			}
		}
		if personaIndex < 0 {
			t.Fatal("canonical register omitted first persona class")
		}
		mutated.Threats[personaIndex].ControlTodos = nil
		if len(mutated.Validate()) == 0 {
			t.Error("canonical validator accepted a persona threat with no control")
		}
		mutated = *record
		mutated.Threats = append([]threatregister.AgentThreat(nil), record.Threats...)
		mutated.Threats[personaIndex].RedTeamCaseID = "wrong-case"
		if len(mutated.Validate()) == 0 {
			t.Error("canonical validator accepted a persona threat with an incorrect case ID")
		}
	}
	if len(missing) != 0 {
		t.Fatalf("controls without a persona threat mapping: %v", missing)
	}
}

func personaCaseID(class string) string {
	switch class {
	case "PEER_MESSAGE_INJECTION", "CROSS_INVOKER_CONTEXT_BLEED":
		return "peer-injection"
	case "AUDIENCE_LEAKAGE", "OUTPUT_EXFILTRATION", "STALE_MEMBERSHIP":
		return "audience-leak"
	case "APPROVAL_HIJACK_AND_FATIGUE":
		return "approval-hijack"
	case "HANDLE_IMPERSONATION":
		return "handle-impersonation"
	case "PERSONA_RECRUITMENT_LOOP":
		return "recruitment"
	case "SUSPENDED_PERSONA_RUN":
		return "suspend-race"
	default:
		return ""
	}
}
