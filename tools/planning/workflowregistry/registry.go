// Package workflowregistry owns the WF-DISC-001 research registry. It scans
// the workflow research corpus, registers every artifact with its truthful
// discovery state and a content digest, and reports orphan, stale, duplicate
// and dishonestly promoted entries as exact findings. It is kernel-pure: file
// parsing and sorting only, no database, network or mutable global.
//
// Reachability class and ownership: planning conformance tooling, not a
// runtime capability. It is intentionally outside every shipped binary's
// dependency closure and claims no runtime capability ownership; it is
// accountable to the Platform Engineering owner like the rest of the
// planning-policy checker cohort, recorded in
// planning/specs/specification-ownership-registry.md.
package workflowregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Artifact roles. WORKFLOW documents carry a workflow identity; SAMPLE files
// are exploratory sketches; CATALOG files version the shared dimensions;
// INDEX files link the corpus together; SUPPORT files are engine machinery.
const (
	KindWorkflow = "WORKFLOW"
	KindSample   = "SAMPLE"
	KindCatalog  = "CATALOG"
	KindIndex    = "INDEX"
	KindSupport  = "SUPPORT"
)

// Discovery states from the workflow exploration index. EXTRACTED, REFERENCE
// and EXPLORED describe research progress; CONTRACTED and IMPLEMENTED assert
// production contracts and require evidence (see contract_ref, evidence).
const (
	StateExtracted   = "EXTRACTED"
	StateReference   = "REFERENCE"
	StateExplored    = "EXPLORED"
	StateContracted  = "CONTRACTED"
	StateImplemented = "IMPLEMENTED"
)

// Artifact is one registered research file.
type Artifact struct {
	Path                string   `json:"path"`
	Kind                string   `json:"kind"`
	Digest              string   `json:"digest"`
	WorkflowID          string   `json:"workflow_id,omitempty"`
	Intents             []string `json:"intents,omitempty"`
	TargetIntent        string   `json:"target_intent,omitempty"`
	Domain              string   `json:"domain,omitempty"`
	KernelFamily        string   `json:"kernel_family,omitempty"`
	States              []string `json:"states,omitempty"`
	Owner               string   `json:"owner,omitempty"`
	ContractRef         string   `json:"contract_ref,omitempty"`
	Evidence            []string `json:"evidence,omitempty"`
	StepsPresent        bool     `json:"steps_present"`
	DependenciesPresent bool     `json:"dependencies_present"`
	pending             []Finding
}

// Finding is one exact registry diagnostic.
type Finding struct {
	Path   string `json:"path,omitempty"`
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Detail string `json:"detail"`
}

// Registry is the complete scan result.
type Registry struct {
	Artifacts []Artifact `json:"artifacts"`
	Findings  []Finding  `json:"findings,omitempty"`
	Digest    string     `json:"digest"`
	collected bool
}

// OK reports whether the registry holds no findings.
func (r Registry) OK() bool { return len(r.Findings) == 0 }

var linkPattern = regexp.MustCompile(`\]\(([^)#]+\.md)(?:#[^)]*)?\)`)

// Scan registers every Markdown document and catalog.yaml below root. Paths
// are slash-relative to root; artifacts and findings sort deterministically.
func Scan(root string) (Registry, error) {
	if strings.TrimSpace(root) == "" {
		return Registry{}, errors.New("workflowregistry: scan root is empty")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Registry{}, fmt.Errorf("workflowregistry: resolve root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return Registry{}, fmt.Errorf("workflowregistry: stat root: %w", err)
	}
	if !info.IsDir() {
		return Registry{}, fmt.Errorf("workflowregistry: root %s is not a directory", root)
	}
	registry := Registry{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() != "catalog.yaml" && !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		artifact, err := parseArtifact(root, path)
		if err != nil {
			return err
		}
		registry.Artifacts = append(registry.Artifacts, artifact)
		return nil
	})
	if err != nil {
		return Registry{}, err
	}
	sort.Slice(registry.Artifacts, func(i, j int) bool { return registry.Artifacts[i].Path < registry.Artifacts[j].Path })
	registry.checkDuplicates()
	registry.checkIndexLinks(root)
	registry.sortFindings()
	registry.Digest = digestRegistry(registry.Artifacts)
	return registry, nil
}

