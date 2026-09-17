// GARN-002: determine garnishment priority and concurrent composition.
//
// ComposePriority sequences the ACTIVE orders competing for one worker
// against one pinned priority rule pack. Orders in earlier precedence
// bands run first; orders sharing a band withhold concurrently in one
// run group. Ambiguous jurisdiction, order type or priority returns
// review instead of a sequence. The package is kernel-pure.
package garnishment

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const prioritySchemaVersion = 1

// PriorityVersion is the rejection version carried by CompositionReview.
const PriorityVersion = "garnishment-priority/v1"

var (
	// ErrCompositionReview is the GARN-002 sentinel. Ambiguous
	// jurisdiction, order type or priority must fail with this error
	// carrying the offending field, state and version.
	ErrCompositionReview = errors.New("GARN_002_REVIEW_REQUIRED")
)

// CompositionReview is the stable GARN-002 failure shape.
type CompositionReview struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *CompositionReview) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrCompositionReview, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the GARN_002_REVIEW_REQUIRED sentinel to errors.Is.
func (r *CompositionReview) Unwrap() error { return ErrCompositionReview }

func compositionReview(field, state, reason string) error {
	return &CompositionReview{Field: field, State: state, Version: PriorityVersion, Reason: reason}
}

// PriorityRulePack pins the legal precedence of wage-attachment order
// types. The first type outranks every later one; its digest binds the
// exact pack a composition was computed against.
type PriorityRulePack struct {
	Ref             string
	Version         string
	Precedence      []OrderType
	CanonicalDigest string
}

// NewPriorityRulePack seals one precedence list under a canonical digest.
func NewPriorityRulePack(ref, version string, precedence []OrderType) (PriorityRulePack, error) {
	pack := PriorityRulePack{Ref: ref, Version: version, Precedence: append([]OrderType(nil), precedence...)}
	if err := pack.validateShape(); err != nil {
		return PriorityRulePack{}, err
	}
	pack.CanonicalDigest = pack.computedDigest()
	return pack, nil
}

func (p PriorityRulePack) validateShape() error {
	if strings.TrimSpace(p.Ref) == "" || strings.TrimSpace(p.Version) == "" {
		return compositionReview("rulepack.identity", "MISSING", "rule pack ref and version are required")
	}
	if len(p.Precedence) == 0 {
		return compositionReview("rulepack.precedence", "MISSING", "at least one order type is required")
	}
	seen := make(map[OrderType]bool, len(p.Precedence))
	for _, kind := range p.Precedence {
		if !kind.Valid() {
			return compositionReview("rulepack.precedence", "UNDECLARED", fmt.Sprintf("order type %q is not declared", kind))
		}
		if seen[kind] {
			return compositionReview("rulepack.precedence", "DUPLICATE", fmt.Sprintf("order type %q is listed twice", kind))
		}
		seen[kind] = true
	}
	return nil
}

func (p PriorityRulePack) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.garnishment.PriorityRulePack", prioritySchemaVersion).
		String("ref", p.Ref).String("version", p.Version).Count("precedence", len(p.Precedence))
	for _, kind := range p.Precedence {
		w.String("type", string(kind))
	}
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Validate checks the pack shape and its seal.
func (p PriorityRulePack) Validate() error {
	if err := p.validateShape(); err != nil {
		return err
	}
	if p.CanonicalDigest == "" || p.CanonicalDigest != p.computedDigest() {
		return compositionReview("rulepack.pack", "UNSEALED", "rule pack digest mismatch")
	}
	return nil
}

// Band reports the precedence band of one order type: lower wins.
func (p PriorityRulePack) Band(kind OrderType) (int, bool) {
	for i, candidate := range p.Precedence {
		if candidate == kind {
			return i, true
		}
	}
	return 0, false
}

// PlacedOrder is one order's position in the composition. Rank is the
// global sequence; orders sharing a RunGroup withhold concurrently while
// groups run in sequence.
type PlacedOrder struct {
	OrderID   string
	OrderType OrderType
	Rank      int
	RunGroup  int
}

// PriorityComposition is the sealed sequence of one worker's competing
// orders under one exact rule pack.
type PriorityComposition struct {
	TenantID        string
	PersonRef       string
	RulePackRef     string
	RulePackVersion string
	RulePackDigest  string
	Sequence        []PlacedOrder
	CanonicalDigest string
}

