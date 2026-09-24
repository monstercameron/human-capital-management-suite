package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

// BindOutcome implements app.OutcomeBinder. The projection update is a
// compare-and-swap and is idempotent for the exact tuple already stored; a
// different tuple never overwrites the first terminal fact.
func (s *Store) BindOutcome(ctx context.Context, in app.OutcomeBinding) error {
	if in.Tenant == "" {
		return fmt.Errorf("pgstore: bind outcome requires a tenant")
	}
	if in.ExpectedInstanceVersion == 0 {
		return fmt.Errorf("pgstore: bind outcome requires an instance version")
	}
	if err := in.Receipt.Validate(); err != nil {
		return err
	}
	intentID, err := uuid.Parse(in.Receipt.IntentID)
	if err != nil {
		return fmt.Errorf("pgstore: bind outcome intent id: %w", err)
	}
	tenantID := TenantID(in.Tenant)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: begin bind outcome: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	var (
		request, execution, business, consistency, obligation                                         string
		version                                                                                       int64
		legalReceiptRef, legalReceiptDigest, legalBindingDigest, legalProposalID, legalMaterialDigest *string
		legalAppliedObligations, legalObligationDischarges                                            []byte
	)
	err = tx.QueryRow(ctx, `
		SELECT request_state, execution_state, business_state, consistency_state,
			obligation_state, instance_version,
			legal_evaluation_receipt_ref, legal_evaluation_receipt_digest,
			legal_evaluation_binding_digest, legal_evaluation_proposal_revision_id,
			legal_evaluation_material_digest, legal_applied_obligations,
			legal_obligation_discharges
		FROM intent_instance
		WHERE tenant_id = $1 AND intent_id = $2
		FOR UPDATE`, tenantID, intentID).Scan(&request, &execution, &business, &consistency, &obligation, &version,
		&legalReceiptRef, &legalReceiptDigest, &legalBindingDigest, &legalProposalID, &legalMaterialDigest,
		&legalAppliedObligations, &legalObligationDischarges)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("pgstore: %w", app.ErrIntentNotFound)
		}
		return fmt.Errorf("pgstore: load intent outcome projection: %w", err)
	}
	current, err := app.LifecycleFromColumns(request, execution, business, consistency, obligation)
	if err != nil {
		return err
	}
	if current == in.Receipt.Dimensions {
		if sameLegalEvidence(in.Receipt.LegalEvidence, legalReceiptRef, legalReceiptDigest, legalBindingDigest, legalProposalID, legalMaterialDigest, legalAppliedObligations, legalObligationDischarges) {
			return nil
		}
		return fmt.Errorf("%w: stored legal evidence differs", app.ErrOutcomeProjectionConflict)
	}
	if uint64(version) != in.ExpectedInstanceVersion {
		return fmt.Errorf("%w: stored instance version %d, expected %d", app.ErrOutcomeProjectionConflict, version, in.ExpectedInstanceVersion)
	}
	var legalRef, legalDigest, bindingDigest, proposalID, materialDigest any
	var appliedObligations, obligationDischarges any
	if evidence := in.Receipt.LegalEvidence; evidence != nil {
		legalRef, legalDigest, bindingDigest = evidence.ReceiptRef, evidence.ReceiptDigest, evidence.BindingDigest
		proposalID, materialDigest = evidence.ProposalRevisionID, evidence.MaterialDigest
		encodedApplied, marshalErr := json.Marshal(evidence.AppliedObligations)
		if marshalErr != nil {
			return fmt.Errorf("pgstore: encode applied legal obligations: %w", marshalErr)
		}
		encodedDischarges, marshalErr := json.Marshal(evidence.Discharges)
		if marshalErr != nil {
			return fmt.Errorf("pgstore: encode legal obligation discharges: %w", marshalErr)
		}
		appliedObligations, obligationDischarges = encodedApplied, encodedDischarges
	}
	if err := applyInstanceLifecycle(ctx, tx, tenantID, intentID, in.ExpectedInstanceVersion+1, in.Receipt.Dimensions, in.Receipt.RecordedAt); err != nil {
		return err
	}
	updated, err := tx.Exec(ctx, `
		UPDATE intent_instance
		SET commit_receipt_ref = NULLIF($3, ''), repair_ref = NULLIF($4, ''),
			legal_evaluation_receipt_ref = $5,
			legal_evaluation_receipt_digest = $6,
			legal_evaluation_binding_digest = $7,
			legal_evaluation_proposal_revision_id = $8,
			legal_evaluation_material_digest = $9,
			legal_applied_obligations = $10,
			legal_obligation_discharges = $11
		WHERE tenant_id = $1 AND intent_id = $2 AND instance_version = $12`,
		tenantID, intentID, in.Receipt.CommitReceiptRef, in.Receipt.RepairRef,
		legalRef, legalDigest, bindingDigest, proposalID, materialDigest,
		appliedObligations, obligationDischarges, int64(in.ExpectedInstanceVersion+1))
	if err != nil {
		return fmt.Errorf("pgstore: bind intent outcome: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("%w: concurrent outcome update", app.ErrOutcomeProjectionConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgstore: commit intent outcome: %w", err)
	}
	return nil
}

func sameLegalEvidence(evidence *intent.LegalObligationEvidence, ref, receiptDigest, bindingDigest, proposalID, materialDigest *string, appliedJSON, dischargeJSON []byte) bool {
	if evidence == nil {
		return ref == nil && receiptDigest == nil && bindingDigest == nil && proposalID == nil && materialDigest == nil
	}
	if ref == nil || receiptDigest == nil || bindingDigest == nil || proposalID == nil || materialDigest == nil {
		return false
	}
	var applied []intent.LegalBoundObligation
	if len(appliedJSON) > 0 && string(appliedJSON) != "null" && json.Unmarshal(appliedJSON, &applied) != nil {
		return false
	}
	var discharges []intent.LegalObligationDischarge
	if len(dischargeJSON) > 0 && string(dischargeJSON) != "null" && json.Unmarshal(dischargeJSON, &discharges) != nil {
		return false
	}
	return *ref == evidence.ReceiptRef && *receiptDigest == evidence.ReceiptDigest && *bindingDigest == evidence.BindingDigest &&
		*proposalID == evidence.ProposalRevisionID && *materialDigest == evidence.MaterialDigest &&
		reflect.DeepEqual(applied, evidence.AppliedObligations) && reflect.DeepEqual(discharges, evidence.Discharges)
}
