// Package selectedjurisdiction contains cross-slice semantic conformance
// checks. It deliberately knows nothing about workflow execution or legal
// rules: those remain owned by governance/legal and are supplied as a pinned
// LegalContext.
package selectedjurisdiction

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServingContractID identifies the cross-slice jurisdiction contract
// composed by the shipped application cell.
const ServingContractID = "hcmnext.conformance.selected-jurisdiction/v1"

// ValidateServingContract proves both slices use a verified legal context and
// that an explicit ambiguity cannot produce an allowed result.
func ValidateServingContract() error {
	pack, err := legal.CaliforniaPromotionPack()
	if err != nil {
		return fmt.Errorf("selected jurisdiction: serving contract pack: %w", err)
	}
	registry := legal.NewRegistry()
	if err := registry.Register(pack); err != nil {
		return fmt.Errorf("selected jurisdiction: serving contract registry: %w", err)
	}
	signer, err := legal.NewSigner(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)))
	if err != nil {
		return fmt.Errorf("selected jurisdiction: serving contract signer: %w", err)
	}
	effectiveDate, err := values.NewLocalDate(2026, time.March, 1)
	if err != nil {
		return fmt.Errorf("selected jurisdiction: serving contract effective date: %w", err)
	}
	knownInstant, err := values.NewInstantFromUnix(1770000000, 0)
	if err != nil {
		return fmt.Errorf("selected jurisdiction: serving contract known-at: %w", err)
	}
	knownAt, err := values.NewKnownAt(knownInstant)
	if err != nil {
		return fmt.Errorf("selected jurisdiction: serving contract known-at wrapper: %w", err)
	}
	recordedAt, err := values.NewInstantFromUnix(1770100000, 0)
	if err != nil {
		return fmt.Errorf("selected jurisdiction: serving contract recorded-at: %w", err)
	}
	context, err := legal.Resolve(legal.LegalContextInput{
		LegalEntityID:          "serving-contract",
		WorkLocation:           legal.Jurisdiction{Country: "US", State: "CA"},
		EmploymentJurisdiction: legal.Jurisdiction{Country: "US", State: "CA"},
		EffectiveDate:          effectiveDate,
		KnownAt:                knownAt,
	}, registry, signer, recordedAt)
	if err != nil {
		return fmt.Errorf("selected jurisdiction: serving contract context: %w", err)
	}
	for _, slice := range []Slice{Promotion, MedicalLeave} {
		allowed, err := Evaluate(Request{Slice: slice, Context: context})
		if err != nil || allowed.Status != Allowed || allowed.CompositionDigest == "" {
			return fmt.Errorf("selected jurisdiction: serving contract allowed %s = %+v: %v", slice, allowed, err)
		}
		blocked, err := Evaluate(Request{Slice: slice, Context: context, Ambiguous: true})
		if err != nil || blocked.Status != Blocked || blocked.StaleEffects != 0 {
			return fmt.Errorf("selected jurisdiction: serving contract ambiguous %s = %+v: %v", slice, blocked, err)
		}
	}
	return nil
}

// Slice identifies a product slice covered by this conformance contract.
type Slice string

const (
	Promotion    Slice = "PROMOTION"
	MedicalLeave Slice = "MEDICAL_LEAVE"
)

// Status is the fail-closed cross-slice disposition.
type Status string

const (
	Allowed        Status = "ALLOWED"
	Blocked        Status = "BLOCKED"
	ReviewRequired Status = "REVIEW_REQUIRED"
	ReplanRequired Status = "REPLAN_REQUIRED"
)

var (
	ErrMissingContext = errors.New("selected jurisdiction: legal context is required")
	ErrUnknownSlice   = errors.New("selected jurisdiction: unsupported slice")
)

// Request describes one semantic evaluation. Context is immutable and must be
// resolved by legal.Resolve; callers cannot provide a jurisdiction string as a
// substitute. Previous is the context used for an earlier proposal, if any.
type Request struct {
	Slice          Slice
	Context        *legal.LegalContext
	Previous       *legal.LegalContext
	Ambiguous      bool
	RuleChange     bool
	MaterialChange bool
}

// Result is deterministic evidence shared by Promotion and Medical Leave.
// SuccessorProposal is present for a material replan and StaleEffects is
// always zero: a stale proposal can never create effects.
type Result struct {
	Status               Status
	Slice                Slice
	SelectedJurisdiction legal.Jurisdiction
	RulePackReleases     []legal.RulePackRelease
	CompositionDigest    string
	SuccessorProposal    string
	StaleEffects         int
}

// Evaluate enforces the common selected-jurisdiction semantics. Ambiguity is
// explicit input because resolution itself is owned by governance/legal; the
// evaluator never guesses a jurisdiction.
func Evaluate(req Request) (Result, error) {
	if req.Slice != Promotion && req.Slice != MedicalLeave {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownSlice, req.Slice)
	}
	if req.Context == nil {
		return Result{Status: Blocked, Slice: req.Slice}, ErrMissingContext
	}
	if err := req.Context.Verify(); err != nil {
		return Result{Status: Blocked, Slice: req.Slice}, fmt.Errorf("%w: context verification: %v", ErrMissingContext, err)
	}
	if req.Ambiguous {
		// No selected jurisdiction is released when the caller reports
		// ambiguity; retaining one here would make a blocked result look usable.
		return Result{Status: Blocked, Slice: req.Slice}, nil
	}
	result := base(req, Allowed)
	if req.Previous != nil && !sameReleases(req.Previous.RulePackReleases(), result.RulePackReleases) {
		req.RuleChange = true
	}
	if req.RuleChange || req.MaterialChange {
		result.Status = ReplanRequired
		result.SuccessorProposal = successor(result)
		return result, nil
	}
	if req.Context.Confidence() == legal.ConfidenceAsserted {
		result.Status = ReviewRequired
	}
	return result, nil
}

func base(req Request, status Status) Result {
	r := Result{Status: status, Slice: req.Slice, SelectedJurisdiction: req.Context.Jurisdiction(), RulePackReleases: req.Context.RulePackReleases()}
	r.CompositionDigest = digest(req.Slice, r.SelectedJurisdiction, r.RulePackReleases)
	return r
}

func sameReleases(a, b []legal.RulePackRelease) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func digest(slice Slice, jurisdiction legal.Jurisdiction, releases []legal.RulePackRelease) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|", slice, jurisdiction)
	for _, r := range releases {
		fmt.Fprintf(&b, "%s:%d.%d:%s|", r.PackID, r.Version, r.MinorVersion, r.Jurisdiction)
	}
	s := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(s[:])
}

func successor(r Result) string {
	s := sha256.Sum256([]byte("successor|" + r.CompositionDigest))
	return "proposal-" + hex.EncodeToString(s[:8])
}
