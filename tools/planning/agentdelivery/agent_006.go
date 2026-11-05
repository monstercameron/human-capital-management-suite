// Package agentdelivery validates the AGENT-006 scope exchange artifact.
//
// AGENT-006 is a planning gate. The artifact records a proposed agent pilot:
// the first cohort, one served provider, admitted jobs, the schedule and
// workflow slice, displaced work, the spend ceiling, stop criteria and
// accountable reviewers. It adopts the AGENT2-001 run modes, side-effect
// tiers, approval defaults and the G-AGENT-OBO gate as the release position
// on the Phase 1 autonomous-execution deferral. It grants no launch authority
// and claims neither committed Phase 1 scope nor chat-release scope.
package agentdelivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
)

const ArtifactPath = "definitions/planning/gates/agent-006-scope-exchange.json"

type Record struct {
	TodoID                  string            `json:"todo_id"`
	Version                 string            `json:"version"`
	Status                  string            `json:"status"`
	ScopeDecision           string            `json:"scope_decision"`
	FirstCohort             string            `json:"first_cohort"`
	ServedProvider          string            `json:"served_provider"`
	DeferredProviders       []string          `json:"deferred_providers"`
	AdmittedJobs            []AdmittedJob     `json:"admitted_jobs"`
	ExcludedTiers           []string          `json:"excluded_tiers"`
	ScheduleSlice           string            `json:"schedule_slice"`
	WorkflowSlice           string            `json:"workflow_slice"`
	DisplacedWork           []DisplacedWork   `json:"displaced_work"`
	SpendCeiling            string            `json:"spend_ceiling"`
	StopCriteria            []string          `json:"stop_criteria"`
	Owners                  []Owner           `json:"owners"`
	Reviewers               []Reviewer        `json:"reviewers"`
	AdoptedRunModes         []string          `json:"adopted_run_modes"`
	AdoptedTiers            []string          `json:"adopted_tiers"`
	AdoptedApprovalDefaults []ApprovalDefault `json:"adopted_approval_defaults"`
	AdoptedGate             AdoptedGate       `json:"adopted_gate"`
	Phase1Position          string            `json:"phase1_position"`
	SLOGate                 SLOGate           `json:"slo_gate"`
	DesignOnlyEvidence      []Evidence        `json:"design_only_evidence"`
	ServedCapability        string            `json:"served_capability"`
	ExecutionPlanRef        string            `json:"execution_plan_ref"`
	Evidence                []Evidence        `json:"source_evidence"`
	Approval                Approval          `json:"approval"`
	CanonicalDigest         string            `json:"canonical_digest"`
}

type AdmittedJob struct {
	Name        string `json:"name"`
	TierCeiling string `json:"tier_ceiling"`
	Governance  string `json:"governance"`
}

type DisplacedWork struct {
	Workstream  string `json:"workstream"`
	Disposition string `json:"disposition"`
	Owner       string `json:"owner"`
}

type Owner struct {
	Role           string `json:"role"`
	Status         string `json:"status"`
	Responsibility string `json:"responsibility"`
}

type Reviewer struct {
	Role           string `json:"role"`
	Responsibility string `json:"responsibility"`
}

type ApprovalDefault struct {
	Tiers []string `json:"tiers"`
	Rule  string   `json:"rule"`
}

type AdoptedGate struct {
	ID             string   `json:"id"`
	RequiredTodos  []string `json:"required_todos"`
	TenantOptIn    bool     `json:"tenant_opt_in"`
	T3Default      string   `json:"t3_default"`
	T4FirstRelease string   `json:"t4_first_release"`
	ShadowPeriod   string   `json:"shadow_period"`
}

type SLOGate struct {
	WorkflowSLOs        []SLO    `json:"workflow_slos"`
	ChatSLOs            []SLO    `json:"chat_slos"`
	Status              string   `json:"status"`
	RequiredSignerRoles []string `json:"required_signer_roles"`
	Signers             []string `json:"signers"`
}

type SLO struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

