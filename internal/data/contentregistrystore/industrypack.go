package contentregistrystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/industrypack"
)

var (
	_ industrypack.PublicationStore        = (*Store)(nil)
	_ industrypack.SignedActivationStore   = (*Store)(nil)
	_ industrypack.IndustryActivationStore = (*Store)(nil)
)

func publicationEntitlement(report industrypack.PublicationReport, tenant string) (string, int64, string, error) {
	binding := report.Entitlement
	d := binding.Decision
	if !d.Allowed() || d.TenantID != tenant || d.Capability != industrypack.IndustryEntitlementCapability(report.Industry) || !report.Industry.Valid() ||
		d.ContractID == "" || d.ContractRevision == 0 || binding.Fingerprint() == "" || binding.EvaluatedAt.IsZero() {
		return "", 0, "", refusal(CodeInvalid, ErrInvalid, "publication has no accepted tenant entitlement")
	}
	return d.ContractID, int64(d.ContractRevision), binding.Fingerprint(), nil
}

func activationEntitlement(receipt industrypack.ActivationReceipt) (string, int64, string, error) {
	binding := receipt.Entitlement
	d := binding.Decision
	if !d.Allowed() || d.TenantID != receipt.Target.Tenant || d.Capability != industrypack.IndustryEntitlementCapability(receipt.Industry) || !receipt.Industry.Valid() ||
		d.ContractID == "" || d.ContractRevision == 0 || binding.Fingerprint() == "" || binding.EvaluatedAt.IsZero() {
		return "", 0, "", refusal(CodeInvalid, ErrInvalid, "activation has no accepted tenant entitlement")
	}
	return d.ContractID, int64(d.ContractRevision), binding.Fingerprint(), nil
}

func jsonPayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, refusal(CodeInvalid, ErrInvalid, "industry pack payload cannot be encoded")
	}
	return payload, nil
}

func insertUnique(ctx context.Context, tx dbport.Tx, query string, args ...any) error {
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return refusal(CodeDuplicate, ErrDuplicate, "industry pack publication or activation already exists")
		}
		return fmt.Errorf("contentregistrystore: save industry pack record: %w", err)
	}
	return nil
}

// SavePublication persists an accepted publication report and the exact
// commercial snapshot decision in one tenant-scoped transaction.
func (s *Store) SavePublication(ctx context.Context, tenantID string, report industrypack.PublicationReport) (industrypack.ActivationEffects, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return industrypack.ActivationEffects{}, err
	}
	contractID, revision, fingerprint, err := publicationEntitlement(report, tenantID)
	if err != nil {
		return industrypack.ActivationEffects{}, err
	}
	acceptedAt := report.Entitlement.EvaluatedAt.UTC()
	payload, err := jsonPayload(report)
	if err != nil {
		return industrypack.ActivationEffects{}, err
	}
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		return insertUnique(ctx, tx, `
			INSERT INTO industry_pack_publication
				(tenant_id,row_id,pack_id,version,industry,pack_digest,entitlement_contract_id,entitlement_revision,entitlement_fingerprint,report,accepted_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11)`,
			tid, uuid.New(), report.PackID, report.Version, string(report.Industry), report.Digest,
			contractID, revision, fingerprint, payload, acceptedAt)
	})
	if err != nil {
		return industrypack.ActivationEffects{}, err
	}
	return industrypack.ActivationEffects{AuthoritativeRows: 1}, nil
}

func (s *Store) saveActivation(ctx context.Context, receipt industrypack.ActivationReceipt, composition any) (industrypack.ActivationEffects, error) {
	tenantID := receipt.Target.Tenant
	tid, err := parseTenant(tenantID)
	if err != nil {
		return industrypack.ActivationEffects{}, err
	}
	contractID, revision, fingerprint, err := activationEntitlement(receipt)
	if err != nil {
		return industrypack.ActivationEffects{}, err
	}
	receiptJSON, err := jsonPayload(receipt)
	if err != nil {
		return industrypack.ActivationEffects{}, err
	}
	var compositionJSON []byte
	if composition != nil {
		compositionJSON, err = jsonPayload(composition)
		if err != nil {
			return industrypack.ActivationEffects{}, err
		}
	}
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		return insertUnique(ctx, tx, `
			INSERT INTO industry_pack_activation
				(tenant_id,row_id,pack_id,version,industry,target_cell,bundle_digest,receipt_digest,entitlement_contract_id,entitlement_revision,entitlement_fingerprint,receipt,composition,accepted_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14)`,
			tid, uuid.New(), receipt.PackID, receipt.Version, string(receipt.Industry), receipt.Target.Cell, receipt.BundleDigest, receipt.Digest,
			contractID, revision, fingerprint, receiptJSON, nullableJSON(compositionJSON), receipt.Entitlement.EvaluatedAt.UTC())
	})
	if err != nil {
		return industrypack.ActivationEffects{}, err
	}
	return industrypack.ActivationEffects{AuthoritativeRows: 1}, nil
}

func nullableJSON(payload []byte) any {
	if payload == nil {
		return nil
	}
	return string(payload)
}

// SaveActivation persists an accepted signed activation.
func (s *Store) SaveActivation(ctx context.Context, receipt industrypack.ActivationReceipt) (industrypack.ActivationEffects, error) {
	return s.saveActivation(ctx, receipt, nil)
}

// SaveIndustryActivation persists the accepted proof, signed receipt and
// composed industry result atomically for the target tenant.
func (s *Store) SaveIndustryActivation(ctx context.Context, result industrypack.IndustryResult, receipt industrypack.ActivationReceipt) (industrypack.ActivationEffects, error) {
	if result.PackID != receipt.PackID || result.Version != receipt.Version || result.Industry != receipt.Industry || result.BundleDigest != receipt.BundleDigest ||
		result.Entitlement.Fingerprint() != receipt.Entitlement.Fingerprint() {
		return industrypack.ActivationEffects{}, refusal(CodeInvalid, ErrInvalid, "composition does not match accepted activation receipt")
	}
	return s.saveActivation(ctx, receipt, result)
}
