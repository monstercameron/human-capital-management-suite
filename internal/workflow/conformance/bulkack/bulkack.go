// Package bulkack proves bounded bulk policy acknowledgement without
// false legal satisfaction (CONF-022): a versioned audience snapshot
// partitions within concurrency and cost limits, each child binds
// recipient, artifact, version, idempotency and verified signal, a
// REQUIRED_SET join accounts for acknowledged, unreachable, excluded
// and degraded results, mandatory gaps create human work, and terminal
// dimensions never overstate legal satisfaction. The parent retains
// typed aggregates and child references, never protected payloads.
package bulkack

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Recipient acknowledgement states: the closed per-recipient vocabulary.
const (
	RecipientAcknowledged = "ACKNOWLEDGED"
	RecipientUnreachable  = "UNREACHABLE"
	RecipientExcluded     = "EXCLUDED"
	RecipientDegraded     = "DEGRADED"
	RecipientPending      = "PENDING_MANDATORY"
)

// ServingContractID identifies the read-only bulk acknowledgement contract
// composed by the shipped application cell.
const ServingContractID = "hcmnext.conformance.bulk-policy-acknowledgement/v1"

// ValidateServingContract exercises a bounded mandatory acknowledgement
// through the production API. The missing signal must remain pending, which
// proves the served path cannot turn delivery into legal satisfaction.
func ValidateServingContract() error {
	snapshot, err := FreezeAudience("serving/v1", []Recipient{{
		ID: "serving-recipient", PayloadRef: "policy:serving", Locale: "en-US",
		Accessible: true, Mandatory: true, AuthorityScope: []string{"policy.read"},
	}})
	if err != nil {
		return fmt.Errorf("bulkack: serving contract audience: %w", err)
	}
	partitions, err := Partition(snapshot, []string{"policy.read"}, "artifact/serving", 1, 1, 1, 1)
	if err != nil {
		return fmt.Errorf("bulkack: serving contract partition: %w", err)
	}
	if len(partitions) != 1 || len(partitions[0]) != 1 {
		return fmt.Errorf("bulkack: serving contract partition shape is invalid")
	}
	child := partitions[0][0]
	aggregate, err := AggregateOutcomes(snapshot, []Child{child}, []ChildOutcome{{
		ChildID: child.ChildID, IdempotencyKey: child.IdempotencyKey, Reachable: true,
	}}, "REQUIRED_SET")
	if err != nil {
		return fmt.Errorf("bulkack: serving contract aggregate: %w", err)
	}
	if aggregate.LegalSatisfaction || aggregate.States[child.RecipientID] != RecipientPending {
		return fmt.Errorf("bulkack: serving contract overstated mandatory acknowledgement")
	}
	if err := aggregate.Verify(); err != nil {
		return fmt.Errorf("bulkack: serving contract seal: %w", err)
	}
	return nil
}

// Recipient is one audience member by reference: payloads never enter the
// acknowledgement plane.
type Recipient struct {
	ID             string
	PayloadRef     string
	Locale         string
	Accessible     bool
	Excluded       bool
	Mandatory      bool
	AuthorityScope []string
}

// AudienceSnapshot is the frozen versioned audience.
type AudienceSnapshot struct {
	Version    string
	Recipients []Recipient
	Digest     string
}

