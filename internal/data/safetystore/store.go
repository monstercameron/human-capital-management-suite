// Package safetystore persists the six immutable safety revision families.
// The caller supplies a transaction that is already scoped with
// internal/data/tenancy.WithTenant; the store never owns transaction lifetime.
package safetystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/safety"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Executor is the caller-owned transaction capability. A bare connection
// cannot satisfy it: revision checks and inserts must share tenant-scoped
// transaction state.
type Executor = dbport.Tx

// ErrorCode is stable machine-readable classification for store refusals.
type ErrorCode string

const (
	CodeInvalid            ErrorCode = "SAFETY_INVALID"
	CodeDuplicateRevision  ErrorCode = "SAFETY_DUPLICATE_REVISION"
	CodeVersionConflict    ErrorCode = "SAFETY_VERSION_CONFLICT"
	CodeNotFound           ErrorCode = "SAFETY_NOT_FOUND"
	CodeReferenceConflict  ErrorCode = "SAFETY_REFERENCE_CONFLICT"
	CodeIntegrityViolation ErrorCode = "SAFETY_INTEGRITY_VIOLATION"
)

var (
	ErrInvalid           = errors.New("safetystore: invalid row")
	ErrDuplicate         = errors.New("safetystore: duplicate revision")
	ErrVersionConflict   = errors.New("safetystore: version conflict")
	ErrNotFound          = errors.New("safetystore: not found")
	ErrReferenceConflict = errors.New("safetystore: reference conflict")
	ErrIntegrity         = errors.New("safetystore: integrity violation")

	// Compatibility names make the revision fault contract explicit.
	ErrDuplicateRevision = ErrDuplicate
	ErrStaleRevision     = ErrVersionConflict
)

// Error is a classified refusal. Callers can use errors.Is for the stable
// category and inspect Code without parsing PostgreSQL driver text.
type Error struct {
	Code   ErrorCode
	Cause  error
	Detail string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s: %s", e.Code, e.Cause, e.Detail) }
func (e *Error) Unwrap() error { return e.Cause }

func refusal(code ErrorCode, cause error, detail string) error {
	return &Error{Code: code, Cause: cause, Detail: detail}
}

// Store is a tenant-bound adapter over a caller-owned transaction. The
// transaction must have tenancy.WithTenant applied before the first method.
// Keeping the tenant in the adapter prevents a call from writing a different
// tenant than the transaction was assembled for.
type Store struct {
	Executor Executor
	TenantID uuid.UUID
}

// New binds an executor and tenant to a safety repository.
func New(ex dbport.Tx, tenantID uuid.UUID) Store {
	return Store{Executor: ex, TenantID: tenantID}
}

var _ safety.Store = Store{}

func (s Store) ready() error {
	if s.Executor == nil {
		return refusal(CodeInvalid, ErrInvalid, "executor is required")
	}
	if s.TenantID == uuid.Nil {
		return refusal(CodeInvalid, ErrInvalid, "tenant id is required")
	}
	return nil
}

// SaveIncident appends an incident revision.
func (s Store) SaveIncident(in safety.IncidentRevision) error {
	if err := s.ready(); err != nil {
		return err
	}
	normalized, err := safety.NewIncidentRevision(in)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	caseRef, incidentID, err := ids(normalized.CaseRef, normalized.ID)
	if err != nil {
		return err
	}
	compartmentID, err := parseID("compartment_ref", normalized.CompartmentRef)
	if err != nil {
		return err
	}
	workerID, err := parseID("worker_ref", normalized.WorkerRef)
	if err != nil {
		return err
	}
	reporterID, err := parseID("reporter_ref", normalized.ReporterRef)
	if err != nil {
		return err
	}
	locationID, err := parseID("location_ref", normalized.LocationRef)
	if err != nil {
		return err
	}
	if err := s.checkRevisionHead("safety_incident_revision", "incident_id", caseRef, incidentID, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest); err != nil {
		return err
	}
	affected, err := s.Executor.Exec(context.Background(), `
		INSERT INTO safety_incident_revision
		(tenant_id,row_id,case_ref,compartment_ref,incident_id,revision,parent_revision,parent_digest,canonical_digest,incident_at,kind,worker_ref,reporter_ref,location_ref,description,status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT DO NOTHING`, s.TenantID, uuid.New(), caseRef, compartmentID, incidentID, normalized.Revision, nullableRevision(normalized.ParentRevision), nullableDigest(normalized.ParentDigest), storageDigest(normalized.CanonicalDigest), normalized.IncidentAt.Time(), string(normalized.Kind), workerID, reporterID, locationID, normalized.Description, string(normalized.Status))
	if err != nil {
		return fmt.Errorf("safetystore: insert incident: %w", err)
	}
	if affected == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "incident revision identity already exists")
	}
	return nil
}

