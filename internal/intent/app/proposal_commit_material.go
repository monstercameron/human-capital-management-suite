package app

// WF-RUN-034: the commit material a served promotion proposal carries.
//
// The PROMOUX-016 terminal resolver materializes the promotion's commit
// command from the approved proposal revision alone. It needs three facts the
// placement projection never recorded: the approved annualized base pay, and
// the manager the promoted assignment reports to (its worker identity and the
// relationship reference the assignment records). This file adds them as
// planned writes in the resolver's own field vocabulary
// (simcomp's rewards.compensation.annualized_base_pay, org's manager
// relationship fields), derived from the governed compensation simulation and
// the worker's durable manager, never from the form.
//
// The manager is pinned even when the promotion keeps it: approval must see
// the manager the commit will attest a cycle check for, and the simulation
// only records changed placement fields. A write whose proposed text equals
// its current text is refused by the kernel, so an unchanged manager is pinned
// as a current/proposed state assertion pair instead; the resolver accepts
// that pair as the approved, unchanged manager.
//
// promote_worker is ZERO_EFFECT in its scheduled release, so the revision may
// not declare the payroll and IAM sync effects; the resolver authorizes those
// outbox legs from the approved pay and placement writes instead.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Commit-material field paths, pinned to the resolver's vocabulary.
const (
	// CommitPayFieldPath is the approved annualized base pay write.
	CommitPayFieldPath = "rewards.compensation.annualized_base_pay"
	// CommitManagerIDFieldPath and CommitRelationshipFieldPath are the
	// pinned manager writes.
	CommitManagerIDFieldPath    = org.ManagerWorkerIDField
	CommitRelationshipFieldPath = org.ManagerRelationshipIDField
	// PlacementFieldPrefix prefixes every placement write proposalFor emits.
	PlacementFieldPrefix = "assignment."
)

// appendCommitMaterial adds the pay and manager writes to spec. The pay write
// is emitted only when the compensation simulation evaluated both sides; the
// manager writes only when the worker's durable manager is known.
func appendCommitMaterial(
	spec *intent.ProposalSpec, tenant values.TenantId, primary intent.SubjectReference, subject values.EntityRef,
	watermark values.RevisionToken, effective values.EffectiveInterval, result promotion.SimulationResult, managerWorkerID string,
) error {
	if result.CompensationState == promotion.CompensationEvaluated {
		current, err := moneyText(result.Compensation.Current.AnnualizedBase)
		if err != nil {
			return fmt.Errorf("app: current annualized base pay: %w", err)
		}
		proposed, err := moneyText(result.Compensation.Proposed.AnnualizedBase)
		if err != nil {
			return fmt.Errorf("app: proposed annualized base pay: %w", err)
		}
		key, err := values.NewResourceKey(tenant, values.Kind("compensation"), "worker", subject.Id)
		if err != nil {
			return fmt.Errorf("app: compensation resource key: %w", err)
		}
		spec.CurrentState = append(spec.CurrentState, intent.StateAssertion{Subject: primary, ResourceKey: key, FieldPath: CommitPayFieldPath, CanonicalText: current})
		spec.ProposedState = append(spec.ProposedState, intent.StateAssertion{Subject: primary, ResourceKey: key, FieldPath: CommitPayFieldPath, CanonicalText: proposed})
		spec.Writes = append(spec.Writes, plannedWrite(primary, key, CommitPayFieldPath, current, proposed, watermark, effective))
	}
	if manager := strings.TrimSpace(managerWorkerID); manager != "" {
		key, err := values.NewResourceKey(tenant, values.Kind("manager_relationship"), "worker", subject.Id)
		if err != nil {
			return fmt.Errorf("app: manager relationship resource key: %w", err)
		}
		// The served proposal keeps the worker's manager, and the kernel and
		// the candidate store refuse a write whose proposed text equals its
		// current text, so the pinned manager travels as a current and
		// proposed state assertion pair (material, digest-bound) rather than
		// as a write.
		for _, field := range []string{CommitManagerIDFieldPath, CommitRelationshipFieldPath} {
			spec.CurrentState = append(spec.CurrentState, intent.StateAssertion{Subject: primary, ResourceKey: key, FieldPath: field, CanonicalText: manager})
			spec.ProposedState = append(spec.ProposedState, intent.StateAssertion{Subject: primary, ResourceKey: key, FieldPath: field, CanonicalText: manager})
		}
	}
	return nil
}

