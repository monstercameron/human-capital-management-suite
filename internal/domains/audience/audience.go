// Package audience resolves message recipients and their eligible delivery
// endpoints from read-only facts. It contains no persistence or provider
// integration; callers supply current facts and an already-scoped disclosure
// decision.
package audience

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	audienceSchema  = "hcmnext.domains.audience.Resolution"
	version         = 1
	maxDefaultDepth = 32
)

// Version is the audience domain contract version.
func Version() int { return version }

var (
	ErrInvalidRequest   = errors.New("audience: request is invalid")
	ErrInvalidFact      = errors.New("audience: fact is invalid")
	ErrFactsPortMissing = errors.New("audience: required facts port is missing")
	ErrFactsReadFailed  = errors.New("audience: facts port failed")
	ErrInvalidDecision  = errors.New("audience: disclosure decision is invalid")
	ErrInvalidPolicy    = errors.New("audience: delivery policy is invalid")
)

// SourceKind names the governed expression that produced a candidate.
type SourceKind string

const (
	SourceExplicit        SourceKind = "EXPLICIT_SUBJECT"
	SourceOrgUnit         SourceKind = "ORG_UNIT_MEMBER"
	SourceManagementChain SourceKind = "MANAGEMENT_CHAIN"
	SourceRoleHolder      SourceKind = "ROLE_HOLDER"
)

// OrgUnitSelector asks for the members of one org unit at the request's
// effective instant.
type OrgUnitSelector struct{ OrgUnit string }

// ManagementChainSelector asks for a subject's current direct-manager chain.
// The org domain applies per-hop disclosure before this package sees a
// manager reference.
type ManagementChainSelector struct {
	Subject  values.EntityRef
	MaxDepth int
}

// RoleSelector asks for current holders of a role, optionally within a
// caller-visible scope token.
type RoleSelector struct {
	Role  string
	Scope string
}

// AudienceSpec is the closed message-audience vocabulary for this package.
type AudienceSpec struct {
	ExplicitSubjects []values.EntityRef
	OrgUnits         []OrgUnitSelector
	ManagementChains []ManagementChainSelector
	RoleHolders      []RoleSelector
}

// Expression returns the canonical description of this specification. It is
// safe to bind to a message intent before resolving the current audience.
func (s AudienceSpec) Expression() string { return specDescription(s) }

func (s AudienceSpec) Validate(tenant values.TenantId) error {
	for _, ref := range s.ExplicitSubjects {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("%w: explicit subject: %v", ErrInvalidRequest, err)
		}
		if ref.Tenant != tenant {
			return fmt.Errorf("%w: explicit subject crosses tenant", ErrInvalidRequest)
		}
	}
	for _, selector := range s.OrgUnits {
		if strings.TrimSpace(selector.OrgUnit) == "" {
			return fmt.Errorf("%w: org unit is required", ErrInvalidRequest)
		}
	}
	for _, selector := range s.ManagementChains {
		if err := selector.Subject.Validate(); err != nil {
			return fmt.Errorf("%w: manager subject: %v", ErrInvalidRequest, err)
		}
		if selector.Subject.Tenant != tenant || selector.Subject.Kind != people.KindWorker {
			return fmt.Errorf("%w: manager subject is outside tenant or is not a worker", ErrInvalidRequest)
		}
		if selector.MaxDepth < 1 || selector.MaxDepth > maxDefaultDepth {
			return fmt.Errorf("%w: manager max depth must be 1..%d", ErrInvalidRequest, maxDefaultDepth)
		}
	}
	for _, selector := range s.RoleHolders {
		if strings.TrimSpace(selector.Role) == "" {
			return fmt.Errorf("%w: role is required", ErrInvalidRequest)
		}
	}
	return nil
}

// OrgUnitQuery is the bitemporal query sent to an org-unit facts port.
type OrgUnitQuery struct {
	Tenant  values.TenantId
	OrgUnit string
	AsOf    values.Instant
}

// OrgUnitMemberSet is one consistent read of an org-unit population.
type OrgUnitMemberSet struct {
	Members       []values.EntityRef
	PolicyVersion string
	Watermark     values.RevisionToken
}

func (s OrgUnitMemberSet) validate(tenant values.TenantId) error {
	seen := make(map[string]struct{}, len(s.Members))
	for _, member := range s.Members {
		if err := member.Validate(); err != nil {
			return fmt.Errorf("%w: org-unit member: %v", ErrInvalidFact, err)
		}
		if member.Tenant != tenant {
			return fmt.Errorf("%w: org-unit member crosses tenant", ErrInvalidFact)
		}
		if _, ok := seen[member.String()]; ok {
			return fmt.Errorf("%w: duplicate org-unit member %s", ErrInvalidFact, member)
		}
		seen[member.String()] = struct{}{}
	}
	if s.PolicyVersion == "" {
		return fmt.Errorf("%w: org-unit policy version is required", ErrInvalidFact)
	}
	return nil
}

