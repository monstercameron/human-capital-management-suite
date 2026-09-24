// Package commercialstore persists the commercial and partner-application
// boundary from PERSIST-COMMERCIAL-001. It stores no billing state and makes no
// entitlement or installation decisions; those remain domain operations.
package commercialstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp"
)

// DB is the database capability required by Store.
type DB interface{ dbport.Beginner }

// Error and ErrorCode are aliases so callers can classify adapter failures
// without importing PostgreSQL.
type Error = commercial.StoreError
type ErrorCode = commercial.StoreCode

const (
	CodeInvalid             = commercial.StoreInvalidCode
	CodeNotFound            = commercial.StoreNotFoundCode
	CodeDuplicateRevision   = commercial.StoreDuplicateRevisionCode
	CodeStaleCAS            = commercial.StoreStaleCASCode
	CodeFingerprintMismatch = commercial.StoreFingerprintCode
)

var (
	ErrInvalid             = commercial.ErrStoreInvalid
	ErrNotFound            = commercial.ErrStoreNotFound
	ErrDuplicate           = commercial.ErrStoreDuplicateRevision
	ErrVersionConflict     = commercial.ErrStoreStaleCAS
	ErrFingerprintMismatch = commercial.ErrStoreFingerprint
)

// Store is the PostgreSQL adapter for the commercial contract port plus the
// tenant-scoped partner installation and data-processing review records.
type Store struct {
	db      DB
	resolve func(string) uuid.UUID
}

var _ commercial.ContractRevisionStore = (*Store)(nil)

// New constructs a store. Tenant IDs are UUID strings by default; the
// optional resolver allows a logical tenant key to be mapped to its physical
// tenant UUID without putting database concerns into the domain package.
func New(db DB, resolver ...func(string) uuid.UUID) *Store {
	var resolve func(string) uuid.UUID
	if len(resolver) > 0 {
		resolve = resolver[0]
	}
	return &Store{db: db, resolve: resolve}
}

func storeError(code ErrorCode, detail string, err error) error {
	return &Error{Code: code, Detail: detail, Err: err}
}

func (s *Store) tenant(value string) (uuid.UUID, error) {
	if s == nil || s.db == nil {
		return uuid.Nil, storeError(CodeInvalid, "database is required", nil)
	}
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return uuid.Nil, storeError(CodeInvalid, "tenant is required", nil)
	}
	if s.resolve != nil {
		id := s.resolve(value)
		if id != uuid.Nil {
			return id, nil
		}
	}
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, storeError(CodeInvalid, "tenant is not a non-nil UUID", err)
	}
	return id, nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return storeError(CodeInvalid, "context is required", nil)
	}
	return ctx.Err()
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil || tenantID == uuid.Nil {
		return storeError(CodeInvalid, "database and tenant are required", nil)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("commercialstore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commercialstore: commit transaction: %w", err)
	}
	return nil
}

func (s *Store) catalog(ctx context.Context, fn func(dbport.Tx) error) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return storeError(CodeInvalid, "database is required", nil)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("commercialstore: begin catalog transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	return fn(tx)
}

type contractCapabilities struct {
	Values []string                    `json:"values"`
	Bound  commercial.EntitlementBound `json:"bound"`
}

func marshal(value any) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("commercialstore: encode JSON: %w", err)
	}
	return b, nil
}

func unmarshal(raw []byte, out any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if len(value) == 64 && !strings.Contains(value, ":") {
		return "sha256:" + value
	}
	return value
}

func maxRevision(ctx context.Context, q dbport.Querier, table, keyColumn, key string, tenantID uuid.UUID) (uint64, error) {
	var latest int64
	query := fmt.Sprintf("SELECT COALESCE(MAX(revision), 0) FROM %s WHERE tenant_id=$1 AND %s=$2", table, keyColumn)
	if err := q.QueryRow(ctx, query, tenantID, key).Scan(&latest); err != nil {
		return 0, fmt.Errorf("commercialstore: read %s tip: %w", table, err)
	}
	if latest < 0 {
		return 0, storeError(CodeInvalid, table+" has a negative revision", nil)
	}
	return uint64(latest), nil
}

func mapDuplicate(table, key string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return storeError(CodeDuplicateRevision, fmt.Sprintf("%s %s already exists", table, key), ErrDuplicate)
	}
	return fmt.Errorf("commercialstore: %s %s: %w", table, key, err)
}