// GetIncident loads one incident revision.
func (s Store) GetIncident(id string, revision uint64) (safety.IncidentRevision, bool) {
	if s.ready() != nil {
		return safety.IncidentRevision{}, false
	}
	var (
		rowID, caseRef, compartmentRef, incidentID                     uuid.UUID
		rev                                                            int64
		parentRevision                                                 *int64
		parentDigest                                                   *string
		digest                                                         string
		at                                                             time.Time
		kind, workerRef, reporterRef, locationRef, description, status *string
	)
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return safety.IncidentRevision{}, false
	}
	err = s.Executor.QueryRow(context.Background(), `SELECT row_id,case_ref,compartment_ref,incident_id,revision,parent_revision,parent_digest,canonical_digest,incident_at,kind,worker_ref::text,reporter_ref::text,location_ref::text,description,status FROM safety_incident_revision WHERE tenant_id=$1 AND incident_id=$2 AND revision=$3`, s.TenantID, parsedID, revision).Scan(&rowID, &caseRef, &compartmentRef, &incidentID, &rev, &parentRevision, &parentDigest, &digest, &at, &kind, &workerRef, &reporterRef, &locationRef, &description, &status)
	if err != nil {
		return safety.IncidentRevision{}, false
	}
	_ = rowID
	got, err := safety.NewIncidentRevision(safety.IncidentRevision{ID: incidentID.String(), CaseRef: caseRef.String(), CompartmentRef: compartmentRef.String(), Revision: uint64(rev), ParentRevision: uint64(int64Value(parentRevision)), ParentDigest: domainDigest(stringValue(parentDigest)), IncidentAt: newInstant(at), Kind: safety.IncidentKind(stringValue(kind)), WorkerRef: stringValue(workerRef), ReporterRef: stringValue(reporterRef), LocationRef: stringValue(locationRef), Description: stringValue(description), Status: safety.IncidentStatus(stringValue(status)), CanonicalDigest: domainDigest(digest)})
	if err != nil || got.CanonicalDigest != domainDigest(digest) {
		return safety.IncidentRevision{}, false
	}
	return got, true
}

// SaveInjury appends a medical injury revision.
func (s Store) SaveInjury(in safety.InjuryRevision) error {
	if err := s.ready(); err != nil {
		return err
	}
	normalized, err := safety.NewInjuryRevision(in)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	caseRef, injuryID, err := ids(normalized.CaseRef, normalized.ID)
	if err != nil {
		return err
	}
	compartmentID, err := parseID("compartment_ref", normalized.CompartmentRef)
	if err != nil {
		return err
	}
	incidentID, err := parseID("incident_ref", normalized.IncidentRef)
	if err != nil {
		return err
	}
	if err := s.requireIncident(caseRef, incidentID); err != nil {
		return err
	}
	if err := s.checkRevisionHead("safety_injury_revision", "injury_id", caseRef, injuryID, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest); err != nil {
		return err
	}
	workerID, err := parseID("worker_ref", normalized.WorkerRef)
	if err != nil {
		return err
	}
	medicalID, err := parseID("medical_evidence_ref", normalized.MedicalEvidenceRef)
	if err != nil {
		return err
	}
	affected, err := s.Executor.Exec(context.Background(), `INSERT INTO safety_injury_revision (tenant_id,row_id,case_ref,compartment_ref,injury_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,worker_ref,kind,medical_evidence_ref,severity) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`, s.TenantID, uuid.New(), caseRef, compartmentID, injuryID, normalized.Revision, nullableRevision(normalized.ParentRevision), nullableDigest(normalized.ParentDigest), storageDigest(normalized.CanonicalDigest), incidentID, workerID, string(normalized.Kind), medicalID, normalized.Severity)
	if err != nil {
		return fmt.Errorf("safetystore: insert injury: %w", err)
	}
	if affected == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "injury revision identity already exists")
	}
	return nil
}

