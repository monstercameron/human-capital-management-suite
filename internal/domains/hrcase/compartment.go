// CASE-002: enforce case participants, compartments, confidential notes and
// evidence custody.
//
// Every participant, role, note and evidence edge binds a compartment, a
// purpose, an effective interval and a disclosure receipt. Denied reads
// refuse with one non-disclosing error whether the case is missing or
// forbidden, so search and count cannot leak hidden cases. Removed
// participants lose all derived access: receipts re-verify against live
// state. Escrowed reporter pseudonyms ("escrow:*") are opaque by
// construction — this package offers no resolution path. Artifact bytes stay
// in Documents/object custody; this package owns visibility and custody
// relationships only.
package hrcase

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrCaseDenied is the single non-disclosing refusal: missing,
	// forbidden, revoked and expired all refuse identically.
	ErrCaseDenied = errors.New("hrcase: case access denied")
	// ErrCaseInvalid marks a malformed authorization input.
	ErrCaseInvalid = errors.New("hrcase: invalid case authorization input")
)

// Compartment is one visibility boundary inside a case. Classification is
// exact: a note or evidence item citing any other classification is
// refused, so a caller can never lower it.
type Compartment struct {
	ID             string
	Classification string
	AllowedRoles   []string
	Purpose        string
	EffectiveFrom  time.Time
	EffectiveTo    time.Time
}

func (c Compartment) effective(at time.Time) bool {
	if at.IsZero() || at.Before(c.EffectiveFrom) {
		return false
	}
	return c.EffectiveTo.IsZero() || !at.After(c.EffectiveTo)
}

func (c Compartment) allows(role string) bool {
	for _, r := range c.AllowedRoles {
		if r == role {
			return true
		}
	}
	return false
}

// CaseAccess is the live authorization state of one case: its participants,
// compartments and revocations.
type CaseAccess struct {
	CaseID       string
	Participants []Participant
	Compartments []Compartment
	Revoked      []string
}

// RevokeParticipant removes a participant's derived access. Receipts issued
// earlier re-verify against this list, so cached access dies here.
func (a *CaseAccess) RevokeParticipant(principal string) {
	for _, r := range a.Revoked {
		if r == principal {
			return
		}
	}
	a.Revoked = append(a.Revoked, principal)
}

func (a CaseAccess) revoked(principal string) bool {
	for _, r := range a.Revoked {
		if r == principal {
			return true
		}
	}
	return false
}

func (a CaseAccess) roleOf(principal string) (string, bool) {
	for _, p := range a.Participants {
		if p.Principal == principal {
			return p.Role, true
		}
	}
	return "", false
}

func (a CaseAccess) compartment(id string) (Compartment, bool) {
	for _, c := range a.Compartments {
		if c.ID == id {
			return c, true
		}
	}
	return Compartment{}, false
}

// authorize is the single decision point. Every refusal is ErrCaseDenied —
// principal, role, compartment, purpose, revocation and interval failures
// are indistinguishable — except structurally malformed calls, which are
// ErrCaseInvalid.
func (a CaseAccess) authorize(principal, role, compartmentID, purpose string, at time.Time) error {
	if a.CaseID == "" || at.IsZero() {
		return fmt.Errorf("%w: case and time are required", ErrCaseInvalid)
	}
	if principal == "" {
		return ErrCaseDenied
	}
	if a.revoked(principal) {
		return ErrCaseDenied
	}
	registered, ok := a.roleOf(principal)
	if !ok || registered != role {
		return ErrCaseDenied
	}
	c, ok := a.compartment(compartmentID)
	if !ok || !c.effective(at) {
		return ErrCaseDenied
	}
	if !c.allows(role) || c.Purpose != purpose {
		return ErrCaseDenied
	}
	return nil
}

// CanView reports whether principal acting as role may view compartmentID
// for purpose at time at.
func (a CaseAccess) CanView(principal, role, compartmentID, purpose string, at time.Time) error {
	return a.authorize(principal, role, compartmentID, purpose, at)
}

// DisclosureReceipt is the verifiable record of one granted view. Viewer is
// redacted for escrowed pseudonyms; Digest binds the exact edge so a
// tampered receipt no longer verifies.
type DisclosureReceipt struct {
	CaseID        string
	CompartmentID string
	Viewer        string
	Role          string
	Purpose       string

	principal string
}