// PutContractRevision appends exactly the next fixed-price contract revision.
// An expected revision, when supplied, is a compare-and-swap against the
// current tip; zero is the expected tip for a first revision.
func (s *Store) PutContractRevision(ctx context.Context, contract commercial.ContractRevision, expected ...uint64) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if len(expected) > 1 {
		return storeError(CodeInvalid, "at most one expected revision is allowed", nil)
	}
	if contract.Status == "" {
		contract.Status = commercial.StatusActive
	}
	if err := contract.Validate(); err != nil {
		return storeError(CodeInvalid, "contract revision: "+err.Error(), err)
	}
	tenantID, err := s.tenant(contract.TenantID)
	if err != nil {
		return err
	}
	var want uint64
	if len(expected) == 1 {
		want = expected[0]
	}
	snapshot, err := commercial.NewEntitlementSnapshot(contract)
	if err != nil {
		return storeError(CodeInvalid, "contract fingerprint: "+err.Error(), err)
	}
	payload, err := marshal(contractCapabilities{Values: append([]string(nil), contract.Capabilities...), Bound: contract.Bound})
	if err != nil {
		return storeError(CodeInvalid, err.Error(), err)
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		latest, err := maxRevision(ctx, tx, "commercial_contract_revision", "contract_id", contract.ContractID, tenantID)
		if err != nil {
			return err
		}
		if contract.Revision <= latest {
			return storeError(CodeDuplicateRevision, fmt.Sprintf("contract %s revision %d", contract.ContractID, contract.Revision), ErrDuplicate)
		}
		if want != latest || contract.Revision != latest+1 {
			return &Error{Code: CodeStaleCAS, Detail: fmt.Sprintf("contract %s expected %d, current %d", contract.ContractID, want, latest), Expected: want, Actual: latest, Err: ErrVersionConflict}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO commercial_contract_revision
			(tenant_id,row_id,contract_id,revision,effective_from,effective_to,status,capabilities,bound,price_cents,currency,fingerprint)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12)`,
			tenantID, uuid.New(), contract.ContractID, int64(contract.Revision), contract.EffectiveFrom.UTC(), contract.EffectiveTo.UTC(), contract.Status, payload, contract.Bound.Valid(), contract.PriceCents, contract.Currency, snapshot.Fingerprint())
		if err != nil {
			return mapDuplicate("commercial_contract_revision", contract.ContractID, err)
		}
		return nil
	})
}

type contractRow struct {
	TenantID    uuid.UUID
	ContractID  string
	Revision    int64
	Status      string
	From        *time.Time
	To          *time.Time
	Payload     []byte
	Bound       bool
	Price       *int64
	Currency    *string
	Fingerprint string
}

func materializeContract(row contractRow) (commercial.ContractRevision, error) {
	var payload contractCapabilities
	if err := unmarshal(row.Payload, &payload); err != nil {
		return commercial.ContractRevision{}, storeError(CodeInvalid, "contract capabilities JSON: "+err.Error(), err)
	}
	if row.From == nil || row.To == nil || row.Price == nil || row.Currency == nil {
		return commercial.ContractRevision{}, storeError(CodeInvalid, "stored contract has a missing required value", nil)
	}
	contract := commercial.ContractRevision{
		TenantID: row.TenantID.String(), ContractID: row.ContractID, Revision: uint64(row.Revision), EffectiveFrom: row.From.UTC(), EffectiveTo: row.To.UTC(),
		Status: commercial.ContractStatus(row.Status), Capabilities: append([]string(nil), payload.Values...), Bound: payload.Bound, PriceCents: *row.Price, Currency: *row.Currency,
	}
	if !row.Bound || !contract.Bound.Valid() {
		return commercial.ContractRevision{}, storeError(CodeInvalid, "stored contract bound does not match its payload", nil)
	}
	if err := contract.Validate(); err != nil {
		return commercial.ContractRevision{}, storeError(CodeInvalid, "stored contract: "+err.Error(), err)
	}
	snapshot, err := commercial.NewEntitlementSnapshot(contract)
	if err != nil || snapshot.Fingerprint() != row.Fingerprint {
		return commercial.ContractRevision{}, storeError(CodeFingerprintMismatch, "stored contract fingerprint does not match its content", err)
	}
	return contract, nil
}

func loadContract(ctx context.Context, q dbport.Querier, tenantID uuid.UUID, id string, revision uint64) (commercial.ContractRevision, error) {
	var row contractRow
	err := q.QueryRow(ctx, `
		SELECT tenant_id,contract_id,revision,effective_from,effective_to,status,capabilities::text,bound,price_cents,currency,fingerprint
		FROM commercial_contract_revision
		WHERE tenant_id=$1 AND contract_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(
		&row.TenantID, &row.ContractID, &row.Revision, &row.From, &row.To, &row.Status, &row.Payload, &row.Bound, &row.Price, &row.Currency, &row.Fingerprint)
	if errors.Is(err, dbport.ErrNoRows) {
		return commercial.ContractRevision{}, storeError(CodeNotFound, "contract revision not found", ErrNotFound)
	}
	if err != nil {
		return commercial.ContractRevision{}, fmt.Errorf("commercialstore: load contract revision: %w", err)
	}
	return materializeContract(row)
}