// GetInjury loads one injury revision.
func (s Store) GetInjury(id string, revision uint64) (safety.InjuryRevision, bool) {
	if s.ready() != nil {
		return safety.InjuryRevision{}, false
	}
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return safety.InjuryRevision{}, false
	}
	var rowID, caseRef, compartmentRef, injuryID, incidentID, workerID uuid.UUID
	var rev int64
	var parentRevision *int64
	var parentDigest *string
	var digest string
	var kind, medical, severity *string
	err = s.Executor.QueryRow(context.Background(), `SELECT row_id,case_ref,compartment_ref,injury_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,worker_ref,kind,medical_evidence_ref::text,severity FROM safety_injury_revision WHERE tenant_id=$1 AND injury_id=$2 AND revision=$3`, s.TenantID, parsedID, revision).Scan(&rowID, &caseRef, &compartmentRef, &injuryID, &rev, &parentRevision, &parentDigest, &digest, &incidentID, &workerID, &kind, &medical, &severity)
	if err != nil {
		return safety.InjuryRevision{}, false
	}
	_ = rowID
	got, err := safety.NewInjuryRevision(safety.InjuryRevision{ID: injuryID.String(), CaseRef: caseRef.String(), CompartmentRef: compartmentRef.String(), Revision: uint64(rev), ParentRevision: uint64(int64Value(parentRevision)), ParentDigest: domainDigest(stringValue(parentDigest)), IncidentRef: incidentID.String(), WorkerRef: workerID.String(), Kind: safety.InjuryKind(stringValue(kind)), MedicalEvidenceRef: stringValue(medical), Severity: stringValue(severity), CanonicalDigest: domainDigest(digest)})
	if err != nil || got.CanonicalDigest != domainDigest(digest) {
		return safety.InjuryRevision{}, false
	}
	return got, true
}

// SaveReportability appends a reportability determination revision.
func (s Store) SaveReportability(in safety.ReportabilityDeterminationRevision) error {
	if err := s.ready(); err != nil {
		return err
	}
	normalized, err := safety.NewReportabilityDeterminationRevision(in)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	caseRef, recordID, err := ids(normalized.CaseRef, normalized.ID)
	if err != nil {
		return err
	}
	compartmentID, err := parseID("compartment_ref", normalized.CompartmentRef)
	if err != nil {
		return err
	}
	incidentID, err := parseID("incident_ref", normalized.IncidentRef)
	if err != nil {
		return err
	}
	if err := s.requireIncident(caseRef, incidentID); err != nil {
		return err
	}
	if err := s.checkRevisionHead("safety_reportability_revision", "reportability_id", caseRef, recordID, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest); err != nil {
		return err
	}
	clock, _ := json.Marshal(string(normalized.Clock))
	affected, err := s.Executor.Exec(context.Background(), `INSERT INTO safety_reportability_revision (tenant_id,row_id,case_ref,compartment_ref,reportability_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,incident_at,class,clock,rule_citation,deadline,rationale) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,$15,$16) ON CONFLICT DO NOTHING`, s.TenantID, uuid.New(), caseRef, compartmentID, recordID, normalized.Revision, nullableRevision(normalized.ParentRevision), nullableDigest(normalized.ParentDigest), storageDigest(normalized.CanonicalDigest), incidentID, normalized.IncidentAt.Time(), string(normalized.Class), string(clock), normalized.RuleCitation, normalized.Deadline.Time(), normalized.Rationale)
	if err != nil {
		return fmt.Errorf("safetystore: insert reportability: %w", err)
	}
	if affected == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "reportability revision identity already exists")
	}
	return nil
}

