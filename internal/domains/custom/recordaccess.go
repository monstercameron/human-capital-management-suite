package custom

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrInvalidRecordRule = errors.New("custom: invalid record rule")
	ErrInvalidAccessDeps = errors.New("custom: invalid record access dependencies")
)

// Record-rule effects and relationships: the closed vocabularies.
const (
	EffectAllow = "ALLOW"
	EffectDeny  = "DENY"

	// RelationshipManagerOf matches when the principal manages the record's
	// owner, resolved through the relationship closure.
	RelationshipManagerOf = "MANAGER_OF"
)

// RecordRule is one declared record-level access rule. The dimensions
// combine with AND: OwnerField names the record field holding the owner,
// ScopeField names the record field holding the scope, and Relationship
// resolves the principal against the owner through the closure. ScopeValues,
// when set, pins the rule to listed scope values instead of the principal's
// scopes (used by deny rules such as containment).
type RecordRule struct {
	ID           string
	Effect       string
	OwnerField   string
	ScopeField   string
	ScopeValues  []string
	Relationship string
}

// Principal is the authenticated caller record rules evaluate against.
type Principal struct {
	ID     string
	Scopes []string
}

// Closure resolves relationships between a principal and a record owner. A
// production implementation reads the org closure tables; the answer is a
// boolean, never a chain, so search filtering costs one lookup per distinct
// owner per request at most.
type Closure interface {
	Related(ctx context.Context, tenant, relationship, principal, owner string) (bool, error)
}

// PlatformPolicy is the platform's own read verdict. Record rules combine
// with it under security accumulation: a platform deny removes access no
// rule can re-grant.
type PlatformPolicy interface {
	MayRead(ctx context.Context, principal Principal, record CustomRecordRevision) (bool, error)
}

// PlatformPolicyFunc adapts a function to the PlatformPolicy port.
type PlatformPolicyFunc func(ctx context.Context, principal Principal, record CustomRecordRevision) (bool, error)

// MayRead implements PlatformPolicy.
func (f PlatformPolicyFunc) MayRead(ctx context.Context, principal Principal, record CustomRecordRevision) (bool, error) {
	return f(ctx, principal, record)
}

// AllowPlatformPolicy allows every read: the tests' platform baseline.
func AllowPlatformPolicy() PlatformPolicy {
	return PlatformPolicyFunc(func(context.Context, Principal, CustomRecordRevision) (bool, error) { return true, nil })
}

// DenyPrincipalPolicy denies one principal: the tests' accumulation probe.
func DenyPrincipalPolicy(id string) PlatformPolicy {
	return PlatformPolicyFunc(func(_ context.Context, principal Principal, _ CustomRecordRevision) (bool, error) {
		return principal.ID != id, nil
	})
}

// AccessDeps wires record evaluation to the relationship closure and the
// platform policy.
type AccessDeps struct {
	Closure  Closure
	Platform PlatformPolicy
}

// MemoryClosure is an in-memory Closure fixture: ManagedBy maps a manager to
// the owners they manage. Calls counts Related answers served (cache misses
// at the evaluator are what the benchmark pins, so this counts evaluator
// calls, not map hits).
type MemoryClosure struct {
	mu        sync.Mutex
	ManagedBy map[string][]string
	calls     int
}

// NewMemoryClosure returns an empty closure.
func NewMemoryClosure() *MemoryClosure { return &MemoryClosure{ManagedBy: map[string][]string{}} }

// Related implements Closure. Unknown relationships answer false: a
// relationship the closure does not know never grants.
func (c *MemoryClosure) Related(_ context.Context, _, relationship, principal, owner string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if relationship != RelationshipManagerOf {
		return false, nil
	}
	for _, managed := range c.ManagedBy[principal] {
		if managed == owner {
			return true, nil
		}
	}
	return false, nil
}

// Calls returns the number of Related answers served.
func (c *MemoryClosure) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// Decision is one record verdict with its human-readable reason.
type Decision struct {
	Allowed bool
	Reason  string
}

// Evaluate decides one record: platform deny removes access first, then any
// matching deny rule, then any matching allow rule; otherwise the default is
// deny. A type without record rules defers to the platform alone, so
// existing types keep working unchanged.
func Evaluate(ctx context.Context, def CustomObjectDefinition, principal Principal, record CustomRecordRevision, deps *AccessDeps) (Decision, error) {
	if deps == nil || deps.Platform == nil {
		return Decision{}, fmt.Errorf("%w: platform policy is required", ErrInvalidAccessDeps)
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	allowed, err := deps.Platform.MayRead(ctx, principal, record)
	if err != nil {
		return Decision{}, err
	}
	if !allowed {
		return Decision{Reason: "platform policy denies"}, nil
	}
	evaluator := &evaluator{closure: deps.Closure, cache: map[string]bool{}}
	for _, rule := range def.RecordRules {
		if rule.Effect != EffectDeny {
			continue
		}
		match, err := evaluator.match(ctx, rule, principal, record)
		if err != nil {
			return Decision{}, err
		}
		if match {
			return Decision{Reason: "deny rule " + rule.ID}, nil
		}
	}
	for _, rule := range def.RecordRules {
		if rule.Effect != EffectAllow {
			continue
		}
		match, err := evaluator.match(ctx, rule, principal, record)
		if err != nil {
			return Decision{}, err
		}
		if match {
			return Decision{Allowed: true, Reason: "allow rule " + rule.ID}, nil
		}
	}
	if len(def.RecordRules) == 0 {
		return Decision{Allowed: true, Reason: "no record rules: platform allows"}, nil
	}
	return Decision{Reason: "no allow rule matches"}, nil
}

// Filter is the shared enforcement every consumer - query, rollup, export -
// must call: it returns only the records the principal may read, in input
// order. Closure answers are cached per call, so closure work grows with
// distinct owners, never with records.
func Filter(ctx context.Context, def CustomObjectDefinition, principal Principal, records []CustomRecordRevision, deps *AccessDeps) ([]CustomRecordRevision, error) {
	if deps == nil || deps.Platform == nil {
		return nil, fmt.Errorf("%w: platform policy is required", ErrInvalidAccessDeps)
	}
	evaluator := &evaluator{closure: deps.Closure, cache: map[string]bool{}}
	kept := make([]CustomRecordRevision, 0, len(records))
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		allowed, err := deps.Platform.MayRead(ctx, principal, record)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		denied := false
		for _, rule := range def.RecordRules {
			if rule.Effect != EffectDeny {
				continue
			}
			match, err := evaluator.match(ctx, rule, principal, record)
			if err != nil {
				return nil, err
			}
			if match {
				denied = true
				break
			}
		}
		if denied {
			continue
		}
		if len(def.RecordRules) == 0 {
			kept = append(kept, record)
			continue
		}
		for _, rule := range def.RecordRules {
			if rule.Effect != EffectAllow {
				continue
			}
			match, err := evaluator.match(ctx, rule, principal, record)
			if err != nil {
				return nil, err
			}
			if match {
				kept = append(kept, record)
				break
			}
		}
	}
	return kept, nil
}