// OrgUnitFacts is the read-only port for effective org-unit membership.
type OrgUnitFacts interface {
	OrgUnitMembersAt(context.Context, OrgUnitQuery) (OrgUnitMemberSet, error)
}

// RoleQuery is the bitemporal query sent to a role facts port.
type RoleQuery struct {
	Tenant values.TenantId
	Role   string
	Scope  string
	AsOf   values.Instant
}

// RoleHolderSet is one consistent read of a scoped role population.
type RoleHolderSet struct {
	Holders       []values.EntityRef
	PolicyVersion string
	Watermark     values.RevisionToken
}

func (s RoleHolderSet) validate(tenant values.TenantId) error {
	seen := make(map[string]struct{}, len(s.Holders))
	for _, holder := range s.Holders {
		if err := holder.Validate(); err != nil {
			return fmt.Errorf("%w: role holder: %v", ErrInvalidFact, err)
		}
		if holder.Tenant != tenant {
			return fmt.Errorf("%w: role holder crosses tenant", ErrInvalidFact)
		}
		if _, ok := seen[holder.String()]; ok {
			return fmt.Errorf("%w: duplicate role holder %s", ErrInvalidFact, holder)
		}
		seen[holder.String()] = struct{}{}
	}
	if s.PolicyVersion == "" {
		return fmt.Errorf("%w: role policy version is required", ErrInvalidFact)
	}
	return nil
}

// RoleFacts is the read-only port for effective role holders.
type RoleFacts interface {
	RoleHoldersAt(context.Context, RoleQuery) (RoleHolderSet, error)
}

// DisclosureDecision is the caller's already-evaluated right to address a
// principal. A denied decision must name a stable policy reason; no protected
// fact is inferred from a missing decision.
type DisclosureDecision struct {
	Allowed       bool
	Reason        string
	PolicyVersion string
}

// DisclosureScope is deliberately a function supplied by the authorization
// layer. Audience resolution never constructs its own broader policy.
type DisclosureScope func(values.EntityRef, SourceKind) DisclosureDecision

// Request is the pure input to ResolveAudience. ResolvedAt is supplied by the
// caller so replaying the same request does not read a wall clock.
type Request struct {
	Tenant           values.TenantId
	AsOf             values.Instant
	ResolvedAt       values.Instant
	Spec             AudienceSpec
	Scope            DisclosureScope
	OrgUnits         OrgUnitFacts
	Roles            RoleFacts
	Managers         org.WorkerFacts
	ManagerAuthorize org.Authorizer
}

func (r Request) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidRequest, err)
	}
	if err := r.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrInvalidRequest, err)
	}
	if err := r.ResolvedAt.Validate(); err != nil {
		return fmt.Errorf("%w: resolved-at: %v", ErrInvalidRequest, err)
	}
	if r.Scope == nil {
		return fmt.Errorf("%w: disclosure scope is required", ErrInvalidRequest)
	}
	if err := r.Spec.Validate(r.Tenant); err != nil {
		return err
	}
	if len(r.Spec.OrgUnits) > 0 && r.OrgUnits == nil {
		return ErrFactsPortMissing
	}
	if len(r.Spec.RoleHolders) > 0 && r.Roles == nil {
		return ErrFactsPortMissing
	}
	if len(r.Spec.ManagementChains) > 0 && r.Managers == nil {
		return ErrFactsPortMissing
	}
	return nil
}

// CandidateRecord records that a candidate was considered without exposing a
// raw principal in an audit-safe audience explanation.
type CandidateRecord struct {
	PrincipalDigest string
	Sources         []SourceKind
}

// Exclusion records a candidate refused by disclosure or by a facts/policy
// boundary. PrincipalDigest is intentionally a digest, including for explicit
// candidates, so an explanation cannot become a recipient enumeration channel.
type Exclusion struct {
	PrincipalDigest string
	Source          SourceKind
	Reason          string
}

// Resolution is the current authorized audience snapshot.
type Resolution struct {
	Expression           string
	Candidates           []CandidateRecord
	Exclusions           []Exclusion
	ExclusionCounts      map[string]int
	Principals           []values.EntityRef
	PolicyVersions       []string
	RelationshipVersions []string
	ResolvedAt           values.Instant
	InputsDigest         string
	ResultDigest         string
}

func refDigest(ref values.EntityRef) string {
	return canonicalbytes.Digest(ref.Canonical())
}

func sourceRank(source SourceKind) int {
	switch source {
	case SourceExplicit:
		return 0
	case SourceOrgUnit:
		return 1
	case SourceManagementChain:
		return 2
	case SourceRoleHolder:
		return 3
	default:
		return 4
	}
}

type candidate struct {
	ref     values.EntityRef
	sources map[SourceKind]struct{}
}

func addPolicyVersion(versions *[]string, version string) {
	if version == "" {
		return
	}
	for _, existing := range *versions {
		if existing == version {
			return
		}
	}
	*versions = append(*versions, version)
}

