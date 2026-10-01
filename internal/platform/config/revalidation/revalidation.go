// Package revalidation prevents paused or waiting work from resuming against
// revoked or incompatible configuration. The classifier is pure; Registry is
// an in-memory atomic adapter suitable for control-plane integration tests.
package revalidation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

const schemaVersion = 1

// ServingContractID identifies the revalidation contract composed by the
// shipped application cell.
const ServingContractID = "hcmnext.config.revalidation/v1"

// ValidateServingContract proves the served path fences a changed workload
// before it can resume. The check is deterministic and keeps all state local
// to this invocation.
func ValidateServingContract() error {
	dependency := func(key string, kind DependencyKind) Dependency {
		return Dependency{Key: key, Kind: kind, Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64), Active: true}
	}
	dependencies := []Dependency{
		dependency("serving:rule", KindRule),
		dependency("serving:form", KindForm),
		dependency("serving:mapping", KindMapping),
		dependency("serving:population", KindPopulation),
		dependency("serving:schema", KindSchema),
		dependency("serving:capability", KindCapability),
	}
	workload := Workload{
		ID:           "serving-contract:config-revalidation",
		TenantID:     "serving-contract",
		State:        "PAUSED",
		Dependencies: dependencies,
	}
	registry := NewRegistry()
	if err := registry.Put(workload); err != nil {
		return fmt.Errorf("revalidation: serving contract put: %w", err)
	}
	graph := Graph{SchemaVersion: schemaVersion}
	decision, err := registry.Revalidate(workload.ID, dependencies, graph)
	if err != nil {
		return fmt.Errorf("revalidation: serving contract baseline: %w", err)
	}
	if decision.Status != StatusValid {
		return fmt.Errorf("revalidation: serving contract baseline status = %s", decision.Status)
	}
	current := append([]Dependency(nil), dependencies...)
	current[0].Active = false
	decision, err = registry.Revalidate(workload.ID, current, graph)
	if err != nil {
		return fmt.Errorf("revalidation: serving contract revoke: %w", err)
	}
	if decision.Status != StatusInvalidated || decision.FenceToken == 0 {
		return fmt.Errorf("revalidation: serving contract revoke decision = %+v", decision)
	}
	stored, ok := registry.Get(workload.ID)
	if !ok || !stored.Fenced || stored.FenceToken != decision.FenceToken {
		return fmt.Errorf("revalidation: serving contract did not retain fence: %+v", stored)
	}
	return nil
}

// Version reports the live configuration revalidation schema version.
func Version() int { return schemaVersion }

// Explain describes the deterministic dependency-closure contract.
func Explain() string {
	return "CONFIG-010 v1: atomic workload fencing and deterministic invalidate, replan, migrate, or block outcomes"
}

// Status is the result of revalidating one active workload.
type Status string

const (
	StatusValid       Status = "VALID"
	StatusInvalidated Status = "INVALIDATED"
	StatusReplan      Status = "REPLAN_REQUIRED"
	StatusMigrate     Status = "MIGRATE_REQUIRED"
	StatusBlocked     Status = "BLOCKED"
)

// DependencyKind names the supported configuration object types.
type DependencyKind string

const (
	KindRule       DependencyKind = "RULE"
	KindForm       DependencyKind = "FORM"
	KindMapping    DependencyKind = "MAPPING"
	KindPopulation DependencyKind = "POPULATION"
	KindSchema     DependencyKind = "SCHEMA"
	KindCapability DependencyKind = "CAPABILITY"
)

// Dependency is the version and lifecycle fact pinned by a workload.
type Dependency struct {
	Key            string         `json:"key"`
	Kind           DependencyKind `json:"kind"`
	Version        string         `json:"version"`
	Digest         string         `json:"digest"`
	Active         bool           `json:"active"`
	ReplacementKey string         `json:"replacement_key"`
}

// Workload is a paused, waiting, or approval-pending unit that must be fenced
// before any timer, task, or human action can resume.
type Workload struct {
	ID           string       `json:"id"`
	TenantID     string       `json:"tenant_id"`
	State        string       `json:"state"`
	Dependencies []Dependency `json:"dependencies"`
	FenceToken   uint64       `json:"fence_token"`
	Fenced       bool         `json:"fenced"`
}

