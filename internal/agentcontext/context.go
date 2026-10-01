package agentcontext

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

var (
	ErrInvalidRequest = errors.New("agentcontext: invalid request")
	ErrStaleSource    = errors.New("agentcontext: source is stale")
	ErrRecheckDenied  = errors.New("agentcontext: late recheck denied")
)

// FactKind records who can assert a context fact. User claims and model
// inference remain explicitly untrusted even when retained for explanation.
type FactKind string

const (
	CanonicalHCMFact FactKind = "CANONICAL_HCM"
	ExternalFact     FactKind = "EXTERNAL_OBSERVATION"
	DocumentPolicy   FactKind = "DOCUMENT_POLICY"
	UserClaim        FactKind = "USER_CLAIM"
	ModelInference   FactKind = "MODEL_INFERENCE"
)

// Source is an owner-issued provenance and freshness envelope for one fact.
type Source struct {
	Owner           string
	ID              string
	Version         string
	Kind            FactKind
	Classification  string
	Audience        []string
	ObservedAt      time.Time
	FreshUntil      time.Time
	RevocationToken string
}

// Fact separates the value from its owner and trust classification.
type Fact struct {
	Key    string
	Value  string
	Source Source
}

// Principal identifies a server-validated actor in the invocation chain.
type Principal struct {
	ID   string
	Kind string
}

// Snapshot is a typed aggregation returned by the owning services. Its fields
// must come from current owner APIs, never from chat text or model output.
type Snapshot struct {
	TenantID, OrganizationID, AgentID, AgentVersion, InstallationID string
	TriggerID, InvokerID, Purpose, SubjectID, ResourceID            string
	ConversationID, OutputAudience, TemporalMode                    string
	Principals                                                      []Principal
	SourceGrants, CapabilityGrants                                  []string
	AuthZVersion, LegalPolicyVersion                                string
	Classification                                                  string
	TimeZone, Locale, Autonomy, Risk                                string
	BudgetID, CorrelationID                                         string
	Sources                                                         []Source
	Facts                                                           []Fact
	RevocationTokens                                                []string
}

// Resolver obtains a composite snapshot from the authoritative owning
// services after the invocation identity has been resolved.
type Resolver interface {
	ResolveAgentContext(context.Context, ResolveRequest) (Snapshot, error)
}

// Rechecker revalidates all authority and source invalidators in a context.
// Implementations must consult current owners at the point of delivery.
type Rechecker interface {
	RecheckAgentContext(context.Context, RecheckRequest) (RecheckDecision, error)
}

// ResolveRequest contains only identifiers established by server-side
// invocation admission. ChatClaims are checked for forbidden authority claims.
type ResolveRequest struct {
	InvocationID string
	ChatClaims   map[string]string
}

// RecheckRequest pins the context identity and versions that must still hold.
type RecheckRequest struct {
	ContextDigest      string
	TenantID           string
	Audience           string
	AuthZVersion       string
	LegalPolicyVersion string
	Revocations        []string
	Sources            []SourcePin
}

// SourcePin identifies the exact source versions, audiences, and invalidators
// that must remain valid at the final boundary.
type SourcePin struct {
	Owner, ID, Version, Classification, RevocationToken string
	Kind                                                FactKind
	Audience                                            []string
	ObservedAt, FreshUntil                              time.Time
}

// RecheckDecision reports whether every pinned owner decision remains current.
type RecheckDecision struct {
	Allowed            bool
	Audience           string
	AuthZVersion       string
	LegalPolicyVersion string
	Revocations        []string
	Sources            []SourcePin
}

// EffectiveAgentContext is immutable-by-convention output suitable for a run
// trace. Build clones all slices so later input mutation cannot widen it.
type EffectiveAgentContext struct {
	Snapshot Snapshot
	Digest   string
	AsOf     time.Time
}

// Builder composes the owner resolver with the mandatory late recheck port.
type Builder struct {
	resolver Resolver
	recheck  Rechecker
	now      func() time.Time
}

// NewBuilder requires both owner resolution and a late recheck implementation.
func NewBuilder(resolver Resolver, rechecker Rechecker, now func() time.Time) (*Builder, error) {
	if resolver == nil || rechecker == nil || now == nil {
		return nil, fmt.Errorf("%w: resolver, rechecker, and clock are required", ErrInvalidRequest)
	}
	return &Builder{resolver: resolver, recheck: rechecker, now: now}, nil
}