// ComposePriority determines the sequence and concurrency of the ACTIVE
// orders competing for one worker. It writes nothing and preserves
// caller-owned slices.
func ComposePriority(tenant, person string, pack PriorityRulePack, orders []AttachmentOrder) (PriorityComposition, error) {
	if err := pack.Validate(); err != nil {
		return PriorityComposition{}, err
	}
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(person) == "" {
		return PriorityComposition{}, compositionReview("composition.scope", "MISSING", "tenant and worker are required")
	}
	seen := make(map[int]string, len(orders))
	for _, order := range orders {
		if order.Status != OrderActive {
			return PriorityComposition{}, compositionReview("order."+order.OrderID+".status", "NOT_ACTIVE", "only active orders compose")
		}
		if order.TenantID != tenant {
			return PriorityComposition{}, compositionReview("order."+order.OrderID+".tenant", "MISMATCH", "composition never crosses tenant")
		}
		if order.PersonRef != person {
			return PriorityComposition{}, compositionReview("order."+order.OrderID+".person", "MISMATCH", "composition is bound to one worker")
		}
		if !order.OrderType.Valid() {
			return PriorityComposition{}, compositionReview("order."+order.OrderID+".type", "UNDECLARED", "order type is not declared")
		}
		if _, ok := pack.Band(order.OrderType); !ok {
			return PriorityComposition{}, compositionReview("order."+order.OrderID+".type", "UNCOVERED", "rule pack assigns no precedence band")
		}
		if strings.TrimSpace(order.Jurisdiction) == "" {
			return PriorityComposition{}, compositionReview("order."+order.OrderID+".jurisdiction", "MISSING", "jurisdiction is required to sequence")
		}
		if prior, dup := seen[order.Priority]; dup {
			return PriorityComposition{}, compositionReview("order.priority", "AMBIGUOUS", fmt.Sprintf("orders %q and %q claim priority %d", prior, order.OrderID, order.Priority))
		}
		seen[order.Priority] = order.OrderID
	}
	ranked := append([]AttachmentOrder(nil), orders...)
	sort.Slice(ranked, func(i, j int) bool {
		bandI, _ := pack.Band(ranked[i].OrderType)
		bandJ, _ := pack.Band(ranked[j].OrderType)
		if bandI != bandJ {
			return bandI < bandJ
		}
		if ranked[i].Priority != ranked[j].Priority {
			return ranked[i].Priority < ranked[j].Priority
		}
		if !ranked[i].EffectiveFrom.Equal(ranked[j].EffectiveFrom) {
			return ranked[i].EffectiveFrom.Before(ranked[j].EffectiveFrom)
		}
		return ranked[i].OrderID < ranked[j].OrderID
	})
	composition := PriorityComposition{
		TenantID: tenant, PersonRef: person,
		RulePackRef: pack.Ref, RulePackVersion: pack.Version, RulePackDigest: pack.CanonicalDigest,
	}
	group := 0
	previous := -1
	for i, order := range ranked {
		band, _ := pack.Band(order.OrderType)
		if i == 0 || band != previous {
			group++
			previous = band
		}
		composition.Sequence = append(composition.Sequence, PlacedOrder{
			OrderID: order.OrderID, OrderType: order.OrderType, Rank: i + 1, RunGroup: group,
		})
	}
	composition.CanonicalDigest = composition.computedDigest()
	return composition, nil
}

func (c PriorityComposition) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.garnishment.PriorityComposition", prioritySchemaVersion).
		String("tenant", c.TenantID).String("person", c.PersonRef).
		String("rulepack_ref", c.RulePackRef).String("rulepack_version", c.RulePackVersion).
		String("rulepack_digest", c.RulePackDigest).Count("sequence", len(c.Sequence))
	for _, placed := range c.Sequence {
		w.String("order", placed.OrderID).String("type", string(placed.OrderType)).
			Int("rank", int64(placed.Rank)).Int("run_group", int64(placed.RunGroup))
	}
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Verify checks the seal and internal consistency of a previously
// computed composition: ranks run 1..n in order, run groups ascend by at
// most one per step, and the digest matches.
func (c PriorityComposition) Verify() error {
	if strings.TrimSpace(c.TenantID) == "" || strings.TrimSpace(c.PersonRef) == "" {
		return compositionReview("composition.scope", "MISSING", "tenant and worker are required")
	}
	if strings.TrimSpace(c.RulePackDigest) == "" || c.CanonicalDigest == "" {
		return compositionReview("composition.seal", "UNSEALED", "rule pack binding and digest are required")
	}
	for i, placed := range c.Sequence {
		if strings.TrimSpace(placed.OrderID) == "" || !placed.OrderType.Valid() {
			return compositionReview("composition.sequence", "INVALID", "every placement names a declared order")
		}
		if placed.Rank != i+1 {
			return compositionReview("composition.sequence", "MISMATCH", "ranks must run 1..n in order")
		}
		if i == 0 {
			if placed.RunGroup != 1 {
				return compositionReview("composition.sequence", "MISMATCH", "the first run group is 1")
			}
			continue
		}
		previous := c.Sequence[i-1].RunGroup
		if placed.RunGroup != previous && placed.RunGroup != previous+1 {
			return compositionReview("composition.sequence", "MISMATCH", "run groups ascend by at most one")
		}
	}
	if c.computedDigest() != c.CanonicalDigest {
		return compositionReview("composition.seal", "MISMATCH", "canonical digest mismatch")
	}
	return nil
}