// proposalReservationFor derives the budget hold a minted revision implies:
// the approved raise (proposed minus current annualized base pay) against the
// worker's organization unit's pool, from the revision's own material. A
// revision with no pay write or no organization unit implies no hold.
func proposalReservationFor(rev intent.ProposalRevision) (promotionbudget.ProposalReservation, bool, error) {
	var current, proposed, orgUnit string
	for _, w := range rev.Writes {
		if w.FieldPath == CommitPayFieldPath {
			current, proposed = w.CurrentCanonicalText, w.ProposedCanonicalText
		}
	}
	for _, s := range rev.ProposedState {
		if s.FieldPath == PlacementFieldPrefix+people.FieldOrgUnit.String() {
			orgUnit = s.CanonicalText
		}
	}
	if current == "" || proposed == "" || strings.TrimSpace(orgUnit) == "" {
		return promotionbudget.ProposalReservation{}, false, nil
	}
	proposalID, err := uuid.Parse(rev.ProposalRevisionID)
	if err != nil {
		return promotionbudget.ProposalReservation{}, false, nil
	}
	start, ok := effectiveStartOf(rev.EffectiveTime)
	if !ok {
		return promotionbudget.ProposalReservation{}, false, nil
	}
	cur, err := values.NewDecimal(current, 2, values.RoundingExactRequired)
	if err != nil {
		return promotionbudget.ProposalReservation{}, false, fmt.Errorf("app: current pay %q: %w", current, err)
	}
	next, err := values.NewDecimal(proposed, 2, values.RoundingExactRequired)
	if err != nil {
		return promotionbudget.ProposalReservation{}, false, fmt.Errorf("app: proposed pay %q: %w", proposed, err)
	}
	raise, err := next.Sub(cur)
	if err != nil {
		return promotionbudget.ProposalReservation{}, false, err
	}
	if raise.Sign() < 0 {
		zero, _ := values.NewDecimal("0.00", 2, values.RoundingExactRequired)
		raise = zero
	}
	return promotionbudget.ProposalReservation{
		ProposalRevisionID: proposalID, OrgUnit: orgUnit, Amount: raise.String(),
		ProducedAt: rev.CreatedAt.Time(), EffectiveStart: start,
	}, true, nil
}

// effectiveStartOf is the promotion's effective business day for either kind
// of approved interval: the same day-aligned coordinate the terminal resolver
// reads and writes the commit at.
func effectiveStartOf(interval values.EffectiveInterval) (time.Time, bool) {
	if date, ok := interval.StartDate(); ok {
		return time.Date(int(date.Year()), date.Month(), int(date.Day()), 0, 0, 0, 0, time.UTC), true
	}
	if instant, ok := interval.StartInstant(); ok {
		at := instant.Time().UTC()
		return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}

func plannedWrite(subject intent.SubjectReference, key values.ResourceKey, field, current, proposed string, watermark values.RevisionToken, effective values.EffectiveInterval) intent.PlannedWrite {
	return intent.PlannedWrite{
		Subject: subject, ResourceKey: key, FieldPath: field,
		CurrentCanonicalText: current, ProposedCanonicalText: proposed,
		SourceAuthorityDecision: "authority.local_master/v1", ExpectedRevision: watermark,
		Operation: intent.WriteOperationUpdate, EffectiveInterval: effective,
	}
}

// moneyText renders an amount at cents, refusing a fraction of a cent rather
// than rounding it into a different approved pay.
func moneyText(m values.Money) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	cents, err := m.Amount().Quantize(2, values.RoundingExactRequired)
	if err != nil {
		return "", err
	}
	return cents.String(), nil
}

// PinnedManagerFromRevisions answers [PinnedManagerReader] from the durable
// proposal revisions an intent recorded: the manager the latest revision
// pinned, or false when the intent has recorded none.
func PinnedManagerFromRevisions(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) PinnedManagerReader {
	return func(ctx context.Context, tenant values.TenantId, intentID string) (string, bool, error) {
		if db == nil || tenantUUID == nil {
			return "", false, nil
		}
		tenantID := tenantUUID(tenant)
		id, err := uuid.Parse(intentID)
		if err != nil || tenantID == uuid.Nil {
			return "", false, nil
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			return "", false, fmt.Errorf("app: read the pinned manager: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			return "", false, fmt.Errorf("app: read the pinned manager: %w", err)
		}
		var latest *int64
		if err := tx.QueryRow(ctx, `SELECT max(revision) FROM proposal_revision WHERE tenant_id = $1 AND intent_id = $2`,
			tenantID, id).Scan(&latest); err != nil {
			return "", false, fmt.Errorf("app: read the intent's proposal revisions: %w", err)
		}
		if latest == nil || *latest <= 0 {
			return "", false, nil
		}
		row, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenantID, id, uint64(*latest))
		if err != nil {
			return "", false, fmt.Errorf("app: load the intent's proposal revision: %w", err)
		}
		revision, err := intentcontrol.DecodeFullProposal(row.Payload, pinnedDigest{expected: row.MaterialDigest})
		if err != nil {
			return "", false, fmt.Errorf("app: decode the intent's proposal revision: %w", err)
		}
		for _, assertion := range revision.ProposedState {
			if assertion.FieldPath == CommitManagerIDFieldPath && strings.TrimSpace(assertion.CanonicalText) != "" {
				return assertion.CanonicalText, true, nil
			}
		}
		return "", false, nil
	}
}

// pinnedDigest verifies a decoded revision against the digest its own row
// records: the pinned manager is only ever read from the stored material.
type pinnedDigest struct{ expected string }

func (v pinnedDigest) VerifyProposalDigest(rev intent.ProposalRevision) error {
	if rev.MaterialDigest.Digest != v.expected {
		return fmt.Errorf("app: proposal digest %q does not match stored %q", rev.MaterialDigest.Digest, v.expected)
	}
	return nil
}

var _ intentcontrol.FullProposalVerifier = pinnedDigest{}