func audienceDigest(version string, recipients []Recipient) string {
	ordered := append([]Recipient(nil), recipients...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	parts := []string{"bulk-audience", version}
	for _, recipient := range ordered {
		scope := append([]string(nil), recipient.AuthorityScope...)
		sort.Strings(scope)
		parts = append(parts, strings.Join([]string{recipient.ID, recipient.PayloadRef, recipient.Locale, fmt.Sprint(recipient.Accessible, recipient.Excluded, recipient.Mandatory), strings.Join(scope, ",")}, "\x01"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// FreezeAudience freezes the audience. Audience changes after this point
// refuse: every child binds the frozen digest.
func FreezeAudience(version string, recipients []Recipient) (AudienceSnapshot, error) {
	if strings.TrimSpace(version) == "" {
		return AudienceSnapshot{}, fmt.Errorf("bulkack: audience version is required")
	}
	if len(recipients) == 0 {
		return AudienceSnapshot{}, fmt.Errorf("bulkack: audience is empty")
	}
	seen := make(map[string]bool, len(recipients))
	for _, recipient := range recipients {
		if strings.TrimSpace(recipient.ID) == "" || seen[recipient.ID] {
			return AudienceSnapshot{}, fmt.Errorf("bulkack: recipient identities must be unique and non-empty")
		}
		seen[recipient.ID] = true
		if strings.TrimSpace(recipient.PayloadRef) == "" || strings.TrimSpace(recipient.Locale) == "" {
			return AudienceSnapshot{}, fmt.Errorf("bulkack: recipient %s needs a payload reference and a locale", recipient.ID)
		}
		if len(recipient.AuthorityScope) == 0 {
			return AudienceSnapshot{}, fmt.Errorf("bulkack: recipient %s needs an authority scope", recipient.ID)
		}
	}
	ordered := append([]Recipient(nil), recipients...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	return AudienceSnapshot{Version: version, Recipients: ordered, Digest: audienceDigest(version, ordered)}, nil
}

// Child is one partitioned acknowledgement unit bound to the frozen
// audience. DeliveryProof and AckProof stay distinct: delivery never
// substitutes for acknowledgement.
type Child struct {
	ChildID         string
	RecipientID     string
	AudienceDigest  string
	ArtifactVersion string
	IdempotencyKey  string
	Scope           []string
	AckProof        string
	DeliveryProof   string
	VerifiedSignal  bool
}

func childDigest(audience, recipient string, index int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"bulkack-child", audience, recipient, fmt.Sprint(index)}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Partition splits the frozen audience into deterministic partitions
// within the concurrency and cost limits.
func Partition(snapshot AudienceSnapshot, parentScope []string, artifactVersion string, maxPerPartition, maxPartitions, maxCost, costPerChild int) ([][]Child, error) {
	if snapshot.Digest == "" {
		return nil, fmt.Errorf("bulkack: partitioning needs a frozen audience")
	}
	if maxPerPartition <= 0 || maxPartitions <= 0 || maxCost <= 0 || costPerChild <= 0 {
		return nil, fmt.Errorf("bulkack: partition limits must be positive")
	}
	if strings.TrimSpace(artifactVersion) == "" {
		return nil, fmt.Errorf("bulkack: artifact version is required")
	}
	allowed := make(map[string]bool, len(parentScope))
	for _, scope := range parentScope {
		allowed[scope] = true
	}
	var partitions [][]Child
	for index, recipient := range snapshot.Recipients {
		for _, scope := range recipient.AuthorityScope {
			if !allowed[scope] {
				return nil, fmt.Errorf("bulkack: child for %s broadens authority", recipient.ID)
			}
		}
		partition := index / maxPerPartition
		if partition >= maxPartitions {
			return nil, fmt.Errorf("bulkack: audience exceeds the partition limit")
		}
		for len(partitions) <= partition {
			partitions = append(partitions, nil)
		}
		partitions[partition] = append(partitions[partition], Child{
			ChildID: childDigest(snapshot.Digest, recipient.ID, index), RecipientID: recipient.ID,
			AudienceDigest: snapshot.Digest, ArtifactVersion: artifactVersion,
			IdempotencyKey: "ack:" + snapshot.Version + ":" + recipient.ID,
			Scope:          append([]string(nil), recipient.AuthorityScope...),
		})
	}
	total := len(snapshot.Recipients)
	if total*costPerChild > maxCost {
		return nil, fmt.Errorf("bulkack: acknowledgement cost exceeds its limit")
	}
	return partitions, nil
}

// ChildOutcome is one recorded child signal.
type ChildOutcome struct {
	ChildID        string
	IdempotencyKey string
	Acknowledged   bool
	Reachable      bool
	VerifiedSignal bool
	Cancelled      bool
}

// Aggregate is the terminal dimension set: per-recipient states, the join
// verdict, human-work items for mandatory gaps and the legal-satisfaction
// flag that never overstates.
type Aggregate struct {
	AudienceDigest    string
	States            map[string]string
	JoinVerdict       string
	HumanWork         []string
	LegalSatisfaction bool
	ChildRefs         []string
	Digest            string
}

func aggregateDigest(audience string, states map[string]string, verdict string, humanWork []string, satisfied bool, refs []string) string {
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := []string{"bulkack-aggregate", audience, verdict, fmt.Sprint(satisfied)}
	for _, id := range ids {
		parts = append(parts, id+"="+states[id])
	}
	work := append([]string(nil), humanWork...)
	sort.Strings(work)
	parts = append(parts, work...)
	sorted := append([]string(nil), refs...)
	sort.Strings(sorted)
	parts = append(parts, sorted...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// AggregateOutcomes folds child outcomes into terminal dimensions.
// Partition retries deduplicate by idempotency key; JOIN timeouts and
// missing mandatory acknowledgements stay pending with human work, never
// success; cancelled children keep their state; inaccessible content
// degrades instead of satisfying.
func AggregateOutcomes(snapshot AudienceSnapshot, children []Child, outcomes []ChildOutcome, joinVerdict string) (Aggregate, error) {
	if snapshot.Digest == "" {
		return Aggregate{}, fmt.Errorf("bulkack: aggregation needs a frozen audience")
	}
	byRecipient := make(map[string]Recipient, len(snapshot.Recipients))
	for _, recipient := range snapshot.Recipients {
		byRecipient[recipient.ID] = recipient
	}
	aggregate := Aggregate{AudienceDigest: snapshot.Digest, States: make(map[string]string, len(snapshot.Recipients)), JoinVerdict: joinVerdict}
	seenKey := make(map[string]bool, len(outcomes))
	byChild := make(map[string]ChildOutcome, len(outcomes))
	for _, outcome := range outcomes {
		if seenKey[outcome.IdempotencyKey] {
			continue
		}
		seenKey[outcome.IdempotencyKey] = true
		byChild[outcome.ChildID] = outcome
	}
	for _, child := range children {
		if child.AudienceDigest != snapshot.Digest {
			return Aggregate{}, fmt.Errorf("bulkack: child for %s binds a drifted audience", child.RecipientID)
		}
		recipient := byRecipient[child.RecipientID]
		outcome, recorded := byChild[child.ChildID]
		aggregate.ChildRefs = append(aggregate.ChildRefs, child.ChildID)
		switch {
		case recipient.Excluded:
			aggregate.States[child.RecipientID] = RecipientExcluded
		case !recorded || (!outcome.Acknowledged && outcome.Reachable && !outcome.Cancelled):
			// Missing or timed-out mandatory signals stay pending.
			aggregate.States[child.RecipientID] = RecipientPending
			if recipient.Mandatory {
				aggregate.HumanWork = append(aggregate.HumanWork, "ack:"+child.RecipientID)
			}
		case !outcome.Reachable:
			aggregate.States[child.RecipientID] = RecipientUnreachable
			if recipient.Mandatory {
				aggregate.HumanWork = append(aggregate.HumanWork, "reach:"+child.RecipientID)
			}
		case outcome.Cancelled:
			aggregate.States[child.RecipientID] = RecipientPending
			if recipient.Mandatory {
				aggregate.HumanWork = append(aggregate.HumanWork, "requeue:"+child.RecipientID)
			}
		case !recipient.Accessible || !outcome.VerifiedSignal:
			aggregate.States[child.RecipientID] = RecipientDegraded
		case outcome.Acknowledged && child.AckProof != "" && child.AckProof != child.DeliveryProof:
			aggregate.States[child.RecipientID] = RecipientAcknowledged
		default:
			// Delivery without a distinct acknowledgement never satisfies.
			aggregate.States[child.RecipientID] = RecipientPending
			if recipient.Mandatory {
				aggregate.HumanWork = append(aggregate.HumanWork, "ack:"+child.RecipientID)
			}
		}
	}
	sort.Strings(aggregate.HumanWork)
	sort.Strings(aggregate.ChildRefs)
	satisfied := true
	for id, state := range aggregate.States {
		if byRecipient[id].Mandatory && state != RecipientAcknowledged {
			satisfied = false
		}
	}
	aggregate.LegalSatisfaction = satisfied && len(aggregate.States) == len(snapshot.Recipients)
	aggregate.Digest = aggregateDigest(snapshot.Digest, aggregate.States, joinVerdict, aggregate.HumanWork, aggregate.LegalSatisfaction, aggregate.ChildRefs)
	return aggregate, nil
}

// Verify recomputes the aggregate seal.
func (aggregate Aggregate) Verify() error {
	if aggregate.Digest == "" || aggregateDigest(aggregate.AudienceDigest, aggregate.States, aggregate.JoinVerdict, aggregate.HumanWork, aggregate.LegalSatisfaction, aggregate.ChildRefs) != aggregate.Digest {
		return fmt.Errorf("bulkack: aggregate seal is broken")
	}
	return nil
}

// AudienceRegistry guards snapshots for concurrent acknowledgement.
type AudienceRegistry struct {
	mu        sync.Mutex
	snapshots map[string]AudienceSnapshot
}

// NewAudienceRegistry starts an empty registry.
func NewAudienceRegistry() *AudienceRegistry {
	return &AudienceRegistry{snapshots: make(map[string]AudienceSnapshot)}
}

// Record freezes one audience version. Re-recording is idempotent.
func (registry *AudienceRegistry) Record(version string, recipients []Recipient) (AudienceSnapshot, error) {
	snapshot, err := FreezeAudience(version, recipients)
	if err != nil {
		return AudienceSnapshot{}, err
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if prior, ok := registry.snapshots[version]; ok {
		if prior.Digest != snapshot.Digest {
			return AudienceSnapshot{}, fmt.Errorf("bulkack: audience %s changed after freeze", version)
		}
		return prior, nil
	}
	registry.snapshots[version] = snapshot
	return snapshot, nil
}
