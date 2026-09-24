package subscription

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrInvalidScope = errors.New("subscription: invalid authorization scope")
	ErrScopeDenied  = errors.New("subscription: authorization scope denied")
)

const scopeRule = "subscription.scope.exact-declared-event-resource-field"

// ScopeGrant is the closed-world disclosure grant presented by the trust
// boundary. Empty lists mean that the grant authorizes nothing; '*' is never a
// wildcard. Exactly one subscriber identity must be bound to the grant.
type ScopeGrant struct {
	PrincipalRef string
	PartnerRef   string
	TenantScope  string
	Purpose      string
	EventKinds   []EventKind
	Resources    []string
	Fields       map[EventKind][]string
}

// AuthorizationDecision is the redaction-safe result of checking a grant.
// It records the exact rule and required scope without any event payload.
type AuthorizationDecision struct {
	SubscriptionID string
	Revision       uint64
	Subscriber     Subscriber
	Allowed        bool
	Rule           string
	Reason         string
	EventKinds     []EventKind
	Resources      []string
	Fields         map[EventKind][]string
	Digest         string
}

// AuthorizationEvent is the immutable evidence fact for an authorization
// check. Both allow and deny outcomes are digest-addressed so callers can
// retain the decision without storing payload data.
type AuthorizationEvent struct {
	SubscriptionID string
	Revision       uint64
	Allowed        bool
	Rule           string
	Reason         string
	GrantDigest    string
	DecisionDigest string
	Digest         string
}

// Validate verifies the immutable authorization evidence digests.
func (e AuthorizationEvent) Validate() error {
	if strings.TrimSpace(e.SubscriptionID) == "" || e.Revision == 0 || e.GrantDigest == "" ||
		e.DecisionDigest == "" || e.Digest == "" {
		return ErrInvalidScope
	}
	d := AuthorizationDecision{SubscriptionID: e.SubscriptionID, Revision: e.Revision,
		Allowed: e.Allowed, Rule: e.Rule, Reason: e.Reason, Digest: e.DecisionDigest}
	if decisionDigest(d, e.GrantDigest) != e.DecisionDigest || authorizationEventDigest(d, e.GrantDigest) != e.Digest {
		return ErrInvalidScope
	}
	return nil
}

// Validate checks a grant without consulting a principal, database, or
// provider. Scope is exact and deny-by-default.
func (g ScopeGrant) Validate() error {
	if (strings.TrimSpace(g.PrincipalRef) == "") == (strings.TrimSpace(g.PartnerRef) == "") {
		return fmt.Errorf("%w: exactly one principal or partner is required", ErrInvalidScope)
	}
	for name, value := range map[string]string{"tenant_scope": g.TenantScope, "purpose": g.Purpose} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidScope, name)
		}
	}
	if len(g.EventKinds) == 0 {
		return fmt.Errorf("%w: no event kinds are granted", ErrInvalidScope)
	}
	if err := validateScopeKinds(g.EventKinds); err != nil {
		return err
	}
	if err := validateScopeStrings(g.Resources, "resource"); err != nil {
		return err
	}
	for kind, fields := range g.Fields {
		if !kind.Valid() {
			return fmt.Errorf("%w: unknown field grant event kind %q", ErrInvalidScope, kind)
		}
		if err := validateScopeStrings(fields, "field"); err != nil {
			return err
		}
	}
	return nil
}

