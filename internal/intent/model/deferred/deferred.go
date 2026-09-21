// Package deferred encodes the review-only source vocabulary for DB-016's
// deferred domains. It is a model registry, not a command or persistence
// surface: publishing one of these rows never grants a write capability.
package deferred

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

// DB016PreviewDigest is the digest of the complete, generated DB-016 preview
// set: the ten migration previews and storage-disposition.deferred.yaml.
// The golden test cross-checks this value against tools/gen/deferredschema.
const DB016PreviewDigest = "sha256:d7ebdf78cecd22a8a6a19aec97cc1cb81208e60b3ca7fa2bfb72fc11e527c1a4"

// ErrWriteAuthority classifies a deferred source that would expose a
// production mutation capability.
var ErrWriteAuthority = errors.New("deferred source set: write authority")

// AuthorityClass describes who will own authoritative writes when a deferred
// domain is eventually funded. Neither class is a production write grant.
type AuthorityClass string

const (
	AuthorityNativeDeferred        AuthorityClass = "NATIVE_DEFERRED"
	AuthorityExternalMastered      AuthorityClass = "EXTERNAL_MASTERED"
	AuthorityClassNativeDeferred                  = AuthorityNativeDeferred
	AuthorityClassExternalMastered                = AuthorityExternalMastered
)

func (a AuthorityClass) Valid() bool {
	return a == AuthorityNativeDeferred || a == AuthorityExternalMastered
}

// SourceSystemClass is the bounded class of system from which the deferred
// domain's source vocabulary is expected to arrive.
type SourceSystemClass string

const (
	SourcePayrollProcessor      SourceSystemClass = "PAYROLL_PROCESSOR"
	SourceBenefitsAdministrator SourceSystemClass = "BENEFITS_ADMINISTRATOR"
	SourceTimekeepingSystem     SourceSystemClass = "TIMEKEEPING_SYSTEM"
	SourceLeavePlatform         SourceSystemClass = "LEAVE_PLATFORM"
	SourceApplicantTracking     SourceSystemClass = "APPLICANT_TRACKING_SYSTEM"
	SourceTalentPlatform        SourceSystemClass = "TALENT_PLATFORM"
	SourceLearningManagement    SourceSystemClass = "LEARNING_MANAGEMENT_SYSTEM"
	SourceCaseManagement        SourceSystemClass = "CASE_MANAGEMENT_SYSTEM"
	SourceIdentityAccess        SourceSystemClass = "IDENTITY_ACCESS_SYSTEM"
	SourceRegulatoryAuthority   SourceSystemClass = "REGULATORY_AUTHORITY"

	SourceSystemPayroll    = SourcePayrollProcessor
	SourceSystemBenefits   = SourceBenefitsAdministrator
	SourceSystemTime       = SourceTimekeepingSystem
	SourceSystemLeave      = SourceLeavePlatform
	SourceSystemRecruiting = SourceApplicantTracking
	SourceSystemTalent     = SourceTalentPlatform
	SourceSystemLearning   = SourceLearningManagement
	SourceSystemCase       = SourceCaseManagement
	SourceSystemAccess     = SourceIdentityAccess
	SourceSystemRegulatory = SourceRegulatoryAuthority
)

func (s SourceSystemClass) Valid() bool {
	switch s {
	case SourcePayrollProcessor, SourceBenefitsAdministrator, SourceTimekeepingSystem,
		SourceLeavePlatform, SourceApplicantTracking, SourceTalentPlatform,
		SourceLearningManagement, SourceCaseManagement, SourceIdentityAccess,
		SourceRegulatoryAuthority:
		return true
	default:
		return false
	}
}

// EntityBinding links one typed model entity to the representative DB-016
// preview table that mirrors it.
type EntityBinding struct {
	Ref          model.EntityRef
	PreviewTable string
}

func (e EntityBinding) Validate() error {
	if err := e.Ref.Validate(); err != nil {
		return fmt.Errorf("entity %s: %w", e.Ref.String(), err)
	}
	if strings.TrimSpace(e.PreviewTable) == "" {
		return errors.New("preview table is required")
	}
	return nil
}

