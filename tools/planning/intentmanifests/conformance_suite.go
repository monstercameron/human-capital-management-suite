package intentmanifests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	// ErrInvalidConformanceInput reports a malformed registry row or route
	// that can never yield a suite, even after review.
	ErrInvalidConformanceInput = errors.New("intentmanifests: invalid conformance input")
	// ErrConformanceGap reports an implemented feature/channel that lacks a
	// required conformance vector, an unexercised enabled route, or any
	// other missing proof. It is the RED signal, never a silent pass.
	ErrConformanceGap = errors.New("intentmanifests: feature-intent conformance gap")
)

// ConformanceAvailability records whether a registry entry is exercised by
// the generated suite or is explicitly unavailable and therefore unserved.
type ConformanceAvailability string

const (
	ConformanceAvailable           ConformanceAvailability = "AVAILABLE"
	ConformanceUnavailableDeferred ConformanceAvailability = "UNAVAILABLE_DEFERRED"
	ConformanceUnavailableMissing  ConformanceAvailability = "UNAVAILABLE_MISSING"
)

// ConformanceVectors is the seven-vector proof carried by every available
// suite entry. Family/channel templates supply the mechanics; the bound
// intent, capability, governance profile and evidence expectation supply the
// domain-specific values.
type ConformanceVectors struct {
	Behavior            string `json:"behavior"`
	DenialVector        string `json:"denial_vector"`
	IdempotencyVector   string `json:"idempotency_vector"`
	ZeroBypass          string `json:"zero_bypass"`
	LifecycleOracle     string `json:"lifecycle_oracle"`
	EvidenceExpectation string `json:"evidence_expectation"`
	DepthAssertion      string `json:"depth_assertion"`
}

// ConformanceCase is the generated suite row for one registry feature.
type ConformanceCase struct {
	FeatureID      string                  `json:"feature_id"`
	GroupName      string                  `json:"group_name"`
	Classification SemanticClassification  `json:"classification"`
	Role           FeatureIntentRole       `json:"role"`
	BoundIntent    string                  `json:"bound_intent"`
	Depth          string                  `json:"depth"`
	Availability   ConformanceAvailability `json:"availability"`
	Reason         string                  `json:"reason,omitempty"`
	Vectors        ConformanceVectors      `json:"vectors,omitempty"`
	Routes         []string                `json:"routes,omitempty"`
}

// ConformanceSuite is the deterministic generated plan. Digest excludes
// itself and is stable across input ordering.
type ConformanceSuite struct {
	SchemaVersion   int               `json:"schema_version"`
	Entries         []ConformanceCase `json:"entries"`
	EnabledRoutes   []string          `json:"enabled_routes"`
	UncoveredRoutes []string          `json:"uncovered_routes,omitempty"`
	Digest          string            `json:"digest"`
}

// GenerateFeatureIntentConformanceSuite compiles a coverage registry and the
// enabled route list into a conformance suite plan. It is pure and
// deterministic: it performs no I/O and consults no implementation phase
// beyond the registry rows it is given.
//
// Every DEFINED|PARTIAL|IMPLIED row must name a real binding, role,
// evidence expectation, channel and depth, and must resolve to all seven
// vectors; otherwise the function returns ErrConformanceGap naming the
// missing proof. DEFERRED|MISSING rows resolve to explicitly unavailable
// entries and are therefore never mistaken for implementation. Every enabled
// route must be named as some entry's channel; an unexercised route is a
// gap, not a silent pass.
func GenerateFeatureIntentConformanceSuite(registry FeatureIntentCoverageRegistry, enabledRoutes []string) (ConformanceSuite, error) {
	suite := ConformanceSuite{SchemaVersion: 1}
	routes := append([]string(nil), enabledRoutes...)
	for i, route := range routes {
		route = strings.TrimSpace(route)
		if route == "" {
			return ConformanceSuite{}, fmt.Errorf("%w: enabled route %d is empty", ErrInvalidConformanceInput, i)
		}
		routes[i] = route
	}
	sort.Strings(routes)
	suite.EnabledRoutes = routes

	entries := make([]ConformanceCase, 0, len(registry.Features))
	covered := make(map[string]bool, len(routes))
	for _, feature := range registry.Features {
		entry, err := conformanceCaseFor(feature)
		if err != nil {
			return ConformanceSuite{}, err
		}
		entries = append(entries, entry)
		channel := strings.TrimSpace(feature.Channel)
		if channel != "" {
			covered[channel] = true
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].FeatureID < entries[j].FeatureID })
	suite.Entries = entries

	var uncovered []string
	for _, route := range routes {
		if !covered[route] {
			uncovered = append(uncovered, route)
		}
	}
	if len(uncovered) != 0 {
		return ConformanceSuite{}, fmt.Errorf("%w: enabled route(s) %s have no conformance entry", ErrConformanceGap, strings.Join(uncovered, ", "))
	}
	suite.Digest = suite.computeDigest()
	return suite, nil
}

