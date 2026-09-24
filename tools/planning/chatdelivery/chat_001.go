// Package chatdelivery validates the CHAT-001 scope exchange artifact.
//
// CHAT-001 is a planning gate. The artifact records a proposed separate Gate C
// commitment and its prerequisites; it does not grant launch authority or make
// a customer/pilot deployment claim.
package chatdelivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

const ArtifactPath = "definitions/planning/gates/chat-001-scope-exchange.json"

type Record struct {
	TodoID          string          `json:"todo_id"`
	Status          string          `json:"status"`
	ScopeDecision   string          `json:"scope_decision"`
	ReleaseOwner    string          `json:"release_owner"`
	DisplacedWork   []DisplacedWork `json:"displaced_work"`
	Owners          []Owner         `json:"owners"`
	PilotTenants    []string        `json:"pilot_tenants"`
	PilotCriteria   []string        `json:"pilot_selection_criteria"`
	SLOs            []SLO           `json:"slos"`
	Activation      []string        `json:"activation_conditions"`
	Deactivation    []string        `json:"deactivation_conditions"`
	Blockers        []string        `json:"unmet_gates"`
	Evidence        []Evidence      `json:"source_evidence"`
	PreserveP1A     bool            `json:"preserve_p1a_inventory"`
	PreserveP1B     bool            `json:"preserve_p1b_inventory"`
	Approval        Approval        `json:"approval"`
	CanonicalDigest string          `json:"canonical_digest"`
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

type Evidence struct {
	Path  string `json:"path"`
	Claim string `json:"claim"`
}

type SLO struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

type Approval struct {
	Status        string   `json:"status"`
	RequiredRoles []string `json:"required_roles"`
	Signers       []string `json:"signers"`
}

type Violation struct{ Field, Issue string }

func (v Violation) String() string { return fmt.Sprintf("CHAT-001: %s: %s", v.Field, v.Issue) }

type canonicalRecord struct {
	TodoID        string          `json:"todo_id"`
	Status        string          `json:"status"`
	ScopeDecision string          `json:"scope_decision"`
	ReleaseOwner  string          `json:"release_owner"`
	DisplacedWork []DisplacedWork `json:"displaced_work"`
	Owners        []Owner         `json:"owners"`
	PilotTenants  []string        `json:"pilot_tenants"`
	PilotCriteria []string        `json:"pilot_selection_criteria"`
	SLOs          []SLO           `json:"slos"`
	Activation    []string        `json:"activation_conditions"`
	Deactivation  []string        `json:"deactivation_conditions"`
	Blockers      []string        `json:"unmet_gates"`
	Evidence      []Evidence      `json:"source_evidence"`
	PreserveP1A   bool            `json:"preserve_p1a_inventory"`
	PreserveP1B   bool            `json:"preserve_p1b_inventory"`
	Approval      Approval        `json:"approval"`
}

func canonical(r Record) canonicalRecord {
	return canonicalRecord{r.TodoID, r.Status, r.ScopeDecision, r.ReleaseOwner, r.DisplacedWork,
		r.Owners, r.PilotTenants, r.PilotCriteria, r.SLOs, r.Activation, r.Deactivation, r.Blockers,
		r.Evidence, r.PreserveP1A, r.PreserveP1B, r.Approval}
}

func Digest(r Record) (string, error) {
	b, err := json.Marshal(canonical(r))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
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
	if r.TodoID != "CHAT-001" {
		add("todo_id", "must be CHAT-001")
	}
	if r.Status != "PROPOSED" {
		add("status", "must remain PROPOSED until approval evidence exists")
	}
	if r.ScopeDecision != "SEPARATE_GATE_C" {
		add("scope_decision", "must preserve a separate Gate C commitment")
	}
	if r.ReleaseOwner == "" {
		add("release_owner", "must name the accountable release owner role")
	}
	if len(r.DisplacedWork) == 0 {
		add("displaced_work", "must identify the capacity exchange")
	}
	for i, w := range r.DisplacedWork {
		if w.Workstream == "" || w.Disposition == "" || w.Owner == "" {
			add(fmt.Sprintf("displaced_work[%d]", i), "workstream, disposition and owner are required")
		}
	}
	if len(r.Owners) == 0 {
		add("owners", "must name accountable release-gate roles")
	}
	for i, owner := range r.Owners {
		if owner.Role == "" || owner.Status == "" || owner.Responsibility == "" {
			add(fmt.Sprintf("owners[%d]", i), "role, assignment status and responsibility are required")
		}
	}
	if len(r.PilotTenants) == 0 {
		add("pilot_tenants", "must name the pilot selection or explicitly record UNSELECTED")
	}
	if len(r.PilotCriteria) == 0 {
		add("pilot_selection_criteria", "must define pilot-tenant selection criteria")
	}
	if len(r.SLOs) == 0 {
		add("slos", "must record measurable reliability targets")
	}
	for i, s := range r.SLOs {
		if s.Name == "" || s.Target == "" {
			add(fmt.Sprintf("slos[%d]", i), "name and target are required")
		}
	}
	if len(r.Activation) == 0 {
		add("activation_conditions", "must record activation conditions")
	}
	if len(r.Deactivation) == 0 {
		add("deactivation_conditions", "must define pilot stop and rollback conditions")
	}
	if len(r.Blockers) == 0 {
		add("unmet_gates", "must record remaining external gates")
	}
	if len(r.Evidence) == 0 {
		add("source_evidence", "must cite repository evidence for the draft")
	}
	for i, item := range r.Evidence {
		if item.Path == "" || item.Claim == "" {
			add(fmt.Sprintf("source_evidence[%d]", i), "path and claim are required")
		}
	}
	if !r.PreserveP1A || !r.PreserveP1B {
		add("preserve_p1_inventory", "must preserve existing P1A and P1B inventory")
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