// DomainSource is one deferred domain's source-authority declaration.
type DomainSource struct {
	Domain        string
	Entities      []EntityBinding
	SourceSystem  SourceSystemClass
	Authority     AuthorityClass
	AuthorityRef  string
	PreviewDigest string
}

// DeferredDomain is the descriptive alias used by callers that want to make
// the non-authoritative status explicit at the call site.
type DeferredDomain = DomainSource

func (d DomainSource) Validate() error {
	if strings.TrimSpace(d.Domain) == "" {
		return errors.New("deferred source domain is required")
	}
	if !d.SourceSystem.Valid() {
		return fmt.Errorf("%s: invalid source system class %q", d.Domain, d.SourceSystem)
	}
	if !d.Authority.Valid() {
		return fmt.Errorf("%s: invalid authority class %q", d.Domain, d.Authority)
	}
	if strings.TrimSpace(d.AuthorityRef) == "" {
		return fmt.Errorf("%s: authority reference is required", d.Domain)
	}
	if d.PreviewDigest != DB016PreviewDigest || !validDigest(d.PreviewDigest) {
		return fmt.Errorf("%s: DB-016 preview digest is not the pinned digest", d.Domain)
	}
	if len(d.Entities) != 2 {
		return fmt.Errorf("%s: want exactly two preview entities, got %d", d.Domain, len(d.Entities))
	}
	seen := make(map[string]struct{}, len(d.Entities))
	for _, entity := range d.Entities {
		if err := entity.Validate(); err != nil {
			return fmt.Errorf("%s: %w", d.Domain, err)
		}
		key := entity.Ref.String()
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%s: duplicate entity %s", d.Domain, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validDigest(s string) bool {
	if len(s) != len("sha256:")+64 || !strings.HasPrefix(s, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(s, "sha256:"))
	return err == nil
}

// Sources returns the complete DB-016 set in its stable domain order.
func Sources() []DomainSource {
	return []DomainSource{
		{Domain: "payroll", Entities: entities("PayrollRun", "payroll_run", "PayrollLedgerEntry", "payroll_ledger_entry"), SourceSystem: SourcePayrollProcessor, Authority: AuthorityExternalMastered, AuthorityRef: "authority.payroll_external/v1", PreviewDigest: DB016PreviewDigest},
		{Domain: "benefits", Entities: entities("BenefitElection", "benefit_election", "BenefitElectionRevision", "benefit_election_revision"), SourceSystem: SourceBenefitsAdministrator, Authority: AuthorityExternalMastered, AuthorityRef: "authority.benefits_external/v1", PreviewDigest: DB016PreviewDigest},
		{Domain: "time", Entities: entities("Timecard", "timecard", "TimecardRevision", "timecard_revision"), SourceSystem: SourceTimekeepingSystem, Authority: AuthorityExternalMastered, AuthorityRef: "authority.time_external/v1", PreviewDigest: DB016PreviewDigest},
		{Domain: "leave", Entities: entities("LeaveRequest", "leave_request_preview", "LeaveRecord", "leave_record_preview"), SourceSystem: SourceLeavePlatform, Authority: AuthorityNativeDeferred, AuthorityRef: "authority.leave_native_deferred/v1", PreviewDigest: DB016PreviewDigest},
		{Domain: "recruiting", Entities: entities("Requisition", "requisition", "RequisitionRevision", "requisition_revision"), SourceSystem: SourceApplicantTracking, Authority: AuthorityExternalMastered, AuthorityRef: "authority.recruiting_external/v1", PreviewDigest: DB016PreviewDigest},
		{Domain: "talent", Entities: entities("PerformanceReview", "performance_review", "PerformanceReviewRevision", "performance_review_revision"), SourceSystem: SourceTalentPlatform, Authority: AuthorityNativeDeferred, AuthorityRef: "authority.talent_native_deferred/v1", PreviewDigest: DB016PreviewDigest},
		{Domain: "learning", Entities: entities("LearningEnrollment", "learning_enrollment", "LearningCompletion", "learning_completion"), SourceSystem: SourceLearningManagement, Authority: AuthorityExternalMastered, AuthorityRef: "authority.learning_external/v1", PreviewDigest: DB016PreviewDigest},
		{Domain: "case", Entities: entities("Case", "hr_case", "CaseTransition", "case_transition"), SourceSystem: SourceCaseManagement, Authority: AuthorityNativeDeferred, AuthorityRef: "authority.case_native_deferred/v1", PreviewDigest: DB016PreviewDigest},
		{Domain: "access", Entities: entities("AccessGrant", "access_grant", "AccessOperation", "access_operation"), SourceSystem: SourceIdentityAccess, Authority: AuthorityExternalMastered, AuthorityRef: "authority.access_external/v1", PreviewDigest: DB016PreviewDigest},
		{Domain: "regulatory", Entities: entities("GovernmentFiling", "government_filing", "FilingSubmissionAttempt", "filing_submission_attempt"), SourceSystem: SourceRegulatoryAuthority, Authority: AuthorityExternalMastered, AuthorityRef: "authority.regulatory_external/v1", PreviewDigest: DB016PreviewDigest},
	}
}

// Catalog is the model-registry spelling for Sources.
func Catalog() []DomainSource { return Sources() }

// DeferredSources is an explicit spelling for callers that want to emphasize
// that this catalog is not a live authority registry.
func DeferredSources() []DomainSource { return Sources() }

func entities(name1, table1, name2, table2 string) []EntityBinding {
	return []EntityBinding{
		{Ref: model.EntityRef{Name: name1, Version: 1}, PreviewTable: table1},
		{Ref: model.EntityRef{Name: name2, Version: 1}, PreviewTable: table2},
	}
}

// Validate checks the complete typed set and its stable identity.
func Validate(sources []DomainSource) error {
	if len(sources) != 10 {
		return fmt.Errorf("deferred source set: want 10 domains, got %d", len(sources))
	}
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if err := source.Validate(); err != nil {
			return err
		}
		if _, ok := seen[source.Domain]; ok {
			return fmt.Errorf("deferred source set: duplicate domain %q", source.Domain)
		}
		seen[source.Domain] = struct{}{}
	}
	return nil
}