// Build resolves and validates current context. Chat-supplied tenant or role
// values are rejected instead of being silently ignored or treated as proof.
func (b *Builder) Build(ctx context.Context, request ResolveRequest) (EffectiveAgentContext, error) {
	if b == nil || b.resolver == nil || b.recheck == nil || b.now == nil || strings.TrimSpace(request.InvocationID) == "" {
		return EffectiveAgentContext{}, fmt.Errorf("%w: builder and invocation are required", ErrInvalidRequest)
	}
	if hasAuthorityClaim(request.ChatClaims) {
		return EffectiveAgentContext{}, fmt.Errorf("%w: chat cannot assert tenant or role", ErrInvalidRequest)
	}
	snapshot, err := b.resolver.ResolveAgentContext(ctx, request)
	if err != nil {
		return EffectiveAgentContext{}, err
	}
	asOf := b.now().UTC()
	if err := validateSnapshot(snapshot, asOf); err != nil {
		return EffectiveAgentContext{}, err
	}
	snapshot = cloneSnapshot(snapshot)
	return EffectiveAgentContext{Snapshot: snapshot, Digest: digest(snapshot), AsOf: asOf}, nil
}

// Recheck is the required final authorization seam before output or effects.
// A changed policy version or revocation set fails closed.
func (b *Builder) Recheck(ctx context.Context, effective EffectiveAgentContext) error {
	if b == nil || b.recheck == nil || effective.Digest == "" || effective.Digest != digest(effective.Snapshot) {
		return fmt.Errorf("%w: context is absent or modified", ErrRecheckDenied)
	}
	req := RecheckRequest{ContextDigest: effective.Digest, TenantID: effective.Snapshot.TenantID, Audience: effective.Snapshot.OutputAudience, AuthZVersion: effective.Snapshot.AuthZVersion, LegalPolicyVersion: effective.Snapshot.LegalPolicyVersion, Revocations: slices.Clone(effective.Snapshot.RevocationTokens), Sources: sourcePins(effective.Snapshot)}
	decision, err := b.recheck.RecheckAgentContext(ctx, req)
	if err != nil {
		return err
	}
	if !decision.Allowed || decision.Audience != req.Audience || decision.AuthZVersion != req.AuthZVersion || decision.LegalPolicyVersion != req.LegalPolicyVersion || !sameSet(decision.Revocations, req.Revocations) || !sameSourcePins(decision.Sources, req.Sources) {
		return ErrRecheckDenied
	}
	return nil
}

func hasAuthorityClaim(claims map[string]string) bool {
	for key := range claims {
		normalized := strings.ToLower(strings.TrimSpace(key))
		normalized = strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(normalized)
		if normalized == "tenant" || normalized == "tenantid" || normalized == "role" || normalized == "roles" || normalized == "userrole" || normalized == "userroles" {
			return true
		}
	}
	return false
}

func sourcePins(snapshot Snapshot) []SourcePin {
	pins := make([]SourcePin, 0, len(snapshot.Sources)+len(snapshot.Facts))
	for _, source := range snapshot.Sources {
		pins = append(pins, pin(source))
	}
	for _, fact := range snapshot.Facts {
		pins = append(pins, pin(fact.Source))
	}
	return pins
}

func pin(source Source) SourcePin {
	return SourcePin{Owner: source.Owner, ID: source.ID, Version: source.Version, Classification: source.Classification, RevocationToken: source.RevocationToken, Kind: source.Kind, Audience: slices.Clone(source.Audience), ObservedAt: source.ObservedAt, FreshUntil: source.FreshUntil}
}

func sameSourcePins(left, right []SourcePin) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		a, b := left[i], right[i]
		if a.Owner != b.Owner || a.ID != b.ID || a.Version != b.Version || a.Classification != b.Classification || a.RevocationToken != b.RevocationToken || a.Kind != b.Kind || !a.ObservedAt.Equal(b.ObservedAt) || !a.FreshUntil.Equal(b.FreshUntil) || !sameSet(a.Audience, b.Audience) {
			return false
		}
	}
	return true
}

