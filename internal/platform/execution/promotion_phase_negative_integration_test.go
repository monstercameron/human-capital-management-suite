package execution

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestPromotionPhaseNegativeProvenanceMatrix proves the served RULE-004
// lookup does not turn an invalid successor, late writer, legacy row, or
// provenance-reader failure into permission to continue. The final case
// drives the same facts through CurrencyGuard.Check, which must return the
// storage error rather than silently falling back to the baseline.
func TestPromotionPhaseNegativeProvenanceMatrix(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, *promotionPhaseFixture)
		useGuard  bool
		want      string
		wantErr   error
		wantPhase string
	}{
		{
			name: "postcommit third-grade successor fails closed",
			prepare: func(t *testing.T, f *promotionPhaseFixture) {
				appendThirdGradeSuccessor(t, f)
				recordPromotionSuccess(t, f)
			},
			want: "no exact provenance", wantPhase: "POSTCOMMIT",
		},
		{
			name: "writer successor after checked-at is unknown",
			prepare: func(t *testing.T, f *promotionPhaseFixture) {
				commitPromotionSuccessorAt(t, f, f.checkedAt.Add(time.Minute))
				recordPromotionSuccess(t, f)
			},
			want: "not known at checked_at", wantPhase: "POSTCOMMIT",
		},
		{
			name: "legacy provenance row is not exact proof",
			prepare: func(t *testing.T, f *promotionPhaseFixture) {
				appendLegacyPromotionProof(t, f)
				recordPromotionSuccess(t, f)
			},
			want: "no exact provenance", wantPhase: "POSTCOMMIT",
		},
		{
			name: "malformed modern proof does not bind successor",
			prepare: func(t *testing.T, f *promotionPhaseFixture) {
				appendUnprovenSuccessor(t, f)
				appendMalformedModernPromotionProof(t, f)
				recordPromotionSuccess(t, f)
			},
			want: "does not bind the live successor", wantPhase: "POSTCOMMIT",
		},
		{
			name:     "proof reader SQL failure does not allow precommit baseline",
			prepare:  func(t *testing.T, f *promotionPhaseFixture) {},
			useGuard: true, want: "assignment proof read", wantPhase: "PRECOMMIT",
			wantErr: errInjectedPromotionProofRead,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newPromotionPhaseFixture(t)
			tc.prepare(t, f)
			facts := negativePromotionFacts(t, f)
			ctx := context.Background()

			var gotErr error
			var got execute.RuleApproval
			if tc.useGuard {
				injected := &promotionProofFailingExecutor{Tx: phaseTransaction(t, f), err: errInjectedPromotionProofRead}
				defer injected.Rollback(ctx)
				guard := execute.CurrencyGuard{
					Proposal: runtime.MemoryProposalFacts{},
					Approval: runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
						f.revision.ProposalRevisionID: {{DecisionID: "decision:negative-proof", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: f.revision.MaterialDigest.Digest}},
					}},
					Rules: facts,
				}
				var verdict execute.CurrencyVerdict
				verdict, gotErr = guard.Check(ctx, injected, execute.CurrencyCheckRequest{
					TenantID: f.tenant, InstanceID: f.instance, Proposal: runtime.ProposalBinding{Revision: f.revision}, CheckedAt: f.checkedAt,
				})
				if verdict.Blocked {
					t.Fatalf("guard verdict = %+v, want reader error before a fail-closed verdict", verdict)
				}
			} else {
				gotErr = withPromotionPhaseTx(t, f, func(tx dbport.Tx) error {
					var lookupErr error
					got, lookupErr = facts.Lookup(ctx, tx, f.tenant, f.revision, f.checkedAt)
					return lookupErr
				})
			}

			if gotErr == nil || !strings.Contains(gotErr.Error(), tc.want) {
				t.Fatalf("phase %s error = %v, want %q", tc.wantPhase, gotErr, tc.want)
			}
			if tc.wantErr != nil && !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("error = %v, want injected reader failure %v", gotErr, tc.wantErr)
			}
			if errors.Is(gotErr, promotioncommit.ErrAssignmentWriteEvidenceMissing) && tc.wantErr != nil {
				t.Fatalf("SQL failure was mislabeled as missing evidence: %v", gotErr)
			}
			if got.Resolved {
				t.Fatalf("lookup = %+v, want unresolved after provenance refusal", got)
			}
		})
	}
}

var errInjectedPromotionProofRead = errors.New("injected promotion proof reader SQL failure")

func negativePromotionFacts(t *testing.T, f *promotionPhaseFixture) *ServedRuleFacts {
	t.Helper()
	return &ServedRuleFacts{
		Thresholds: &stubRuleDeriver{input: servedFactsInput(t, "15.00")},
		Approval: stubServedApprovalFacts{decisions: []runtime.ApprovalDecisionFact{{
			DecisionID: "decision:negative-proof", Outcome: runtime.ApprovalOutcomeApproved,
			ProposalDigest: f.revision.MaterialDigest.Digest,
		}}},
	}
}