// GetContractRevision loads one immutable contract revision.
func (s *Store) GetContractRevision(ctx context.Context, tenant, id string, revision uint64) (commercial.ContractRevision, error) {
	tenantID, err := s.tenant(tenant)
	if err != nil {
		return commercial.ContractRevision{}, err
	}
	var out commercial.ContractRevision
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		out, err = loadContract(ctx, tx, tenantID, id, revision)
		return err
	})
	return out, err
}

// ListContractRevisions returns an immutable contract history in revision
// order.
func (s *Store) ListContractRevisions(ctx context.Context, tenant, id string) ([]commercial.ContractRevision, error) {
	tenantID, err := s.tenant(tenant)
	if err != nil {
		return nil, err
	}
	var out []commercial.ContractRevision
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT tenant_id,contract_id,revision,effective_from,effective_to,status,capabilities::text,bound,price_cents,currency,fingerprint
			FROM commercial_contract_revision WHERE tenant_id=$1 AND contract_id=$2 ORDER BY revision`, tenantID, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row contractRow
			if err := rows.Scan(&row.TenantID, &row.ContractID, &row.Revision, &row.From, &row.To, &row.Status, &row.Payload, &row.Bound, &row.Price, &row.Currency, &row.Fingerprint); err != nil {
				return err
			}
			contract, err := materializeContract(row)
			if err != nil {
				return err
			}
			out = append(out, contract)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(out) == 0 {
			return storeError(CodeNotFound, "contract revisions not found", ErrNotFound)
		}
		return nil
	})
	return out, err
}

// PutEntitlementSnapshot records a frozen snapshot only when its fingerprint
// matches the named contract revision in the same tenant.
func (s *Store) PutEntitlementSnapshot(ctx context.Context, snapshot commercial.EntitlementSnapshot) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return storeError(CodeInvalid, "entitlement snapshot: "+err.Error(), err)
	}
	tenantID, err := s.tenant(snapshot.TenantID())
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		contract, err := loadContract(ctx, tx, tenantID, snapshot.ContractID(), snapshot.Revision())
		if err != nil {
			return err
		}
		candidate, err := commercial.NewEntitlementSnapshot(contract)
		if err != nil || candidate.Fingerprint() != snapshot.Fingerprint() {
			return storeError(CodeFingerprintMismatch, "snapshot fingerprint does not match contract revision", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO entitlement_snapshot (tenant_id,row_id,contract_id,revision,fingerprint,frozen_at)
			VALUES ($1,$2,$3,$4,$5,now())`, tenantID, uuid.New(), snapshot.ContractID(), int64(snapshot.Revision()), snapshot.Fingerprint())
		if err != nil {
			return mapDuplicate("entitlement_snapshot", snapshot.ContractID(), err)
		}
		return nil
	})
}

