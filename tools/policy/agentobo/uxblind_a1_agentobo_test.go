package agentobo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/threatregister"
	"gopkg.in/yaml.v3"
)

const repoRoot = "../../.."

type decisionRecord struct {
	SchemaVersion int    `yaml:"schema_version"`
	DecisionID    string `yaml:"decision_id"`
	Status        string `yaml:"status"`
	DecisionDate  string `yaml:"decision_date"`
	Owner         string `yaml:"owner"`
	RunModes      []struct {
		ID                 string   `yaml:"id"`
		DefaultFor         []string `yaml:"default_for"`
		EffectiveAuthority string   `yaml:"effective_authority"`
		AllowedTiers       []string `yaml:"allowed_tiers"`
		ForbiddenTiers     []string `yaml:"forbidden_tiers"`
		ActorRule          string   `yaml:"actor_rule"`
	} `yaml:"run_modes"`
	SideEffectTiers []struct {
		ID     string `yaml:"id"`
		Name   string `yaml:"name"`
		Effect string `yaml:"effect"`
	} `yaml:"side_effect_tiers"`
	ApprovalDefaults []struct {
		Tiers []string `yaml:"tiers"`
		Rule  string   `yaml:"rule"`
	} `yaml:"approval_defaults"`
	BatchMaxItems   int    `yaml:"batch_max_items"`
	HighRiskRule    string `yaml:"high_risk_rule"`
	ApprovalSurface string `yaml:"approval_surface"`
	Invariants      struct {
		AgentIsApprover                   bool `yaml:"agent_is_approver"`
		AgentIsApprovalTaskDecisionTarget bool `yaml:"agent_is_approval_task_decision_target"`
		AgentMayChangeOwnGrants           bool `yaml:"agent_may_change_own_grants"`
		AgentMayChangeOwnPlanSkills       bool `yaml:"agent_may_change_own_plan_skills"`
		AgentMayChangeOwnConnections      bool `yaml:"agent_may_change_own_connections"`
	} `yaml:"invariants"`
	TaskLimits struct {
		MaximumLifetime        string `yaml:"maximum_lifetime"`
		MaximumSteps           int    `yaml:"maximum_steps"`
		ActiveWallClockPerWake string `yaml:"active_wall_clock_per_wake"`
		SpendCeiling           string `yaml:"spend_ceiling"`
	} `yaml:"task_limits"`
	Phase1 struct {
		AutonomousExecution string `yaml:"autonomous_execution"`
		Rationale           string `yaml:"rationale"`
		Gate                struct {
			ID             string   `yaml:"id"`
			RequiredTodos  []string `yaml:"required_todos"`
			TenantOptIn    bool     `yaml:"tenant_opt_in"`
			T3Default      string   `yaml:"t3_default"`
			T4FirstRelease string   `yaml:"t4_first_release"`
			ShadowPeriod   string   `yaml:"shadow_period"`
			Pushes         []string `yaml:"pushes"`
		} `yaml:"gate"`
	} `yaml:"phase1"`
	ModelGateway struct {
		ImplementationTodo      string   `yaml:"implementation_todo"`
		Module                  string   `yaml:"module"`
		Gateway                 string   `yaml:"gateway"`
		AllAgentCalls           []string `yaml:"all_agent_calls"`
		TypedOutput             string   `yaml:"typed_output"`
		FreeTextParsing         bool     `yaml:"free_text_parsing"`
		ProviderSelection       string   `yaml:"provider_selection"`
		WebSearch               string   `yaml:"web_search"`
		OtherLLMClientLibraries string   `yaml:"other_llm_client_libraries"`
	} `yaml:"model_gateway"`
}

type threatRegister struct {
	SchemaVersion         int      `yaml:"schema_version"`
	RegisterID            string   `yaml:"register_id"`
	Status                string   `yaml:"status"`
	DecisionRef           string   `yaml:"decision_ref"`
	Owner                 string   `yaml:"owner"`
	RequiredAttackClasses []string `yaml:"required_attack_classes"`
	RequiredControlTodos  []string `yaml:"required_control_todos"`
	Tooling               struct {
		Family            string `yaml:"family"`
		Representation    string `yaml:"representation"`
		CanonicalIdentity string `yaml:"canonical_identity"`
	} `yaml:"tooling"`
	Threats []struct {
		ID            string   `yaml:"id"`
		Asset         string   `yaml:"asset"`
		TrustBoundary string   `yaml:"trust_boundary"`
		AttackClass   string   `yaml:"attack_class"`
		Scenario      string   `yaml:"scenario"`
		Severity      string   `yaml:"severity"`
		Detection     string   `yaml:"detection"`
		Recovery      string   `yaml:"recovery"`
		Owner         string   `yaml:"owner"`
		ControlTodos  []string `yaml:"control_todos"`
		RedTeamCase   string   `yaml:"red_team_case"`
		Mitigations   []string `yaml:"mitigations"`
		Tests         []struct {
			Kind string `yaml:"kind"`
			Name string `yaml:"name"`
		} `yaml:"tests"`
	} `yaml:"threats"`
}