// ResolveAcceptedIntents appends an UNRESOLVED_INTENT finding for every
// target intent outside the accepted catalog. Legacy intents are historical
// source behavior and stay non-authoritative evidence, so they are exempt. A
// nil accepted list disables the check.
func (r *Registry) ResolveAcceptedIntents(accepted []string) {
	if accepted == nil {
		return
	}
	member := make(map[string]bool, len(accepted))
	for _, id := range accepted {
		member[id] = true
	}
	for _, artifact := range r.Artifacts {
		if artifact.TargetIntent == "" || member[artifact.TargetIntent] {
			continue
		}
		r.Findings = append(r.Findings, Finding{
			Path: artifact.Path, Code: "UNRESOLVED_INTENT", Field: "target_intent",
			Detail: "target intent " + artifact.TargetIntent + " is not an accepted definition",
		})
	}
	r.sortFindings()
}

func parseArtifact(root, path string) (Artifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("workflowregistry: read %s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return Artifact{}, err
	}
	rel = filepath.ToSlash(rel)
	sum := sha256.Sum256(data)
	artifact := Artifact{
		Path:   rel,
		Kind:   classify(rel),
		Digest: "sha256:" + hex.EncodeToString(sum[:]),
		Domain: domainOf(rel),
	}
	if artifact.Kind == KindCatalog && strings.HasSuffix(rel, ".yaml") {
		artifact.WorkflowID = ""
		return artifact, nil
	}
	fields := textBlockFields(string(data))
	artifact.WorkflowID = firstOf(fields, "workflow_id")
	artifact.Intents = append(artifact.Intents, fields["legacy_intent"]...)
	artifact.Intents = append(artifact.Intents, fields["target_intent"]...)
	if targets := fields["target_intent"]; len(targets) > 0 {
		artifact.TargetIntent = targets[0]
	}
	artifact.KernelFamily = firstOf(fields, "kernel_family")
	artifact.States = parseStates(firstOf(fields, "state"))
	artifact.Owner = firstOf(fields, "owner_domain")
	artifact.ContractRef = firstOf(fields, "contract_ref")
	artifact.Evidence = append(artifact.Evidence, fields["evidence"]...)
	artifact.Evidence = append(artifact.Evidence, fields["evidence_link"]...)
	artifact.Evidence = append(artifact.Evidence, fields["evidence_links"]...)
	body := strings.ToLower(string(data))
	artifact.StepsPresent = strings.Contains(body, "## steps")
	artifact.DependenciesPresent = strings.Contains(body, "dependenc")
	artifact.checkIdentity()
	return artifact, nil
}

func classify(rel string) string {
	base := rel
	if index := strings.LastIndex(rel, "/"); index >= 0 {
		base = rel[index+1:]
	}
	switch {
	case base == "catalog.yaml" || base == "catalog.md":
		return KindCatalog
	case strings.HasPrefix(rel, "samples/"):
		return KindSample
	case strings.HasPrefix(rel, "_engine/") || strings.HasPrefix(rel, "_shared/"):
		return KindSupport
	case base == "README.md" || base == "discovery-backlog.md" || base == "plan.md" ||
		strings.Contains(base, "register") || strings.Contains(base, "registry") ||
		strings.Contains(base, "profile") || strings.Contains(base, "vocabulary"):
		return KindIndex
	default:
		return KindWorkflow
	}
}

func domainOf(rel string) string {
	if index := strings.Index(rel, "/"); index > 0 {
		return rel[:index]
	}
	return ""
}

// textBlockFields reads key: value lines inside ```text fences. Only text
// fences are parsed; any other fenced language is skipped entirely.
func textBlockFields(body string) map[string][]string {
	fields := map[string][]string{}
	fence := ""
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			language := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			if fence != "" {
				fence = ""
			} else {
				fence = language
			}
			continue
		}
		if fence != "text" {
			continue
		}
		colon := strings.Index(trimmed, ":")
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:colon])
		value := strings.TrimSpace(trimmed[colon+1:])
		if key == "" || value == "" || strings.Contains(key, " ") {
			continue
		}
		fields[key] = append(fields[key], value)
	}
	return fields
}

func firstOf(fields map[string][]string, key string) string {
	if values := fields[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}

func parseStates(raw string) []string {
	var out []string
	for _, token := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == '+' || r == '|' || r == ',' || r == ' ' || r == '\t'
	}) {
		if token != "" {
			out = append(out, token)
		}
	}
	return out
}

func validState(state string) bool {
	switch state {
	case StateExtracted, StateReference, StateExplored, StateContracted, StateImplemented:
		return true
	default:
		return false
	}
}

func promoted(state string) bool {
	return state == StateContracted || state == StateImplemented
}

