package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/popscale"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidFreeze       = errors.New("search: invalid population freeze request")
	ErrSelectionNotVisible = errors.New("search: selected subject was not an authorized visible result")
	ErrStaleWatermark      = errors.New("search: authorized result is below the required freshness watermark")
	ErrActionNotFresh      = errors.New("search: action governance is not fresh for the frozen snapshot")
	ErrActionBound         = errors.New("search: action exceeds its declared bound")
)

// Exclusion records a visible result intentionally left out of a selection.
// It never represents a denied or hidden subject, so it cannot become a
// side-channel for the search prefilter.
type Exclusion struct {
	Subject values.EntityRef
	Reason  string
}

// FreezeRequest converts only authorized search results into a population
// snapshot. The population definition and restriction versions remain owned
// by the population/privacy engines; this package only coordinates the
// cross-engine binding.
type FreezeRequest struct {
	Envelope        QueryEnvelope
	Results         []Result
	Selected        []values.EntityRef
	Exclusions      []Exclusion
	Definition      population.Definition
	RevisionVersion string
	Versions        population.PolicyVersions
	AsOf            values.Instant
	KnownAt         values.KnownAt
	Watermarks      map[population.SubjectKind]values.Instant
}

// FrozenSelection is the durable handoff from search to a bounded action. It
// retains the population snapshot digest and the query/index/policy bindings,
// but not search text, scores, hidden results, or a live query.
type FrozenSelection struct {
	Snapshot           population.Snapshot
	Tenant             values.TenantId
	Purpose            string
	EnvelopeDigest     string
	QueryDigest        string
	SemanticPlanDigest string
	IndexDigest        string
	PolicyDigest       string
	MinimumWatermark   values.Instant
	Selection          []values.EntityRef
	Exclusions         []Exclusion
}

func (f FrozenSelection) Validate() error {
	if f.Snapshot.Digest == "" || f.Tenant == "" || f.Purpose == "" || f.EnvelopeDigest == "" || f.QueryDigest == "" || f.SemanticPlanDigest == "" || f.IndexDigest == "" || f.PolicyDigest == "" {
		return ErrInvalidFreeze
	}
	if err := f.MinimumWatermark.Validate(); err != nil {
		return fmt.Errorf("%w: watermark: %v", ErrInvalidFreeze, err)
	}
	if f.Snapshot.MembershipProtected {
		return ErrInvalidFreeze
	}
	selection := make(map[string]bool, len(f.Selection))
	for _, subject := range f.Selection {
		if err := subject.Validate(); err != nil || subject.Tenant != f.Tenant {
			return fmt.Errorf("%w: selection subject is outside frozen tenant", ErrInvalidFreeze)
		}
		selection[subject.String()] = true
	}
	session, err := popscale.NewSession(f.Snapshot, popscale.Caller{MembershipDisclosed: true}, 256)
	if err != nil {
		return ErrInvalidFreeze
	}
	seen := make(map[string]bool, len(f.Selection))
	if err := session.Walk(func(page popscale.Page) error {
		for _, id := range page.Subjects {
			if !selection[id] || seen[id] {
				return ErrInvalidFreeze
			}
			seen[id] = true
		}
		return nil
	}); err != nil || len(seen) != len(selection) {
		return ErrInvalidFreeze
	}
	return nil
}