// GetReportability loads one reportability revision.
func (s Store) GetReportability(id string, revision uint64) (safety.ReportabilityDeterminationRevision, bool) {
	if s.ready() != nil {
		return safety.ReportabilityDeterminationRevision{}, false
	}
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return safety.ReportabilityDeterminationRevision{}, false
	}
	var rowID, caseRef, compartmentRef, recordID, incidentID uuid.UUID
	var rev int64
	var parentRevision *int64
	var parentDigest *string
	var digest, clockJSON string
	var incidentAt, deadline time.Time
	var class, rule, rationale *string
	err = s.Executor.QueryRow(context.Background(), `SELECT row_id,case_ref,compartment_ref,reportability_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,incident_at,class,clock::text,rule_citation,deadline,rationale FROM safety_reportability_revision WHERE tenant_id=$1 AND reportability_id=$2 AND revision=$3`, s.TenantID, parsedID, revision).Scan(&rowID, &caseRef, &compartmentRef, &recordID, &rev, &parentRevision, &parentDigest, &digest, &incidentID, &incidentAt, &class, &clockJSON, &rule, &deadline, &rationale)
	if err != nil {
		return safety.ReportabilityDeterminationRevision{}, false
	}
	_ = rowID
	var clock string
	if json.Unmarshal([]byte(clockJSON), &clock) != nil {
		return safety.ReportabilityDeterminationRevision{}, false
	}
	got, err := safety.NewReportabilityDeterminationRevision(safety.ReportabilityDeterminationRevision{ID: recordID.String(), CaseRef: caseRef.String(), CompartmentRef: compartmentRef.String(), Revision: uint64(rev), ParentRevision: uint64(int64Value(parentRevision)), ParentDigest: domainDigest(stringValue(parentDigest)), IncidentRef: incidentID.String(), IncidentAt: newInstant(incidentAt), Class: safety.ReportabilityClass(stringValue(class)), Clock: safety.ReportabilityClock(clock), RuleCitation: stringValue(rule), Deadline: newInstant(deadline), Rationale: stringValue(rationale), CanonicalDigest: domainDigest(digest)})
	if err != nil || got.CanonicalDigest != domainDigest(digest) {
		return safety.ReportabilityDeterminationRevision{}, false
	}
	return got, true
}

// SaveClaim appends a claims revision and proves its incident belongs to the same case.
func (s Store) SaveClaim(in safety.ClaimRevision) error {
	if err := s.ready(); err != nil {
		return err
	}
	normalized, err := safety.NewClaimRevision(in)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	caseRef, recordID, err := ids(normalized.CaseRef, normalized.ID)
	if err != nil {
		return err
	}
	compartmentID, err := parseID("compartment_ref", normalized.CompartmentRef)
	if err != nil {
		return err
	}
	incidentID, err := parseID("incident_ref", normalized.IncidentRef)
	if err != nil {
		return err
	}
	workerID, err := parseID("worker_ref", normalized.WorkerRef)
	if err != nil {
		return err
	}
	authorityID, err := parseID("authority_ref", normalized.AuthorityRef)
	if err != nil {
		return err
	}
	if err := s.requireIncident(caseRef, incidentID); err != nil {
		return err
	}
	if err := s.checkRevisionHead("safety_claim_revision", "claim_id", caseRef, recordID, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest); err != nil {
		return err
	}
	affected, err := s.Executor.Exec(context.Background(), `INSERT INTO safety_claim_revision (tenant_id,row_id,case_ref,compartment_ref,claim_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,worker_ref,claim_ref,authority_ref,status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`, s.TenantID, uuid.New(), caseRef, compartmentID, recordID, normalized.Revision, nullableRevision(normalized.ParentRevision), nullableDigest(normalized.ParentDigest), storageDigest(normalized.CanonicalDigest), incidentID, workerID, normalized.ClaimRef, authorityID, string(normalized.Status))
	if err != nil {
		return fmt.Errorf("safetystore: insert claim: %w", err)
	}
	if affected == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "claim revision identity already exists")
	}
	return nil
}