// GetEntitlementSnapshot loads and revalidates the frozen snapshot against its
// named contract revision.
func (s *Store) GetEntitlementSnapshot(ctx context.Context, tenant, id string, revision uint64) (commercial.EntitlementSnapshot, error) {
	tenantID, err := s.tenant(tenant)
	if err != nil {
		return commercial.EntitlementSnapshot{}, err
	}
	var out commercial.EntitlementSnapshot
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var fingerprint string
		err := tx.QueryRow(ctx, `SELECT fingerprint FROM entitlement_snapshot WHERE tenant_id=$1 AND contract_id=$2 AND revision=$3 ORDER BY frozen_at DESC LIMIT 1`, tenantID, id, int64(revision)).Scan(&fingerprint)
		if errors.Is(err, dbport.ErrNoRows) {
			return storeError(CodeNotFound, "entitlement snapshot not found", ErrNotFound)
		}
		if err != nil {
			return err
		}
		contract, err := loadContract(ctx, tx, tenantID, id, revision)
		if err != nil {
			return err
		}
		out, err = commercial.NewEntitlementSnapshot(contract)
		if err != nil || out.Fingerprint() != fingerprint {
			return storeError(CodeFingerprintMismatch, "stored entitlement fingerprint does not match contract", err)
		}
		return nil
	})
	return out, err
}

type applicationVersionPayload struct {
	Requester            string                   `json:"requester"`
	AgreementRef         string                   `json:"agreement_ref"`
	DeclaredCapabilities []string                 `json:"declared_capabilities"`
	DataClasses          []string                 `json:"data_classes"`
	RedirectEndpoints    []partnerapp.EndpointRef `json:"redirect_endpoints"`
	CallbackEndpoints    []partnerapp.EndpointRef `json:"callback_endpoints"`
	ContactRef           string                   `json:"contact_ref"`
	LegalRef             string                   `json:"legal_ref"`
	Review               partnerapp.ReviewRecord  `json:"review"`
}

// GetApplication reads one shared platform catalog entry. Catalog writes are
// intentionally not exposed through this app-role adapter.
func (s *Store) GetApplication(ctx context.Context, applicationID string) (partnerapp.PartnerApplication, error) {
	var out partnerapp.PartnerApplication
	err := s.catalog(ctx, func(tx dbport.Tx) error {
		var payload struct {
			PartnerRef, ApplicationID                                               string
			AgreementRef, ContactRef, LegalRef                                      *string
			DeclaredCapabilities, DataClasses, RedirectEndpoints, CallbackEndpoints []byte
			LegalRefValue                                                           *string
		}
		var agreement, contact, legalRef *string
		var capabilities, classes, redirects, callbacks []byte
		err := tx.QueryRow(ctx, `SELECT partner_ref,application_id,agreement_ref,declared_capabilities::text,data_classes::text,redirect_endpoints::text,callback_endpoints::text,contact_ref,legal_ref FROM partner_application WHERE application_id=$1`, applicationID).Scan(
			&payload.PartnerRef, &payload.ApplicationID, &agreement, &capabilities, &classes, &redirects, &callbacks, &contact, &legalRef)
		if errors.Is(err, dbport.ErrNoRows) {
			return storeError(CodeNotFound, "partner application not found", ErrNotFound)
		}
		if err != nil {
			return err
		}
		out = partnerapp.PartnerApplication{PartnerRef: payload.PartnerRef, ApplicationID: payload.ApplicationID}
		if agreement != nil {
			out.AgreementRef = *agreement
		}
		if contact != nil {
			out.ContactRef = *contact
		}
		if legalRef != nil {
			out.LegalRef = *legalRef
		}
		if err := unmarshal(capabilities, &out.DeclaredCapabilities); err != nil {
			return err
		}
		if err := unmarshal(classes, &out.DataClasses); err != nil {
			return err
		}
		if err := unmarshal(redirects, &out.RedirectEndpoints); err != nil {
			return err
		}
		if err := unmarshal(callbacks, &out.CallbackEndpoints); err != nil {
			return err
		}
		if err := out.Validate(); err != nil {
			return storeError(CodeInvalid, "partner application: "+err.Error(), err)
		}
		return nil
	})
	return out, err
}