type Evidence struct {
	Path  string `json:"path"`
	Claim string `json:"claim"`
}

type Approval struct {
	Status        string   `json:"status"`
	RequiredRoles []string `json:"required_roles"`
	Signers       []string `json:"signers"`
}

type Violation struct{ Field, Issue string }

func (v Violation) String() string { return fmt.Sprintf("AGENT-006: %s: %s", v.Field, v.Issue) }

type canonicalRecord struct {
	TodoID                  string            `json:"todo_id"`
	Version                 string            `json:"version"`
	Status                  string            `json:"status"`
	ScopeDecision           string            `json:"scope_decision"`
	FirstCohort             string            `json:"first_cohort"`
	ServedProvider          string            `json:"served_provider"`
	DeferredProviders       []string          `json:"deferred_providers"`
	AdmittedJobs            []AdmittedJob     `json:"admitted_jobs"`
	ExcludedTiers           []string          `json:"excluded_tiers"`
	ScheduleSlice           string            `json:"schedule_slice"`
	WorkflowSlice           string            `json:"workflow_slice"`
	DisplacedWork           []DisplacedWork   `json:"displaced_work"`
	SpendCeiling            string            `json:"spend_ceiling"`
	StopCriteria            []string          `json:"stop_criteria"`
	Owners                  []Owner           `json:"owners"`
	Reviewers               []Reviewer        `json:"reviewers"`
	AdoptedRunModes         []string          `json:"adopted_run_modes"`
	AdoptedTiers            []string          `json:"adopted_tiers"`
	AdoptedApprovalDefaults []ApprovalDefault `json:"adopted_approval_defaults"`
	AdoptedGate             AdoptedGate       `json:"adopted_gate"`
	Phase1Position          string            `json:"phase1_position"`
	SLOGate                 SLOGate           `json:"slo_gate"`
	DesignOnlyEvidence      []Evidence        `json:"design_only_evidence"`
	ServedCapability        string            `json:"served_capability"`
	ExecutionPlanRef        string            `json:"execution_plan_ref"`
	Evidence                []Evidence        `json:"source_evidence"`
	Approval                Approval          `json:"approval"`
}

func canonical(r Record) canonicalRecord {
	return canonicalRecord{r.TodoID, r.Version, r.Status, r.ScopeDecision, r.FirstCohort,
		r.ServedProvider, r.DeferredProviders, r.AdmittedJobs, r.ExcludedTiers, r.ScheduleSlice,
		r.WorkflowSlice, r.DisplacedWork, r.SpendCeiling, r.StopCriteria, r.Owners, r.Reviewers,
		r.AdoptedRunModes, r.AdoptedTiers, r.AdoptedApprovalDefaults, r.AdoptedGate,
		r.Phase1Position, r.SLOGate, r.DesignOnlyEvidence, r.ServedCapability,
		r.ExecutionPlanRef, r.Evidence, r.Approval}
}