// GetClaim loads one claims revision.
func (s Store) GetClaim(id string, revision uint64) (safety.ClaimRevision, bool) {
	if s.ready() != nil {
		return safety.ClaimRevision{}, false
	}
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return safety.ClaimRevision{}, false
	}
	var rowID, caseRef, compartmentRef, recordID, incidentID, workerID, authorityID uuid.UUID
	var rev int64
	var parentRevision *int64
	var parentDigest *string
	var digest string
	var claimRef, status *string
	err = s.Executor.QueryRow(context.Background(), `SELECT row_id,case_ref,compartment_ref,claim_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,worker_ref,claim_ref,authority_ref,status FROM safety_claim_revision WHERE tenant_id=$1 AND claim_id=$2 AND revision=$3`, s.TenantID, parsedID, revision).Scan(&rowID, &caseRef, &compartmentRef, &recordID, &rev, &parentRevision, &parentDigest, &digest, &incidentID, &workerID, &claimRef, &authorityID, &status)
	if err != nil {
		return safety.ClaimRevision{}, false
	}
	_ = rowID
	got, err := safety.NewClaimRevision(safety.ClaimRevision{ID: recordID.String(), CaseRef: caseRef.String(), CompartmentRef: compartmentRef.String(), Revision: uint64(rev), ParentRevision: uint64(int64Value(parentRevision)), ParentDigest: domainDigest(stringValue(parentDigest)), IncidentRef: incidentID.String(), WorkerRef: workerID.String(), ClaimRef: stringValue(claimRef), AuthorityRef: authorityID.String(), Status: safety.ClaimStatus(stringValue(status)), CanonicalDigest: domainDigest(digest)})
	if err != nil || got.CanonicalDigest != domainDigest(digest) {
		return safety.ClaimRevision{}, false
	}
	return got, true
}

// SaveRestriction appends a medical work-restriction revision.
func (s Store) SaveRestriction(in safety.WorkRestrictionRevision) error {
	if err := s.ready(); err != nil {
		return err
	}
	normalized, err := safety.NewWorkRestrictionRevision(in)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	caseRef, recordID, err := ids(normalized.CaseRef, normalized.ID)
	if err != nil {
		return err
	}
	compartmentID, err := parseID("compartment_ref", normalized.CompartmentRef)
	if err != nil {
		return err
	}
	incidentID, err := parseID("incident_ref", normalized.IncidentRef)
	if err != nil {
		return err
	}
	workerID, err := parseID("worker_ref", normalized.WorkerRef)
	if err != nil {
		return err
	}
	medicalID, err := parseID("medical_evidence_ref", normalized.MedicalEvidenceRef)
	if err != nil {
		return err
	}
	if err := s.requireIncident(caseRef, incidentID); err != nil {
		return err
	}
	if err := s.checkRevisionHead("safety_work_restriction_revision", "work_restriction_id", caseRef, recordID, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest); err != nil {
		return err
	}
	affected, err := s.Executor.Exec(context.Background(), `INSERT INTO safety_work_restriction_revision (tenant_id,row_id,case_ref,compartment_ref,work_restriction_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,worker_ref,kind,medical_evidence_ref,status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`, s.TenantID, uuid.New(), caseRef, compartmentID, recordID, normalized.Revision, nullableRevision(normalized.ParentRevision), nullableDigest(normalized.ParentDigest), storageDigest(normalized.CanonicalDigest), incidentID, workerID, string(normalized.Kind), medicalID, string(normalized.Status))
	if err != nil {
		return fmt.Errorf("safetystore: insert restriction: %w", err)
	}
	if affected == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "work restriction revision identity already exists")
	}
	return nil
}

// GetRestriction loads one work-restriction revision.
func (s Store) GetRestriction(id string, revision uint64) (safety.WorkRestrictionRevision, bool) {
	if s.ready() != nil {
		return safety.WorkRestrictionRevision{}, false
	}
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return safety.WorkRestrictionRevision{}, false
	}
	var rowID, caseRef, compartmentRef, recordID, incidentID, workerID uuid.UUID
	var rev int64
	var parentRevision *int64
	var parentDigest *string
	var digest string
	var kind, medical, status *string
	err = s.Executor.QueryRow(context.Background(), `SELECT row_id,case_ref,compartment_ref,work_restriction_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,worker_ref,kind,medical_evidence_ref::text,status FROM safety_work_restriction_revision WHERE tenant_id=$1 AND work_restriction_id=$2 AND revision=$3`, s.TenantID, parsedID, revision).Scan(&rowID, &caseRef, &compartmentRef, &recordID, &rev, &parentRevision, &parentDigest, &digest, &incidentID, &workerID, &kind, &medical, &status)
	if err != nil {
		return safety.WorkRestrictionRevision{}, false
	}
	_ = rowID
	got, err := safety.NewWorkRestrictionRevision(safety.WorkRestrictionRevision{ID: recordID.String(), CaseRef: caseRef.String(), CompartmentRef: compartmentRef.String(), Revision: uint64(rev), ParentRevision: uint64(int64Value(parentRevision)), ParentDigest: domainDigest(stringValue(parentDigest)), IncidentRef: incidentID.String(), WorkerRef: workerID.String(), Kind: safety.RestrictionKind(stringValue(kind)), MedicalEvidenceRef: stringValue(medical), Status: safety.RestrictionStatus(stringValue(status)), CanonicalDigest: domainDigest(digest)})
	if err != nil || got.CanonicalDigest != domainDigest(digest) {
		return safety.WorkRestrictionRevision{}, false
	}
	return got, true
}