// FreezeAuthorized makes membership immutable and checks that the source
// watermark is at least the query's minimum watermark. It never re-runs a
// search and never accepts a subject that was absent from the visible result
// set.
func FreezeAuthorized(req FreezeRequest) (FrozenSelection, error) {
	if err := req.Envelope.Validate(); err != nil {
		return FrozenSelection{}, err
	}
	if err := req.Definition.Validate(); err != nil {
		return FrozenSelection{}, fmt.Errorf("%w: definition: %v", ErrInvalidFreeze, err)
	}
	if req.RevisionVersion == "" || req.Envelope.Tenant != req.Definition.Scope.Tenant {
		return FrozenSelection{}, fmt.Errorf("%w: revision and tenant scope are required", ErrInvalidFreeze)
	}
	if err := req.Versions.Validate(); err != nil {
		return FrozenSelection{}, fmt.Errorf("%w: policy versions: %v", ErrInvalidFreeze, err)
	}
	if err := req.AsOf.Validate(); err != nil {
		return FrozenSelection{}, fmt.Errorf("%w: as-of: %v", ErrInvalidFreeze, err)
	}
	if err := req.KnownAt.Instant().Validate(); err != nil {
		return FrozenSelection{}, fmt.Errorf("%w: known-at: %v", ErrInvalidFreeze, err)
	}
	if len(req.Watermarks) == 0 {
		return FrozenSelection{}, fmt.Errorf("%w: no source watermark", ErrInvalidFreeze)
	}
	for kind, watermark := range req.Watermarks {
		if err := watermark.Validate(); err != nil {
			return FrozenSelection{}, fmt.Errorf("%w: watermark %s: %v", ErrInvalidFreeze, kind, err)
		}
		if watermark.Compare(req.Envelope.MinimumWatermark) < 0 {
			return FrozenSelection{}, fmt.Errorf("%w: %s", ErrStaleWatermark, kind)
		}
	}

	visible := make(map[string]Result, len(req.Results))
	for _, result := range req.Results {
		if err := result.Subject.Validate(); err != nil || result.Subject.Tenant != req.Envelope.Tenant || result.PolicyDigest != req.Envelope.PolicyDigest || result.SourceDigest == "" || result.AuthorizationEvidence == "" || result.Watermark.Compare(req.Envelope.MinimumWatermark) < 0 {
			return FrozenSelection{}, fmt.Errorf("%w: result is not bound to the authorized envelope", ErrInvalidFreeze)
		}
		key := result.Subject.String()
		if _, exists := visible[key]; exists {
			return FrozenSelection{}, fmt.Errorf("%w: duplicate result", ErrInvalidFreeze)
		}
		visible[key] = result
	}

	selected := make([]values.EntityRef, 0, len(req.Selected))
	selectedKeys := make(map[string]bool, len(req.Selected))
	for _, subject := range req.Selected {
		if err := subject.Validate(); err != nil {
			return FrozenSelection{}, fmt.Errorf("%w: selected subject: %v", ErrInvalidFreeze, err)
		}
		key := subject.String()
		if _, ok := visible[key]; !ok {
			return FrozenSelection{}, fmt.Errorf("%w: %s", ErrSelectionNotVisible, key)
		}
		if selectedKeys[key] {
			return FrozenSelection{}, fmt.Errorf("%w: duplicate selected subject", ErrInvalidFreeze)
		}
		selectedKeys[key] = true
		selected = append(selected, subject)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].String() < selected[j].String() })

	exclusions := make([]Exclusion, 0, len(req.Exclusions))
	seenExclusions := make(map[string]bool, len(req.Exclusions))
	for _, exclusion := range req.Exclusions {
		if err := exclusion.Subject.Validate(); err != nil || exclusion.Reason == "" {
			return FrozenSelection{}, fmt.Errorf("%w: invalid exclusion", ErrInvalidFreeze)
		}
		key := exclusion.Subject.String()
		if _, ok := visible[key]; !ok || selectedKeys[key] || seenExclusions[key] {
			return FrozenSelection{}, fmt.Errorf("%w: exclusion is not a distinct visible result", ErrInvalidFreeze)
		}
		seenExclusions[key] = true
		exclusions = append(exclusions, exclusion)
	}
	sort.Slice(exclusions, func(i, j int) bool { return exclusions[i].Subject.String() < exclusions[j].Subject.String() })

	members := make([]population.Member, 0, len(selected))
	for _, subject := range selected {
		members = append(members, population.Member{Subject: subject, Outcome: population.OutcomeIncluded})
	}
	restricted := population.RestrictedResult{
		Members: members, Completeness: population.CompletenessComplete,
		Count: values.Value(len(members)), Versions: req.Versions,
	}
	snapshot, err := population.Freeze(req.Definition, req.RevisionVersion, restricted, req.AsOf, req.KnownAt, req.Watermarks)
	if err != nil {
		return FrozenSelection{}, fmt.Errorf("%w: population snapshot: %v", ErrInvalidFreeze, err)
	}
	return FrozenSelection{
		Snapshot: snapshot, Tenant: req.Envelope.Tenant, Purpose: req.Envelope.Purpose, EnvelopeDigest: req.Envelope.Digest(), QueryDigest: req.Envelope.QueryDigest,
		SemanticPlanDigest: req.Envelope.SemanticPlanDigest, IndexDigest: req.Envelope.IndexDigest,
		PolicyDigest: req.Envelope.PolicyDigest, MinimumWatermark: req.Envelope.MinimumWatermark,
		Selection: append([]values.EntityRef(nil), selected...), Exclusions: append([]Exclusion(nil), exclusions...),
	}, nil
}

// ActionTemplate is a bounded, versioned operation template. It is not an
// execution plan and cannot itself create a domain effect.
type ActionTemplate struct {
	ID         string
	Version    string
	MaxMembers int
}

func (t ActionTemplate) Validate() error {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Version) == "" || t.MaxMembers <= 0 || t.MaxMembers > 1000 {
		return ErrActionBound
	}
	return nil
}

// ActionRequest is what a fresh governance evaluator must authorize. It cites
// the frozen snapshot, never the original live search.
type ActionRequest struct {
	Tenant          values.TenantId
	Purpose         string
	SnapshotDigest  string
	PolicyDigest    string
	TemplateID      string
	TemplateVersion string
	MemberCount     int
	At              values.Instant
}