func (s *Store) GetApplicationVersion(ctx context.Context, applicationID, version string, revision uint64) (partnerapp.ApplicationVersion, error) {
	var out partnerapp.ApplicationVersion
	err := s.catalog(ctx, func(tx dbport.Tx) error {
		var state, digest string
		var successor *string
		var review []byte
		err := tx.QueryRow(ctx, `SELECT state,review::text,successor_version,digest FROM partner_application_version_revision WHERE application_id=$1 AND version=$2 AND revision=$3`, applicationID, version, int64(revision)).Scan(&state, &review, &successor, &digest)
		if errors.Is(err, dbport.ErrNoRows) {
			return storeError(CodeNotFound, "partner application version not found", ErrNotFound)
		}
		if err != nil {
			return err
		}
		var payload applicationVersionPayload
		if err := unmarshal(review, &payload); err != nil {
			return err
		}
		var successorValue string
		if successor != nil {
			successorValue = *successor
		}
		out = partnerapp.ApplicationVersion{ApplicationID: applicationID, Version: version, Revision: revision, State: partnerapp.LifecycleState(state), Requester: payload.Requester, Review: payload.Review, AgreementRef: payload.AgreementRef, DeclaredCapabilities: payload.DeclaredCapabilities, DataClasses: payload.DataClasses, RedirectEndpoints: payload.RedirectEndpoints, CallbackEndpoints: payload.CallbackEndpoints, ContactRef: payload.ContactRef, LegalRef: payload.LegalRef, SuccessorVersion: successorValue, Digest: digest}
		if err := out.Verify(); err != nil {
			return storeError(CodeFingerprintMismatch, "stored application version digest does not match", err)
		}
		return nil
	})
	return out, err
}

