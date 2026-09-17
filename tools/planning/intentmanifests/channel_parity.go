package intentmanifests

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"sort"
	"strings"
)

var (
	// ErrChannelRefused is the closed FEATURE-CONF-001 refusal: unknown,
	// unavailable, deferred, unlisted-surface or unauthenticated routes fail
	// here, never through a silent downgrade.
	ErrChannelRefused = errors.New("FEATURE_CONF_001_REFUSED")
	// ErrChannelDrift rejects a manifest whose channels disagree about
	// semantics, governance or emitted children.
	ErrChannelDrift = errors.New("FEATURE_CONF_001_DRIFT")
)

// ChannelActor is the closed caller vocabulary of the parity matrix.
type ChannelActor string

const (
	ActorEmployee   ChannelActor = "EMPLOYEE"
	ActorManager    ChannelActor = "MANAGER"
	ActorHRAdmin    ChannelActor = "HR_ADMIN"
	ActorDelegate   ChannelActor = "DELEGATE"
	ActorAgent      ChannelActor = "AGENT"
	ActorPartnerApp ChannelActor = "PARTNER_APP"
	ActorScheduler  ChannelActor = "SCHEDULER"
	ActorEvent      ChannelActor = "EVENT"
	ActorOperator   ChannelActor = "OPERATOR"
)

// AllChannelActors is the generated matrix actor set.
var AllChannelActors = []ChannelActor{
	ActorEmployee, ActorManager, ActorHRAdmin, ActorDelegate, ActorAgent,
	ActorPartnerApp, ActorScheduler, ActorEvent, ActorOperator,
}

// Surface is the closed invocation-surface vocabulary.
type Surface string

const (
	SurfaceGWC         Surface = "GWC"
	SurfaceMobileKiosk Surface = "MOBILE_KIOSK"
	SurfaceGRPC        Surface = "GRPC"
	SurfaceHTTP        Surface = "HTTP"
	SurfaceCLI         Surface = "CLI"
	SurfaceBULK        Surface = "BULK"
)

// AllSurfaces is the generated matrix surface set.
var AllSurfaces = []Surface{SurfaceGWC, SurfaceMobileKiosk, SurfaceGRPC, SurfaceHTTP, SurfaceCLI, SurfaceBULK}

// Availability is the closed feature-availability vocabulary.
type Availability string

const (
	AvailabilityAvailable   Availability = "AVAILABLE"
	AvailabilityDeferred    Availability = "DEFERRED"
	AvailabilityUnavailable Availability = "UNAVAILABLE"
)

// SurfacePolicy is the explicit per-surface contract. Presentation names the
// allowed authorization/presentation variance; semantic overrides are
// unrepresentable here by design, so ergonomics can never fork meaning.
type SurfacePolicy struct {
	Allowed          bool
	Presentation     string
	DefinitionDigest string
	SchemaDigest     string
}

// allSurfaces builds the default explicit policy: every surface allowed with
// its own presentation name and no semantic override.
func allSurfaces() map[Surface]SurfacePolicy {
	out := map[Surface]SurfacePolicy{}
	for _, surface := range AllSurfaces {
		out[surface] = SurfacePolicy{Allowed: true, Presentation: strings.ToLower(string(surface))}
	}
	return out
}

// ChannelBinding is one generated route: a feature, its intent role, the
// semantic digests every channel must agree on, and the explicit per-surface
// policy. Group is the source-group number from the coverage registry.
type ChannelBinding struct {
	FeatureID          string
	Group              int
	Role               SemanticClassification
	IntentID           string
	DefinitionDigest   string
	SchemaDigest       string
	RequiresSimulation bool
	ChildIntents       []string
	Availability       Availability
	AllowedActors      []ChannelActor
	Surfaces           map[Surface]SurfacePolicy
}

// ActorClaim is the authenticated caller. An empty authentication flag is
// never trusted: caller context alone authorizes nothing.
type ActorClaim struct {
	Actor         ChannelActor
	Authenticated bool
}

// ChannelResolution is the per-route answer. Semantic fields are identical
// for every actor and surface; only Authorized, AuthPolicy and Presentation
// legitimately vary.
type ChannelResolution struct {
	FeatureID          string
	Role               SemanticClassification
	IntentID           string
	DefinitionDigest   string
	SchemaDigest       string
	SimulationRequired bool
	Children           []string
	RequestDigest      string
	Authorized         bool
	AuthPolicy         string
	Presentation       string
}

// Manifest is the one generated route/action manifest that drives every
// channel adapter and test.
type Manifest struct {
	bindings []ChannelBinding
	byID     map[string]ChannelBinding
	digest   string
}

// Bindings returns the bound routes in feature order.
func (m Manifest) Bindings() []ChannelBinding { return append([]ChannelBinding(nil), m.bindings...) }

// Digest returns the canonical matrix digest.
func (m Manifest) Digest() string { return m.digest }