// evaluator answers rule matches with a per-call closure cache.
type evaluator struct {
	closure Closure
	cache   map[string]bool
}

func (e *evaluator) match(ctx context.Context, rule RecordRule, principal Principal, record CustomRecordRevision) (bool, error) {
	if rule.OwnerField != "" && rule.Relationship == "" {
		owner, ok := stringField(record, rule.OwnerField)
		if !ok || owner != principal.ID {
			return false, nil
		}
	}
	if rule.Relationship != "" {
		if e.closure == nil {
			return false, fmt.Errorf("%w: rule %s needs a relationship closure", ErrInvalidAccessDeps, rule.ID)
		}
		owner, ok := stringField(record, rule.OwnerField)
		if !ok {
			return false, nil
		}
		key := strings.Join([]string{rule.Relationship, principal.ID, owner}, "\x00")
		related, ok := e.cache[key]
		if !ok {
			answer, err := e.closure.Related(ctx, "", rule.Relationship, principal.ID, owner)
			if err != nil {
				return false, err
			}
			related = answer
			e.cache[key] = answer
		}
		if !related {
			return false, nil
		}
	}
	if rule.ScopeField != "" {
		scope, ok := stringField(record, rule.ScopeField)
		if !ok {
			return false, nil
		}
		if len(rule.ScopeValues) > 0 {
			matched := false
			for _, pinned := range rule.ScopeValues {
				if scope == pinned {
					matched = true
					break
				}
			}
			if !matched {
				return false, nil
			}
		} else {
			matched := false
			for _, allowed := range principal.Scopes {
				if scope == allowed {
					matched = true
					break
				}
			}
			if !matched {
				return false, nil
			}
		}
	}
	return true, nil
}

func stringField(record CustomRecordRevision, name string) (string, bool) {
	value, ok := record.FieldValues[name]
	if !ok {
		return "", false
	}
	text, ok := value.Value.(string)
	if !ok {
		return "", false
	}
	return text, true
}

// validateRecordRules checks the declared rules: unique ids, a known effect,
// at least one dimension, relationship rules naming an owner field, and
// every named field declared on the definition.
func validateRecordRules(def CustomObjectDefinition) error {
	seen := map[string]bool{}
	for _, rule := range def.RecordRules {
		if strings.TrimSpace(rule.ID) == "" {
			return fmt.Errorf("%w: record rule id required", ErrInvalidRecordRule)
		}
		if seen[rule.ID] {
			return fmt.Errorf("%w: record rule %q twice", ErrInvalidRecordRule, rule.ID)
		}
		seen[rule.ID] = true
		if rule.Effect != EffectAllow && rule.Effect != EffectDeny {
			return fmt.Errorf("%w: rule %q has effect %q", ErrInvalidRecordRule, rule.ID, rule.Effect)
		}
		if rule.OwnerField == "" && rule.ScopeField == "" && rule.Relationship == "" {
			return fmt.Errorf("%w: rule %q names no dimension", ErrInvalidRecordRule, rule.ID)
		}
		if rule.Relationship != "" && rule.OwnerField == "" {
			return fmt.Errorf("%w: rule %q resolves a relationship without an owner field", ErrInvalidRecordRule, rule.ID)
		}
		for _, field := range []string{rule.OwnerField, rule.ScopeField} {
			if field == "" {
				continue
			}
			if _, ok := def.Fields[field]; !ok {
				return fmt.Errorf("%w: rule %q names undeclared field %q", ErrInvalidRecordRule, rule.ID, field)
			}
		}
	}
	return nil
}

// canonicalRecordRules renders the declared rules deterministically for the
// definition digest. Rule-less definitions render nothing, so their digests
// never change.
func canonicalRecordRules(rules []RecordRule) []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		pinned := append([]string(nil), rule.ScopeValues...)
		sort.Strings(pinned)
		out = append(out, strings.Join([]string{rule.ID, rule.Effect, rule.OwnerField, rule.ScopeField, rule.Relationship, strings.Join(pinned, ",")}, "\x01"))
	}
	sort.Strings(out)
	return out
}