func (a *Artifact) checkIdentity() {
	switch a.Kind {
	case KindWorkflow:
		if a.WorkflowID == "" {
			a.pending = append(a.pending, Finding{Path: a.Path, Code: "ORPHAN_WORKFLOW_DOCUMENT", Detail: "workflow document declares no workflow id"})
			return
		}
		if len(a.Intents) == 0 {
			a.pending = append(a.pending, Finding{Path: a.Path, Code: "MISSING_IDENTITY_FIELD", Field: "legacy_intent|target_intent", Detail: "workflow declares no intent"})
		}
		if a.KernelFamily == "" {
			a.pending = append(a.pending, Finding{Path: a.Path, Code: "MISSING_IDENTITY_FIELD", Field: "kernel_family", Detail: "workflow declares no kernel family"})
		}
		if len(a.States) == 0 {
			a.pending = append(a.pending, Finding{Path: a.Path, Code: "MISSING_IDENTITY_FIELD", Field: "state", Detail: "workflow declares no discovery state"})
		}
		for _, state := range a.States {
			if !validState(state) {
				a.pending = append(a.pending, Finding{Path: a.Path, Code: "UNKNOWN_STATE", Field: "state", Detail: "unknown discovery state " + state})
			}
		}
		if a.Owner == "" {
			a.pending = append(a.pending, Finding{Path: a.Path, Code: "MISSING_IDENTITY_FIELD", Field: "owner_domain", Detail: "workflow declares no owner domain"})
		}
		for _, state := range a.States {
			if promoted(state) && (a.ContractRef == "" || len(a.Evidence) == 0) {
				a.pending = append(a.pending, Finding{Path: a.Path, Code: "STATE_PROMOTION_WITHOUT_EVIDENCE", Field: "state", Detail: "state " + state + " requires a typed contract ref and fresh evidence links"})
			}
		}
	case KindSample:
		if a.WorkflowID == "" {
			a.pending = append(a.pending, Finding{Path: a.Path, Code: "ORPHAN_WORKFLOW_DOCUMENT", Detail: "sample declares no workflow id"})
		}
	case KindCatalog:
		if strings.HasSuffix(a.Path, ".yaml") {
			return
		}
	}
}

// pending findings move into the registry exactly once, no matter how often
// the registry is sorted or re-resolved.
func (r *Registry) collectPending() {
	if r.collected {
		return
	}
	r.collected = true
	for _, artifact := range r.Artifacts {
		r.Findings = append(r.Findings, artifact.pending...)
	}
}

func (r *Registry) checkDuplicates() {
	seen := map[string]string{}
	for _, artifact := range r.Artifacts {
		if artifact.WorkflowID == "" {
			continue
		}
		if first, ok := seen[artifact.WorkflowID]; ok {
			r.Findings = append(r.Findings,
				Finding{Path: first, Code: "DUPLICATE_WORKFLOW_ID", Field: "workflow_id", Detail: "workflow id " + artifact.WorkflowID + " is declared more than once"},
				Finding{Path: artifact.Path, Code: "DUPLICATE_WORKFLOW_ID", Field: "workflow_id", Detail: "workflow id " + artifact.WorkflowID + " is declared more than once"})
			continue
		}
		seen[artifact.WorkflowID] = artifact.Path
	}
}

func (r *Registry) checkIndexLinks(root string) {
	for _, artifact := range r.Artifacts {
		if artifact.Kind != KindIndex {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifact.Path)))
		if err != nil {
			continue
		}
		dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(artifact.Path)))
		for _, match := range linkPattern.FindAllStringSubmatch(string(data), -1) {
			target := match[1]
			resolved := target
			if !filepath.IsAbs(target) {
				resolved = filepath.Join(dir, filepath.FromSlash(target))
			}
			abs, err := filepath.Abs(resolved)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(root, abs)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
				r.Findings = append(r.Findings, Finding{Path: artifact.Path, Code: "DANGLING_INDEX_LINK", Field: target, Detail: "index links a missing artifact"})
			}
		}
	}
}

func (r *Registry) sortFindings() {
	r.collectPending()
	sort.Slice(r.Findings, func(i, j int) bool {
		if r.Findings[i].Path != r.Findings[j].Path {
			return r.Findings[i].Path < r.Findings[j].Path
		}
		if r.Findings[i].Code != r.Findings[j].Code {
			return r.Findings[i].Code < r.Findings[j].Code
		}
		return r.Findings[i].Field < r.Findings[j].Field
	})
	unique := r.Findings[:0]
	seen := map[string]bool{}
	for _, finding := range r.Findings {
		key := finding.Path + "\x00" + finding.Code + "\x00" + finding.Field + "\x00" + finding.Detail
		if !seen[key] {
			seen[key] = true
			unique = append(unique, finding)
		}
	}
	r.Findings = unique
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
}

func digestRegistry(artifacts []Artifact) string {
	var lines []string
	for _, artifact := range artifacts {
		lines = append(lines, artifact.Path+"\x00"+artifact.Digest)
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
