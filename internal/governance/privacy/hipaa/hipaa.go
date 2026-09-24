package hipaa

// REV-099-02: the platform moves ePHI-adjacent records without a business
// associate program on record. This package is the versioned program: every
// subprocessor, the service it provides, and the business associate
// agreement covering it. Flow approval requires a current BAA; an expired
// agreement refuses approval before any record moves.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	// ErrProgramInvalid reports a program record that fails validation.
	ErrProgramInvalid = errors.New("hipaa: business associate program is invalid")
	// ErrBAARequired reports a flow with no BAA reference at all.
	ErrBAARequired = errors.New("hipaa: current business associate agreement is required")
	// ErrBAAExpired reports a flow whose BAA lapsed before the approval time.
	ErrBAAExpired = errors.New("hipaa: business associate agreement has expired")
	// ErrCoverageUnclear reports that HIPAA applicability has not been resolved.
	ErrCoverageUnclear = errors.New("hipaa: coverage applicability is unresolved")
	// ErrHIPAANotApplicable reports that a flow is outside the HIPAA program scope.
	ErrHIPAANotApplicable = errors.New("hipaa: flow is explicitly outside HIPAA scope")
	// ErrScopeExceedsMinimumNecessary reports MEDICAL fields outside the approved scope.
	ErrScopeExceedsMinimumNecessary = errors.New("hipaa: medical scope exceeds minimum necessary")
	// ErrCopyInventoryMismatch reports a missing/stale copy inventory or an unlisted processor.
	ErrCopyInventoryMismatch = errors.New("hipaa: copy inventory does not match the program")
)

// ProgramVersion identifies the program record contract.
const ProgramVersion = 1

// Applicability records why this program is being applied. HIPAA coverage is
// conditional on the health plan/entity being a covered entity or the service
// provider being a business associate (45 CFR 160.103); MEDICAL classification
// alone does not establish statutory coverage. ASSUMED is handled
// conservatively as applicable until a documented determination says otherwise.
type Applicability string

const (
	ApplicabilityConfirmed     Applicability = "CONFIRMED"
	ApplicabilityAssumed       Applicability = "ASSUMED"
	ApplicabilityUnresolved    Applicability = "UNRESOLVED"
	ApplicabilityNotApplicable Applicability = "NOT_APPLICABLE"
)

// EntityRole records the HIPAA relationship whose breach obligations apply.
type EntityRole string

const (
	EntityRoleCoveredEntity                  EntityRole = "COVERED_ENTITY"
	EntityRoleBusinessAssociate              EntityRole = "BUSINESS_ASSOCIATE"
	EntityRoleBusinessAssociateSubcontractor EntityRole = "BUSINESS_ASSOCIATE_SUBCONTRACTOR"
)

// Valid reports whether role is a declared HIPAA relationship.
func (role EntityRole) Valid() bool {
	return role == EntityRoleCoveredEntity || role == EntityRoleBusinessAssociate || role == EntityRoleBusinessAssociateSubcontractor
}

// MinimumNecessaryScope is an explicit allowlist of MEDICAL-classified fields.
// It deliberately has no wildcard or inherited PII scope.
type MinimumNecessaryScope struct {
	Version string   `json:"version"`
	Fields  []string `json:"fields"`
}

// CopyInventory is the processor view of RECORDS-COPY-001's current census.
// Digest, CertifiedAt, and CertifiedCopyCount must come from
// privacymeta.CertifyCopyInventory; its processor and field rows must be read
// in the same tenant-scoped REPEATABLE READ transaction. This package checks
// the certificate binding metadata, freshness, completeness, and scopes.
type CopyInventory struct {
	TenantID           string          `json:"tenant_id"`
	Version            string          `json:"version"`
	Digest             string          `json:"digest"`
	Watermark          string          `json:"watermark"`
	AsOf               time.Time       `json:"as_of"`
	CertifiedAt        time.Time       `json:"certified_at"`
	CertifiedCopyCount int             `json:"certified_copy_count"`
	Complete           bool            `json:"complete"`
	Processors         []CopyProcessor `json:"processors"`
}

// CopyProcessor is one processor and field scope found in a copy inventory.
type CopyProcessor struct {
	CopyID       string   `json:"copy_id,omitempty"`
	Subprocessor string   `json:"subprocessor"`
	DataCategory string   `json:"data_category"`
	Fields       []string `json:"fields"`
}