// SaveCorrectiveAction appends an operational corrective-action revision.
func (s Store) SaveCorrectiveAction(in safety.CorrectiveActionRevision) error {
	if err := s.ready(); err != nil {
		return err
	}
	normalized, err := safety.NewCorrectiveActionRevision(in)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	caseRef, recordID, err := ids(normalized.CaseRef, normalized.ID)
	if err != nil {
		return err
	}
	compartmentID, err := parseID("compartment_ref", normalized.CompartmentRef)
	if err != nil {
		return err
	}
	incidentID, err := parseID("incident_ref", normalized.IncidentRef)
	if err != nil {
		return err
	}
	ownerID, err := parseID("owner_ref", normalized.OwnerRef)
	if err != nil {
		return err
	}
	var verification any = nil
	if normalized.VerificationEvidenceRef != "" {
		verification, err = parseID("verification_evidence_ref", normalized.VerificationEvidenceRef)
		if err != nil {
			return err
		}
	}
	if err := s.requireIncident(caseRef, incidentID); err != nil {
		return err
	}
	if err := s.checkRevisionHead("safety_corrective_action_revision", "corrective_action_id", caseRef, recordID, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest); err != nil {
		return err
	}
	affected, err := s.Executor.Exec(context.Background(), `INSERT INTO safety_corrective_action_revision (tenant_id,row_id,case_ref,compartment_ref,corrective_action_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,owner_ref,due_rule,verification_evidence_ref,action,status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT DO NOTHING`, s.TenantID, uuid.New(), caseRef, compartmentID, recordID, normalized.Revision, nullableRevision(normalized.ParentRevision), nullableDigest(normalized.ParentDigest), storageDigest(normalized.CanonicalDigest), incidentID, ownerID, normalized.DueRule, verification, normalized.Action, string(normalized.Status))
	if err != nil {
		return fmt.Errorf("safetystore: insert corrective action: %w", err)
	}
	if affected == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "corrective-action revision identity already exists")
	}
	return nil
}

// GetCorrectiveAction loads one corrective-action revision.
func (s Store) GetCorrectiveAction(id string, revision uint64) (safety.CorrectiveActionRevision, bool) {
	if s.ready() != nil {
		return safety.CorrectiveActionRevision{}, false
	}
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return safety.CorrectiveActionRevision{}, false
	}
	var rowID, caseRef, compartmentRef, recordID, incidentID, ownerID uuid.UUID
	var rev int64
	var parentRevision *int64
	var parentDigest *string
	var digest string
	var owner, verification, dueRule, status *string
	var action string
	err = s.Executor.QueryRow(context.Background(), `SELECT row_id,case_ref,compartment_ref,corrective_action_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,owner_ref::text,due_rule,verification_evidence_ref::text,action,status FROM safety_corrective_action_revision WHERE tenant_id=$1 AND corrective_action_id=$2 AND revision=$3`, s.TenantID, parsedID, revision).Scan(&rowID, &caseRef, &compartmentRef, &recordID, &rev, &parentRevision, &parentDigest, &digest, &incidentID, &owner, &dueRule, &verification, &action, &status)
	if err != nil {
		return safety.CorrectiveActionRevision{}, false
	}
	_ = rowID
	if owner == nil {
		return safety.CorrectiveActionRevision{}, false
	}
	ownerID, err = uuid.Parse(*owner)
	if err != nil {
		return safety.CorrectiveActionRevision{}, false
	}
	_ = ownerID
	got, err := safety.NewCorrectiveActionRevision(safety.CorrectiveActionRevision{ID: recordID.String(), CaseRef: caseRef.String(), CompartmentRef: compartmentRef.String(), Revision: uint64(rev), ParentRevision: uint64(int64Value(parentRevision)), ParentDigest: domainDigest(stringValue(parentDigest)), IncidentRef: incidentID.String(), OwnerRef: *owner, DueRule: stringValue(dueRule), VerificationEvidenceRef: stringValue(verification), Action: action, Status: safety.CorrectiveActionStatus(stringValue(status)), CanonicalDigest: domainDigest(digest)})
	if err != nil || got.CanonicalDigest != domainDigest(digest) {
		return safety.CorrectiveActionRevision{}, false
	}
	return got, true
}

