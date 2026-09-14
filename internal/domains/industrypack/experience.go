package industrypack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"sort"
	"strings"
)

// PACK-003: bind pack workflows, forms, skills and metrics.
//
// A pack's workflow, form, skill and metric references bind only to published
// registry artifacts of the same kind, identity, version and digest, and only
// when every declaration a reviewer needs is explicit: the side effects an
// artifact may cause (NONE is a declaration, absence is not), the authority
// scopes any side-effecting workflow or form runs under, the locales it ships
// (including the pack's default), the accessibility conformance of every form,
// and the unit, aggregation and definition of every metric. Every dependency
// -- on another bound artifact or on PACK-002 reference data, rules and
// formulas -- must resolve. An artifact that tries to override a rule the
// content binding publishes as non-overridable (a mandatory country rule) is
// rejected. An artifact that needs a deferred capability binds gated, never
// active.
//
// A rejection is PACK_003_REJECTED naming the offending field, state and
// version, and activation persists nothing unless the binding was accepted.

// ExperienceKind is the closed vocabulary of bound experience artifacts.
type ExperienceKind string

// Experience kinds.
const (
	ExperienceWorkflow ExperienceKind = "WORKFLOW"
	ExperienceForm     ExperienceKind = "FORM"
	ExperienceSkill    ExperienceKind = "SKILL"
	ExperienceMetric   ExperienceKind = "METRIC"
)

// RejectionCode is the stable PACK-003 refusal code.
const RejectionCode = "PACK_003_REJECTED"

// Rejection states.
const (
	StateUnresolved         = "UNRESOLVED"
	StateDigestMismatch     = "DIGEST_MISMATCH"
	StateUnknownDependency  = "UNKNOWN_DEPENDENCY"
	StateMissingDeclaration = "MISSING_DECLARATION"
	StateMandatoryOverride  = "MANDATORY_OVERRIDE"
	StateInvalidManifest    = "INVALID_MANIFEST"
)

// SideEffectNone is the explicit declaration that an artifact causes no side
// effect.
const SideEffectNone = "NONE"

// ErrExperienceRejected is the sentinel every PACK-003 rejection unwraps to.
var ErrExperienceRejected = errors.New("industrypack: experience binding rejected")

// ExperienceRejection names exactly what was refused.
type ExperienceRejection struct {
	Code    string
	Field   string
	State   string
	Version string
	Detail  string
}

func (e *ExperienceRejection) Error() string {
	return fmt.Sprintf("%s: %s %s@%s: %s", e.Code, e.Field, e.State, e.Version, e.Detail)
}

// Unwrap exposes the sentinel.
func (e *ExperienceRejection) Unwrap() error { return ErrExperienceRejected }

func reject(field, state, version, format string, args ...any) *ExperienceRejection {
	return &ExperienceRejection{Code: RejectionCode, Field: field, State: state, Version: version, Detail: fmt.Sprintf(format, args...)}
}

// ExperienceRef is an exact artifact identity.
type ExperienceRef struct {
	Kind    ExperienceKind `json:"kind"`
	ID      string         `json:"id"`
	Version string         `json:"version"`
}

func (r ExperienceRef) key() string { return string(r.Kind) + "|" + r.ID + "@" + r.Version }

// MetricDefinition is a metric's explicit semantics.
type MetricDefinition struct {
	Unit             string `json:"unit"`
	Aggregation      string `json:"aggregation"`
	DefinitionDigest string `json:"definition_digest"`
}

// ExperienceArtifact is one published registry artifact.
type ExperienceArtifact struct {
	Ref                  ExperienceRef     `json:"ref"`
	Digest               string            `json:"digest"`
	SideEffects          []string          `json:"side_effects"`
	AuthorityScopes      []string          `json:"authority_scopes,omitempty"`
	Locales              []string          `json:"locales"`
	Accessibility        string            `json:"accessibility,omitempty"`
	Metric               *MetricDefinition `json:"metric,omitempty"`
	RequiredCapabilities []string          `json:"required_capabilities,omitempty"`
	Dependencies         []ExperienceRef   `json:"dependencies,omitempty"`
	ContentDependencies  []ContentRef      `json:"content_dependencies,omitempty"`
	OverridesContent     *ContentRef       `json:"overrides_content,omitempty"`
}

// ExperienceBindingSpec is the input to [BindExperience].
type ExperienceBindingSpec struct {
	Pack          IndustryPack
	Registry      []ExperienceArtifact
	Content       Binding
	DefaultLocale string
	// DeferredCapabilities are capabilities not yet released; an artifact
	// that requires one binds gated.
	DeferredCapabilities []string
}

// BoundExperience is one bound artifact.
type BoundExperience struct {
	ExperienceArtifact
	Active bool   `json:"active"`
	Gate   string `json:"gate,omitempty"`
}