func (s *Store) ListApplicationVersionRevisions(ctx context.Context, applicationID, version string) ([]partnerapp.ApplicationVersion, error) {
	var revisions []int64
	err := s.catalog(ctx, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM partner_application_version_revision WHERE application_id=$1 AND version=$2 ORDER BY revision`, applicationID, version)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return err
			}
			revisions = append(revisions, revision)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, storeError(CodeNotFound, "partner application version not found", ErrNotFound)
	}
	out := make([]partnerapp.ApplicationVersion, 0, len(revisions))
	for _, revision := range revisions {
		v, err := s.GetApplicationVersion(ctx, applicationID, version, uint64(revision))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

type installationBindingPayload struct {
	ApplicationID string   `json:"application_id"`
	Version       string   `json:"version"`
	Revision      uint64   `json:"revision"`
	Digest        string   `json:"digest"`
	TenantScope   string   `json:"tenant_scope"`
	Purpose       string   `json:"purpose"`
	Capabilities  []string `json:"capabilities"`
}

func installationJSON(i partnerapp.Installation) (binding []byte, dataClasses, fieldScopes []byte, approver []byte, err error) {
	binding, err = marshal(installationBindingPayload{ApplicationID: i.VersionBinding.ApplicationID, Version: i.VersionBinding.Version, Revision: i.VersionBinding.Revision, Digest: i.VersionBinding.Digest, TenantScope: i.TenantScope, Purpose: i.Purpose, Capabilities: i.Capabilities})
	if err != nil {
		return
	}
	dataClasses, err = marshal(i.DataClasses)
	if err != nil {
		return
	}
	fieldScopes, err = marshal(i.FieldScopes)
	if err != nil {
		return
	}
	if i.ApproverEvidence.Approver != "" || i.ApproverEvidence.EvidenceRef != "" || i.ApproverEvidence.Requester != "" || i.ApproverEvidence.ReviewRef != "" {
		approver, err = marshal(i.ApproverEvidence)
	}
	return
}

// PutInstallation appends an immutable installation revision and its matching
// append-only lifecycle event atomically.
func (s *Store) PutInstallation(ctx context.Context, tenantID uuid.UUID, installation partnerapp.Installation, event partnerapp.InstallationEvent) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if tenantID == uuid.Nil {
		return storeError(CodeInvalid, "tenant is required", nil)
	}
	if err := installation.Verify(); err != nil {
		return storeError(CodeInvalid, "installation: "+err.Error(), err)
	}
	if err := event.Verify(); err != nil || event.InstallationID != installation.InstallationID || event.Revision != installation.Revision || event.State != installation.State || event.InstallationDigest != installation.Digest {
		return storeError(CodeInvalid, "installation event does not bind its revision", err)
	}
	binding, classes, scopes, approver, err := installationJSON(installation)
	if err != nil {
		return storeError(CodeInvalid, err.Error(), err)
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		latest, err := maxRevision(ctx, tx, "partner_installation", "installation_id", installation.InstallationID, tenantID)
		if err != nil {
			return err
		}
		if installation.Revision <= latest {
			return storeError(CodeDuplicateRevision, "installation revision already exists", ErrDuplicate)
		}
		if installation.Revision != latest+1 {
			return &Error{Code: CodeStaleCAS, Detail: "installation revision does not follow the current tip", Expected: latest, Actual: latest, Err: ErrVersionConflict}
		}
		var previousDigest string
		var previousState partnerapp.GrantState
		if latest > 0 {
			if err := tx.QueryRow(ctx, `SELECT digest,state FROM partner_installation WHERE tenant_id=$1 AND installation_id=$2 AND revision=$3`, tenantID, installation.InstallationID, int64(latest)).Scan(&previousDigest, &previousState); err != nil {
				return err
			}
		}
		var eventLatest int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(event_sequence),0) FROM partner_installation_event WHERE tenant_id=$1 AND installation_id=$2`, tenantID, installation.InstallationID).Scan(&eventLatest); err != nil {
			return err
		}
		eventSequence := installation.Revision
		if eventSequence != uint64(eventLatest+1) {
			return &Error{Code: CodeStaleCAS, Detail: "installation event sequence is not the next sequence", Expected: uint64(eventLatest + 1), Actual: eventSequence, Err: ErrVersionConflict}
		}
		if event.PreviousDigest != previousDigest || event.PreviousState != previousState {
			return storeError(CodeStaleCAS, "installation event predecessor does not match history", ErrVersionConflict)
		}
		_, err = tx.Exec(ctx, `INSERT INTO partner_installation (tenant_id,row_id,installation_id,revision,state,version_binding,organization_scope,population_scope,data_classes,field_scopes,requester,approver_evidence,review_ref,digest) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9::jsonb,$10::jsonb,$11,$12::jsonb,$13,$14)`, tenantID, uuid.New(), installation.InstallationID, int64(installation.Revision), installation.State, binding, installation.OrganizationScope, installation.PopulationScope, classes, scopes, installation.Requester, approver, installation.ReviewRef, installation.Digest)
		if err != nil {
			return mapDuplicate("partner_installation", installation.InstallationID, err)
		}
		var prevDigest any
		if event.PreviousDigest != "" {
			prevDigest = event.PreviousDigest
		}
		var prevState any
		if event.PreviousState != "" {
			prevState = event.PreviousState
		}
		var actor any
		if event.Actor != "" {
			actor = event.Actor
		}
		var evidence any
		if event.EvidenceRef != "" {
			evidence = event.EvidenceRef
		}
		var reviewRef any
		if event.ReviewRef != "" {
			reviewRef = event.ReviewRef
		}
		var installationDigest any
		if event.InstallationDigest != "" {
			installationDigest = event.InstallationDigest
		}
		_, err = tx.Exec(ctx, `INSERT INTO partner_installation_event (tenant_id,row_id,installation_id,revision,previous_digest,previous_state,state,actor,evidence_ref,review_ref,installation_digest,digest,event_sequence) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, tenantID, uuid.New(), event.InstallationID, int64(event.Revision), prevDigest, prevState, event.State, actor, evidence, reviewRef, installationDigest, event.Digest, int64(eventSequence))
		if err != nil {
			return mapDuplicate("partner_installation_event", installation.InstallationID, err)
		}
		return nil
	})
}

func materializeInstallation(id string, revision int64, state string, bindingRaw []byte, organization, population *string, classesRaw, scopesRaw []byte, requester string, approverRaw []byte, reviewRef, digest string) (partnerapp.Installation, error) {
	var binding installationBindingPayload
	if err := unmarshal(bindingRaw, &binding); err != nil {
		return partnerapp.Installation{}, err
	}
	var classes, scopes []string
	if err := unmarshal(classesRaw, &classes); err != nil {
		return partnerapp.Installation{}, err
	}
	if err := unmarshal(scopesRaw, &scopes); err != nil {
		return partnerapp.Installation{}, err
	}
	i := partnerapp.Installation{InstallationID: id, Revision: uint64(revision), State: partnerapp.GrantState(state), VersionBinding: partnerapp.ApplicationVersionBinding{ApplicationID: binding.ApplicationID, Version: binding.Version, Revision: binding.Revision, Digest: binding.Digest}, TenantScope: binding.TenantScope, Purpose: binding.Purpose, Capabilities: binding.Capabilities, DataClasses: classes, FieldScopes: scopes, Digest: digest}
	if organization != nil {
		i.OrganizationScope = *organization
	}
	if population != nil {
		i.PopulationScope = *population
	}
	if requester != "" {
		i.Requester = requester
	}
	if approverRaw != nil {
		if err := unmarshal(approverRaw, &i.ApproverEvidence); err != nil {
			return partnerapp.Installation{}, err
		}
	}
	if reviewRef != "" {
		i.ReviewRef = reviewRef
	}
	if err := i.Verify(); err != nil {
		return partnerapp.Installation{}, storeError(CodeFingerprintMismatch, "stored installation digest does not match", err)
	}
	return i, nil
}

func loadInstallation(ctx context.Context, q dbport.Querier, tenantID uuid.UUID, id string, revision uint64) (partnerapp.Installation, error) {
	var revisionValue int64
	var state, digest string
	var organization, population, requester, reviewRef *string
	var binding, classes, scopes, approver []byte
	err := q.QueryRow(ctx, `SELECT installation_id,revision,state,version_binding::text,organization_scope,population_scope,data_classes::text,field_scopes::text,requester,approver_evidence::text,review_ref,digest FROM partner_installation WHERE tenant_id=$1 AND installation_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(&id, &revisionValue, &state, &binding, &organization, &population, &classes, &scopes, &requester, &approver, &reviewRef, &digest)
	if errors.Is(err, dbport.ErrNoRows) {
		return partnerapp.Installation{}, storeError(CodeNotFound, "installation revision not found", ErrNotFound)
	}
	if err != nil {
		return partnerapp.Installation{}, err
	}
	var requesterValue, reviewValue string
	if requester != nil {
		requesterValue = *requester
	}
	if reviewRef != nil {
		reviewValue = *reviewRef
	}
	return materializeInstallation(id, revisionValue, state, binding, organization, population, classes, scopes, requesterValue, approver, reviewValue, digest)
}

// GetInstallation loads one tenant-scoped installation revision.
func (s *Store) GetInstallation(ctx context.Context, tenantID uuid.UUID, id string, revision uint64) (partnerapp.Installation, error) {
	var out partnerapp.Installation
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		out, err = loadInstallation(ctx, tx, tenantID, id, revision)
		return err
	})
	return out, err
}