func conformanceCaseFor(feature FeatureIntentCoverage) (ConformanceCase, error) {
	id := strings.TrimSpace(feature.FeatureID)
	if id == "" {
		return ConformanceCase{}, fmt.Errorf("%w: feature with empty id", ErrInvalidConformanceInput)
	}
	if !ValidSemanticClassifications[feature.Classification] || strings.TrimSpace(string(feature.Classification)) == "" {
		return ConformanceCase{}, fmt.Errorf("%w: feature %s has unknown classification %q", ErrInvalidConformanceInput, id, string(feature.Classification))
	}
	if strings.TrimSpace(string(feature.Role)) == "" || !ValidFeatureIntentRoles[feature.Role] {
		return ConformanceCase{}, fmt.Errorf("%w: feature %s has unknown role %q", ErrInvalidConformanceInput, id, string(feature.Role))
	}
	bound := strings.TrimSpace(feature.BoundIntentID)
	switch {
	case feature.CoverageStatus == CoverageDeferred || bound == DeferredIntentBinding:
		return ConformanceCase{
			FeatureID: id, GroupName: feature.GroupName, Classification: feature.Classification,
			Role: feature.Role, BoundIntent: bound, Depth: strings.TrimSpace(feature.Depth),
			Availability: ConformanceUnavailableDeferred,
			Reason:       fmt.Sprintf("feature %s is deferred to %s and therefore unavailable", id, strings.TrimSpace(feature.DispositionTarget)),
		}, nil
	case feature.CoverageStatus == CoverageMissing || bound == "" || bound == "MISSING":
		return ConformanceCase{
			FeatureID: id, GroupName: feature.GroupName, Classification: feature.Classification,
			Role: feature.Role, BoundIntent: bound, Depth: strings.TrimSpace(feature.Depth),
			Availability: ConformanceUnavailableMissing,
			Reason:       fmt.Sprintf("feature %s is missing a binding and therefore unavailable", id),
		}, nil
	}
	switch feature.CoverageStatus {
	case CoverageDefined, CoveragePartial, CoverageImplied:
	default:
		return ConformanceCase{}, fmt.Errorf("%w: feature %s has unknown coverage status %q", ErrInvalidConformanceInput, id, string(feature.CoverageStatus))
	}
	// Implemented rows must resolve every vector. REVIEW_REQUIRED has no
	// family template by construction: it needs review, not a suite.
	switch feature.Classification {
	case ClassCreate, ClassConsume, ClassEmitChild, ClassObserve, ClassNonMaterial:
	default:
		return ConformanceCase{}, fmt.Errorf("%w: feature %s classification %s needs review before suite generation", ErrConformanceGap, id, feature.Classification)
	}
	evidence := strings.TrimSpace(feature.EvidenceExpectation)
	if evidence == "" {
		return ConformanceCase{}, fmt.Errorf("%w: implemented feature %s lacks an evidence expectation", ErrConformanceGap, id)
	}
	channel := strings.TrimSpace(feature.Channel)
	if channel == "" {
		return ConformanceCase{}, fmt.Errorf("%w: implemented feature %s lacks a channel/route", ErrConformanceGap, id)
	}
	depth := strings.TrimSpace(feature.Depth)
	if depth == "" {
		return ConformanceCase{}, fmt.Errorf("%w: implemented feature %s lacks a declared depth", ErrConformanceGap, id)
	}
	governance := strings.TrimSpace(feature.GovernanceProfile)
	if governance == "" {
		governance = "owner:" + strings.TrimSpace(feature.Owner)
	}
	if strings.TrimSpace(governance) == "" || governance == "owner:" {
		return ConformanceCase{}, fmt.Errorf("%w: implemented feature %s lacks a governance denial vector", ErrConformanceGap, id)
	}
	capability := strings.TrimSpace(feature.Capability)
	if capability == "" {
		capability = "intent:" + bound
	}
	phase := strings.TrimSpace(feature.Phase)
	if phase == "" {
		return ConformanceCase{}, fmt.Errorf("%w: implemented feature %s lacks a lifecycle/result oracle", ErrConformanceGap, id)
	}
	var behavior string
	switch feature.Classification {
	case ClassCreate:
		behavior = "typed_create:" + bound
	case ClassConsume:
		behavior = "typed_consume:" + bound
	case ClassEmitChild:
		behavior = "typed_emit:" + bound
	case ClassObserve:
		behavior = "typed_observe:" + bound
	case ClassNonMaterial:
		behavior = "mechanic_template:" + capability
	}
	return ConformanceCase{
		FeatureID: id, GroupName: feature.GroupName, Classification: feature.Classification,
		Role: feature.Role, BoundIntent: bound, Depth: depth,
		Availability: ConformanceAvailable,
		Vectors: ConformanceVectors{
			Behavior:            behavior,
			DenialVector:        "governance_denial:" + governance,
			IdempotencyVector:   "idempotency_replay:" + bound,
			ZeroBypass:          "zero_bypass:" + capability,
			LifecycleOracle:     "lifecycle_oracle:" + phase + "/" + depth,
			EvidenceExpectation: evidence,
			DepthAssertion:      "depth:" + depth,
		},
		Routes: []string{channel},
	}, nil
}

func (s ConformanceSuite) computeDigest() string {
	copy := s
	copy.Digest = ""
	b, _ := json.Marshal(copy)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