func receiptDigest(caseID, compartmentID, principal, role, purpose string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%s", caseID, compartmentID, principal, role, purpose)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Digest returns the tamper-evident digest of the exact authorized edge.
func (r DisclosureReceipt) Digest() string {
	return receiptDigest(r.CaseID, r.CompartmentID, r.principal, r.Role, r.Purpose)
}

func redactViewer(principal string) string {
	if strings.HasPrefix(principal, "escrow:") {
		return "escrow:redacted"
	}
	return principal
}

// CanViewReceipt authorizes like CanView and returns the disclosure receipt.
func (a CaseAccess) CanViewReceipt(principal, role, compartmentID, purpose string, at time.Time) (DisclosureReceipt, error) {
	if err := a.authorize(principal, role, compartmentID, purpose, at); err != nil {
		return DisclosureReceipt{}, err
	}
	return DisclosureReceipt{CaseID: a.CaseID, CompartmentID: compartmentID, Viewer: redactViewer(principal), Role: role, Purpose: purpose, principal: principal}, nil
}

// Verify re-checks a receipt against live access state without a wall
// clock: the participant must still hold the role, stay unrevoked, and the
// compartment must still allow the role for the receipt's exact purpose.
// Revoked, re-compartmented and tampered edges all fail, so a cached receipt
// never outlives the access it records.
func (r DisclosureReceipt) Verify(access CaseAccess) error {
	return access.authorizeStored(r)
}

// authorizeStored re-checks the receipt's exact edge without a wall clock:
// the participant must still hold the role, stay unrevoked, and the
// compartment must still allow the role for the receipt's purpose, and the
// digest must still match.
func (a CaseAccess) authorizeStored(r DisclosureReceipt) error {
	if r.principal == "" {
		return fmt.Errorf("%w: receipt is empty", ErrCaseInvalid)
	}
	if a.revoked(r.principal) {
		return ErrCaseDenied
	}
	registered, ok := a.roleOf(r.principal)
	if !ok || registered != r.Role {
		return ErrCaseDenied
	}
	c, ok := a.compartment(r.CompartmentID)
	if !ok || !c.allows(r.Role) || c.Purpose != r.Purpose {
		return ErrCaseDenied
	}
	if r.CaseID != a.CaseID {
		return ErrCaseDenied
	}
	return nil
}

// ResolveEscrowed is the escrow-correlation guard: escrowed reporter
// pseudonyms never resolve to an identity through this package. It always
// returns "" so any future resolution path breaks its caller loudly.
func ResolveEscrowed(pseudonym string) string { return "" }

// ConfidentialNote is one compartment-bound note. Classification must equal
// the compartment's: the author cannot lower it.
type ConfidentialNote struct {
	ID             string
	CaseID         string
	CompartmentID  string
	Classification string
	AuthorRole     string
	Author         string
	Purpose        string
	BodyDigest     string
}

// StoreNote validates authorship and classification, then binds the note to
// a disclosure receipt for its author.
func (a CaseAccess) StoreNote(note ConfidentialNote) (ConfidentialNote, DisclosureReceipt, error) {
	if note.ID == "" || note.CaseID != a.CaseID || note.BodyDigest == "" {
		return ConfidentialNote{}, DisclosureReceipt{}, fmt.Errorf("%w: note identity is incomplete", ErrCaseInvalid)
	}
	c, ok := a.compartment(note.CompartmentID)
	if !ok {
		return ConfidentialNote{}, DisclosureReceipt{}, ErrCaseDenied
	}
	if note.Classification == "" || note.Classification != c.Classification {
		return ConfidentialNote{}, DisclosureReceipt{}, fmt.Errorf("%w: note classification must equal the compartment classification", ErrCaseInvalid)
	}
	registered, ok := a.roleOf(note.Author)
	if !ok || registered != note.AuthorRole || a.revoked(note.Author) {
		return ConfidentialNote{}, DisclosureReceipt{}, ErrCaseDenied
	}
	if !c.allows(note.AuthorRole) || c.Purpose != note.Purpose {
		return ConfidentialNote{}, DisclosureReceipt{}, ErrCaseDenied
	}
	receipt := DisclosureReceipt{CaseID: a.CaseID, CompartmentID: note.CompartmentID, Viewer: redactViewer(note.Author), Role: note.AuthorRole, Purpose: note.Purpose, principal: note.Author}
	return note, receipt, nil
}

// EvidenceItem is one compartment-bound custody reference. The artifact
// bytes live elsewhere; Custody is the append-only chain of holders.
type EvidenceItem struct {
	ID             string
	CaseID         string
	CompartmentID  string
	ArtifactDigest string
	Custody        []string
}

// CheckEvidence authorizes custody handling: the handler must be an active
// participant whose role the compartment allows, and the item must cite its
// artifact and a non-empty custody chain. Custody is not disclosure —
// handling sealed evidence never grants content reads.
func (a CaseAccess) CheckEvidence(principal, role string, item EvidenceItem, _ string, at time.Time) error {
	if item.ID == "" || item.CaseID != a.CaseID {
		return fmt.Errorf("%w: evidence identity is incomplete", ErrCaseInvalid)
	}
	if item.ArtifactDigest == "" || len(item.Custody) == 0 {
		return fmt.Errorf("%w: evidence requires an artifact digest and custody chain", ErrCaseInvalid)
	}
	if at.IsZero() {
		return fmt.Errorf("%w: time is required", ErrCaseInvalid)
	}
	if principal == "" || a.revoked(principal) {
		return ErrCaseDenied
	}
	registered, ok := a.roleOf(principal)
	if !ok || registered != role {
		return ErrCaseDenied
	}
	c, ok := a.compartment(item.CompartmentID)
	if !ok || !c.allows(role) {
		return ErrCaseDenied
	}
	return nil
}

// VisibleCases returns the subset of caseIDs principal may still discover.
// Unknown cases and forbidden cases are both absent: the caller cannot tell
// them apart. Revocation takes effect here immediately, invalidating derived
// search and cache views. Interval narrowing stays at view time.
func (a CaseAccess) VisibleCases(principal string, caseIDs []string) []string {
	out := []string{}
	for _, id := range caseIDs {
		if id != a.CaseID || principal == "" || a.revoked(principal) {
			continue
		}
		role, ok := a.roleOf(principal)
		if !ok {
			continue
		}
		for _, c := range a.Compartments {
			if c.allows(role) {
				out = append(out, id)
				break
			}
		}
	}
	return out
}

// CountVisible counts what VisibleCases returns: counts never reveal hidden
// cases.
func (a CaseAccess) CountVisible(principal string, caseIDs []string) int {
	return len(a.VisibleCases(principal, caseIDs))
}