// Compatibility is a reviewed compatibility or migration edge.
type Compatibility struct {
	Kind         DependencyKind `json:"kind"`
	Key          string         `json:"key"`
	FromVersion  string         `json:"from_version"`
	FromDigest   string         `json:"from_digest"`
	ToVersion    string         `json:"to_version"`
	ToDigest     string         `json:"to_digest"`
	Compatible   bool           `json:"compatible"`
	MigrationRef string         `json:"migration_ref"`
	Reviewed     bool           `json:"reviewed"`
}

// Graph is the published compatibility view used for one revalidation.
type Graph struct {
	SchemaVersion int             `json:"schema_version"`
	Dependencies  []Dependency    `json:"dependencies"`
	Edges         []Compatibility `json:"edges"`
}

// Decision is immutable evidence for one revalidation attempt.
type Decision struct {
	WorkloadID     string   `json:"workload_id"`
	Status         Status   `json:"status"`
	FenceToken     uint64   `json:"fence_token"`
	Changed        []string `json:"changed"`
	Reasons        []string `json:"reasons"`
	EvidenceDigest string   `json:"evidence_digest"`
}

// Validate returns structural defects in the workload and graph.
func Validate(w Workload, current []Dependency, graph Graph) []string {
	var out []string
	if strings.TrimSpace(w.ID) == "" {
		out = append(out, "missing workload id")
	}
	if strings.TrimSpace(w.TenantID) == "" {
		out = append(out, "missing tenant id")
	}
	if !map[string]bool{"PAUSED": true, "WAITING": true, "APPROVAL_PENDING": true}[strings.ToUpper(w.State)] {
		out = append(out, "workload state must be PAUSED, WAITING, or APPROVAL_PENDING")
	}
	if len(w.Dependencies) == 0 {
		out = append(out, "workload has no configuration dependencies")
	}
	if graph.SchemaVersion != schemaVersion {
		out = append(out, "unsupported graph schema")
	}
	if duplicateKeys(w.Dependencies) {
		out = append(out, "workload dependencies must be unique")
	}
	if duplicateKeys(current) {
		out = append(out, "current dependencies must be unique")
	}
	for _, d := range append(append([]Dependency(nil), w.Dependencies...), current...) {
		if strings.TrimSpace(d.Key) == "" || strings.TrimSpace(string(d.Kind)) == "" || strings.TrimSpace(d.Version) == "" || strings.TrimSpace(d.Digest) == "" {
			out = append(out, "dependency requires key, kind, version, and digest")
		}
	}
	sort.Strings(out)
	return out
}

// Check returns the first structural defect.
func Check(w Workload, current []Dependency, graph Graph) error {
	if defects := Validate(w, current, graph); len(defects) > 0 {
		return fmt.Errorf("revalidation: %s", defects[0])
	}
	return nil
}

// Revalidate classifies the full dependency closure without changing w.
func Revalidate(w Workload, current []Dependency, graph Graph) (Decision, error) {
	if err := Check(w, current, graph); err != nil {
		return Decision{}, err
	}
	currentByKey := make(map[string]Dependency, len(current))
	for _, d := range current {
		currentByKey[d.Key] = d
	}
	var changed, reasons []string
	status := StatusValid
	for _, pinned := range sortedDependencies(w.Dependencies) {
		live, ok := currentByKey[pinned.Key]
		if !ok {
			status = maxStatus(status, StatusBlocked)
			changed = append(changed, pinned.Key)
			reasons = append(reasons, pinned.Key+": current dependency is unknown")
			continue
		}
		if !live.Active {
			changed = append(changed, pinned.Key)
			if live.ReplacementKey != "" && reviewedMigration(pinned, live, live.ReplacementKey, graph) {
				status = maxStatus(status, StatusMigrate)
				reasons = append(reasons, pinned.Key+": revoked dependency has a reviewed replacement")
			} else {
				status = maxStatus(status, StatusInvalidated)
				reasons = append(reasons, pinned.Key+": dependency was revoked")
			}
			continue
		}
		if live.Version == pinned.Version && live.Digest == pinned.Digest {
			continue
		}
		changed = append(changed, pinned.Key)
		if reviewedMigration(pinned, live, live.Key, graph) {
			status = maxStatus(status, StatusMigrate)
			reasons = append(reasons, pinned.Key+": changed dependency has a reviewed migration")
		} else {
			status = maxStatus(status, StatusReplan)
			reasons = append(reasons, pinned.Key+": version or digest is incompatible without reviewed migration")
		}
	}
	sort.Strings(changed)
	sort.Strings(reasons)
	decision := Decision{WorkloadID: w.ID, Status: status, FenceToken: w.FenceToken, Changed: changed, Reasons: reasons}
	digest, err := decisionDigest(decision)
	if err != nil {
		return Decision{}, err
	}
	decision.EvidenceDigest = digest
	return decision, nil
}