// ValidateCatalog validates the complete source set and the compiled-in
// capability registry in one pure check.
func ValidateCatalog() error {
	if err := Validate(Sources()); err != nil {
		return err
	}
	return ValidateBootstrapNoWriteAuthority()
}

// ValidateNoWriteAuthority proves that the supplied registered capabilities
// cannot publish a mutation for the deferred vocabulary. It inspects only
// registry values; it never registers, changes, or persists a capability.
func ValidateNoWriteAuthority(reg *capability.Registry) error {
	if reg == nil {
		return errors.New("deferred source set: capability registry is nil")
	}
	for _, record := range reg.List() {
		definition := record.Definition
		if definition.EffectClass.IsWrite() {
			return fmt.Errorf("%w: capability %s/%d has write effect %s", ErrWriteAuthority, definition.ID, definition.Version, definition.EffectClass)
		}
		if len(definition.WriteData.DataDomains) != 0 || len(definition.WriteData.FieldPaths) != 0 {
			return fmt.Errorf("%w: capability %s/%d declares WriteData", ErrWriteAuthority, definition.ID, definition.Version)
		}
	}
	return nil
}

// ValidateBootstrapNoWriteAuthority applies the authority check to the
// compiled-in Phase 1 capability registry.
func ValidateBootstrapNoWriteAuthority() error {
	reg, err := capability.NewBootstrapRegistry()
	if err != nil {
		return fmt.Errorf("build bootstrap capability registry: %w", err)
	}
	return ValidateNoWriteAuthority(reg)
}

// Canonical returns deterministic JSON for a source set, or nil when invalid.
func Canonical(sources []DomainSource) []byte {
	if err := Validate(sources); err != nil {
		return nil
	}
	b, err := json.Marshal(sources)
	if err != nil {
		return nil
	}
	return b
}

// Digest returns the stable identity of a valid encoded source set.
func Digest(sources []DomainSource) string {
	b := Canonical(sources)
	if b == nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain describes the set without granting an execution or write path.
func Explain() string {
	return "DB-016 deferred source vocabulary: ten typed domains, DRAFT/CONFORMANCE preview digest, and no registered write authority"
}