// ListInstallationRevisions returns the immutable installation history.
func (s *Store) ListInstallationRevisions(ctx context.Context, tenantID uuid.UUID, id string) ([]partnerapp.Installation, error) {
	var revisions []int64
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM partner_installation WHERE tenant_id=$1 AND installation_id=$2 ORDER BY revision`, tenantID, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return err
			}
			revisions = append(revisions, revision)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, storeError(CodeNotFound, "installation revisions not found", ErrNotFound)
	}
	out := make([]partnerapp.Installation, 0, len(revisions))
	for _, revision := range revisions {
		i, err := s.GetInstallation(ctx, tenantID, id, uint64(revision))
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

// GetInstallationEvents loads append-only lifecycle events in sequence order.
func (s *Store) GetInstallationEvents(ctx context.Context, tenantID uuid.UUID, id string) ([]partnerapp.InstallationEvent, error) {
	var out []partnerapp.InstallationEvent
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT installation_id,revision,previous_digest,previous_state,state,actor,evidence_ref,review_ref,installation_digest,digest,event_sequence FROM partner_installation_event WHERE tenant_id=$1 AND installation_id=$2 ORDER BY event_sequence`, tenantID, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e partnerapp.InstallationEvent
			var revision, sequence int64
			var previousDigest, previousState, actor, evidence, review, installationDigest *string
			if err := rows.Scan(&e.InstallationID, &revision, &previousDigest, &previousState, &e.State, &actor, &evidence, &review, &installationDigest, &e.Digest, &sequence); err != nil {
				return err
			}
			e.Revision = uint64(revision)
			if uint64(sequence) != e.Revision {
				return storeError(CodeFingerprintMismatch, "installation event sequence does not match revision", nil)
			}
			if previousDigest != nil {
				e.PreviousDigest = *previousDigest
			}
			if previousState != nil {
				e.PreviousState = partnerapp.GrantState(*previousState)
			}
			if actor != nil {
				e.Actor = *actor
			}
			if evidence != nil {
				e.EvidenceRef = *evidence
			}
			if review != nil {
				e.ReviewRef = *review
			}
			if installationDigest != nil {
				e.InstallationDigest = *installationDigest
			}
			if err := e.Verify(); err != nil {
				return storeError(CodeFingerprintMismatch, "stored installation event is invalid", err)
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// PutDataProcessingReview appends one immutable review record.
func (s *Store) PutDataProcessingReview(ctx context.Context, tenantID uuid.UUID, review partnerapp.DataProcessingReview) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if tenantID == uuid.Nil {
		return storeError(CodeInvalid, "tenant is required", nil)
	}
	if err := review.Verify(); err != nil {
		return storeError(CodeInvalid, "review: "+err.Error(), err)
	}
	dataClasses, err := marshal(review.DataClasses)
	if err != nil {
		return storeError(CodeInvalid, err.Error(), err)
	}
	scopes, err := marshal(review.ScopeRefs)
	if err != nil {
		return storeError(CodeInvalid, err.Error(), err)
	}
	destinations, err := marshal(review.DestinationRefs)
	if err != nil {
		return storeError(CodeInvalid, err.Error(), err)
	}
	processors, err := marshal(review.ProcessorRefs)
	if err != nil {
		return storeError(CodeInvalid, err.Error(), err)
	}
	residency, err := marshal(review.ResidencyRefs)
	if err != nil {
		return storeError(CodeInvalid, err.Error(), err)
	}
	items, err := marshal(review.Items)
	if err != nil {
		return storeError(CodeInvalid, err.Error(), err)
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO partner_data_processing_review (tenant_id,row_id,application_id,version,submitter,owner_ref,reviewer,reviewed_at,expires_at,data_classes,scope_refs,destination_refs,processor_refs,residency_refs,items,digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12::jsonb,$13::jsonb,$14::jsonb,$15::jsonb,$16)`, tenantID, uuid.New(), review.ApplicationID, review.Version, review.Submitter, review.OwnerRef, review.Reviewer, review.ReviewedAt.UTC(), review.ExpiresAt.UTC(), dataClasses, scopes, destinations, processors, residency, items, storageDigest(review.Digest))
		if err != nil {
			return mapDuplicate("partner_data_processing_review", review.ApplicationID+"/"+review.Version, err)
		}
		return nil
	})
}

// ListDataProcessingReviews returns immutable reviews for one application
// version, newest first by review time.
func (s *Store) ListDataProcessingReviews(ctx context.Context, tenantID uuid.UUID, applicationID, version string) ([]partnerapp.DataProcessingReview, error) {
	var out []partnerapp.DataProcessingReview
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT application_id,version,submitter,owner_ref,reviewer,reviewed_at,expires_at,data_classes::text,scope_refs::text,destination_refs::text,processor_refs::text,residency_refs::text,items::text,digest FROM partner_data_processing_review WHERE tenant_id=$1 AND application_id=$2 AND version=$3 ORDER BY reviewed_at DESC`, tenantID, applicationID, version)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r partnerapp.DataProcessingReview
			var classes, scopes, destinations, processors, residency, items []byte
			if err := rows.Scan(&r.ApplicationID, &r.Version, &r.Submitter, &r.OwnerRef, &r.Reviewer, &r.ReviewedAt, &r.ExpiresAt, &classes, &scopes, &destinations, &processors, &residency, &items, &r.Digest); err != nil {
				return err
			}
			r.Digest = domainDigest(r.Digest)
			if err := unmarshal(classes, &r.DataClasses); err != nil {
				return err
			}
			if err := unmarshal(scopes, &r.ScopeRefs); err != nil {
				return err
			}
			if err := unmarshal(destinations, &r.DestinationRefs); err != nil {
				return err
			}
			if err := unmarshal(processors, &r.ProcessorRefs); err != nil {
				return err
			}
			if err := unmarshal(residency, &r.ResidencyRefs); err != nil {
				return err
			}
			if err := unmarshal(items, &r.Items); err != nil {
				return err
			}
			if err := r.Verify(); err != nil {
				return storeError(CodeFingerprintMismatch, "stored review digest does not match", err)
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, storeError(CodeNotFound, "data-processing review not found", ErrNotFound)
	}
	return out, nil
}