// ExperienceBinding is the accepted binding.
type ExperienceBinding struct {
	PackID    string            `json:"pack_id"`
	Version   int               `json:"version"`
	Artifacts []BoundExperience `json:"artifacts"`
	Digest    string            `json:"digest"`
}

// BindExperience resolves and checks every workflow, form, skill and metric
// reference of a pack.
func BindExperience(spec ExperienceBindingSpec) (ExperienceBinding, error) {
	pack := spec.Pack
	if err := pack.Validate(); err != nil {
		return ExperienceBinding{}, reject("manifest", StateInvalidManifest, fmt.Sprint(pack.Version), "%v", err)
	}
	if strings.TrimSpace(spec.DefaultLocale) == "" {
		return ExperienceBinding{}, reject("default_locale", StateMissingDeclaration, fmt.Sprint(pack.Version), "the pack declares no default locale")
	}
	registry := map[string]ExperienceArtifact{}
	for _, a := range spec.Registry {
		registry[a.Ref.key()] = a
	}
	content := map[string]BoundContent{}
	for _, c := range spec.Content.Contents {
		content[c.Ref.Key()] = c
	}
	deferred := map[string]bool{}
	for _, c := range spec.DeferredCapabilities {
		deferred[c] = true
	}

	var bound []ExperienceArtifact
	for _, section := range []struct {
		field string
		kind  ExperienceKind
		refs  []PinnedRef
	}{
		{"workflow_definition_refs", ExperienceWorkflow, pack.WorkflowDefinitionRefs},
		{"form_refs", ExperienceForm, pack.FormRefs},
		{"skill_refs", ExperienceSkill, pack.SkillRefs},
		{"metric_refs", ExperienceMetric, pack.MetricRefs},
	} {
		for i, ref := range section.refs {
			field := fmt.Sprintf("%s[%d]", section.field, i)
			a, ok := registry[ExperienceRef{Kind: section.kind, ID: ref.Identity(), Version: ref.Version}.key()]
			switch {
			case !ok:
				return ExperienceBinding{}, reject(field, StateUnresolved, ref.Version, "%s %s is not published at this version", section.kind, ref.Identity())
			case ref.Digest != "" && ref.Digest != a.Digest:
				return ExperienceBinding{}, reject(field, StateDigestMismatch, ref.Version, "%s %s digest differs from the pinned digest", section.kind, ref.Identity())
			}
			if err := checkDeclarations(field, a, spec.DefaultLocale); err != nil {
				return ExperienceBinding{}, err
			}
			bound = append(bound, a)
		}
	}
	present := map[string]bool{}
	for _, a := range bound {
		present[a.Ref.key()] = true
	}
	sort.Slice(bound, func(i, j int) bool { return bound[i].Ref.key() < bound[j].Ref.key() })
	out := ExperienceBinding{PackID: pack.packID(), Version: pack.Version, Artifacts: []BoundExperience{}}
	for _, a := range bound {
		field := string(a.Ref.Kind) + ":" + a.Ref.ID
		for _, dep := range a.Dependencies {
			if !present[dep.key()] {
				return ExperienceBinding{}, reject(field+".dependencies", StateUnknownDependency, a.Ref.Version, "depends on %s %s@%s, which this pack does not bind", dep.Kind, dep.ID, dep.Version)
			}
		}
		for _, dep := range a.ContentDependencies {
			if _, ok := content[dep.Key()]; !ok {
				return ExperienceBinding{}, reject(field+".content_dependencies", StateUnknownDependency, a.Ref.Version, "depends on %s, which the content binding does not contain", dep.Key())
			}
		}
		if o := a.OverridesContent; o != nil {
			target, ok := content[o.Key()]
			switch {
			case !ok:
				return ExperienceBinding{}, reject(field+".overrides_content", StateUnknownDependency, a.Ref.Version, "overrides %s, which the content binding does not contain", o.Key())
			case !target.Overridable:
				return ExperienceBinding{}, reject(field+".overrides_content", StateMandatoryOverride, a.Ref.Version, "%s is mandatory and cannot be overridden", o.Key())
			}
		}
		b := BoundExperience{ExperienceArtifact: a, Active: true}
		for _, c := range a.RequiredCapabilities {
			if deferred[c] {
				b.Active, b.Gate = false, "DEFERRED_CAPABILITY:"+c
				break
			}
		}
		out.Artifacts = append(out.Artifacts, b)
	}
	body, _ := json.Marshal(struct {
		PackID    string            `json:"pack_id"`
		Version   int               `json:"version"`
		Content   string            `json:"content_digest"`
		Artifacts []BoundExperience `json:"artifacts"`
	}{out.PackID, out.Version, spec.Content.CanonicalDigest, out.Artifacts})
	sum := sha256.Sum256(body)
	out.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return out, nil
}

