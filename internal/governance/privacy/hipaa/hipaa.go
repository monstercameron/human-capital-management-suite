package hipaa

// REV-099-02: the platform moves ePHI-adjacent records without a business
// associate program on record. This package is the versioned program: every
// subprocessor, the service it provides, and the business associate
// agreement covering it. Flow approval requires a current BAA; an expired
// agreement refuses approval before any record moves.

import (
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
)

// ProgramVersion identifies the program record contract.
const ProgramVersion = 1

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
}

// Covers reports whether the agreement covers the approval instant.
func (a BusinessAssociateAgreement) Covers(at time.Time) bool {
	if at.Before(a.EffectiveFrom) {
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
}

// Validate checks the program record: versioned, every agreement named with
// a service and a reference, no duplicate subprocessors.
func (p HIPAABusinessAssociateProgram) Validate() error {
	if p.Version != ProgramVersion {
		return fmt.Errorf("%w: program version %d, want %d", ErrProgramInvalid, p.Version, ProgramVersion)
	}
	seen := make(map[string]bool, len(p.Agreements))
	for i, agreement := range p.Agreements {
		if strings.TrimSpace(agreement.Subprocessor) == "" || strings.TrimSpace(agreement.Service) == "" || strings.TrimSpace(agreement.Reference) == "" {
			return fmt.Errorf("%w: agreement %d names no subprocessor, service or reference", ErrProgramInvalid, i)
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

// ApproveFlow admits a flow only under a current BAA naming the
// subprocessor. An expired agreement refuses; a missing reference refuses
// before any record moves.
func ApproveFlow(program HIPAABusinessAssociateProgram, subprocessor string, at time.Time) (FlowApproval, error) {
	if err := program.Validate(); err != nil {
		return FlowApproval{}, err
	}
	for _, agreement := range program.Agreements {
		if agreement.Subprocessor != subprocessor {
			continue
		}
		if !agreement.Covers(at) {
			return FlowApproval{}, fmt.Errorf("%w: %q lapsed %s", ErrBAAExpired, agreement.Reference, agreement.EffectiveTo.Format(time.DateOnly))
		}
		return FlowApproval{Subprocessor: subprocessor, Reference: agreement.Reference, ProgramVersion: program.Version}, nil
	}
	return FlowApproval{}, fmt.Errorf("%w: no agreement names %q", ErrBAARequired, subprocessor)
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
		lines = append(lines, agreement.Subprocessor+" :: "+agreement.Service+" :: "+agreement.Reference+" :: "+agreement.EffectiveFrom.Format(time.DateOnly)+" :: "+until)
	}
	return strings.Join(lines, "\n")
}