func addCandidate(candidates map[string]*candidate, ref values.EntityRef, source SourceKind) {
	key := ref.String()
	entry := candidates[key]
	if entry == nil {
		entry = &candidate{ref: ref, sources: make(map[SourceKind]struct{})}
		candidates[key] = entry
	}
	entry.sources[source] = struct{}{}
}

func specDescription(spec AudienceSpec) string {
	parts := make([]string, 0, len(spec.ExplicitSubjects)+len(spec.OrgUnits)+len(spec.ManagementChains)+len(spec.RoleHolders))
	for _, ref := range spec.ExplicitSubjects {
		parts = append(parts, string(SourceExplicit)+"("+ref.String()+")")
	}
	for _, selector := range spec.OrgUnits {
		parts = append(parts, string(SourceOrgUnit)+"("+selector.OrgUnit+")")
	}
	for _, selector := range spec.ManagementChains {
		parts = append(parts, string(SourceManagementChain)+"("+selector.Subject.String()+")")
	}
	for _, selector := range spec.RoleHolders {
		parts = append(parts, string(SourceRoleHolder)+"("+selector.Role+":"+selector.Scope+")")
	}
	sort.Strings(parts)
	return strings.Join(parts, " OR ")
}

func requestDigest(r Request) (string, error) {
	w := canonicalbytes.New("hcmnext.domains.audience.Request", version).
		String("tenant", string(r.Tenant)).Value("as_of", r.AsOf).Value("resolved_at", r.ResolvedAt)
	for _, ref := range r.Spec.ExplicitSubjects {
		w.Value("explicit", ref)
	}
	for _, selector := range r.Spec.OrgUnits {
		w.String("org_unit", selector.OrgUnit)
	}
	for _, selector := range r.Spec.ManagementChains {
		w.Value("manager_subject", selector.Subject).Int("manager_depth", int64(selector.MaxDepth))
	}
	for _, selector := range r.Spec.RoleHolders {
		w.String("role", selector.Role).String("role_scope", selector.Scope)
	}
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

func decisionFor(scope DisclosureScope, ref values.EntityRef, source SourceKind) (DisclosureDecision, error) {
	decision := scope(ref, source)
	if !decision.Allowed && decision.Reason == "" {
		return DisclosureDecision{}, fmt.Errorf("%w: denied %s has no reason", ErrInvalidDecision, ref)
	}
	return decision, nil
}

// ResolveAudience resolves explicit subjects, org-unit members, management
// chains, and role holders. It deduplicates by tenant-scoped entity reference,
// applies disclosure after candidate collection, and sorts every exposed set.
func ResolveAudience(ctx context.Context, facts Request) (Resolution, error) {
	if err := facts.Validate(); err != nil {
		return Resolution{}, err
	}
	inputsDigest, err := requestDigest(facts)
	if err != nil {
		return Resolution{}, err
	}
	candidates := make(map[string]*candidate)
	policyVersions := make([]string, 0)
	relationshipVersions := make([]string, 0)
	for _, ref := range facts.Spec.ExplicitSubjects {
		addCandidate(candidates, ref, SourceExplicit)
	}
	for _, selector := range facts.Spec.OrgUnits {
		set, readErr := facts.OrgUnits.OrgUnitMembersAt(ctx, OrgUnitQuery{Tenant: facts.Tenant, OrgUnit: selector.OrgUnit, AsOf: facts.AsOf})
		if readErr != nil {
			return Resolution{}, fmt.Errorf("%w: org unit %s: %v", ErrFactsReadFailed, selector.OrgUnit, readErr)
		}
		if err := set.validate(facts.Tenant); err != nil {
			return Resolution{}, err
		}
		addPolicyVersion(&policyVersions, set.PolicyVersion)
		for _, ref := range set.Members {
			addCandidate(candidates, ref, SourceOrgUnit)
		}
	}
	for _, selector := range facts.Spec.RoleHolders {
		set, readErr := facts.Roles.RoleHoldersAt(ctx, RoleQuery{Tenant: facts.Tenant, Role: selector.Role, Scope: selector.Scope, AsOf: facts.AsOf})
		if readErr != nil {
			return Resolution{}, fmt.Errorf("%w: role %s: %v", ErrFactsReadFailed, selector.Role, readErr)
		}
		if err := set.validate(facts.Tenant); err != nil {
			return Resolution{}, err
		}
		addPolicyVersion(&policyVersions, set.PolicyVersion)
		for _, ref := range set.Holders {
			addCandidate(candidates, ref, SourceRoleHolder)
		}
	}
	for _, selector := range facts.Spec.ManagementChains {
		authorize := facts.ManagerAuthorize
		if authorize == nil {
			authorize = func(f org.ManagerRelationshipFact) people.AuthorizationDecision {
				decision := facts.Scope(f.Manager, SourceManagementChain)
				if decision.PolicyVersion == "" {
					decision.PolicyVersion = "audience.scope/unspecified"
				}
				result := people.AuthorizationDecision{PolicyVersion: decision.PolicyVersion, Purpose: "message.audience", SubjectDisclosable: decision.Allowed, SubjectDenialReason: decision.Reason, Fields: map[people.FieldID]people.FieldRuling{
					people.FieldManagerRelation: {Effect: people.EffectAllow},
				}}
				return result
			}
		}
		managerResult, resolveErr := org.ResolveManagerRelationships(ctx, facts.Managers, org.ManagerResolutionRequest{Tenant: facts.Tenant, Worker: selector.Subject, AsOf: facts.AsOf, MaxDepth: selector.MaxDepth, Authorize: authorize})
		if resolveErr != nil {
			return Resolution{}, fmt.Errorf("%w: manager chain for %s: %v", ErrFactsReadFailed, selector.Subject, resolveErr)
		}
		addPolicyVersion(&policyVersions, managerResult.PolicyVersion)
		addPolicyVersion(&relationshipVersions, org.ManagerResolutionRulePack)
		for _, hop := range managerResult.Chain {
			if hop.Disclosure == people.DisclosureWithheld {
				continue
			}
			if hop.Manager.Access != people.AccessAuthorized {
				continue
			}
			decision, decisionErr := decisionFor(facts.Scope, hop.Manager.Value, SourceManagementChain)
			if decisionErr != nil {
				return Resolution{}, decisionErr
			}
			if decision.Allowed {
				addCandidate(candidates, hop.Manager.Value, SourceManagementChain)
				addPolicyVersion(&policyVersions, decision.PolicyVersion)
			}
		}
	}

	keys := make([]string, 0, len(candidates))
	for key := range candidates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := Resolution{Expression: specDescription(facts.Spec), ExclusionCounts: make(map[string]int), ResolvedAt: facts.ResolvedAt, InputsDigest: inputsDigest}
	for _, key := range keys {
		entry := candidates[key]
		sources := make([]SourceKind, 0, len(entry.sources))
		for source := range entry.sources {
			sources = append(sources, source)
		}
		sort.Slice(sources, func(i, j int) bool { return sourceRank(sources[i]) < sourceRank(sources[j]) })
		candidateRecord := CandidateRecord{PrincipalDigest: refDigest(entry.ref), Sources: sources}
		result.Candidates = append(result.Candidates, candidateRecord)
		allowed := true
		var deniedBy SourceKind
		var denialReason string
		for _, source := range sources {
			decision, decisionErr := decisionFor(facts.Scope, entry.ref, source)
			if decisionErr != nil {
				return Resolution{}, decisionErr
			}
			addPolicyVersion(&policyVersions, decision.PolicyVersion)
			if !decision.Allowed {
				allowed = false
				deniedBy = source
				denialReason = decision.Reason
				break
			}
		}
		if !allowed {
			result.Exclusions = append(result.Exclusions, Exclusion{PrincipalDigest: refDigest(entry.ref), Source: deniedBy, Reason: denialReason})
			result.ExclusionCounts[denialReason]++
			continue
		}
		result.Principals = append(result.Principals, entry.ref)
	}
	sort.Strings(policyVersions)
	sort.Strings(relationshipVersions)
	result.PolicyVersions = policyVersions
	result.RelationshipVersions = relationshipVersions
	result.ResultDigest = resolutionDigest(result)
	return result, nil
}

// Resolve is an alias for ResolveAudience for callers that use the package as
// the audience capability.
func Resolve(ctx context.Context, request Request) (Resolution, error) {
	return ResolveAudience(ctx, request)
}

func resolutionDigest(result Resolution) string {
	w := canonicalbytes.New(audienceSchema, version).
		String("expression", result.Expression).Value("resolved_at", result.ResolvedAt).
		String("inputs_digest", result.InputsDigest).Count("principals", len(result.Principals))
	for _, principal := range result.Principals {
		w.Value("principal", principal)
	}
	for _, candidate := range result.Candidates {
		w.String("candidate", candidate.PrincipalDigest).Count("candidate_sources", len(candidate.Sources))
		for _, source := range candidate.Sources {
			w.String("candidate_source", string(source))
		}
	}
	for _, exclusion := range result.Exclusions {
		w.String("excluded", exclusion.PrincipalDigest).String("excluded_source", string(exclusion.Source)).String("excluded_reason", exclusion.Reason)
	}
	for _, policy := range result.PolicyVersions {
		w.String("policy", policy)
	}
	for _, relationship := range result.RelationshipVersions {
		w.String("relationship", relationship)
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Canonical returns a safe, deterministic representation of the resolution.
func (r Resolution) Canonical() []byte {
	if r.ResultDigest == "" {
		return nil
	}
	w := canonicalbytes.New(audienceSchema, version).String("result_digest", r.ResultDigest).String("inputs_digest", r.InputsDigest)
	for _, principal := range r.Principals {
		w.Value("principal", principal)
	}
	for _, exclusion := range r.Exclusions {
		w.String("excluded", exclusion.PrincipalDigest).String("reason", exclusion.Reason)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// AudienceExplanation is bounded and digest-oriented; it does not repeat a
// protected candidate identity.
type AudienceExplanation struct {
	Expression           string
	CandidateCount       int
	ChosenCount          int
	ExcludedCount        int
	ExclusionCounts      map[string]int
	PolicyVersions       []string
	RelationshipVersions []string
	ResolvedAt           values.Instant
	ResultDigest         string
}

func (r Resolution) Explain() AudienceExplanation {
	counts := make(map[string]int, len(r.ExclusionCounts))
	for reason, count := range r.ExclusionCounts {
		counts[reason] = count
	}
	return AudienceExplanation{Expression: r.Expression, CandidateCount: len(r.Candidates), ChosenCount: len(r.Principals), ExcludedCount: len(r.Exclusions), ExclusionCounts: counts, PolicyVersions: append([]string(nil), r.PolicyVersions...), RelationshipVersions: append([]string(nil), r.RelationshipVersions...), ResolvedAt: r.ResolvedAt, ResultDigest: r.ResultDigest}
}

// Explain is the package-level explanation symbol required by the domain
// contract.
func Explain(r Resolution) AudienceExplanation { return r.Explain() }

// Channel is a governed human-delivery channel.
type Channel string

const (
	ChannelEmail   Channel = "EMAIL"
	ChannelSMS     Channel = "SMS"
	ChannelPush    Channel = "PUSH"
	ChannelInbox   Channel = "INBOX"
	ChannelVoice   Channel = "VOICE"
	ChannelPostal  Channel = "POSTAL"
	ChannelWebhook Channel = "WEBHOOK"
)

func (c Channel) valid() bool {
	switch c {
	case ChannelEmail, ChannelSMS, ChannelPush, ChannelInbox, ChannelVoice, ChannelPostal, ChannelWebhook:
		return true
	default:
		return false
	}
}

// DeliveryPolicy is the purpose-specific channel boundary. An endpoint never
// makes a forbidden channel eligible.
type DeliveryPolicy struct {
	Purpose                 string
	AllowedChannels         []Channel
	Mandatory               bool
	AllowQuietHoursOverride bool
	AllowedOwnership        []string
}

func (p DeliveryPolicy) Validate() error {
	if strings.TrimSpace(p.Purpose) == "" || len(p.AllowedChannels) == 0 {
		return fmt.Errorf("%w: purpose and allowed channels are required", ErrInvalidPolicy)
	}
	seen := make(map[Channel]struct{}, len(p.AllowedChannels))
	for _, channel := range p.AllowedChannels {
		if !channel.valid() {
			return fmt.Errorf("%w: channel %q", ErrInvalidPolicy, channel)
		}
		if _, ok := seen[channel]; ok {
			return fmt.Errorf("%w: duplicate channel %q", ErrInvalidPolicy, channel)
		}
		seen[channel] = struct{}{}
	}
	for _, ownership := range p.AllowedOwnership {
		if ownership != "BUSINESS" && ownership != "PERSONAL" {
			return fmt.Errorf("%w: ownership %q", ErrInvalidPolicy, ownership)
		}
	}
	return nil
}

// PreferencesQuery scopes a preferences read to one already-authorized
// audience member.
type PreferencesQuery struct {
	Tenant    values.TenantId
	Principal values.EntityRef
	Purpose   string
	At        values.Instant
}

// Endpoint is a digest-only destination. Address material is never accepted
// by this package.
type Endpoint struct {
	Principal      values.EntityRef
	Channel        Channel
	EndpointDigest string
	Verified       bool
	Ownership      string
	Locale         string
	PurposeScope   []string
	EffectiveFrom  values.Instant
	EffectiveTo    *values.Instant
	Status         string
}

func (e Endpoint) Validate(tenant values.TenantId) error {
	if err := e.Principal.Validate(); err != nil || e.Principal.Tenant != tenant {
		return fmt.Errorf("%w: endpoint principal", ErrInvalidFact)
	}
	if !e.Channel.valid() {
		return fmt.Errorf("%w: endpoint channel %q", ErrInvalidFact, e.Channel)
	}
	if len(e.EndpointDigest) != 64 {
		return fmt.Errorf("%w: endpoint digest must be sha256 hex", ErrInvalidFact)
	}
	if _, err := hex.DecodeString(e.EndpointDigest); err != nil || strings.ToLower(e.EndpointDigest) != e.EndpointDigest {
		return fmt.Errorf("%w: endpoint digest must be lowercase hex", ErrInvalidFact)
	}
	if e.Ownership != "BUSINESS" && e.Ownership != "PERSONAL" {
		return fmt.Errorf("%w: endpoint ownership %q", ErrInvalidFact, e.Ownership)
	}
	if e.EffectiveTo != nil && e.EffectiveFrom.IsSet() && !e.EffectiveFrom.Before(*e.EffectiveTo) {
		return fmt.Errorf("%w: endpoint interval is empty or inverted", ErrInvalidFact)
	}
	return nil
}

// QuietHours is a recurring UTC-offset window declared by a recipient
// preference. Start==End denotes a full-day quiet window.
type QuietHours struct {
	StartMinute      int
	EndMinute        int
	UTCOffsetMinutes int
	Weekdays         []time.Weekday
}

func (q QuietHours) Validate() error {
	if q.StartMinute < 0 || q.StartMinute > 1439 || q.EndMinute < 0 || q.EndMinute > 1439 || q.UTCOffsetMinutes < -14*60 || q.UTCOffsetMinutes > 14*60 {
		return fmt.Errorf("%w: quiet-hours boundary", ErrInvalidFact)
	}
	seen := make(map[time.Weekday]struct{}, len(q.Weekdays))
	for _, weekday := range q.Weekdays {
		if weekday < time.Sunday || weekday > time.Saturday {
			return fmt.Errorf("%w: quiet-hours weekday", ErrInvalidFact)
		}
		if _, ok := seen[weekday]; ok {
			return fmt.Errorf("%w: duplicate quiet-hours weekday", ErrInvalidFact)
		}
		seen[weekday] = struct{}{}
	}
	return nil
}

func (q QuietHours) contains(at values.Instant) bool {
	zone := time.FixedZone("preference", q.UTCOffsetMinutes*60)
	local := at.Time().In(zone)
	if len(q.Weekdays) > 0 {
		found := false
		for _, weekday := range q.Weekdays {
			if weekday == local.Weekday() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	minute := local.Hour()*60 + local.Minute()
	if q.StartMinute == q.EndMinute {
		return true
	}
	if q.StartMinute < q.EndMinute {
		return minute >= q.StartMinute && minute < q.EndMinute
	}
	return minute >= q.StartMinute || minute < q.EndMinute
}

// Preference is one purpose-independent recipient preference for one channel.
type Preference struct {
	Principal  values.EntityRef
	Channel    Channel
	Enabled    bool
	OptOut     bool
	QuietHours []QuietHours
	Locale     string
	Version    string
}

func (p Preference) Validate(tenant values.TenantId) error {
	if err := p.Principal.Validate(); err != nil || p.Principal.Tenant != tenant {
		return fmt.Errorf("%w: preference principal", ErrInvalidFact)
	}
	if !p.Channel.valid() || p.Version == "" {
		return fmt.Errorf("%w: preference channel/version", ErrInvalidFact)
	}
	for _, quiet := range p.QuietHours {
		if err := quiet.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// PreferencesSnapshot is the read-only endpoint/preference view for one
// principal at one instant.
type PreferencesSnapshot struct {
	Endpoints     []Endpoint
	Preferences   []Preference
	PolicyVersion string
}

func (s PreferencesSnapshot) validate(tenant values.TenantId, principal values.EntityRef) error {
	seenEndpoints := make(map[string]struct{}, len(s.Endpoints))
	for _, endpoint := range s.Endpoints {
		if err := endpoint.Validate(tenant); err != nil {
			return err
		}
		if endpoint.Principal != principal {
			return fmt.Errorf("%w: endpoint belongs to another principal", ErrInvalidFact)
		}
		key := string(endpoint.Channel) + "\x00" + endpoint.EndpointDigest
		if _, ok := seenEndpoints[key]; ok {
			return fmt.Errorf("%w: duplicate endpoint", ErrInvalidFact)
		}
		seenEndpoints[key] = struct{}{}
	}
	seenPreferences := make(map[Channel]struct{}, len(s.Preferences))
	for _, preference := range s.Preferences {
		if err := preference.Validate(tenant); err != nil {
			return err
		}
		if preference.Principal != principal {
			return fmt.Errorf("%w: preference belongs to another principal", ErrInvalidFact)
		}
		if _, ok := seenPreferences[preference.Channel]; ok {
			return fmt.Errorf("%w: duplicate channel preference", ErrInvalidFact)
		}
		seenPreferences[preference.Channel] = struct{}{}
	}
	return nil
}

// PreferencesFacts is the read-only port used by delivery planning.
type PreferencesFacts interface {
	PreferencesAt(context.Context, PreferencesQuery) (PreferencesSnapshot, error)
}

// EndpointSelection is the safe subset of an eligible endpoint.
type EndpointSelection struct {
	Channel        Channel
	EndpointDigest string
	Verified       bool
	Ownership      string
	Locale         string
}

// EndpointExclusion explains why one endpoint was not selected.
type EndpointExclusion struct {
	Channel        Channel
	EndpointDigest string
	Reason         string
}

// RecipientDeliveryPlan is the independent delivery decision for one member.
type RecipientDeliveryPlan struct {
	Principal         values.EntityRef
	Locale            string
	PreferenceVersion string
	Endpoints         []EndpointSelection
	Excluded          []EndpointExclusion
}

// DeliveryRequest plans endpoints for an already resolved audience.
type DeliveryRequest struct {
	Audience    Resolution
	Policy      DeliveryPolicy
	Preferences PreferencesFacts
	At          values.Instant
}

func (r DeliveryRequest) Validate() error {
	if r.Preferences == nil {
		return ErrFactsPortMissing
	}
	if err := r.Policy.Validate(); err != nil {
		return err
	}
	if err := r.At.Validate(); err != nil {
		return fmt.Errorf("%w: delivery instant: %v", ErrInvalidRequest, err)
	}
	if len(r.Audience.Principals) == 0 && r.Audience.ResultDigest == "" {
		return fmt.Errorf("%w: audience resolution is empty", ErrInvalidRequest)
	}
	return nil
}

// DeliveryPlan is a per-member, preference-aware endpoint plan.
type DeliveryPlan struct {
	Purpose            string
	AudienceDigest     string
	Plans              []RecipientDeliveryPlan
	PolicyVersion      string
	PreferenceVersions []string
	ResolvedAt         values.Instant
	InputsDigest       string
	ResultDigest       string
}

func channelRank(channels []Channel, channel Channel) int {
	for i, allowed := range channels {
		if allowed == channel {
			return i
		}
	}
	return len(channels) + 1
}

func preferenceFor(preferences []Preference, channel Channel) (Preference, bool) {
	for _, preference := range preferences {
		if preference.Channel == channel {
			return preference, true
		}
	}
	return Preference{}, false
}

func purposeAllowed(endpoint Endpoint, purpose string) bool {
	if len(endpoint.PurposeScope) == 0 {
		return true
	}
	for _, allowed := range endpoint.PurposeScope {
		if allowed == purpose {
			return true
		}
	}
	return false
}

func ownershipAllowed(policy DeliveryPolicy, ownership string) bool {
	if len(policy.AllowedOwnership) == 0 {
		return true
	}
	for _, allowed := range policy.AllowedOwnership {
		if allowed == ownership {
			return true
		}
	}
	return false
}

func endpointActive(endpoint Endpoint, at values.Instant) string {
	if endpoint.EffectiveFrom.IsSet() && at.Before(endpoint.EffectiveFrom) {
		return "NOT_YET_EFFECTIVE"
	}
	if endpoint.EffectiveTo != nil && !at.Before(*endpoint.EffectiveTo) {
		return "EXPIRED"
	}
	if endpoint.Status != "" && endpoint.Status != "ACTIVE" {
		return "INACTIVE"
	}
	return ""
}

func deliveryInputDigest(request DeliveryRequest) string {
	w := canonicalbytes.New("hcmnext.domains.audience.DeliveryRequest", version).
		String("audience_digest", request.Audience.ResultDigest).String("purpose", request.Policy.Purpose).Value("at", request.At)
	for _, channel := range request.Policy.AllowedChannels {
		w.String("allowed_channel", string(channel))
	}
	for _, ownership := range request.Policy.AllowedOwnership {
		w.String("allowed_ownership", ownership)
	}
	w.Bool("mandatory", request.Policy.Mandatory).Bool("quiet_override", request.Policy.AllowQuietHoursOverride)
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// ResolveDelivery resolves the eligible endpoint plan for every authorized
// audience member. Rejected endpoints remain represented by digest and reason.
func ResolveDelivery(ctx context.Context, request DeliveryRequest) (DeliveryPlan, error) {
	if err := request.Validate(); err != nil {
		return DeliveryPlan{}, err
	}
	plan := DeliveryPlan{Purpose: request.Policy.Purpose, AudienceDigest: request.Audience.ResultDigest, PolicyVersion: "audience.delivery-policy/1", ResolvedAt: request.At, InputsDigest: deliveryInputDigest(request)}
	for _, member := range request.Audience.Principals {
		snapshot, readErr := request.Preferences.PreferencesAt(ctx, PreferencesQuery{Tenant: member.Tenant, Principal: member, Purpose: request.Policy.Purpose, At: request.At})
		if readErr != nil {
			return DeliveryPlan{}, fmt.Errorf("%w: preferences for %s: %v", ErrFactsReadFailed, member, readErr)
		}
		if err := snapshot.validate(member.Tenant, member); err != nil {
			return DeliveryPlan{}, err
		}
		memberPlan := RecipientDeliveryPlan{Principal: member}
		for _, preference := range snapshot.Preferences {
			addPolicyVersion(&plan.PreferenceVersions, preference.Version)
			if memberPlan.Locale == "" && preference.Locale != "" {
				memberPlan.Locale = preference.Locale
			}
			if memberPlan.PreferenceVersion == "" {
				memberPlan.PreferenceVersion = preference.Version
			}
		}
		for _, endpoint := range snapshot.Endpoints {
			exclusion := EndpointExclusion{Channel: endpoint.Channel, EndpointDigest: endpoint.EndpointDigest}
			if channelRank(request.Policy.AllowedChannels, endpoint.Channel) >= len(request.Policy.AllowedChannels) {
				exclusion.Reason = "PURPOSE_CHANNEL_FORBIDDEN"
			} else if !endpoint.Verified {
				exclusion.Reason = "UNVERIFIED"
			} else if reason := endpointActive(endpoint, request.At); reason != "" {
				exclusion.Reason = reason
			} else if !purposeAllowed(endpoint, request.Policy.Purpose) {
				exclusion.Reason = "PURPOSE_NOT_ALLOWED"
			} else if !ownershipAllowed(request.Policy, endpoint.Ownership) {
				exclusion.Reason = "OWNERSHIP_NOT_ALLOWED"
			} else if preference, ok := preferenceFor(snapshot.Preferences, endpoint.Channel); ok && preference.OptOut {
				exclusion.Reason = "OPTED_OUT"
			} else if preference, ok := preferenceFor(snapshot.Preferences, endpoint.Channel); ok && !preference.Enabled {
				exclusion.Reason = "DISABLED"
			} else if preference, ok := preferenceFor(snapshot.Preferences, endpoint.Channel); ok && quietFor(preference, request.At) && !(request.Policy.Mandatory && request.Policy.AllowQuietHoursOverride) {
				exclusion.Reason = "QUIET_HOURS"
			}
			if exclusion.Reason != "" {
				memberPlan.Excluded = append(memberPlan.Excluded, exclusion)
				continue
			}
			locale := endpoint.Locale
			if preference, ok := preferenceFor(snapshot.Preferences, endpoint.Channel); ok && preference.Locale != "" {
				locale = preference.Locale
			}
			if memberPlan.Locale == "" {
				memberPlan.Locale = locale
			}
			memberPlan.Endpoints = append(memberPlan.Endpoints, EndpointSelection{Channel: endpoint.Channel, EndpointDigest: endpoint.EndpointDigest, Verified: endpoint.Verified, Ownership: endpoint.Ownership, Locale: locale})
		}
		sort.Slice(memberPlan.Endpoints, func(i, j int) bool {
			left, right := channelRank(request.Policy.AllowedChannels, memberPlan.Endpoints[i].Channel), channelRank(request.Policy.AllowedChannels, memberPlan.Endpoints[j].Channel)
			if left != right {
				return left < right
			}
			return memberPlan.Endpoints[i].EndpointDigest < memberPlan.Endpoints[j].EndpointDigest
		})
		sort.Slice(memberPlan.Excluded, func(i, j int) bool {
			left, right := channelRank(request.Policy.AllowedChannels, memberPlan.Excluded[i].Channel), channelRank(request.Policy.AllowedChannels, memberPlan.Excluded[j].Channel)
			if left != right {
				return left < right
			}
			return memberPlan.Excluded[i].EndpointDigest < memberPlan.Excluded[j].EndpointDigest
		})
		plan.Plans = append(plan.Plans, memberPlan)
	}
	sort.Strings(plan.PreferenceVersions)
	plan.ResultDigest = deliveryDigest(plan)
	return plan, nil
}

func quietFor(preference Preference, at values.Instant) bool {
	for _, quiet := range preference.QuietHours {
		if quiet.contains(at) {
			return true
		}
	}
	return false
}

func deliveryDigest(plan DeliveryPlan) string {
	w := canonicalbytes.New("hcmnext.domains.audience.DeliveryPlan", version).
		String("purpose", plan.Purpose).String("audience_digest", plan.AudienceDigest).String("inputs_digest", plan.InputsDigest).Value("resolved_at", plan.ResolvedAt)
	for _, member := range plan.Plans {
		w.Value("principal", member.Principal).String("locale", member.Locale).String("preference_version", member.PreferenceVersion)
		for _, endpoint := range member.Endpoints {
			w.String("channel", string(endpoint.Channel)).String("endpoint_digest", endpoint.EndpointDigest).Bool("verified", endpoint.Verified).String("ownership", endpoint.Ownership).String("endpoint_locale", endpoint.Locale)
		}
		for _, exclusion := range member.Excluded {
			w.String("excluded_channel", string(exclusion.Channel)).String("excluded_digest", exclusion.EndpointDigest).String("excluded_reason", exclusion.Reason)
		}
	}
	for _, version := range plan.PreferenceVersions {
		w.String("preference_policy", version)
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// DeliveryExplanation is safe to expose in an audit trail.
type DeliveryExplanation struct {
	Purpose               string
	MemberCount           int
	EligibleEndpointCount int
	ExcludedEndpointCount int
	PreferenceVersions    []string
	ResultDigest          string
}

func (p DeliveryPlan) Explain() DeliveryExplanation {
	explanation := DeliveryExplanation{Purpose: p.Purpose, MemberCount: len(p.Plans), PreferenceVersions: append([]string(nil), p.PreferenceVersions...), ResultDigest: p.ResultDigest}
	for _, member := range p.Plans {
		explanation.EligibleEndpointCount += len(member.Endpoints)
		explanation.ExcludedEndpointCount += len(member.Excluded)
	}
	return explanation
}
