package threatregister

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentExtension is the threat-model extension for AGENT2-002.
type AgentExtension struct {
	SchemaVersion         int                   `yaml:"schema_version" json:"schema_version"`
	RegisterID            string                `yaml:"register_id" json:"register_id"`
	Title                 string                `yaml:"title" json:"title"`
	Status                string                `yaml:"status" json:"status"`
	DecisionRef           string                `yaml:"decision_ref" json:"decision_ref"`
	PersonaDecisionRef    string                `yaml:"persona_decision_ref" json:"persona_decision_ref,omitempty"`
	Owner                 string                `yaml:"owner" json:"owner"`
	Tooling               AgentExtensionTooling `yaml:"tooling" json:"tooling"`
	RequiredAttackClasses []string              `yaml:"required_attack_classes" json:"required_attack_classes"`
	RequiredControlTodos  []string              `yaml:"required_control_todos" json:"required_control_todos"`
	RedTeamCases          []string              `yaml:"red_team_cases" json:"red_team_cases"`
	Threats               []AgentThreat         `yaml:"threats" json:"threats"`
}

// AgentExtensionTooling identifies the official validator representation.
type AgentExtensionTooling struct {
	Family            string `yaml:"family" json:"family"`
	Representation    string `yaml:"representation" json:"representation"`
	CanonicalIdentity string `yaml:"canonical_identity" json:"canonical_identity"`
}

// AgentThreat is one delegated-agent attack class and its owning controls.
type AgentThreat struct {
	ID            string       `yaml:"id" json:"id"`
	Asset         string       `yaml:"asset" json:"asset"`
	TrustBoundary string       `yaml:"trust_boundary" json:"trust_boundary"`
	AttackClass   string       `yaml:"attack_class" json:"attack_class"`
	Precedent     string       `yaml:"precedent,omitempty" json:"precedent,omitempty"`
	Scenario      string       `yaml:"scenario" json:"scenario"`
	Severity      string       `yaml:"severity" json:"severity"`
	Detection     string       `yaml:"detection" json:"detection"`
	Recovery      string       `yaml:"recovery" json:"recovery"`
	Owner         string       `yaml:"owner" json:"owner"`
	ControlTodos  []string     `yaml:"control_todos" json:"control_todos"`
	RedTeamCase   string       `yaml:"red_team_case" json:"red_team_case"`
	RedTeamCaseID string       `yaml:"red_team_case_id,omitempty" json:"red_team_case_id,omitempty"`
	Mitigations   []string     `yaml:"mitigations" json:"mitigations"`
	Tests         []ThreatTest `yaml:"tests" json:"tests"`
}

var agentAttackClasses = []string{
	"GOAL_HIJACK", "TOOL_MISUSE", "IDENTITY_AND_PRIVILEGE_ABUSE",
	"CONFUSED_DEPUTY_AND_TOKEN_PASSTHROUGH", "MEMORY_AND_CONTEXT_POISONING",
	"INSECURE_AGENT_TO_AGENT_DELEGATION", "CASCADING_FAILURE",
	"HUMAN_AGENT_TRUST_EXPLOITATION", "ROGUE_AGENT_VERSION",
}

var agentControlTodos = []string{
	"AGENT2-003", "AGENT2-004", "AGENT2-005", "AGENT2-006", "AGENT2-007",
	"AGENT2-008", "AGENT2-009", "AGENT2-010", "AGENT2-011", "AGENT2-012",
	"AGENT2-013", "AGENT2-014", "AGENT2-015", "AGENT2-016", "AGENT2-017",
	"AGENT2-018", "AGENT2-019", "AGENT2-020", "AGENT2-021", "AGENT2-022",
	"AGENT-002", "AGENT-003", "AGENT-020", "AGENT-039",
}

var personaAttackClasses = []string{
	"PEER_MESSAGE_INJECTION", "AUDIENCE_LEAKAGE", "OUTPUT_EXFILTRATION",
	"CROSS_INVOKER_CONTEXT_BLEED", "APPROVAL_HIJACK_AND_FATIGUE",
	"HANDLE_IMPERSONATION", "PERSONA_RECRUITMENT_LOOP", "STALE_MEMBERSHIP",
	"SUSPENDED_PERSONA_RUN",
}

var personaControlTodos = []string{
	"AGENTP-005", "AGENTP-006", "AGENTP-007", "AGENTP-008", "AGENTP-009",
	"AGENTP-010", "AGENTP-011", "AGENTP-012", "AGENTP-013", "AGENTP-014",
	"AGENTP-015", "AGENTP-016", "AGENTP-017",
}