func reviewedMigration(from, to Dependency, key string, graph Graph) bool {
	for _, edge := range graph.Edges {
		if edge.Kind == from.Kind && edge.Key == key && edge.FromVersion == from.Version && edge.FromDigest == from.Digest && edge.ToVersion == to.Version && edge.ToDigest == to.Digest && edge.Compatible && edge.Reviewed && strings.TrimSpace(edge.MigrationRef) != "" {
			return true
		}
	}
	return false
}

func maxStatus(a, b Status) Status {
	priority := map[Status]int{StatusValid: 0, StatusMigrate: 1, StatusReplan: 2, StatusInvalidated: 3, StatusBlocked: 4}
	if priority[b] > priority[a] {
		return b
	}
	return a
}

// Registry atomically fences a stored workload when revalidation finds a
// changed or unknown dependency.
type Registry struct {
	mu        sync.RWMutex
	workloads map[string]Workload
}

// NewRegistry creates an empty in-memory revalidation registry.
func NewRegistry() *Registry { return &Registry{workloads: make(map[string]Workload)} }

// Put validates and stores a copy of a workload.
func (r *Registry) Put(w Workload) error {
	if strings.TrimSpace(w.ID) == "" {
		return errors.New("revalidation: workload id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.workloads[w.ID]; exists {
		return errors.New("revalidation: workload already exists")
	}
	r.workloads[w.ID] = cloneWorkload(w)
	return nil
}

// Revalidate fences the workload and increments its token atomically before
// returning the evidence. A valid result leaves a previously fenced workload
// fenced; only Resume can clear that state with matching evidence.
func (r *Registry) Revalidate(id string, current []Dependency, graph Graph) (Decision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.workloads[id]
	if !ok {
		return Decision{}, errors.New("revalidation: workload not found")
	}
	d, err := Revalidate(w, current, graph)
	if err != nil {
		return Decision{}, err
	}
	if d.Status != StatusValid {
		w.FenceToken++
		w.Fenced = true
		d.FenceToken = w.FenceToken
		digest, digestErr := decisionDigest(d)
		if digestErr != nil {
			return Decision{}, digestErr
		}
		d.EvidenceDigest = digest
		r.workloads[id] = cloneWorkload(w)
	}
	return d, nil
}

// Resume clears a fence only for a valid decision with the current evidence
// digest. Replanning or migration must be completed before this is possible.
func (r *Registry) Resume(id, evidenceDigest string, d Decision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.workloads[id]
	if !ok {
		return errors.New("revalidation: workload not found")
	}
	if d.WorkloadID != id || d.Status != StatusValid || d.EvidenceDigest != evidenceDigest || w.Fenced {
		return errors.New("revalidation: resume requires matching unfenced valid evidence")
	}
	return nil
}

// Get returns an immutable copy of the stored workload.
func (r *Registry) Get(id string) (Workload, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.workloads[id]
	return cloneWorkload(w), ok
}

func cloneWorkload(w Workload) Workload {
	w.Dependencies = append([]Dependency(nil), w.Dependencies...)
	return w
}
func sortedDependencies(values []Dependency) []Dependency {
	out := append([]Dependency(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
func duplicateKeys(values []Dependency) bool {
	seen := map[string]bool{}
	for _, d := range values {
		if seen[d.Key] {
			return true
		}
		seen[d.Key] = true
	}
	return false
}
func decisionDigest(d Decision) (string, error) {
	d.EvidenceDigest = ""
	data, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