func (s Store) requireIncident(caseRef, incidentID uuid.UUID) error {
	var exists bool
	if err := s.Executor.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM safety_incident_revision WHERE tenant_id=$1 AND case_ref=$2 AND incident_id=$3)`, s.TenantID, caseRef, incidentID).Scan(&exists); err != nil {
		return fmt.Errorf("safetystore: check incident reference: %w", err)
	}
	if !exists {
		return refusal(CodeReferenceConflict, ErrReferenceConflict, "incident reference is absent from the same tenant and case")
	}
	return nil
}

func (s Store) checkRevisionHead(table, idColumn string, caseRef, id uuid.UUID, revision, parentRevision uint64, parentDigest, candidateDigest string) error {
	key := s.TenantID.String() + ":" + table + ":" + caseRef.String() + ":" + id.String()
	if _, err := s.Executor.Exec(context.Background(), `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		return fmt.Errorf("safetystore: lock revision head: %w", err)
	}
	if revision == 1 {
		var exists bool
		if err := s.Executor.QueryRow(context.Background(), fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE tenant_id=$1 AND case_ref=$2 AND %s=$3 AND revision=$4)", table, idColumn), s.TenantID, caseRef, id, revision).Scan(&exists); err != nil {
			return fmt.Errorf("safetystore: check duplicate revision: %w", err)
		}
		if exists {
			return refusal(CodeDuplicateRevision, ErrDuplicate, "revision identity already exists")
		}
		return nil
	}
	var latestRevision int64
	var latestDigest string
	err := s.Executor.QueryRow(context.Background(), fmt.Sprintf("SELECT revision,canonical_digest FROM %s WHERE tenant_id=$1 AND case_ref=$2 AND %s=$3 ORDER BY revision DESC LIMIT 1", table, idColumn), s.TenantID, caseRef, id).Scan(&latestRevision, &latestDigest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return refusal(CodeVersionConflict, ErrVersionConflict, "successor has no current parent")
		}
		return fmt.Errorf("safetystore: read revision head: %w", err)
	}
	if uint64(latestRevision) == revision {
		if latestDigest == storageDigest(candidateDigest) {
			return refusal(CodeDuplicateRevision, ErrDuplicate, "revision identity already exists")
		}
		return refusal(CodeVersionConflict, ErrVersionConflict, "revision identity contains a different digest")
	}
	if uint64(latestRevision) != parentRevision || latestDigest != storageDigest(parentDigest) {
		return refusal(CodeVersionConflict, ErrVersionConflict, "successor parent is stale")
	}
	return nil
}

func ids(caseRef, id string) (uuid.UUID, uuid.UUID, error) {
	caseID, err := parseID("case_ref", caseRef)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	recordID, err := parseID("id", id)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return caseID, recordID, nil
}
func parseID(field, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, refusal(CodeInvalid, ErrInvalid, field+" must be a UUID")
	}
	return id, nil
}
func nullableRevision(value uint64) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}
func nullableDigest(value string) any {
	if value == "" {
		return nil
	}
	return storageDigest(value)
}
func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }
func domainDigest(value string) string {
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
func newInstant(value time.Time) values.Instant { return values.NewInstant(value.UTC()) }