var personaCaseByAttackClass = map[string]string{
	"PEER_MESSAGE_INJECTION":      "peer-injection",
	"AUDIENCE_LEAKAGE":            "audience-leak",
	"OUTPUT_EXFILTRATION":         "audience-leak",
	"CROSS_INVOKER_CONTEXT_BLEED": "peer-injection",
	"APPROVAL_HIJACK_AND_FATIGUE": "approval-hijack",
	"HANDLE_IMPERSONATION":        "handle-impersonation",
	"PERSONA_RECRUITMENT_LOOP":    "recruitment",
	"STALE_MEMBERSHIP":            "audience-leak",
	"SUSPENDED_PERSONA_RUN":       "suspend-race",
}

var agentControlSet = append(append([]string(nil), agentControlTodos...), personaControlTodos...)
var allAgentAttackClasses = append(append([]string(nil), agentAttackClasses...), personaAttackClasses...)
var allAgentRedTeamCases = []string{"AGENT2-023", "AGENTP-022"}

// AllAgentAttackClasses returns the required delegated-agent threat taxonomy.
func AllAgentAttackClasses() []string { return append([]string(nil), agentAttackClasses...) }

// AllPersonaAttackClasses returns the shared-channel persona attack taxonomy.
func AllPersonaAttackClasses() []string { return append([]string(nil), personaAttackClasses...) }

// AllRegisteredAgentAttackClasses returns both delegated-agent taxonomies.
func AllRegisteredAgentAttackClasses() []string {
	return append([]string(nil), allAgentAttackClasses...)
}

// ThreatsForClasses selects threats in the given attack-class order.
func (r AgentExtension) ThreatsForClasses(classes []string) []AgentThreat {
	wanted := make(map[string]bool, len(classes))
	for _, class := range classes {
		wanted[class] = true
	}
	var selected []AgentThreat
	for _, threat := range r.Threats {
		if wanted[threat.AttackClass] {
			selected = append(selected, threat)
		}
	}
	return selected
}