func checkDeclarations(field string, a ExperienceArtifact, defaultLocale string) error {
	v := a.Ref.Version
	if strings.TrimSpace(a.Digest) == "" {
		return reject(field+".digest", StateMissingDeclaration, v, "artifact has no published digest")
	}
	if len(a.SideEffects) == 0 {
		return reject(field+".side_effects", StateMissingDeclaration, v, "side effects must be declared; NONE is a declaration, absence is not")
	}
	effectful := false
	for _, e := range a.SideEffects {
		if strings.TrimSpace(e) == "" {
			return reject(field+".side_effects", StateMissingDeclaration, v, "blank side effect")
		}
		if e != SideEffectNone {
			effectful = true
		}
	}
	if effectful && len(a.SideEffects) > 1 {
		for _, e := range a.SideEffects {
			if e == SideEffectNone {
				return reject(field+".side_effects", StateMissingDeclaration, v, "NONE cannot be declared beside a side effect")
			}
		}
	}
	if effectful && (a.Ref.Kind == ExperienceWorkflow || a.Ref.Kind == ExperienceForm) && len(a.AuthorityScopes) == 0 {
		return reject(field+".authority_scopes", StateMissingDeclaration, v, "a side-effecting %s must declare its authority scopes", a.Ref.Kind)
	}
	hasDefault := false
	for _, l := range a.Locales {
		hasDefault = hasDefault || l == defaultLocale
	}
	if !hasDefault {
		return reject(field+".locales", StateMissingDeclaration, v, "the artifact does not ship the pack default locale %s", defaultLocale)
	}
	if a.Ref.Kind == ExperienceForm && strings.TrimSpace(a.Accessibility) == "" {
		return reject(field+".accessibility", StateMissingDeclaration, v, "a form must declare its accessibility conformance")
	}
	if a.Ref.Kind == ExperienceMetric && (a.Metric == nil || a.Metric.Unit == "" || a.Metric.Aggregation == "" || a.Metric.DefinitionDigest == "") {
		return reject(field+".metric", StateMissingDeclaration, v, "a metric must declare unit, aggregation and definition")
	}
	return nil
}

// ActivationEffects counts what an activation wrote.
type ActivationEffects struct {
	AuthoritativeRows, BusinessEvents, OutboxEntries, HumanWork, ProviderRequests int
}

// ActivationStore persists an accepted experience binding.
type ActivationStore interface {
	SaveExperienceBinding(ctx context.Context, tenantID string, b ExperienceBinding) (ActivationEffects, error)
}

// ActivateExperience binds and, only when the binding is accepted, persists
// it. A rejection persists nothing and reports zero effects.
func ActivateExperience(ctx context.Context, store ActivationStore, tenantID string, spec ExperienceBindingSpec) (ExperienceBinding, ActivationEffects, error) {
	if store == nil || strings.TrimSpace(tenantID) == "" {
		return ExperienceBinding{}, ActivationEffects{}, fmt.Errorf("%w: store and tenant are required", ErrExperienceRejected)
	}
	b, err := BindExperience(spec)
	if err != nil {
		return ExperienceBinding{}, ActivationEffects{}, err
	}
	effects, err := store.SaveExperienceBinding(ctx, tenantID, b)
	if err != nil {
		return ExperienceBinding{}, ActivationEffects{}, err
	}
	return b, effects, nil
}

// RenderCatalogHTML renders the accessible reviewer catalog of a binding: one
// labelled table with header scopes, every artifact's declarations and its
// active or gated state, in the pack's default locale.
func RenderCatalogHTML(b ExperienceBinding, locale string) string {
	var sb strings.Builder
	esc := html.EscapeString
	sb.WriteString(`<section lang="` + esc(locale) + `" aria-labelledby="pack-catalog-title">`)
	sb.WriteString(`<h2 id="pack-catalog-title">` + esc(b.PackID) + ` v` + fmt.Sprint(b.Version) + `</h2>`)
	sb.WriteString(`<table><caption>Bound workflows, forms, skills and metrics</caption><thead><tr>`)
	for _, h := range []string{"Kind", "Artifact", "Version", "Side effects", "Authority", "Locales", "Accessibility", "State"} {
		sb.WriteString(`<th scope="col">` + h + `</th>`)
	}
	sb.WriteString(`</tr></thead><tbody>`)
	for _, a := range b.Artifacts {
		state := "Active"
		if !a.Active {
			state = "Gated (" + a.Gate + ")"
		}
		sb.WriteString(`<tr><th scope="row">` + esc(string(a.Ref.Kind)) + `</th><td>` + esc(a.Ref.ID) + `</td><td>` + esc(a.Ref.Version) +
			`</td><td>` + esc(strings.Join(a.SideEffects, ", ")) + `</td><td>` + esc(strings.Join(a.AuthorityScopes, ", ")) +
			`</td><td>` + esc(strings.Join(a.Locales, ", ")) + `</td><td>` + esc(a.Accessibility) + `</td><td>` + esc(state) + `</td></tr>`)
	}
	sb.WriteString(`</tbody></table></section>`)
	return sb.String()
}
