package population

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Expected-population completeness (POP-009) reconciles what a population
// should contain against what an observed snapshot discloses. Counts alone
// prove nothing — a roster of three and three observed rows can still hide
// a missing member behind a duplicate — so every expected subject lands in
// exactly one of matched or missing, and every observed subject in exactly
// one of matched, unexpected or duplicate. Repair obligations name what
// must be re-driven; the observed snapshot is never mutated.

// ErrCompletenessDefinitionMismatch is returned when the roster and the
// snapshot do not cite the same population definition.
var ErrCompletenessDefinitionMismatch = errors.New("population: cannot reconcile roster and snapshot of different definitions")

// ErrCompletenessRosterInvalid is returned for a roster with no definition,
// no expectation or a dirty member identity.
var ErrCompletenessRosterInvalid = errors.New("population: expected roster is invalid")

// ExpectedRoster is the membership a population should disclose.
// Authorized optionally bounds who may appear: observed members outside it
// are unauthorized, a stronger finding than merely unexpected. A nil
// Authorized list disables that distinction.
type ExpectedRoster struct {
	DefinitionID string
	Expected     []string
	Authorized   []string
}

// CompletenessReport records the expected and observed partitions with
// the repair obligations the gaps require. Complete is true only when
// nothing is missing, unexpected, duplicated or unauthorized, the
// snapshot is fresh enough and membership was actually disclosed.
type CompletenessReport struct {
	DefinitionID   string
	ObservedDigest string
	ExpectedCount  int
	ObservedCount  int
	Matched        []string
	Missing        []string
	Unexpected     []string
	Duplicates     []string
	Unauthorized   []string
	Stale          bool
	Protected      bool
	Obligations    []Obligation
	Complete       bool
	Digest         string
}

// ReconcileCompleteness compares roster against observed without mutating
// either. requireWatermark, when set, is the knowledge boundary the
// snapshot's watermarks must reach; an older snapshot marks the whole
// report stale because its absences cannot be trusted.
func ReconcileCompleteness(roster ExpectedRoster, observed Snapshot, requireWatermark values.Instant) (CompletenessReport, error) {
	expected, authorized, err := normalizeRoster(roster)
	if err != nil {
		return CompletenessReport{}, err
	}
	if observed.DefinitionID == "" {
		return CompletenessReport{}, fmt.Errorf("%w: observed snapshot cites no definition", ErrSnapshotIncomplete)
	}
	if observed.DefinitionID != roster.DefinitionID {
		return CompletenessReport{}, fmt.Errorf("%w: %q vs %q",
			ErrCompletenessDefinitionMismatch, roster.DefinitionID, observed.DefinitionID)
	}
	rep := CompletenessReport{
		DefinitionID: roster.DefinitionID, ObservedDigest: observed.Digest,
		ExpectedCount: len(expected), Protected: observed.MembershipProtected,
	}
	if observed.MembershipProtected {
		rep.Obligations = []Obligation{{Reason: ObligationFactRedacted, Field: "membership"}}
		rep.Digest = digestCompleteness(rep)
		return rep, nil
	}
	seen := make(map[string]int, len(observed.SubjectIDs))
	var matched, unexpected, duplicates, unauthorized []string
	for _, id := range observed.SubjectIDs {
		seen[id]++
		if seen[id] > 1 {
			duplicates = append(duplicates, id)
			continue
		}
		if expected[id] {
			matched = append(matched, id)
		} else {
			unexpected = append(unexpected, id)
		}
		if authorized != nil && !authorized[id] {
			unauthorized = append(unauthorized, id)
		}
	}
	var missing []string
	for id := range expected {
		if seen[id] == 0 {
			missing = append(missing, id)
		}
	}
	sort.Strings(matched)
	sort.Strings(missing)
	sort.Strings(unexpected)
	sort.Strings(duplicates)
	sort.Strings(unauthorized)
	rep.Matched = matched
	rep.Missing = missing
	rep.Unexpected = unexpected
	rep.Duplicates = duplicates
	rep.Unauthorized = unauthorized
	rep.ObservedCount = len(observed.SubjectIDs)
	rep.Stale = isStale(observed, requireWatermark)
	for _, id := range missing {
		rep.Obligations = append(rep.Obligations, Obligation{Reason: ObligationExpectedAbsent, Field: id})
	}
	for _, id := range unexpected {
		rep.Obligations = append(rep.Obligations, Obligation{Reason: ObligationUnexpectedPresent, Field: id})
	}
	for _, id := range duplicates {
		rep.Obligations = append(rep.Obligations, Obligation{Reason: ObligationDuplicateObserved, Field: id})
	}
	for _, id := range unauthorized {
		rep.Obligations = append(rep.Obligations, Obligation{Reason: ObligationUnauthorizedMember, Field: id})
	}
	if rep.Stale {
		rep.Obligations = append(rep.Obligations, Obligation{Reason: ObligationStaleWatermark, Field: "watermarks"})
	}
	rep.Complete = len(missing) == 0 && len(unexpected) == 0 && len(duplicates) == 0 &&
		len(unauthorized) == 0 && !rep.Stale
	rep.Digest = digestCompleteness(rep)
	return rep, nil
}

func normalizeRoster(roster ExpectedRoster) (map[string]bool, map[string]bool, error) {
	if roster.DefinitionID == "" {
		return nil, nil, fmt.Errorf("%w: definition is required", ErrCompletenessRosterInvalid)
	}
	if len(roster.Expected) == 0 {
		return nil, nil, fmt.Errorf("%w: at least one expected member is required", ErrCompletenessRosterInvalid)
	}
	expected := make(map[string]bool, len(roster.Expected))
	for _, id := range roster.Expected {
		if id == "" {
			return nil, nil, fmt.Errorf("%w: blank expected member", ErrCompletenessRosterInvalid)
		}
		if expected[id] {
			return nil, nil, fmt.Errorf("%w: expected member %q twice", ErrCompletenessRosterInvalid, id)
		}
		expected[id] = true
	}
	var authorized map[string]bool
	if roster.Authorized != nil {
		authorized = make(map[string]bool, len(roster.Authorized))
		for _, id := range roster.Authorized {
			if id == "" {
				return nil, nil, fmt.Errorf("%w: blank authorized member", ErrCompletenessRosterInvalid)
			}
			authorized[id] = true
		}
	}
	return expected, authorized, nil
}

func isStale(observed Snapshot, requireWatermark values.Instant) bool {
	if !requireWatermark.IsSet() {
		return false
	}
	if len(observed.Watermarks) == 0 {
		return true
	}
	for _, wm := range observed.Watermarks {
		if wm.Before(requireWatermark) {
			return true
		}
	}
	return false
}

func digestCompleteness(rep CompletenessReport) string {
	w := writerFor(completenessSchema).
		String("definition_id", rep.DefinitionID).
		String("observed_digest", rep.ObservedDigest).
		Count("expected", rep.ExpectedCount).
		Count("observed", rep.ObservedCount).
		Bool("stale", rep.Stale).
		Bool("protected", rep.Protected).
		Bool("complete", rep.Complete)
	for _, id := range rep.Matched {
		w.String("matched", id)
	}
	for _, id := range rep.Missing {
		w.String("missing", id)
	}
	for _, id := range rep.Unexpected {
		w.String("unexpected", id)
	}
	for _, id := range rep.Duplicates {
		w.String("duplicate", id)
	}
	for _, id := range rep.Unauthorized {
		w.String("unauthorized", id)
	}
	for _, o := range rep.Obligations {
		w.String("obligation", o.Reason.String()+":"+o.Field)
	}
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}