func Digest(r Record) (string, error) {
	b, err := json.Marshal(canonical(r))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func jsonUnmarshal(b []byte, v any) error {
	return json.Unmarshal(b, v)
}

func Load(path string) (Record, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return Record{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return r, nil
}

func Validate(r Record) []Violation {
	var out []Violation
	add := func(field, issue string) { out = append(out, Violation{field, issue}) }
	if r.TodoID != "AGENT-006" {
		add("todo_id", "must be AGENT-006")
	}
	if strings.TrimSpace(r.Version) == "" {
		add("version", "the scope decision is versioned; a version is required")
	}
	if r.Status != "PROPOSED" {
		add("status", "must remain PROPOSED until approval evidence exists")
	}
	if r.ScopeDecision != "SEPARATE_PILOT" {
		add("scope_decision", "must propose a separate pilot: agent delivery is neither committed Phase 1 nor chat-release scope")
	}
	if strings.TrimSpace(r.FirstCohort) == "" {
		add("first_cohort", "must name the first pilot cohort")
	}
	if strings.TrimSpace(r.ServedProvider) == "" {
		add("served_provider", "must name the pilot's one served provider")
	} else if strings.ContainsAny(r.ServedProvider, ",;") {
		add("served_provider", "must name exactly one served provider")
	}
	if len(r.DeferredProviders) == 0 {
		add("deferred_providers", "must record which providers are deferred and why")
	}
	if len(r.AdmittedJobs) == 0 {
		add("admitted_jobs", "must admit at least one agent job")
	}
	for i, job := range r.AdmittedJobs {
		field := fmt.Sprintf("admitted_jobs[%d]", i)
		switch {
		case strings.TrimSpace(job.Name) == "":
			add(field, "name is required")
		case job.TierCeiling != "T0" && job.TierCeiling != "T1" && job.TierCeiling != "T2":
			add(field, "the pilot admits T0 to T2 jobs only; T3 and T4 stay excluded")
		case strings.TrimSpace(job.Governance) == "":
			add(field, "must state the governance path for the job")
		}
	}
	if !reflect.DeepEqual(r.ExcludedTiers, []string{"T3", "T4"}) {
		add("excluded_tiers", "must exclude exactly T3 and T4 from the pilot")
	}
	if strings.TrimSpace(r.ScheduleSlice) == "" {
		add("schedule_slice", "must bound the schedule slice under test")
	}
	if strings.TrimSpace(r.WorkflowSlice) == "" {
		add("workflow_slice", "must bound the workflow slice under test")
	}
	if len(r.DisplacedWork) == 0 {
		add("displaced_work", "must identify the capacity exchange")
	}
	for i, w := range r.DisplacedWork {
		if w.Workstream == "" || w.Disposition == "" || w.Owner == "" {
			add(fmt.Sprintf("displaced_work[%d]", i), "workstream, disposition and owner are required")
		}
	}
	if strings.TrimSpace(r.SpendCeiling) == "" {
		add("spend_ceiling", "must state the pilot cost envelope")
	}
	if len(r.StopCriteria) == 0 {
		add("stop_criteria", "must define pilot stop and rollback conditions")
	}
	roles := map[string]bool{}
	for i, owner := range r.Owners {
		roles[owner.Role] = true
		if owner.Role == "" || owner.Status == "" || owner.Responsibility == "" {
			add(fmt.Sprintf("owners[%d]", i), "role, assignment status and responsibility are required")
		}
	}
	if !roles["Domain owner"] {
		add("owners", "must name the accountable domain owner")
	}
	if !roles["Support owner"] {
		add("owners", "must name the accountable support owner")
	}
	if len(r.Reviewers) == 0 {
		add("reviewers", "must name accountable reviewers")
	}
	for i, reviewer := range r.Reviewers {
		if reviewer.Role == "" || reviewer.Responsibility == "" {
			add(fmt.Sprintf("reviewers[%d]", i), "role and responsibility are required")
		}
	}
	if !reflect.DeepEqual(r.AdoptedRunModes, []string{"ON_BEHALF_OF", "SPONSORED"}) {
		add("adopted_run_modes", "must adopt exactly the AGENT2-001 run modes")
	}
	if !reflect.DeepEqual(r.AdoptedTiers, []string{"T0", "T1", "T2", "T3", "T4"}) {
		add("adopted_tiers", "must adopt exactly the AGENT2-001 side-effect tiers")
	}
	if len(r.AdoptedApprovalDefaults) == 0 {
		add("adopted_approval_defaults", "must adopt the AGENT2-001 approval defaults")
	}
	covered := map[string]bool{}
	for i, row := range r.AdoptedApprovalDefaults {
		field := fmt.Sprintf("adopted_approval_defaults[%d]", i)
		if len(row.Tiers) == 0 || strings.TrimSpace(row.Rule) == "" {
			add(field, "tiers and rule are required")
			continue
		}
		for _, tier := range row.Tiers {
			covered[tier] = true
		}
	}
	for _, tier := range []string{"T0", "T1", "T2", "T3", "T4"} {
		if !covered[tier] {
			add("adopted_approval_defaults", fmt.Sprintf("no adopted rule covers tier %s", tier))
		}
	}
	if r.AdoptedGate.ID != "G-AGENT-OBO" {
		add("adopted_gate.id", "must adopt the G-AGENT-OBO gate")
	}
	if !reflect.DeepEqual(r.AdoptedGate.RequiredTodos, []string{"AGENT2-023", "AGENT2-024", "AGENT2-025"}) {
		add("adopted_gate.required_todos", "must require AGENT2-023, AGENT2-024 and AGENT2-025")
	}
	if !r.AdoptedGate.TenantOptIn {
		add("adopted_gate.tenant_opt_in", "must keep tenant opt-in")
	}
	if strings.TrimSpace(r.AdoptedGate.T3Default) == "" {
		add("adopted_gate.t3_default", "must state the T3 default")
	}
	if strings.TrimSpace(r.AdoptedGate.T4FirstRelease) == "" {
		add("adopted_gate.t4_first_release", "must state the first-release T4 position")
	}
	if strings.TrimSpace(r.AdoptedGate.ShadowPeriod) == "" {
		add("adopted_gate.shadow_period", "must state the shadow period")
	}
	if strings.TrimSpace(r.Phase1Position) == "" {
		add("phase1_position", "must state the release position on the Phase 1 autonomous-execution deferral")
	}
	if len(r.SLOGate.WorkflowSLOs) == 0 {
		add("slo_gate.workflow_slos", "must record the workflow SLO slice under the gate")
	}
	for i, s := range r.SLOGate.WorkflowSLOs {
		if s.Name == "" || s.Target == "" {
			add(fmt.Sprintf("slo_gate.workflow_slos[%d]", i), "name and target are required")
		}
	}
	if len(r.SLOGate.ChatSLOs) == 0 {
		add("slo_gate.chat_slos", "must record the chat SLO slice under the gate")
	}
	for i, s := range r.SLOGate.ChatSLOs {
		if s.Name == "" || s.Target == "" {
			add(fmt.Sprintf("slo_gate.chat_slos[%d]", i), "name and target are required")
		}
	}
	if r.SLOGate.Status != "PENDING" {
		add("slo_gate.status", "must remain PENDING until real signers and evidence are recorded")
	}
	if len(r.SLOGate.RequiredSignerRoles) == 0 {
		add("slo_gate.required_signer_roles", "must name the SLO gate signer roles")
	}
	if len(r.SLOGate.Signers) != 0 {
		add("slo_gate.signers", "must stay empty until the gate is actually signed")
	}
	if len(r.DesignOnlyEvidence) == 0 {
		add("design_only_evidence", "must cite the design-only security evidence")
	}
	for i, item := range r.DesignOnlyEvidence {
		if item.Path == "" || item.Claim == "" {
			add(fmt.Sprintf("design_only_evidence[%d]", i), "path and claim are required")
		}
	}
	if strings.TrimSpace(r.ServedCapability) == "" {
		add("served_capability", "must state the served capability distinctly from design-only evidence")
	}
	if strings.TrimSpace(r.ExecutionPlanRef) == "" {
		add("execution_plan_ref", "must point at the execution plan holding the release decision")
	}
	if len(r.Evidence) == 0 {
		add("source_evidence", "must cite repository evidence for the draft")
	}
	for i, item := range r.Evidence {
		if item.Path == "" || item.Claim == "" {
			add(fmt.Sprintf("source_evidence[%d]", i), "path and claim are required")
		}
	}
	if r.Approval.Status != "PENDING" {
		add("approval.status", "must remain PENDING until real signers and evidence are recorded")
	}
	if len(r.Approval.RequiredRoles) == 0 {
		add("approval.required_roles", "must name the approval roles")
	}
	if len(r.Approval.Signers) != 0 {
		add("approval.signers", "must stay empty until external approval is actually obtained")
	}
	d, err := Digest(r)
	if err != nil || r.CanonicalDigest != d {
		add("canonical_digest", "must match the canonical record digest")
	}
	return out
}
