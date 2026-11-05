package agentdelivery

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func artifact(t *testing.T) Record {
	t.Helper()
	r, err := Load(filepath.Join(repoRoot(t), ArtifactPath))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestTodo_AGENT_006 is the PRIMARY leg: the checked-in scope decision is
// valid, proposes a separate pilot (never committed Phase 1 or chat-release
// scope), names one served provider, and claims no approval.
func TestTodo_AGENT_006(t *testing.T) {
	r := artifact(t)
	if violations := Validate(r); len(violations) != 0 {
		t.Fatalf("AGENT-006 artifact is invalid: %v", violations)
	}
	if r.ScopeDecision != "SEPARATE_PILOT" {
		t.Fatalf("scope_decision = %q, want SEPARATE_PILOT", r.ScopeDecision)
	}
	if r.Status == "APPROVED" || len(r.Approval.Signers) > 0 {
		t.Fatal("planning artifact must not claim external approval")
	}
	if r.SLOGate.Status == "SIGNED" || len(r.SLOGate.Signers) > 0 {
		t.Fatal("the workflow and chat SLO gate must stay unsigned until real signers exist")
	}
	if strings.ContainsAny(r.ServedProvider, ",;") {
		t.Fatalf("served_provider names more than one provider: %q", r.ServedProvider)
	}
}

// oboRecord mirrors the AGENT2-001 decision record just far enough to check
// the AGENT-006 adoption against it.
type oboRecord struct {
	DecisionID string `yaml:"decision_id"`
	RunModes   []struct {
		ID           string   `yaml:"id"`
		AllowedTiers []string `yaml:"allowed_tiers"`
	} `yaml:"run_modes"`
	SideEffectTiers []struct {
		ID string `yaml:"id"`
	} `yaml:"side_effect_tiers"`
	ApprovalDefaults []struct {
		Tiers []string `yaml:"tiers"`
		Rule  string   `yaml:"rule"`
	} `yaml:"approval_defaults"`
	Phase1 struct {
		AutonomousExecution string `yaml:"autonomous_execution"`
		Gate                struct {
			ID             string   `yaml:"id"`
			RequiredTodos  []string `yaml:"required_todos"`
			TenantOptIn    bool     `yaml:"tenant_opt_in"`
			T3Default      string   `yaml:"t3_default"`
			T4FirstRelease string   `yaml:"t4_first_release"`
			ShadowPeriod   string   `yaml:"shadow_period"`
		} `yaml:"gate"`
	} `yaml:"phase1"`
}

func loadOBO(t *testing.T) oboRecord {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "definitions/planning/agent-obo-decisions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var rec oboRecord
	if err := yaml.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func approvalDefaultsIssue(r Record, obo oboRecord) string {
	if len(r.AdoptedApprovalDefaults) != len(obo.ApprovalDefaults) {
		return fmt.Sprintf("adopted %d approval default rows, AGENT2-001 has %d",
			len(r.AdoptedApprovalDefaults), len(obo.ApprovalDefaults))
	}
	for i, row := range obo.ApprovalDefaults {
		got := r.AdoptedApprovalDefaults[i]
		if !reflect.DeepEqual(got.Tiers, row.Tiers) || got.Rule != row.Rule {
			return fmt.Sprintf("approval default row %d = %+v, want tiers %v rule %q",
				i, got, row.Tiers, row.Rule)
		}
	}
	return ""
}

func TestAgentScopeConformanceRejectsRewordedAdoption(t *testing.T) {
	r := cloneRecord(artifact(t))
	obo := loadOBO(t)
	if got := approvalDefaultsIssue(r, obo); got != "" {
		t.Fatalf("clean record reports an issue: %s", got)
	}
	r.AdoptedApprovalDefaults[0].Rule = "run everything without a prompt"
	if got := approvalDefaultsIssue(r, obo); got == "" {
		t.Fatal("a reworded approval rule still conforms")
	}
	r.AdoptedApprovalDefaults = r.AdoptedApprovalDefaults[:2]
	if got := approvalDefaultsIssue(r, obo); got == "" {
		t.Fatal("a dropped approval row still conforms")
	}
}

// TestTodo_AGENT_006_Conformance checks the adoption claim: the run modes,
// tiers, approval defaults and gate the scope decision adopts are exactly
// AGENT2-001's, and every admitted job fits the on-behalf-of ceiling.
func TestTodo_AGENT_006_Conformance(t *testing.T) {
	r := artifact(t)
	obo := loadOBO(t)
	if obo.DecisionID != "AGENT2-001" {
		t.Fatalf("agent-obo-decisions.yaml decision_id = %q", obo.DecisionID)
	}
	var modeIDs []string
	allowed := map[string]bool{}
	for _, m := range obo.RunModes {
		modeIDs = append(modeIDs, m.ID)
		if m.ID == "ON_BEHALF_OF" {
			for _, tier := range m.AllowedTiers {
				allowed[tier] = true
			}
		}
	}
	if !reflect.DeepEqual(r.AdoptedRunModes, modeIDs) {
		t.Fatalf("adopted_run_modes = %v, want %v", r.AdoptedRunModes, modeIDs)
	}
	var tierIDs []string
	for _, tier := range obo.SideEffectTiers {
		tierIDs = append(tierIDs, tier.ID)
	}
	if !reflect.DeepEqual(r.AdoptedTiers, tierIDs) {
		t.Fatalf("adopted_tiers = %v, want %v", r.AdoptedTiers, tierIDs)
	}
	if issue := approvalDefaultsIssue(r, obo); issue != "" {
		t.Fatal(issue)
	}
	gate := obo.Phase1.Gate
	if r.AdoptedGate.ID != gate.ID {
		t.Fatalf("adopted gate = %q, want %q", r.AdoptedGate.ID, gate.ID)
	}
	if !reflect.DeepEqual(r.AdoptedGate.RequiredTodos, gate.RequiredTodos) {
		t.Fatalf("adopted gate todos = %v, want %v", r.AdoptedGate.RequiredTodos, gate.RequiredTodos)
	}
	if !r.AdoptedGate.TenantOptIn || !gate.TenantOptIn {
		t.Fatal("the adopted gate must keep tenant opt-in")
	}
	if r.AdoptedGate.T3Default != gate.T3Default || r.AdoptedGate.T4FirstRelease != gate.T4FirstRelease {
		t.Fatalf("adopted gate weakens the T3/T4 defaults: %+v", r.AdoptedGate)
	}
	if r.AdoptedGate.ShadowPeriod != gate.ShadowPeriod {
		t.Fatalf("adopted shadow period = %q, want %q", r.AdoptedGate.ShadowPeriod, gate.ShadowPeriod)
	}
	if obo.Phase1.AutonomousExecution != "DEFERRED" {
		t.Fatalf("AGENT2-001 autonomous_execution = %q, want DEFERRED", obo.Phase1.AutonomousExecution)
	}
	if !strings.Contains(strings.ToLower(r.Phase1Position), "deferr") {
		t.Fatalf("phase1_position does not state the deferral: %q", r.Phase1Position)
	}
	for _, job := range r.AdmittedJobs {
		if !allowed[job.TierCeiling] {
			t.Fatalf("admitted job %q exceeds the on-behalf-of ceiling with %s", job.Name, job.TierCeiling)
		}
	}
}

// TestTodo_AGENT_006_Golden pins the canonical digest and the violation format.
func TestTodo_AGENT_006_Golden(t *testing.T) {
	r := artifact(t)
	computed, err := Digest(r)
	if err != nil {
		t.Fatal(err)
	}
	if computed != r.CanonicalDigest {
		t.Fatalf("digest changed: got %s want %s", computed, r.CanonicalDigest)
	}
	if got, want := r.CanonicalDigest, "sha256:c843e295f4ead60735fd29b0b7dade53d4a260d0e10eb28fad1a8717b2d06025"; got != want {
		t.Fatalf("AGENT-006 golden digest changed: got %s want %s", got, want)
	}
	if got := (Violation{Field: "status", Issue: "must remain PROPOSED until approval evidence exists"}).String(); got != "AGENT-006: status: must remain PROPOSED until approval evidence exists" {
		t.Fatalf("violation formatting changed: %s", got)
	}
}

// TestTodo_AGENT_006_Integration checks the record against its neighbours:
// the CHAT-001 exchange still holds chat scope separately, and every adopted
// source the record cites exists on disk.
func TestTodo_AGENT_006_Integration(t *testing.T) {
	r := artifact(t)
	root := repoRoot(t)
	chat := struct {
		TodoID        string `json:"todo_id"`
		ScopeDecision string `json:"scope_decision"`
	}{}
	b, err := os.ReadFile(filepath.Join(root, "definitions/planning/gates/chat-001-scope-exchange.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonUnmarshal(b, &chat); err != nil {
		t.Fatal(err)
	}
	if chat.TodoID != "CHAT-001" || chat.ScopeDecision != "SEPARATE_GATE_C" {
		t.Fatalf("CHAT-001 no longer holds chat scope separately: %+v", chat)
	}
	for _, path := range []string{
		r.ExecutionPlanRef,
		"definitions/planning/agent-obo-decisions.yaml",
		"planning/specs/customer-agent-creation-business-context-and-chat.md",
		"internal/agentsecurity/action_compiler.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("cited source %s is missing: %v", path, err)
		}
	}
	for _, job := range r.AdmittedJobs {
		if !strings.Contains(job.Governance, "internal/agentsecurity/action_compiler.go") {
			t.Fatalf("admitted job %q does not route through the AGENT-005 compiler", job.Name)
		}
	}
}

func TestAgentScopeRejectsCommittedScope(t *testing.T) {
	base := artifact(t)
	for _, decision := range []string{"PHASE_1_COMMITTED", "CHAT_RELEASE_SCOPE"} {
		r := base
		r.ScopeDecision = decision
		found := false
		for _, violation := range Validate(r) {
			if violation.Field == "scope_decision" {
				found = true
			}
		}
		if !found {
			t.Fatalf("scope_decision %q must be rejected", decision)
		}
	}
}

func TestAgentScopeRejectsIncompleteRecords(t *testing.T) {
	base := artifact(t)
	cases := []struct {
		name   string
		mutate func(*Record)
		field  string
	}{
		{"wrong todo", func(r *Record) { r.TodoID = "AGENT-007" }, "todo_id"},
		{"missing version", func(r *Record) { r.Version = "" }, "version"},
		{"wrong status", func(r *Record) { r.Status = "APPROVED" }, "status"},
		{"phase 1 scope", func(r *Record) { r.ScopeDecision = "PHASE_1_COMMITTED" }, "scope_decision"},
		{"chat release scope", func(r *Record) { r.ScopeDecision = "CHAT_RELEASE_SCOPE" }, "scope_decision"},
		{"missing cohort", func(r *Record) { r.FirstCohort = "" }, "first_cohort"},
		{"missing provider", func(r *Record) { r.ServedProvider = "" }, "served_provider"},
		{"two providers", func(r *Record) { r.ServedProvider = "Anthropic, OpenAI" }, "served_provider"},
		{"missing deferred providers", func(r *Record) { r.DeferredProviders = nil }, "deferred_providers"},
		{"missing jobs", func(r *Record) { r.AdmittedJobs = nil }, "admitted_jobs"},
		{"nameless job", func(r *Record) { r.AdmittedJobs[0].Name = "" }, "admitted_jobs[0]"},
		{"t3 job", func(r *Record) { r.AdmittedJobs[0].TierCeiling = "T3" }, "admitted_jobs[0]"},
		{"job without governance", func(r *Record) { r.AdmittedJobs[0].Governance = "" }, "admitted_jobs[0]"},
		{"t3 not excluded", func(r *Record) { r.ExcludedTiers = []string{"T4"} }, "excluded_tiers"},
		{"missing schedule slice", func(r *Record) { r.ScheduleSlice = "" }, "schedule_slice"},
		{"missing workflow slice", func(r *Record) { r.WorkflowSlice = "" }, "workflow_slice"},
		{"missing displacement", func(r *Record) { r.DisplacedWork = nil }, "displaced_work"},
		{"incomplete displacement", func(r *Record) { r.DisplacedWork[0].Owner = "" }, "displaced_work[0]"},
		{"missing spend ceiling", func(r *Record) { r.SpendCeiling = "" }, "spend_ceiling"},
		{"missing stop criteria", func(r *Record) { r.StopCriteria = nil }, "stop_criteria"},
		{"missing domain owner", func(r *Record) {
			owners := r.Owners[:0]
			for _, o := range r.Owners {
				if o.Role != "Domain owner" {
					owners = append(owners, o)
				}
			}
			r.Owners = owners
		}, "owners"},
		{"missing support owner", func(r *Record) {
			owners := r.Owners[:0]
			for _, o := range r.Owners {
				if o.Role != "Support owner" {
					owners = append(owners, o)
				}
			}
			r.Owners = owners
		}, "owners"},
		{"incomplete owner", func(r *Record) { r.Owners[0].Responsibility = "" }, "owners[0]"},
		{"missing reviewers", func(r *Record) { r.Reviewers = nil }, "reviewers"},
		{"incomplete reviewer", func(r *Record) { r.Reviewers[0].Responsibility = "" }, "reviewers[0]"},
		{"weakened run modes", func(r *Record) { r.AdoptedRunModes = []string{"ON_BEHALF_OF"} }, "adopted_run_modes"},
		{"weakened tiers", func(r *Record) { r.AdoptedTiers = []string{"T0", "T1"} }, "adopted_tiers"},
		{"empty approval rule", func(r *Record) {
			r.AdoptedApprovalDefaults[0].Rule = ""
		}, "adopted_approval_defaults[0]"},
		{"wrong gate", func(r *Record) { r.AdoptedGate.ID = "G-AGENT-PILOT" }, "adopted_gate.id"},
		{"weakened gate todos", func(r *Record) { r.AdoptedGate.RequiredTodos = nil }, "adopted_gate.required_todos"},
		{"gate without opt-in", func(r *Record) { r.AdoptedGate.TenantOptIn = false }, "adopted_gate.tenant_opt_in"},
		{"missing phase1 position", func(r *Record) { r.Phase1Position = "" }, "phase1_position"},
		{"missing workflow slos", func(r *Record) { r.SLOGate.WorkflowSLOs = nil }, "slo_gate.workflow_slos"},
		{"missing chat slos", func(r *Record) { r.SLOGate.ChatSLOs = nil }, "slo_gate.chat_slos"},
		{"signed slo gate", func(r *Record) { r.SLOGate.Status = "SIGNED" }, "slo_gate.status"},
		{"slo gate signer", func(r *Record) { r.SLOGate.Signers = []string{"someone"} }, "slo_gate.signers"},
		{"missing slo signer roles", func(r *Record) { r.SLOGate.RequiredSignerRoles = nil }, "slo_gate.required_signer_roles"},
		{"missing design-only evidence", func(r *Record) { r.DesignOnlyEvidence = nil }, "design_only_evidence"},
		{"missing served capability", func(r *Record) { r.ServedCapability = "" }, "served_capability"},
		{"missing execution plan ref", func(r *Record) { r.ExecutionPlanRef = "" }, "execution_plan_ref"},
		{"missing source evidence", func(r *Record) { r.Evidence = nil }, "source_evidence"},
		{"incomplete source evidence", func(r *Record) { r.Evidence[0].Path = "" }, "source_evidence[0]"},
		{"approval completed", func(r *Record) { r.Approval.Status = "APPROVED" }, "approval.status"},
		{"missing approval roles", func(r *Record) { r.Approval.RequiredRoles = nil }, "approval.required_roles"},
		{"fabricated signer", func(r *Record) { r.Approval.Signers = []string{"someone"} }, "approval.signers"},
		{"stale digest", func(r *Record) { r.CanonicalDigest = "sha256:bad" }, "canonical_digest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := cloneRecord(base)
			tc.mutate(&r)
			for _, violation := range Validate(r) {
				if violation.Field == tc.field {
					return
				}
			}
			t.Fatalf("expected a %s violation, got %v", tc.field, Validate(r))
		})
	}
}

func cloneRecord(base Record) Record {
	r := base
	r.DeferredProviders = append([]string(nil), base.DeferredProviders...)
	r.AdmittedJobs = append([]AdmittedJob(nil), base.AdmittedJobs...)
	r.ExcludedTiers = append([]string(nil), base.ExcludedTiers...)
	r.DisplacedWork = append([]DisplacedWork(nil), base.DisplacedWork...)
	r.StopCriteria = append([]string(nil), base.StopCriteria...)
	r.Owners = append([]Owner(nil), base.Owners...)
	r.Reviewers = append([]Reviewer(nil), base.Reviewers...)
	r.AdoptedRunModes = append([]string(nil), base.AdoptedRunModes...)
	r.AdoptedTiers = append([]string(nil), base.AdoptedTiers...)
	r.AdoptedApprovalDefaults = append([]ApprovalDefault(nil), base.AdoptedApprovalDefaults...)
	for i := range r.AdoptedApprovalDefaults {
		r.AdoptedApprovalDefaults[i].Tiers = append([]string(nil), base.AdoptedApprovalDefaults[i].Tiers...)
	}
	r.AdoptedGate.RequiredTodos = append([]string(nil), base.AdoptedGate.RequiredTodos...)
	r.SLOGate.WorkflowSLOs = append([]SLO(nil), base.SLOGate.WorkflowSLOs...)
	r.SLOGate.ChatSLOs = append([]SLO(nil), base.SLOGate.ChatSLOs...)
	r.SLOGate.RequiredSignerRoles = append([]string(nil), base.SLOGate.RequiredSignerRoles...)
	r.SLOGate.Signers = append([]string(nil), base.SLOGate.Signers...)
	r.DesignOnlyEvidence = append([]Evidence(nil), base.DesignOnlyEvidence...)
	r.Evidence = append([]Evidence(nil), base.Evidence...)
	r.Approval.RequiredRoles = append([]string(nil), base.Approval.RequiredRoles...)
	r.Approval.Signers = append([]string(nil), base.Approval.Signers...)
	return r
}

func TestAgentScopeLoadRejectsUnreadableOrMalformedSource(t *testing.T) {
	if _, err := Load(filepath.Join(repoRoot(t), "does-not-exist.json")); err == nil {
		t.Fatal("missing artifact should fail to load")
	}
	path := filepath.Join(t.TempDir(), "malformed.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed artifact should fail to decode")
	}
}