func appendThirdGradeSuccessor(t *testing.T, f *promotionPhaseFixture) {
	t.Helper()
	if err := withPromotionPhaseTx(t, f, func(tx dbport.Tx) error {
		a := mustPhase(aggregates.NewAssignment(f.tenant, f.assignment, f.employment, true,
			f.checkedAt.Add(-2*time.Hour), nil, f.checkedAt.Add(-90*time.Minute),
			"ENG-MGR1", "M2", f.baseline.OrganizationRef, nil, "NYC", "US-NY", "1.0000", "manager-rel:new"))
		_, err := (aggregates.PeopleStore{}).PutAssignment(context.Background(), tx, a)
		return err
	}); err != nil {
		t.Fatalf("append third-grade successor: %v", err)
	}
}

func commitPromotionSuccessorAt(t *testing.T, f *promotionPhaseFixture, recordedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := withPromotionPhaseTx(t, f, func(tx dbport.Tx) error {
		people, organization, compensation := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
		worker, err := people.CurrentWorker(ctx, tx, f.tenant, f.worker, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		employment, err := people.CurrentEmployment(ctx, tx, f.tenant, f.employment, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		assignment, err := people.CurrentAssignment(ctx, tx, f.tenant, f.assignment, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		job, err := organization.CurrentJob(ctx, tx, f.tenant, f.job, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		position, err := organization.CurrentJobPosition(ctx, tx, f.tenant, f.position, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		pkg, err := compensation.CurrentCompensationPackage(ctx, tx, f.tenant, f.pkg, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		base, err := compensation.CurrentCompensationComponent(ctx, tx, f.tenant, f.base, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		budget, err := compensation.CurrentBudgetReservation(ctx, tx, f.tenant, f.reservation, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		command := phaseCommand(f, worker.Digest, employment.Digest, assignment.Digest, job.Digest, position.Digest, pkg.Digest, base.Digest, budget.Digest)
		command.RecordedAt = recordedAt
		_, err = (promotioncommit.Writer{People: people, Organization: organization, Compensation: compensation}).Write(ctx, tx, command)
		return err
	}); err != nil {
		t.Fatalf("commit late promotion successor: %v", err)
	}
}

func appendLegacyPromotionProof(t *testing.T, f *promotionPhaseFixture) {
	t.Helper()
	f.db.Exec(t, `INSERT INTO people_promotion_write_evidence (
		tenant_id, write_id, proposal_revision_id, proposal_digest, actor_principal_id,
		authority_decision, worker_id, assignment_id, field_path, current_value,
		proposed_value, expected_revision, effective_from, recorded_at)
		VALUES ($1,$2,$3,'sha256:legacy','legacy','legacy',$4,$5,'assignment.grade','P3','M1','revision:legacy',$6,$7)`,
		f.tenant, uuid.New(), f.revision.ProposalRevisionID, f.worker.String(), f.assignment.String(), f.baseline.EffectiveFrom, f.baseline.RecordedAt)
}

func appendMalformedModernPromotionProof(t *testing.T, f *promotionPhaseFixture) {
	t.Helper()
	var successor aggregates.Assignment
	if err := withPromotionPhaseTx(t, f, func(tx dbport.Tx) error {
		var err error
		effectiveAt, err := phaseEffectiveAt(f.revision)
		if err != nil {
			return err
		}
		successor, err = (aggregates.PeopleStore{}).CurrentAssignment(context.Background(), tx, f.tenant, f.assignment, effectiveAt)
		return err
	}); err != nil {
		t.Fatalf("read malformed-proof successor: %v", err)
	}
	f.db.Exec(t, `INSERT INTO people_promotion_write_evidence (
		tenant_id, write_id, proposal_revision_id, proposal_digest, actor_principal_id,
		authority_decision, worker_id, assignment_id, field_path, current_value,
		proposed_value, expected_revision, effective_from, effective_to, recorded_at,
		intent_id, proposal_revision_number, assignment_row_id, assignment_digest)
		VALUES ($1,$2,$3,$4,'forged-principal','authority:forged',$5,$6,'assignment.grade','P3','M1',$7,$8,$9,$10,$11,1,$12,$13)`,
		f.tenant, uuid.New(), f.revision.ProposalRevisionID, f.revision.MaterialDigest.Digest,
		f.worker.String(), f.assignment.String(),
		mustPhase(values.NewSequenceRevision("assignment:"+f.assignment.String(), 1)).String(), successor.EffectiveFrom, successor.EffectiveTo,
		successor.RecordedAt, f.intent, successor.RowID, successor.Digest)
}

func phaseTransaction(t *testing.T, f *promotionPhaseFixture) dbport.Tx {
	t.Helper()
	conn := f.db.NewConn(t)
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin negative proof transaction: %v", err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, f.tenant); err != nil {
		t.Fatalf("set negative proof tenant: %v", err)
	}
	return tx
}

type promotionProofFailingExecutor struct {
	dbport.Tx
	err error
}

func (e *promotionProofFailingExecutor) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	if strings.Contains(strings.ToLower(sql), "from people_promotion_write_evidence") {
		return nil, e.err
	}
	return e.Tx.Query(ctx, sql, args...)
}