// GovernanceDecision is a fresh, separate authorization result for a proposed
// action. Search authorization alone is never accepted here.
type GovernanceDecision struct {
	Allowed        bool
	SnapshotDigest string
	PolicyDigest   string
	Evidence       string
	EvaluatedAt    values.Instant
}

type ActionGovernance interface {
	AuthorizeAction(context.Context, ActionRequest) (GovernanceDecision, error)
}

type ActionGovernanceFunc func(context.Context, ActionRequest) (GovernanceDecision, error)

func (f ActionGovernanceFunc) AuthorizeAction(ctx context.Context, req ActionRequest) (GovernanceDecision, error) {
	return f(ctx, req)
}

// Child is a deterministic, idempotent proposed child identity. ResultDigest
// is an empty-effect placeholder for the eventual per-member result; no child
// has executed merely because it was prepared.
type Child struct {
	ID           string
	Subject      values.EntityRef
	Status       string
	ResultDigest string
}

// ProposedAction is a governed proposal with no execution authority.
type ProposedAction struct {
	SnapshotDigest     string
	PolicyDigest       string
	TemplateID         string
	TemplateVersion    string
	Status             string
	EffectAuthorized   bool
	Children           []Child
	GovernanceEvidence string
}

// PrepareAction creates a separate, fresh-governed proposal with bounded and
// deterministic child identities. It does not execute any child or use live
// search membership.
func PrepareAction(ctx context.Context, frozen FrozenSelection, template ActionTemplate, at values.Instant, governance ActionGovernance) (ProposedAction, error) {
	if ctx == nil || ctx.Err() != nil || governance == nil {
		return ProposedAction{}, ErrActionNotFresh
	}
	if err := frozen.Validate(); err != nil {
		return ProposedAction{}, err
	}
	if err := template.Validate(); err != nil {
		return ProposedAction{}, err
	}
	if err := at.Validate(); err != nil {
		return ProposedAction{}, ErrActionNotFresh
	}
	if len(frozen.Selection) > template.MaxMembers {
		return ProposedAction{}, fmt.Errorf("%w: %d members exceeds %d", ErrActionBound, len(frozen.Selection), template.MaxMembers)
	}
	request := ActionRequest{
		Tenant: frozen.Tenant, Purpose: frozen.Purpose, SnapshotDigest: frozen.Snapshot.Digest,
		PolicyDigest: frozen.PolicyDigest, TemplateID: template.ID, TemplateVersion: template.Version,
		MemberCount: len(frozen.Selection), At: at,
	}
	// The purpose is part of the population definition and is recovered from
	// the snapshot's immutable selection handoff by the caller's governance
	// adapter; this package never invents a new action purpose.
	decision, err := governance.AuthorizeAction(ctx, request)
	if err != nil || !decision.Allowed || decision.SnapshotDigest != frozen.Snapshot.Digest || decision.PolicyDigest != frozen.PolicyDigest || decision.Evidence == "" || decision.EvaluatedAt.Compare(at) != 0 {
		return ProposedAction{}, ErrActionNotFresh
	}
	children := make([]Child, 0, len(frozen.Selection))
	session, err := popscale.NewSession(frozen.Snapshot, popscale.Caller{MembershipDisclosed: true}, 256)
	if err != nil {
		return ProposedAction{}, fmt.Errorf("%w: population page session", ErrActionBound)
	}
	if err := session.Walk(func(page popscale.Page) error {
		for _, id := range page.Subjects {
			var subject values.EntityRef
			if err := subject.UnmarshalText([]byte(id)); err != nil {
				return fmt.Errorf("%w: snapshot subject: %v", ErrActionBound, err)
			}
			childID := digestParts(frozen.Snapshot.Digest, template.ID, template.Version, id)
			children = append(children, Child{ID: childID, Subject: subject, Status: "PENDING", ResultDigest: digestParts("pending", childID)})
		}
		return nil
	}); err != nil {
		return ProposedAction{}, err
	}
	return ProposedAction{
		SnapshotDigest: frozen.Snapshot.Digest, PolicyDigest: frozen.PolicyDigest,
		TemplateID: template.ID, TemplateVersion: template.Version, Status: "PROPOSED",
		EffectAuthorized: false, Children: children, GovernanceEvidence: decision.Evidence,
	}, nil
}

// RecommendAction is the explicit analysis-to-proposal spelling used by
// callers that want to emphasize that this operation cannot execute.
func RecommendAction(ctx context.Context, frozen FrozenSelection, template ActionTemplate, at values.Instant, governance ActionGovernance) (ProposedAction, error) {
	return PrepareAction(ctx, frozen, template, at, governance)
}

func digestParts(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}
