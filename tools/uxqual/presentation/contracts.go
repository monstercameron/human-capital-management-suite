// Package presentation defines renderer-independent frontend contracts for
// provenance, semantic actions, compatibility, release scope, evidence, and
// ownership. None of these values grant business authority or carry markup.
package presentation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

var ErrInvalid = errors.New("presentation: invalid contract")

type SourceType string

const (
	SourceCanonical SourceType = "CANONICAL_FACT"
	SourceExternal  SourceType = "EXTERNAL_OBSERVATION"
	SourceHuman     SourceType = "HUMAN_OBSERVATION"
	SourceAgent     SourceType = "AGENT_HYPOTHESIS"
	SourceDerived   SourceType = "DERIVED_VALUE"
)

type FieldDisposition string

const (
	DispositionShow        FieldDisposition = "SHOW"
	DispositionMask        FieldDisposition = "MASK"
	DispositionRedact      FieldDisposition = "REDACT"
	DispositionHide        FieldDisposition = "HIDE"
	DispositionSummaryOnly FieldDisposition = "SUMMARY_ONLY"
	DispositionDerivedOnly FieldDisposition = "DERIVED_ONLY"
)

type BoundValue struct {
	Value          json.RawMessage  `json:"value,omitempty"`
	DisplayValue   string           `json:"display_value,omitempty"`
	SourceType     SourceType       `json:"source_type"`
	SourceID       string           `json:"source_id"`
	SourceVersion  string           `json:"source_version"`
	AuthorityClass string           `json:"authority_class"`
	Classification string           `json:"classification"`
	Confidence     *float64         `json:"confidence,omitempty"`
	EffectiveAt    *time.Time       `json:"effective_at,omitempty"`
	RecordedAt     time.Time        `json:"recorded_at"`
	Editable       bool             `json:"editable"`
	Disposition    FieldDisposition `json:"disposition"`
}

func (v BoundValue) Validate() error {
	if !knownSource(v.SourceType) || !knownDisposition(v.Disposition) {
		return fmt.Errorf("%w: source and disposition must use closed vocabularies", ErrInvalid)
	}
	for label, value := range map[string]string{
		"source id": v.SourceID, "source version": v.SourceVersion,
		"authority class": v.AuthorityClass, "classification": v.Classification,
	} {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "<>\r\n") {
			return fmt.Errorf("%w: %s is required and cannot contain markup", ErrInvalid, label)
		}
	}
	if v.RecordedAt.IsZero() {
		return fmt.Errorf("%w: recorded_at is required", ErrInvalid)
	}
	if v.Confidence != nil && (math.IsNaN(*v.Confidence) || *v.Confidence < 0 || *v.Confidence > 1) {
		return fmt.Errorf("%w: confidence must be between zero and one", ErrInvalid)
	}
	if (v.Disposition == DispositionHide || v.Disposition == DispositionRedact) &&
		(len(v.Value) != 0 || v.DisplayValue != "") {
		return fmt.Errorf("%w: hidden and redacted values cannot reach the component tree", ErrInvalid)
	}
	if v.Disposition == DispositionMask && len(v.Value) != 0 {
		return fmt.Errorf("%w: a masked value cannot carry its raw value", ErrInvalid)
	}
	if v.Editable && (v.Disposition != DispositionShow || v.SourceType != SourceCanonical) {
		return fmt.Errorf("%w: only shown canonical facts may be editable", ErrInvalid)
	}
	return nil
}