// LoadAgentExtension reads the extension with unknown YAML fields rejected.
func LoadAgentExtension(path string) (*AgentExtension, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var r AgentExtension
	decoder := yaml.NewDecoder(strings.NewReader(string(content)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &r, nil
}

// Validate reports missing, unknown or duplicated attack/control mappings.
func (r AgentExtension) Validate() []Violation {
	var out []Violation
	add := func(field, issue string) { out = append(out, Violation{Field: field, Issue: issue}) }
	if r.SchemaVersion != 1 {
		add("schema_version", "must equal 1")
	}
	if r.RegisterID != "AGENT2-002" {
		add("register_id", "must equal AGENT2-002")
	}
	if strings.TrimSpace(r.Title) == "" || strings.TrimSpace(r.Owner) == "" {
		add("metadata", "title and owner are required")
	}
	if r.Status != "THREAT_MODELLED" {
		add("status", "must equal THREAT_MODELLED")
	}
	if r.DecisionRef != "AGENT2-001" {
		add("decision_ref", "must reference AGENT2-001")
	}
	if r.PersonaDecisionRef != "AGENTP-001" {
		add("persona_decision_ref", "must reference AGENTP-001")
	}
	if r.Tooling.Family != "tools/planning/threatregister" || r.Tooling.Representation != "REGISTER_EXTENSION" || r.Tooling.CanonicalIdentity != "register_id|asset|trust_boundary|attack_class" {
		add("tooling", "must declare the official threat-register extension identity")
	}
	if !sameStrings(r.RequiredAttackClasses, allAgentAttackClasses) {
		add("required_attack_classes", "does not match the combined agent and persona attack taxonomies")
	}
	if !sameStrings(r.RequiredControlTodos, agentControlSet) {
		add("required_control_todos", "does not match the combined control-todo set")
	}
	if !sameStrings(r.RedTeamCases, allAgentRedTeamCases) {
		add("red_team_cases", "does not match the required agent and persona red-team cases")
	}
	allowed := make(map[string]bool, len(agentControlSet))
	for _, id := range agentControlSet {
		allowed[id] = true
	}
	seenClass, seenID := map[string]bool{}, map[string]bool{}
	for i, th := range r.Threats {
		p := fmt.Sprintf("threats[%d]", i)
		if strings.TrimSpace(th.ID) == "" || seenID[th.ID] {
			add(p+".id", "missing or duplicate threat id")
		}
		seenID[th.ID] = true
		if strings.TrimSpace(th.Asset) == "" || strings.TrimSpace(th.TrustBoundary) == "" {
			add(p, "asset and trust_boundary are required")
		}
		if !contains(allAgentAttackClasses, th.AttackClass) {
			add(p+".attack_class", "unknown required attack class")
		}
		if seenClass[th.AttackClass] {
			add(p+".attack_class", "duplicate attack class")
		}
		seenClass[th.AttackClass] = true
		if strings.TrimSpace(th.Scenario) == "" || strings.TrimSpace(th.Detection) == "" || strings.TrimSpace(th.Recovery) == "" || strings.TrimSpace(th.Owner) == "" {
			add(p, "scenario, detection, recovery, and owner are required")
		}
		isPersona := contains(personaAttackClasses, th.AttackClass)
		if isPersona && strings.TrimSpace(th.Precedent) == "" {
			add(p+".precedent", "persona threats require a named precedent")
		}
		if !validSeverities[th.Severity] {
			add(p+".severity", "unknown severity")
		}
		if len(th.ControlTodos) == 0 {
			add(p+".control_todos", "threat has no owning control todo")
		}
		for _, id := range th.ControlTodos {
			if !allowed[id] || isPersona != strings.HasPrefix(id, "AGENTP-") {
				add(p+".control_todos", "unknown control todo "+id)
			}
		}
		wantCase, wantTest := "AGENT2-023", "TestTodo_AGENT2_023_Security"
		if isPersona {
			wantCase, wantTest = "AGENTP-022", "TestTodo_AGENTP_022_Security"
			if wantID := personaCaseByAttackClass[th.AttackClass]; th.RedTeamCaseID != wantID {
				add(p+".red_team_case_id", "must map to the AGENTP-022 case for its attack class")
			}
		} else if th.RedTeamCaseID != "" {
			add(p+".red_team_case_id", "only persona threats use a case id")
		}
		if th.RedTeamCase != wantCase {
			add(p+".red_team_case", "must map to "+wantCase)
		}
		if len(th.Mitigations) == 0 {
			add(p+".mitigations", "threat has no mitigation mapping")
		}
		if len(th.Tests) == 0 {
			add(p+".tests", "threat has no test mapping")
		}
		for j, test := range th.Tests {
			if test.Kind != TestKindSecurity || test.Name != wantTest {
				add(fmt.Sprintf("%s.tests[%d]", p, j), "must name the family SECURITY test")
			}
		}
	}
	for _, class := range allAgentAttackClasses {
		if !seenClass[class] {
			add("threats", "missing attack class "+class)
		}
	}
	if len(r.Threats) != len(allAgentAttackClasses) {
		add("threats", "must contain exactly one threat per required attack class")
	}
	return out
}

// CanonicalDigest returns SHA-256 over the extension's canonical JSON form.
func (r AgentExtension) CanonicalDigest() (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("marshal canonical agent extension: %w", err)
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}

// CanonicalDigestForClasses pins a threat-class subset for a family golden.
func (r AgentExtension) CanonicalDigestForClasses(classes []string) (string, error) {
	b, err := json.Marshal(r.ThreatsForClasses(classes))
	if err != nil {
		return "", fmt.Errorf("marshal canonical threat subset: %w", err)
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}

// Agent2CanonicalDigest preserves AGENT2-002's original golden projection.
func (r AgentExtension) Agent2CanonicalDigest() (string, error) {
	type agent2Threat struct {
		ID            string       `json:"id"`
		Asset         string       `json:"asset"`
		TrustBoundary string       `json:"trust_boundary"`
		AttackClass   string       `json:"attack_class"`
		Scenario      string       `json:"scenario"`
		Severity      string       `json:"severity"`
		Detection     string       `json:"detection"`
		Recovery      string       `json:"recovery"`
		Owner         string       `json:"owner"`
		ControlTodos  []string     `json:"control_todos"`
		RedTeamCase   string       `json:"red_team_case"`
		Mitigations   []string     `json:"mitigations"`
		Tests         []ThreatTest `json:"tests"`
	}
	type agent2Projection struct {
		SchemaVersion         int                   `json:"schema_version"`
		RegisterID            string                `json:"register_id"`
		Title                 string                `json:"title"`
		Status                string                `json:"status"`
		DecisionRef           string                `json:"decision_ref"`
		Owner                 string                `json:"owner"`
		Tooling               AgentExtensionTooling `json:"tooling"`
		RequiredAttackClasses []string              `json:"required_attack_classes"`
		RequiredControlTodos  []string              `json:"required_control_todos"`
		Threats               []agent2Threat        `json:"threats"`
	}
	threats := r.ThreatsForClasses(agentAttackClasses)
	projected := make([]agent2Threat, 0, len(threats))
	for _, th := range threats {
		projected = append(projected, agent2Threat{
			ID: th.ID, Asset: th.Asset, TrustBoundary: th.TrustBoundary,
			AttackClass: th.AttackClass, Scenario: th.Scenario, Severity: th.Severity,
			Detection: th.Detection, Recovery: th.Recovery, Owner: th.Owner,
			ControlTodos: th.ControlTodos, RedTeamCase: th.RedTeamCase,
			Mitigations: th.Mitigations, Tests: th.Tests,
		})
	}
	payload := agent2Projection{
		SchemaVersion: r.SchemaVersion, RegisterID: r.RegisterID,
		Title: "Threat register for delegated long-horizon agents", Status: r.Status,
		DecisionRef: r.DecisionRef, Owner: r.Owner, Tooling: r.Tooling,
		RequiredAttackClasses: agentAttackClasses, RequiredControlTodos: agentControlTodos,
		Threats: projected,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal AGENT2-002 golden projection: %w", err)
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