func manifestDigest(bindings []ChannelBinding) string {
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		actors := make([]string, 0, len(binding.AllowedActors))
		for _, actor := range binding.AllowedActors {
			actors = append(actors, string(actor))
		}
		sort.Strings(actors)
		surfaces := make([]string, 0, len(binding.Surfaces))
		for surface, policy := range binding.Surfaces {
			surfaces = append(surfaces, string(surface)+"="+policy.Presentation)
		}
		sort.Strings(surfaces)
		parts = append(parts, strings.Join([]string{
			binding.FeatureID, string(binding.Role), binding.IntentID,
			binding.DefinitionDigest, binding.SchemaDigest,
			fmt.Sprintf("%v", binding.RequiresSimulation),
			strings.Join(binding.ChildIntents, ","),
			string(binding.Availability), strings.Join(actors, ","),
			strings.Join(surfaces, ","),
		}, "\x00"))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// BindManifest compiles one route manifest, rejecting every drift at bind
// time: per-surface semantic overrides, material roles without simulation or
// intent, child emitters without children, and duplicate features.
func BindManifest(bindings []ChannelBinding) (Manifest, error) {
	seen := map[string]bool{}
	ordered := append([]ChannelBinding(nil), bindings...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].FeatureID < ordered[j].FeatureID })
	for _, binding := range ordered {
		if strings.TrimSpace(binding.FeatureID) == "" {
			return Manifest{}, fmt.Errorf("%w: feature identity is required", ErrChannelDrift)
		}
		if seen[binding.FeatureID] {
			return Manifest{}, fmt.Errorf("%w: duplicate feature %q", ErrChannelDrift, binding.FeatureID)
		}
		seen[binding.FeatureID] = true
		switch binding.Role {
		case ClassCreate, ClassConsume, ClassEmitChild, ClassObserve, ClassNonMaterial:
		default:
			return Manifest{}, fmt.Errorf("%w: feature %q role %q is not declared", ErrChannelDrift, binding.FeatureID, binding.Role)
		}
		switch binding.Availability {
		case AvailabilityAvailable, AvailabilityDeferred, AvailabilityUnavailable:
		default:
			return Manifest{}, fmt.Errorf("%w: feature %q availability %q is not declared", ErrChannelDrift, binding.FeatureID, binding.Availability)
		}
		if binding.Role == ClassNonMaterial {
			if binding.IntentID != "" {
				return Manifest{}, fmt.Errorf("%w: non-material feature %q binds intent %q", ErrChannelDrift, binding.FeatureID, binding.IntentID)
			}
		} else {
			if binding.Availability == AvailabilityAvailable && strings.TrimSpace(binding.IntentID) == "" {
				return Manifest{}, fmt.Errorf("%w: material feature %q binds no intent", ErrChannelDrift, binding.FeatureID)
			}
			if binding.Availability == AvailabilityAvailable && !binding.RequiresSimulation &&
				(binding.Role == ClassCreate || binding.Role == ClassConsume || binding.Role == ClassEmitChild) {
				return Manifest{}, fmt.Errorf("%w: material feature %q bypasses simulation", ErrChannelDrift, binding.FeatureID)
			}
		}
		if binding.Role == ClassEmitChild && len(binding.ChildIntents) == 0 {
			return Manifest{}, fmt.Errorf("%w: feature %q emits undeclared children", ErrChannelDrift, binding.FeatureID)
		}
		if strings.TrimSpace(binding.DefinitionDigest) == "" || strings.TrimSpace(binding.SchemaDigest) == "" {
			return Manifest{}, fmt.Errorf("%w: feature %q lacks semantic digests", ErrChannelDrift, binding.FeatureID)
		}
		for surface, policy := range binding.Surfaces {
			if policy.DefinitionDigest != "" || policy.SchemaDigest != "" {
				return Manifest{}, fmt.Errorf("%w: feature %q surface %q overrides semantics", ErrChannelDrift, binding.FeatureID, surface)
			}
		}
		if binding.Availability == AvailabilityAvailable && len(binding.Surfaces) == 0 {
			return Manifest{}, fmt.Errorf("%w: available feature %q lists no surface", ErrChannelDrift, binding.FeatureID)
		}
	}
	byID := make(map[string]ChannelBinding, len(ordered))
	for _, binding := range ordered {
		byID[binding.FeatureID] = binding
	}
	return Manifest{bindings: ordered, byID: byID, digest: manifestDigest(ordered)}, nil
}