func loadYAML[T any](t *testing.T, name string, out *T) []byte {
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

func loadDecisionYAML(t *testing.T, out *decisionRecord) []byte {
	t.Helper()
	name := filepath.Join(repoRoot, "definitions", "planning", "agent-obo-decisions.yaml")
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read canonical decision record %s: %v", name, err)
	}
	if err := yaml.Unmarshal(b, out); err != nil {
		t.Fatalf("parse canonical decision record %s: %v", name, err)
	}
	return b
}

func canonicalDigest(t *testing.T, yamlBytes []byte) string {
	t.Helper()
	var value any
	if err := yaml.Unmarshal(yamlBytes, &value); err != nil {
		t.Fatalf("parse YAML for canonical digest: %v", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("canonical JSON: %v", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func sorted(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func TestTodo_AGENT2_001(t *testing.T) {
	var record decisionRecord
	loadDecisionYAML(t, &record)
	if record.SchemaVersion != 1 || record.DecisionID != "AGENT2-001" || record.Status != "DECIDED" || record.DecisionDate != "2026-09-28" {
		t.Fatalf("decision identity = version %d, id %q, status %q, date %q", record.SchemaVersion, record.DecisionID, record.Status, record.DecisionDate)
	}
	if got := len(record.RunModes); got != 2 {
		t.Fatalf("run mode count = %d, want exactly two", got)
	}
	if got := []string{record.RunModes[0].ID, record.RunModes[1].ID}; !reflect.DeepEqual(got, []string{"ON_BEHALF_OF", "SPONSORED"}) {
		t.Fatalf("run mode order = %v", got)
	}
	if !reflect.DeepEqual(record.RunModes[0].DefaultFor, []string{"chat_dm", "agents_page", "user_started_task", "user_owned_schedule"}) || !reflect.DeepEqual(record.RunModes[0].AllowedTiers, []string{"T0", "T1", "T2", "T3", "T4"}) {
		t.Errorf("on-behalf-of defaults = triggers %v tiers %v", record.RunModes[0].DefaultFor, record.RunModes[0].AllowedTiers)
	}
	if !strings.Contains(record.RunModes[0].EffectiveAuthority, "signed_in_user_current_authority intersected") {
		t.Errorf("on-behalf-of authority does not state the user/ceiling intersection: %q", record.RunModes[0].EffectiveAuthority)
	}
	if !strings.Contains(record.RunModes[0].ActorRule, "signed-in user as principal") || !strings.Contains(record.RunModes[1].ActorRule, "nonhuman sponsor") {
		t.Errorf("actor-chain rules do not identify both principals: on-behalf-of=%q sponsored=%q", record.RunModes[0].ActorRule, record.RunModes[1].ActorRule)
	}
	if !reflect.DeepEqual(record.RunModes[1].DefaultFor, []string{"autonomous_channel", "event_schedule", "business_schedule"}) {
		t.Errorf("sponsored defaults = %v", record.RunModes[1].DefaultFor)
	}
	if !reflect.DeepEqual(record.RunModes[1].AllowedTiers, []string{"T0", "T1", "T2"}) || !reflect.DeepEqual(record.RunModes[1].ForbiddenTiers, []string{"T3", "T4"}) {
		t.Errorf("sponsored tiers = allowed %v, forbidden %v", record.RunModes[1].AllowedTiers, record.RunModes[1].ForbiddenTiers)
	}
	if got := len(record.SideEffectTiers); got != 5 {
		t.Fatalf("side-effect tier count = %d, want T0 through T4", got)
	}
	wantNames := []string{"READ", "PRIVATE_DRAFT", "COMMUNICATE", "SUBMIT_GOVERNED", "EXTERNAL_WRITE"}
	wantEffects := []string{
		"read governed records or connection data",
		"create a draft visible only to the signed-in user",
		"post, message, or notify another person or destination",
		"submit a BusinessIntent into its normal approval workflow",
		"write through an administrator-granted external connection",
	}
	for i, want := range wantNames {
		if record.SideEffectTiers[i].ID != "T"+string(rune('0'+i)) || record.SideEffectTiers[i].Name != want {
			t.Errorf("tier[%d] = %s/%s, want T%d/%s", i, record.SideEffectTiers[i].ID, record.SideEffectTiers[i].Name, i, want)
		}
		if record.SideEffectTiers[i].Effect != wantEffects[i] {
			t.Errorf("tier[%d] effect = %q, want %q", i, record.SideEffectTiers[i].Effect, wantEffects[i])
		}
	}
	wantDefaults := []struct {
		tiers []string
		rule  string
	}{
		{[]string{"T0", "T1"}, "run inside the user-confirmed plan without a prompt"},
		{[]string{"T2"}, "per-task confirmation of every destination is required"},
		{[]string{"T3", "T4"}, "the user approves the exact digest per action or per listed batch"},
	}
	if len(record.ApprovalDefaults) != len(wantDefaults) {
		t.Fatalf("approval default count = %d, want %d", len(record.ApprovalDefaults), len(wantDefaults))
	}
	for i, want := range wantDefaults {
		got := record.ApprovalDefaults[i]
		if !reflect.DeepEqual(got.Tiers, want.tiers) || got.Rule != want.rule {
			t.Errorf("approval default[%d] = tiers %v rule %q, want tiers %v rule %q", i, got.Tiers, got.Rule, want.tiers, want.rule)
		}
	}
}

func TestTodo_AGENT2_001_Golden(t *testing.T) {
	var record decisionRecord
	b := loadDecisionYAML(t, &record)
	var golden struct {
		CanonicalDigest string   `json:"canonical_digest"`
		RunModes        []string `json:"run_modes"`
		Tiers           []string `json:"tiers"`
		Gate            string   `json:"gate"`
		RequiredTodos   []string `json:"required_todos"`
	}
	goldenBytes, err := os.ReadFile("testdata/uxblind_a1_agentobo_golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if got := canonicalDigest(t, b); got != golden.CanonicalDigest {
		t.Errorf("decision canonical digest = %s, want %s", got, golden.CanonicalDigest)
	}
	gotModes := []string{record.RunModes[0].ID, record.RunModes[1].ID}
	gotTiers := make([]string, 0, len(record.SideEffectTiers))
	for _, tier := range record.SideEffectTiers {
		gotTiers = append(gotTiers, tier.ID+":"+tier.Name)
	}
	if !reflect.DeepEqual(gotModes, golden.RunModes) || !reflect.DeepEqual(gotTiers, golden.Tiers) || record.Phase1.Gate.ID != golden.Gate || !reflect.DeepEqual(record.Phase1.Gate.RequiredTodos, golden.RequiredTodos) {
		t.Errorf("golden decision shape changed: modes=%v tiers=%v gate=%s required=%v", gotModes, gotTiers, record.Phase1.Gate.ID, record.Phase1.Gate.RequiredTodos)
	}
}

func TestTodo_AGENT2_001_Security(t *testing.T) {
	var record decisionRecord
	loadDecisionYAML(t, &record)
	if record.Invariants.AgentIsApprover || record.Invariants.AgentIsApprovalTaskDecisionTarget || record.Invariants.AgentMayChangeOwnGrants || record.Invariants.AgentMayChangeOwnPlanSkills || record.Invariants.AgentMayChangeOwnConnections {
		t.Fatal("agent is allowed to approve, receive approval decisions, or change its own authority")
	}
	if record.BatchMaxItems != 25 || record.HighRiskRule != "STEP_UP" || record.ApprovalSurface != "PRODUCT_SURFACE_ONLY" {
		t.Fatalf("approval safety defaults = batch %d, risk %q, surface %q", record.BatchMaxItems, record.HighRiskRule, record.ApprovalSurface)
	}
	if record.TaskLimits.MaximumLifetime != "7d" || record.TaskLimits.MaximumSteps != 200 || record.TaskLimits.ActiveWallClockPerWake != "30m" || record.TaskLimits.SpendCeiling != "TENANT_SET" {
		t.Fatalf("task limits = %+v", record.TaskLimits)
	}
	if record.Phase1.AutonomousExecution != "DEFERRED" || record.Phase1.Rationale != "T3 and T4 effects are the signed-in user's own approved submissions; the agent never supplies approval authority" || !record.Phase1.Gate.TenantOptIn || record.Phase1.Gate.T3Default != "DISABLED_PER_TENANT" || record.Phase1.Gate.T4FirstRelease != "DISABLED_EXCEPT_ALLOWLISTED_REVERSIBLE_OPERATIONS" {
		t.Fatalf("unsafe Phase 1 gate defaults: execution=%s opt-in=%v t3=%s t4=%s", record.Phase1.AutonomousExecution, record.Phase1.Gate.TenantOptIn, record.Phase1.Gate.T3Default, record.Phase1.Gate.T4FirstRelease)
	}
	if record.Phase1.Gate.ID != "G-AGENT-OBO" || !reflect.DeepEqual(record.Phase1.Gate.RequiredTodos, []string{"AGENT2-023", "AGENT2-024", "AGENT2-025"}) || record.Phase1.Gate.ShadowPeriod != "T3 steps render as drafts only" || !reflect.DeepEqual(record.Phase1.Gate.Pushes, []string{"unattended_t0_t1_work_for_days", "batched_exact_digest_approval"}) {
		t.Fatalf("unsafe or incomplete Phase 1 gate: %+v", record.Phase1.Gate)
	}
	wantAgentCalls := []string{"planning", "tool_call_selection", "quarantined_extraction", "summarisation", "evaluations"}
	if record.ModelGateway.Module != "github.com/monstercameron/schemaflux" || record.ModelGateway.ImplementationTodo != "AGENT2-026" || record.ModelGateway.Gateway != "SchemaFlux" || !reflect.DeepEqual(record.ModelGateway.AllAgentCalls, wantAgentCalls) || record.ModelGateway.TypedOutput != "schemaflux.Generating[T] validated against Go types" || record.ModelGateway.FreeTextParsing || record.ModelGateway.ProviderSelection != "SCHEMAFLUX_OPTIONS_ONLY" || record.ModelGateway.WebSearch != "SCHEMAFLUX_OPTIONS_ONLY" || record.ModelGateway.OtherLLMClientLibraries != "FORBIDDEN_IN_PRODUCTION" {
		t.Fatalf("model gateway is not SchemaFlux-only: %+v", record.ModelGateway)
	}
}

func TestTodo_AGENT2_002(t *testing.T) {
	record, err := threatregister.LoadAgentExtension(filepath.Join(repoRoot, "definitions", "architecture", "agent-threat-model.yaml"))
	if err != nil {
		t.Fatalf("load canonical agent threat register: %v", err)
	}
	if violations := record.Validate(); len(violations) != 0 {
		t.Fatalf("unified threat register has %d violations: %v", len(violations), violations)
	}
	if record.SchemaVersion != 1 || record.RegisterID != "AGENT2-002" || record.Status != "THREAT_MODELLED" || record.DecisionRef != "AGENT2-001" {
		t.Fatalf("threat register identity = %+v", record)
	}
	if record.Tooling.Family != "tools/planning/threatregister" || record.Tooling.Representation != "REGISTER_EXTENSION" || record.Tooling.CanonicalIdentity != "register_id|asset|trust_boundary|attack_class" {
		t.Fatalf("threat register tooling declaration = %+v", record.Tooling)
	}
	if record.PersonaDecisionRef != "AGENTP-001" || len(record.Threats) != 18 {
		t.Fatalf("unified register must include both decision refs and 18 threats, got persona ref %q and %d threats", record.PersonaDecisionRef, len(record.Threats))
	}
	agentThreats := record.ThreatsForClasses(threatregister.AllAgentAttackClasses())
	classes := make([]string, 0, len(agentThreats))
	controls := map[string]bool{}
	seenIDs := map[string]bool{}
	for _, threat := range agentThreats {
		if seenIDs[threat.ID] || threat.ID == "" {
			t.Fatalf("duplicate or empty threat id %q", threat.ID)
		}
		seenIDs[threat.ID] = true
		classes = append(classes, threat.AttackClass)
		if threat.Asset == "" || threat.TrustBoundary == "" || threat.Scenario == "" || threat.Detection == "" || threat.Recovery == "" || threat.Owner == "" || len(threat.Mitigations) == 0 || len(threat.Tests) == 0 {
			t.Fatalf("threat %s is missing register fields", threat.ID)
		}
		if threat.RedTeamCase != "AGENT2-023" || len(threat.ControlTodos) == 0 {
			t.Fatalf("threat %s is not bound to AGENT2-023 and a control: case=%s controls=%v", threat.ID, threat.RedTeamCase, threat.ControlTodos)
		}
		for _, control := range threat.ControlTodos {
			controls[control] = true
		}
	}
	if !reflect.DeepEqual(classes, threatregister.AllAgentAttackClasses()) {
		t.Fatalf("agent attack class order = %v, want %v", classes, threatregister.AllAgentAttackClasses())
	}
	for _, control := range record.RequiredControlTodos {
		if strings.HasPrefix(control, "AGENTP-") {
			continue
		}
		if !controls[control] {
			t.Errorf("required control %s is not named by any threat", control)
		}
	}
	existing, err := threatregister.LoadRegister(filepath.Join(repoRoot, "definitions", "planning", "gates", "threat-001-register.yaml"))
	if err != nil {
		t.Fatalf("load existing threat-register tooling fixture: %v", err)
	}
	if violations := existing.Validate(); len(violations) != 0 {
		t.Fatalf("existing threat-register tooling rejected its signed fixture: %v", violations)
	}
}

func TestTodo_AGENT2_002_Golden(t *testing.T) {
	record, err := threatregister.LoadAgentExtension(filepath.Join(repoRoot, "definitions", "architecture", "agent-threat-model.yaml"))
	if err != nil {
		t.Fatalf("load canonical agent threat register: %v", err)
	}
	var golden struct {
		CanonicalDigest string   `json:"canonical_digest"`
		AttackClasses   []string `json:"attack_classes"`
		ThreatCount     int      `json:"threat_count"`
		ControlCount    int      `json:"control_count"`
		RedTeamCase     string   `json:"red_team_case"`
	}
	goldenBytes, err := os.ReadFile("testdata/uxblind_a1_agentobo_threat_golden.json")
	if err != nil {
		t.Fatalf("read threat golden: %v", err)
	}
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatalf("parse threat golden: %v", err)
	}
	digest, err := record.Agent2CanonicalDigest()
	if err != nil {
		t.Fatalf("AGENT2-002 canonical digest: %v", err)
	}
	if got := "sha256:" + digest; got != golden.CanonicalDigest {
		t.Errorf("threat canonical digest = %s, want %s", got, golden.CanonicalDigest)
	}
	agentThreats := record.ThreatsForClasses(threatregister.AllAgentAttackClasses())
	classes := make([]string, 0, len(agentThreats))
	controls := map[string]bool{}
	for _, threat := range agentThreats {
		classes = append(classes, threat.AttackClass)
		for _, control := range threat.ControlTodos {
			controls[control] = true
		}
		if threat.RedTeamCase != golden.RedTeamCase {
			t.Errorf("threat %s red-team case = %s, want %s", threat.ID, threat.RedTeamCase, golden.RedTeamCase)
		}
	}
	if !reflect.DeepEqual(classes, golden.AttackClasses) || len(agentThreats) != golden.ThreatCount || len(controls) != golden.ControlCount {
		t.Errorf("golden threat shape changed: classes=%v threats=%d controls=%d", classes, len(agentThreats), len(controls))
	}
}

func TestTodo_AGENT2_002_Security(t *testing.T) {
	record, err := threatregister.LoadAgentExtension(filepath.Join(repoRoot, "definitions", "architecture", "agent-threat-model.yaml"))
	if err != nil {
		t.Fatalf("load canonical agent threat register: %v", err)
	}
	if violations := record.Validate(); len(violations) != 0 {
		t.Fatalf("unified threat register has %d violations: %v", len(violations), violations)
	}
	required := map[string]bool{}
	for _, control := range record.RequiredControlTodos {
		if !strings.HasPrefix(control, "AGENTP-") {
			required[control] = true
		}
	}
	seen := map[string]bool{}
	classHasControl := map[string]bool{}
	classHasCase := map[string]bool{}
	for _, threat := range record.ThreatsForClasses(threatregister.AllAgentAttackClasses()) {
		if threat.RedTeamCase != "AGENT2-023" {
			t.Errorf("threat %s red-team case = %s, want AGENT2-023", threat.ID, threat.RedTeamCase)
		}
		for _, control := range threat.ControlTodos {
			if !required[control] {
				t.Errorf("threat %s names undeclared control %s", threat.ID, control)
			}
			seen[control] = true
			classHasControl[threat.AttackClass] = true
		}
		for _, test := range threat.Tests {
			if test.Kind != "SECURITY" || test.Name != "TestTodo_AGENT2_023_Security" {
				t.Errorf("threat %s test = %+v, want AGENT2-023 security case", threat.ID, test)
			}
			if test.Kind == "SECURITY" && test.Name == "TestTodo_AGENT2_023_Security" {
				classHasCase[threat.AttackClass] = true
			}
		}
	}
	for _, class := range threatregister.AllAgentAttackClasses() {
		if !classHasControl[class] {
			t.Errorf("attack class %s has no control todo", class)
		}
		if !classHasCase[class] {
			t.Errorf("attack class %s has no AGENT2-023 security case", class)
		}
	}
	missing := make([]string, 0)
	for control := range required {
		if !seen[control] {
			missing = append(missing, control)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("controls without a threat mapping: %v", sorted(missing))
	}
}