func (v BoundValue) Digest() (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func knownSource(value SourceType) bool {
	switch value {
	case SourceCanonical, SourceExternal, SourceHuman, SourceAgent, SourceDerived:
		return true
	default:
		return false
	}
}

func knownDisposition(value FieldDisposition) bool {
	switch value {
	case DispositionShow, DispositionMask, DispositionRedact, DispositionHide, DispositionSummaryOnly, DispositionDerivedOnly:
		return true
	default:
		return false
	}
}

type ActionAvailability string

const (
	ActionAvailable       ActionAvailability = "AVAILABLE"
	ActionUnavailableSafe ActionAvailability = "UNAVAILABLE_SAFE_TO_DISCLOSE"
	ActionHidden          ActionAvailability = "HIDDEN"
)

type SemanticAction struct {
	ID                      string             `json:"id"`
	Label                   string             `json:"label,omitempty"`
	RPC                     string             `json:"rpc"`
	Availability            ActionAvailability `json:"availability"`
	ActionToken             string             `json:"action_token,omitempty"`
	ExpectedResourceVersion string             `json:"expected_resource_version,omitempty"`
	IdempotencyKey          string             `json:"idempotency_key,omitempty"`
	InputSchema             string             `json:"input_schema,omitempty"`
	UnavailableReason       string             `json:"unavailable_reason,omitempty"`
	Obligations             []string           `json:"obligations,omitempty"`
}

func (a SemanticAction) Validate() error {
	if strings.TrimSpace(a.ID) == "" || strings.ContainsAny(a.ID, "<>\r\n") {
		return fmt.Errorf("%w: action id is required", ErrInvalid)
	}
	if !pagedef.KnownRPCs()[a.RPC] {
		return fmt.Errorf("%w: action RPC %q is not registered", ErrInvalid, a.RPC)
	}
	switch a.Availability {
	case ActionAvailable:
		if strings.TrimSpace(a.Label) == "" || strings.TrimSpace(a.ActionToken) == "" ||
			strings.TrimSpace(a.ExpectedResourceVersion) == "" || strings.TrimSpace(a.IdempotencyKey) == "" ||
			strings.TrimSpace(a.InputSchema) == "" {
			return fmt.Errorf("%w: an available action requires label, token, expected version, idempotency key, and input schema", ErrInvalid)
		}
		if a.UnavailableReason != "" {
			return fmt.Errorf("%w: an available action cannot carry an unavailable reason", ErrInvalid)
		}
	case ActionUnavailableSafe:
		if strings.TrimSpace(a.Label) == "" || strings.TrimSpace(a.UnavailableReason) == "" || a.ActionToken != "" {
			return fmt.Errorf("%w: safe unavailable actions require a label and reason but no token", ErrInvalid)
		}
	case ActionHidden:
		if a.Label != "" || a.ActionToken != "" || a.UnavailableReason != "" {
			return fmt.Errorf("%w: hidden actions must carry no presentation text or token", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown action availability %q", ErrInvalid, a.Availability)
	}
	for _, obligation := range a.Obligations {
		if strings.TrimSpace(obligation) == "" || strings.ContainsAny(obligation, "<>\r\n") {
			return fmt.Errorf("%w: invalid action obligation", ErrInvalid)
		}
	}
	return nil
}

func (a SemanticAction) Presentable() bool { return a.Availability != ActionHidden }

type DependencyKind string

const (
	DependencyPage      DependencyKind = "PAGE"
	DependencyFloorplan DependencyKind = "FLOORPLAN"
	DependencyWidget    DependencyKind = "WIDGET"
	DependencyToken     DependencyKind = "TOKEN"
	DependencyRPC       DependencyKind = "RPC"
)

type DependencyNode struct {
	Ref  string         `json:"ref"`
	Kind DependencyKind `json:"kind"`
}

type DependencyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type CompatibilityGraph struct {
	Nodes []DependencyNode `json:"nodes"`
	Edges []DependencyEdge `json:"edges"`
}

func (g CompatibilityGraph) Validate() error {
	nodes := make(map[string]DependencyKind, len(g.Nodes))
	for _, node := range g.Nodes {
		if strings.TrimSpace(node.Ref) == "" || !knownDependency(node.Kind) || nodes[node.Ref] != "" {
			return fmt.Errorf("%w: dependency nodes need unique refs and known kinds", ErrInvalid)
		}
		nodes[node.Ref] = node.Kind
	}
	adj := make(map[string][]string, len(nodes))
	for _, edge := range g.Edges {
		if nodes[edge.From] == "" || nodes[edge.To] == "" || edge.From == edge.To {
			return fmt.Errorf("%w: dependency edge %q -> %q is dangling or recursive", ErrInvalid, edge.From, edge.To)
		}
		adj[edge.From] = append(adj[edge.From], edge.To)
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(ref string) bool {
		if visiting[ref] {
			return false
		}
		if visited[ref] {
			return true
		}
		visiting[ref] = true
		for _, dependency := range adj[ref] {
			if !visit(dependency) {
				return false
			}
		}
		visiting[ref] = false
		visited[ref] = true
		return true
	}
	for ref := range nodes {
		if !visit(ref) {
			return fmt.Errorf("%w: compatibility graph contains a cycle", ErrInvalid)
		}
	}
	return nil
}

func (g CompatibilityGraph) Impacted(changedRef string) []string {
	reverse := map[string][]string{}
	for _, edge := range g.Edges {
		reverse[edge.To] = append(reverse[edge.To], edge.From)
	}
	seen := map[string]bool{changedRef: true}
	queue := []string{changedRef}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dependent := range reverse[current] {
			if !seen[dependent] {
				seen[dependent] = true
				queue = append(queue, dependent)
			}
		}
	}
	delete(seen, changedRef)
	result := make([]string, 0, len(seen))
	for ref := range seen {
		result = append(result, ref)
	}
	sort.Strings(result)
	return result
}

func knownDependency(kind DependencyKind) bool {
	switch kind {
	case DependencyPage, DependencyFloorplan, DependencyWidget, DependencyToken, DependencyRPC:
		return true
	default:
		return false
	}
}