// BusinessAssociateAgreement is one BAA with one subprocessor.
type BusinessAssociateAgreement struct {
	// Subprocessor names the receiving party.
	Subprocessor string `json:"subprocessor"`
	// Service names what the subprocessor does with ePHI-adjacent records.
	Service string `json:"service"`
	// Reference is the agreement identifier, e.g. "BAA-2026-0042".
	Reference string `json:"reference"`
	// EffectiveFrom is when coverage starts.
	EffectiveFrom time.Time `json:"effective_from"`
	// EffectiveTo is when coverage ends; the zero time means no expiry.
	EffectiveTo time.Time `json:"effective_to"`
	// CopyReference and CopyDigest locate the current, reviewed agreement copy.
	CopyReference string    `json:"copy_reference"`
	CopyDigest    string    `json:"copy_digest"`
	ReviewedAt    time.Time `json:"reviewed_at"`
	// MedicalFields is the agreement-specific permitted MEDICAL field subset.
	MedicalFields []string `json:"medical_fields"`
}

// Covers reports whether the agreement covers the approval instant.
func (a BusinessAssociateAgreement) Covers(at time.Time) bool {
	if at.Before(a.EffectiveFrom) || at.Before(a.ReviewedAt) {
		return false
	}
	return a.EffectiveTo.IsZero() || !at.After(a.EffectiveTo)
}

// HIPAABusinessAssociateProgram is the versioned program record.
type HIPAABusinessAssociateProgram struct {
	// Version pins the program contract.
	Version int `json:"version"`
	// Agreements cover every subprocessor that touches ePHI-adjacent data.
	Agreements []BusinessAssociateAgreement `json:"agreements"`
	// Applicability is CONFIRMED or ASSUMED. UNRESOLVED fails closed.
	Applicability Applicability `json:"applicability"`
	// MinimumNecessary is the explicit program-wide MEDICAL field allowlist.
	MinimumNecessary MinimumNecessaryScope `json:"minimum_necessary"`
}

// Validate checks the program record: versioned, every agreement named with
// a service and a reference, no duplicate subprocessors.
func (p HIPAABusinessAssociateProgram) Validate() error {
	if p.Version != ProgramVersion {
		return fmt.Errorf("%w: program version %d, want %d", ErrProgramInvalid, p.Version, ProgramVersion)
	}
	if p.Applicability != ApplicabilityConfirmed && p.Applicability != ApplicabilityAssumed && p.Applicability != ApplicabilityUnresolved && p.Applicability != ApplicabilityNotApplicable {
		return fmt.Errorf("%w: invalid applicability %q", ErrProgramInvalid, p.Applicability)
	}
	if strings.TrimSpace(p.MinimumNecessary.Version) == "" || !validFields(p.MinimumNecessary.Fields) {
		return fmt.Errorf("%w: minimum-necessary field allowlist must be versioned and explicit", ErrProgramInvalid)
	}
	seen := make(map[string]bool, len(p.Agreements))
	for i, agreement := range p.Agreements {
		if strings.TrimSpace(agreement.Subprocessor) == "" || strings.TrimSpace(agreement.Service) == "" || strings.TrimSpace(agreement.Reference) == "" || strings.TrimSpace(agreement.CopyReference) == "" || !validDigest(agreement.CopyDigest) || agreement.ReviewedAt.IsZero() {
			return fmt.Errorf("%w: agreement %d lacks subprocessor, service, reference or current-copy evidence", ErrProgramInvalid, i)
		}
		if agreement.Subprocessor != strings.TrimSpace(agreement.Subprocessor) || agreement.Reference != strings.TrimSpace(agreement.Reference) || agreement.CopyReference != strings.TrimSpace(agreement.CopyReference) || agreement.CopyDigest != strings.TrimSpace(agreement.CopyDigest) {
			return fmt.Errorf("%w: agreement %d has a non-canonical identifier", ErrProgramInvalid, i)
		}
		if !validFields(agreement.MedicalFields) || !subset(agreement.MedicalFields, p.MinimumNecessary.Fields) {
			return fmt.Errorf("%w: agreement %q has invalid or excessive MEDICAL fields", ErrProgramInvalid, agreement.Reference)
		}
		if !agreement.EffectiveTo.IsZero() && !agreement.EffectiveTo.After(agreement.EffectiveFrom) {
			return fmt.Errorf("%w: agreement %q ends before it starts", ErrProgramInvalid, agreement.Reference)
		}
		if seen[agreement.Subprocessor] {
			return fmt.Errorf("%w: duplicate subprocessor %q", ErrProgramInvalid, agreement.Subprocessor)
		}
		seen[agreement.Subprocessor] = true
	}
	return nil
}