// GrantDigest returns the stable digest of the grant's exact scope.
func (g ScopeGrant) GrantDigest() string {
	w := canonicalbytes.New("hcmnext.domains.subscription.ScopeGrant", 1)
	w.String("principal_ref", g.PrincipalRef).String("partner_ref", g.PartnerRef).
		String("tenant_scope", g.TenantScope).String("purpose", g.Purpose)
	w.SortedStrings("event_kind", eventKindStrings(g.EventKinds))
	w.SortedStrings("resource", append([]string(nil), g.Resources...))
	for _, kind := range sortedKinds(g.Fields) {
		w.String("field_kind", string(kind)).SortedStrings("field", g.Fields[kind])
	}
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// RequiredResources returns the exact tenant, organization, population and
// filter names a subscription needs. Filter fields are resources too: a
// resource entitlement cannot be silently inferred from a field entitlement.
func RequiredResources(s EventSubscription) []string {
	resources := []string{s.TenantScope}
	if s.OrganizationScopeRef != "" {
		resources = append(resources, s.OrganizationScopeRef)
	}
	if s.PopulationScopeRef != "" {
		resources = append(resources, s.PopulationScopeRef)
	}
	for _, predicate := range s.Filter.Predicates {
		resources = append(resources, predicate.Field)
	}
	return uniqueSorted(resources)
}

// Authorize checks the grant against every event kind, resource, declared
// field and filter field in s. A denied result is returned as a digested event
// by callers using [AuthorizationDecision.Event].
func Authorize(s EventSubscription, g ScopeGrant) (AuthorizationDecision, error) {
	if err := s.Verify(); err != nil {
		return AuthorizationDecision{}, fmt.Errorf("%w: subscription: %v", ErrScopeDenied, err)
	}
	if err := g.Validate(); err != nil {
		return AuthorizationDecision{}, err
	}
	d := AuthorizationDecision{
		SubscriptionID: s.SubscriptionID, Revision: s.Revision, Subscriber: s.Subscriber,
		Rule: scopeRule, EventKinds: append([]EventKind(nil), s.EventKinds...),
		Resources: RequiredResources(s), Fields: requiredFields(s),
	}
	if g.TenantScope != s.TenantScope || g.Purpose == "" {
		return denyDecision(d, "tenant_or_purpose_scope_mismatch", g)
	}
	if !sameSubscriber(s.Subscriber, g) {
		return denyDecision(d, "subscriber_identity_mismatch", g)
	}
	for _, kind := range s.EventKinds {
		if !containsKind(g.EventKinds, kind) {
			return denyDecision(d, fmt.Sprintf("event_kind_not_granted:%s", kind), g)
		}
	}
	for _, resource := range d.Resources {
		if !containsString(g.Resources, resource) {
			return denyDecision(d, fmt.Sprintf("resource_not_granted:%s", resource), g)
		}
	}
	for kind, fields := range d.Fields {
		for _, field := range fields {
			if !containsString(g.Fields[kind], field) {
				return denyDecision(d, fmt.Sprintf("field_not_granted:%s.%s", kind, field), g)
			}
		}
	}
	d.Allowed = true
	d.Digest = decisionDigest(d, g.GrantDigest())
	return d, nil
}

func denyDecision(d AuthorizationDecision, reason string, g ScopeGrant) (AuthorizationDecision, error) {
	d.Reason = reason
	d.Digest = decisionDigest(d, g.GrantDigest())
	return d, fmt.Errorf("%w: %s", ErrScopeDenied, reason)
}

// Event converts a decision into a digest-addressed authorization event.
func (d AuthorizationDecision) Event(g ScopeGrant) AuthorizationEvent {
	grantDigest := g.GrantDigest()
	return AuthorizationEvent{
		SubscriptionID: d.SubscriptionID, Revision: d.Revision, Allowed: d.Allowed,
		Rule: d.Rule, Reason: d.Reason, GrantDigest: grantDigest,
		DecisionDigest: d.Digest, Digest: authorizationEventDigest(d, grantDigest),
	}
}

// Explain returns a deterministic explanation naming the authorization rule.
func (d AuthorizationDecision) Explain() string {
	return fmt.Sprintf("subscription authorization rule=%s allowed=%t reason=%s decision=%s", d.Rule, d.Allowed, d.Reason, d.Digest)
}

// ActivateWithAuthorization is the activation path for a subscription whose
// disclosure scope must be checked before ACTIVE is minted. The evidence
// event is returned even when authorization refuses activation.
func (s EventSubscription) ActivateWithAuthorization(g ScopeGrant, requester, approver string) (EventSubscription, AuthorizationEvent, error) {
	d, authErr := Authorize(s, g)
	event := d.Event(g)
	if authErr != nil {
		return EventSubscription{}, event, authErr
	}
	next, err := s.Activate(requester, approver)
	if err != nil {
		return EventSubscription{}, event, err
	}
	return next, event, nil
}

// MatchAuthorized rechecks the current grant for every active candidate
// before digest matching. A missing or changed grant refuses the whole match;
// it is never silently narrowed to the fields that remain authorized.
func MatchAuthorized(event EventDigest, revisions []EventSubscription, grants map[string]ScopeGrant) ([]SubscriptionMatch, []AuthorizationEvent, error) {
	if err := event.validate(); err != nil {
		return nil, nil, err
	}
	eligible := make([]EventSubscription, 0, len(revisions))
	events := make([]AuthorizationEvent, 0, len(revisions))
	for _, revision := range revisions {
		if revision.State != StateActive || revision.TenantScope != event.TenantScope || !containsKind(revision.EventKinds, event.Kind) {
			continue
		}
		grant, ok := grants[revision.SubscriptionID]
		if !ok {
			d := AuthorizationDecision{SubscriptionID: revision.SubscriptionID, Revision: revision.Revision, Subscriber: revision.Subscriber, Rule: scopeRule, Reason: "grant_missing"}
			events = append(events, d.Event(grant))
			return nil, events, fmt.Errorf("%w: grant missing for %s", ErrScopeDenied, revision.SubscriptionID)
		}
		d, err := Authorize(revision, grant)
		events = append(events, d.Event(grant))
		if err != nil {
			return nil, events, err
		}
		eligible = append(eligible, revision)
	}
	matches, err := MatchActive(event, eligible)
	return matches, events, err
}

func requiredFields(s EventSubscription) map[EventKind][]string {
	out := make(map[EventKind][]string, len(s.EventKinds))
	for _, kind := range s.EventKinds {
		out[kind] = uniqueSorted(append([]string(nil), s.DeclaredFields[kind]...))
	}
	return out
}

func decisionDigest(d AuthorizationDecision, grantDigest string) string {
	w := canonicalbytes.New("hcmnext.domains.subscription.AuthorizationDecision", 1).
		String("subscription_id", d.SubscriptionID).Int("revision", int64(d.Revision)).
		Bool("allowed", d.Allowed).String("rule", d.Rule).String("reason", d.Reason).
		String("grant_digest", grantDigest)
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

func authorizationEventDigest(d AuthorizationDecision, grantDigest string) string {
	w := canonicalbytes.New("hcmnext.domains.subscription.AuthorizationEvent", 1).
		String("subscription_id", d.SubscriptionID).Int("revision", int64(d.Revision)).
		Bool("allowed", d.Allowed).String("rule", d.Rule).String("reason", d.Reason).
		String("grant_digest", grantDigest).String("decision_digest", d.Digest)
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

func validateScopeKinds(kinds []EventKind) error {
	seen := make(map[EventKind]struct{}, len(kinds))
	for _, kind := range kinds {
		if !kind.Valid() || kind == EventKind("*") {
			return fmt.Errorf("%w: event kind %q is not an exact closed value", ErrInvalidScope, kind)
		}
		if _, ok := seen[kind]; ok {
			return fmt.Errorf("%w: duplicate event kind %q", ErrInvalidScope, kind)
		}
		seen[kind] = struct{}{}
	}
	return nil
}

func validateScopeStrings(values []string, label string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || value == "*" {
			return fmt.Errorf("%w: %s %q is not an exact value", ErrInvalidScope, label, value)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%w: duplicate %s %q", ErrInvalidScope, label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func sameSubscriber(s Subscriber, g ScopeGrant) bool {
	return s.PrincipalRef == g.PrincipalRef && s.PartnerRef == g.PartnerRef
}

func eventKindStrings(kinds []EventKind) []string {
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, string(kind))
	}
	return out
}

func sortedKinds(fields map[EventKind][]string) []EventKind {
	out := make([]EventKind, 0, len(fields))
	for kind := range fields {
		out = append(out, kind)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