func validateSnapshot(s Snapshot, now time.Time) error {
	for name, value := range map[string]string{"tenant": s.TenantID, "organization": s.OrganizationID, "agent": s.AgentID, "agent version": s.AgentVersion, "installation": s.InstallationID, "trigger": s.TriggerID, "purpose": s.Purpose, "conversation": s.ConversationID, "audience": s.OutputAudience, "temporal mode": s.TemporalMode, "authorization version": s.AuthZVersion, "legal policy version": s.LegalPolicyVersion, "classification": s.Classification, "correlation": s.CorrelationID} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRequest, name)
		}
	}
	for name, value := range map[string]string{"time zone": s.TimeZone, "locale": s.Locale, "autonomy": s.Autonomy, "risk": s.Risk, "budget": s.BudgetID} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRequest, name)
		}
	}
	if len(s.Principals) == 0 || !validValues(s.SourceGrants) || !validValues(s.CapabilityGrants) {
		return fmt.Errorf("%w: principal chain and current grants are required", ErrInvalidRequest)
	}
	for _, principal := range s.Principals {
		if strings.TrimSpace(principal.ID) == "" || strings.TrimSpace(principal.Kind) == "" {
			return fmt.Errorf("%w: principal chain contains an incomplete actor", ErrInvalidRequest)
		}
	}
	for _, source := range s.Sources {
		if err := validateSource(source, now); err != nil {
			return fmt.Errorf("%w: %s/%s has incomplete or stale provenance", ErrStaleSource, source.Owner, source.ID)
		}
	}
	for _, fact := range s.Facts {
		if strings.TrimSpace(fact.Key) == "" || strings.TrimSpace(fact.Value) == "" {
			return fmt.Errorf("%w: fact requires a value and trust classification", ErrInvalidRequest)
		}
		if err := validateSource(fact.Source, now); err != nil {
			return fmt.Errorf("%w: fact %s has incomplete or stale provenance", ErrStaleSource, fact.Key)
		}
	}
	if !validValues(s.RevocationTokens) {
		return fmt.Errorf("%w: revocation tokens are required", ErrInvalidRequest)
	}
	return nil
}

func validateSource(source Source, now time.Time) error {
	if strings.TrimSpace(source.Owner) == "" || strings.TrimSpace(source.ID) == "" || strings.TrimSpace(source.Version) == "" || source.Kind == "" || source.FreshUntil.IsZero() || !now.Before(source.FreshUntil) || source.ObservedAt.IsZero() || source.ObservedAt.After(now) || strings.TrimSpace(source.Classification) == "" || !validValues(source.Audience) || strings.TrimSpace(source.RevocationToken) == "" {
		return ErrStaleSource
	}
	return nil
}

func validValues(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func cloneSnapshot(s Snapshot) Snapshot {
	s.Principals = slices.Clone(s.Principals)
	s.SourceGrants = slices.Clone(s.SourceGrants)
	s.CapabilityGrants = slices.Clone(s.CapabilityGrants)
	s.RevocationTokens = slices.Clone(s.RevocationTokens)
	s.Sources = slices.Clone(s.Sources)
	s.Facts = slices.Clone(s.Facts)
	for i := range s.Sources {
		s.Sources[i].Audience = slices.Clone(s.Sources[i].Audience)
	}
	for i := range s.Facts {
		s.Facts[i].Source.Audience = slices.Clone(s.Facts[i].Source.Audience)
	}
	return s
}

func digest(s Snapshot) string {
	// Field ordering is explicit, avoiding map serialization and unstable hashes.
	parts := []string{s.TenantID, s.OrganizationID, s.AgentID, s.AgentVersion, s.InstallationID, s.TriggerID, s.InvokerID, s.Purpose, s.SubjectID, s.ResourceID, s.ConversationID, s.OutputAudience, s.TemporalMode, s.AuthZVersion, s.LegalPolicyVersion, s.Classification, s.TimeZone, s.Locale, s.Autonomy, s.Risk, s.BudgetID, s.CorrelationID}
	parts = append(parts, s.SourceGrants...)
	parts = append(parts, s.CapabilityGrants...)
	parts = append(parts, s.RevocationTokens...)
	for _, principal := range s.Principals {
		parts = append(parts, principal.ID, principal.Kind)
	}
	for _, source := range s.Sources {
		parts = append(parts, source.Owner, source.ID, source.Version, string(source.Kind), source.Classification, source.RevocationToken, source.ObservedAt.UTC().Format(time.RFC3339Nano), source.FreshUntil.UTC().Format(time.RFC3339Nano))
		parts = append(parts, source.Audience...)
	}
	for _, fact := range s.Facts {
		source := fact.Source
		parts = append(parts, fact.Key, fact.Value, source.Owner, source.ID, source.Version, string(source.Kind), source.Classification, source.RevocationToken, source.ObservedAt.UTC().Format(time.RFC3339Nano), source.FreshUntil.UTC().Format(time.RFC3339Nano))
		parts = append(parts, source.Audience...)
	}
	h := sha256.New()
	var length [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func sameSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	l := slices.Clone(left)
	r := slices.Clone(right)
	slices.Sort(l)
	slices.Sort(r)
	return slices.Equal(l, r)
}