// BindRegistry derives the parity matrix from the checked-in governance
// registry. DEFINED features become invocable on every surface; deferred and
// unavailable features keep no surface and refuse everywhere.
func BindRegistry(registry FeatureIntentCoverageRegistry) (Manifest, error) {
	bindings := make([]ChannelBinding, 0, len(registry.Features))
	for _, feature := range registry.Features {
		definition := sha256.Sum256([]byte(strings.Join([]string{
			feature.CanonicalIdentity, feature.Capability, feature.CapabilityVersion,
			feature.InputSchema, feature.GovernanceProfile,
		}, "\x00")))
		schema := sha256.Sum256([]byte(strings.Join([]string{
			feature.InputSchema, feature.ResultSchema, feature.CapabilityVersion,
		}, "\x00")))
		binding := ChannelBinding{
			FeatureID:        feature.FeatureID,
			Group:            feature.Group,
			Role:             feature.Classification,
			DefinitionDigest: "sha256:" + hex.EncodeToString(definition[:]),
			SchemaDigest:     "sha256:" + hex.EncodeToString(schema[:]),
			ChildIntents:     append([]string(nil), feature.DeclaredIntentIDs...),
		}
		if feature.Classification == ClassNonMaterial {
			// Non-material mechanics need no intent binding, but a
			// DEFERRED intake row is still not invocable on any channel.
			if feature.CoverageStatus == CoverageDefined {
				binding.Availability = AvailabilityAvailable
				binding.AllowedActors = append([]ChannelActor(nil), AllChannelActors...)
				binding.Surfaces = allSurfaces()
			} else {
				binding.Availability = AvailabilityDeferred
			}
			bindings = append(bindings, binding)
			continue
		}
		if feature.CoverageStatus == CoverageDefined && feature.BoundIntentID != "" && feature.BoundIntentID != "DEFERRED" {
			binding.Availability = AvailabilityAvailable
			binding.IntentID = feature.BoundIntentID
			binding.RequiresSimulation = true
			binding.AllowedActors = append([]ChannelActor(nil), AllChannelActors...)
			binding.Surfaces = allSurfaces()
		} else {
			binding.Availability = AvailabilityDeferred
			if feature.CoverageStatus != CoverageDeferred && feature.CoverageStatus != CoverageMissing && feature.CoverageStatus != CoverageImplied {
				binding.Availability = AvailabilityUnavailable
			}
		}
		bindings = append(bindings, binding)
	}
	return BindManifest(bindings)
}

func requestDigest(binding ChannelBinding, input string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		binding.DefinitionDigest, binding.SchemaDigest, string(binding.Role),
		binding.IntentID, strings.Join(binding.ChildIntents, ","), input,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Resolve answers one route. Identical typed inputs resolve identical
// semantics on every actor and channel; trusted origin and current policy
// alter authorization and presentation only.
func (m Manifest) Resolve(featureID string, claim ActorClaim, surface Surface, input string) (ChannelResolution, error) {
	binding, ok := m.byID[featureID]
	if !ok {
		return ChannelResolution{}, fmt.Errorf("%w: unknown feature %q", ErrChannelRefused, featureID)
	}
	if binding.Availability != AvailabilityAvailable {
		return ChannelResolution{}, fmt.Errorf("%w: feature %q is %s", ErrChannelRefused, featureID, binding.Availability)
	}
	if !claim.Authenticated {
		return ChannelResolution{}, fmt.Errorf("%w: unauthenticated caller context is never trusted", ErrChannelRefused)
	}
	policy, ok := binding.Surfaces[surface]
	if !ok || !policy.Allowed {
		return ChannelResolution{}, fmt.Errorf("%w: feature %q is not served on %q", ErrChannelRefused, featureID, surface)
	}
	if strings.TrimSpace(input) == "" {
		return ChannelResolution{}, fmt.Errorf("%w: typed input is required", ErrChannelRefused)
	}
	authorized := false
	for _, actor := range binding.AllowedActors {
		if actor == claim.Actor {
			authorized = true
			break
		}
	}
	authPolicy := fmt.Sprintf("deny:role:%s", claim.Actor)
	if authorized {
		authPolicy = fmt.Sprintf("allow:role:%s", claim.Actor)
	}
	return ChannelResolution{
		FeatureID: binding.FeatureID, Role: binding.Role, IntentID: binding.IntentID,
		DefinitionDigest: binding.DefinitionDigest, SchemaDigest: binding.SchemaDigest,
		SimulationRequired: binding.RequiresSimulation,
		Children:           append([]string(nil), binding.ChildIntents...),
		RequestDigest:      requestDigest(binding, input),
		Authorized:         authorized, AuthPolicy: authPolicy,
		Presentation: policy.Presentation,
	}, nil
}

// RenderParityHTML renders the governed matrix for GWC review: a labelled,
// captioned table with scoped headers, escaped identifiers and the matrix
// digest a reviewer can compare against API results.
func RenderParityHTML(manifest Manifest, locale string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<html lang="%s"><body><main aria-labelledby="parity-title">`, html.EscapeString(locale)))
	b.WriteString(`<h2 id="parity-title">Feature parity matrix</h2>`)
	b.WriteString(`<table><caption>Route manifest with semantic digests</caption>`)
	b.WriteString(`<thead><tr><th scope="col">Feature</th><th scope="col">Role</th><th scope="col">Intent</th><th scope="col">Digest</th></tr></thead><tbody>`)
	for _, binding := range manifest.Bindings() {
		b.WriteString(fmt.Sprintf(`<tr><th scope="row">%s</th><td>%s</td><td>%s</td><td>%s</td></tr>`,
			html.EscapeString(binding.FeatureID), html.EscapeString(string(binding.Role)),
			html.EscapeString(binding.IntentID), html.EscapeString(manifest.Digest())))
	}
	b.WriteString(`</tbody></table><p>Accessibility: WCAG_2_2_AA</p></main></body></html>`)
	return b.String()
}