// FlowApproval admits one record flow to a subprocessor at an instant.
type FlowApproval struct {
	// Subprocessor is the receiving party.
	Subprocessor string `json:"subprocessor"`
	// Reference is the BAA the flow travels under.
	Reference string `json:"reference"`
	// ProgramVersion pins the program the approval was decided under.
	ProgramVersion int `json:"program_version"`
}

// approveCurrentBAA resolves a current BAA after MEDICAL copy and scope checks.
func approveCurrentBAA(program HIPAABusinessAssociateProgram, subprocessor string, at time.Time) (FlowApproval, error) {
	if err := program.Validate(); err != nil {
		return FlowApproval{}, err
	}
	if program.Applicability == ApplicabilityUnresolved {
		return FlowApproval{}, ErrCoverageUnclear
	}
	for _, agreement := range program.Agreements {
		if agreement.Subprocessor != subprocessor {
			continue
		}
		if !agreement.Covers(at) {
			return FlowApproval{}, fmt.Errorf("%w: %q does not cover approval time %s", ErrBAAExpired, agreement.Reference, at.Format(time.DateOnly))
		}
		return FlowApproval{Subprocessor: subprocessor, Reference: agreement.Reference, ProgramVersion: program.Version}, nil
	}
	return FlowApproval{}, fmt.Errorf("%w: no agreement names %q", ErrBAARequired, subprocessor)
}

// ValidateMedicalFlow cross-checks the current copy inventory against the
// BAA program and enforces a strict, explicit minimum-necessary MEDICAL scope.
// An assumed HIPAA relationship is conservatively subject to the same controls.
func ValidateMedicalFlow(program HIPAABusinessAssociateProgram, inventory CopyInventory, requestedFields []string, at time.Time, maxInventoryAge time.Duration) error {
	if err := program.Validate(); err != nil {
		return err
	}
	if program.Applicability == ApplicabilityUnresolved {
		return ErrCoverageUnclear
	}
	if program.Applicability == ApplicabilityNotApplicable {
		return ErrHIPAANotApplicable
	}
	if strings.TrimSpace(inventory.TenantID) == "" || inventory.Version == "" || !validDigest(inventory.Digest) || strings.TrimSpace(inventory.Watermark) == "" || !inventory.Complete || inventory.AsOf.IsZero() || inventory.CertifiedAt.IsZero() || inventory.CertifiedCopyCount != len(inventory.Processors) || at.IsZero() || maxInventoryAge <= 0 || inventory.AsOf.After(at) || inventory.CertifiedAt.After(at) || at.Sub(inventory.AsOf) > maxInventoryAge || at.Sub(inventory.CertifiedAt) > maxInventoryAge || len(inventory.Processors) == 0 {
		return fmt.Errorf("%w: inventory is absent, incomplete or stale", ErrCopyInventoryMismatch)
	}
	if !validFields(requestedFields) || !subset(requestedFields, program.MinimumNecessary.Fields) {
		return ErrScopeExceedsMinimumNecessary
	}
	seen := make(map[string]bool, len(inventory.Processors))
	for _, processor := range inventory.Processors {
		key := processor.CopyID
		if key == "" {
			key = processor.Subprocessor + "|" + processor.DataCategory
		}
		if strings.TrimSpace(processor.Subprocessor) == "" || processor.Subprocessor != strings.TrimSpace(processor.Subprocessor) || strings.TrimSpace(processor.DataCategory) == "" || seen[key] || !validFields(processor.Fields) {
			return fmt.Errorf("%w: invalid or duplicate processor entry", ErrCopyInventoryMismatch)
		}
		seen[key] = true
		if processor.DataCategory != "MEDICAL" {
			continue
		}
		agreement, ok := agreementFor(program, processor.Subprocessor)
		if !ok {
			return fmt.Errorf("%w: copy inventory processor %s has no BAA", ErrCopyInventoryMismatch, processor.Subprocessor)
		}
		if !agreement.Covers(at) {
			return fmt.Errorf("%w: %q does not cover approval time", ErrBAAExpired, agreement.Reference)
		}
		if !subset(processor.Fields, agreement.MedicalFields) || !subset(processor.Fields, program.MinimumNecessary.Fields) {
			return fmt.Errorf("%w: copy inventory fields for %s exceed BAA scope", ErrScopeExceedsMinimumNecessary, processor.Subprocessor)
		}
	}
	return nil
}

func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && value == strings.ToLower(value)
}

// approveMedicalFlow performs flow checks after the caller has supplied a
// certified tenant inventory. Public approvals must use the sealed wrapper in
// copy_inventory.go so arbitrary caller data cannot stand in for a certificate.
func approveMedicalFlow(program HIPAABusinessAssociateProgram, inventory CopyInventory, subprocessor string, requestedFields []string, at time.Time, maxInventoryAge time.Duration) (FlowApproval, error) {
	if err := program.Validate(); err != nil {
		return FlowApproval{}, err
	}
	if program.Applicability == ApplicabilityUnresolved {
		return FlowApproval{}, ErrCoverageUnclear
	}
	if _, ok := agreementFor(program, subprocessor); !ok {
		return FlowApproval{}, fmt.Errorf("%w: no agreement names %q", ErrBAARequired, subprocessor)
	}
	if err := ValidateMedicalFlow(program, inventory, requestedFields, at, maxInventoryAge); err != nil {
		return FlowApproval{}, err
	}
	var selectedMedicalProcessor bool
	for _, processor := range inventory.Processors {
		if processor.Subprocessor == subprocessor && processor.DataCategory == "MEDICAL" {
			selectedMedicalProcessor = true
			if !subset(requestedFields, processor.Fields) {
				return FlowApproval{}, fmt.Errorf("%w: requested fields are absent from the selected processor copy scope", ErrScopeExceedsMinimumNecessary)
			}
		}
	}
	if !selectedMedicalProcessor {
		return FlowApproval{}, fmt.Errorf("%w: selected subprocessor has no MEDICAL copy", ErrCopyInventoryMismatch)
	}
	return approveCurrentBAA(program, subprocessor, at)
}

func agreementFor(program HIPAABusinessAssociateProgram, subprocessor string) (BusinessAssociateAgreement, bool) {
	for _, agreement := range program.Agreements {
		if agreement.Subprocessor == subprocessor {
			return agreement, true
		}
	}
	return BusinessAssociateAgreement{}, false
}

func validFields(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		if field == "" || field != strings.TrimSpace(field) || field == "*" || seen[field] {
			return false
		}
		seen[field] = true
	}
	return true
}

func subset(fields, allowed []string) bool {
	set := make(map[string]bool, len(allowed))
	for _, field := range allowed {
		set[field] = true
	}
	for _, field := range fields {
		if !set[field] {
			return false
		}
	}
	return true
}

// CanonicalProgram renders the program one agreement-per-line for the golden
// pin.
func CanonicalProgram(program HIPAABusinessAssociateProgram) string {
	agreements := append([]BusinessAssociateAgreement(nil), program.Agreements...)
	sort.Slice(agreements, func(i, j int) bool { return agreements[i].Subprocessor < agreements[j].Subprocessor })
	lines := make([]string, 0, len(agreements))
	for _, agreement := range agreements {
		until := "no-expiry"
		if !agreement.EffectiveTo.IsZero() {
			until = agreement.EffectiveTo.Format(time.DateOnly)
		}
		fields := append([]string(nil), agreement.MedicalFields...)
		sort.Strings(fields)
		lines = append(lines, agreement.Subprocessor+" :: "+agreement.Service+" :: "+agreement.Reference+" :: "+agreement.EffectiveFrom.Format(time.DateOnly)+" :: "+until+" :: "+agreement.CopyReference+" :: "+agreement.CopyDigest+" :: reviewed="+agreement.ReviewedAt.Format(time.DateOnly)+" :: "+strings.Join(fields, ","))
	}
	scope := append([]string(nil), program.MinimumNecessary.Fields...)
	sort.Strings(scope)
	return "applicability=" + string(program.Applicability) + "; minimum_necessary=" + program.MinimumNecessary.Version + "[" + strings.Join(scope, ",") + "]\n" + strings.Join(lines, "\n")
}
