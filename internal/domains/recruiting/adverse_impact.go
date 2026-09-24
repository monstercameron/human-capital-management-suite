package recruiting

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// FunnelOutcomeKind records one completed selection decision at a stage.
type FunnelOutcomeKind string

const (
	FunnelAdvanced FunnelOutcomeKind = "ADVANCED"
	FunnelRejected FunnelOutcomeKind = "REJECTED"
)

// PinnedStageOutcome is one completed decision in a pinned source snapshot.
type PinnedStageOutcome struct {
	CandidacyID string
	FromStage   CandidacyStage
	Outcome     FunnelOutcomeKind
}

// DeclaredApplicantGroup binds a purpose-limited demographic fact to the
// candidacy's existing purpose and consent reference.
type DeclaredApplicantGroup struct {
	CandidacyID      string
	Group            string
	Purpose          string
	AuthorizationRef string
}

// HiringFunnelSnapshot can only be created by PinHiringFunnelSnapshot.
// Its private seal prevents callers from asserting an arbitrary source pin.
type HiringFunnelSnapshot struct {
	SnapshotID   string
	Revision     uint64
	Outcomes     []PinnedStageOutcome
	Groups       []DeclaredApplicantGroup
	sourceDigest string
	seal         string
}

// HiringFunnelPolicy is the declared reporting method. EEOC UGESP Q&A 20
// notes a small number selected can be too few for a determination but
// sets no universal numeric cutoff, so the analyst supplies it.
// https://www.eeoc.gov/es/node/130157
type HiringFunnelPolicy struct {
	MinimumApplicants int
}

type SelectionRateStatus string

const (
	SelectionRateKnown   SelectionRateStatus = "KNOWN"
	SelectionRateUnknown SelectionRateStatus = "UNKNOWN"
)

type HiringGroupRate struct {
	Group      string
	Applicants int
	Selected   int
	Status     SelectionRateStatus
	Rate       float64
	ComparedTo string
	Ratio      float64
	FourFifths bool
}

type HiringTransitionAnalysis struct {
	FromStage CandidacyStage
	ToStage   CandidacyStage
	Groups    []HiringGroupRate
}

// HiringFunnelAnalysis contains aggregate outcomes only; it has no
// candidacy identifiers, consent references, or raw fact records.
type HiringFunnelAnalysis struct {
	SnapshotID  string
	Revision    uint64
	Transitions []HiringTransitionAnalysis
}

// PinHiringFunnelSnapshot derives completed stage decisions from the ATS
// aggregate and its governed stage ledger. Group facts must cite the exact
// purpose and consent reference already bound to each candidacy.
func PinHiringFunnelSnapshot(aggregate *Aggregate, ledger *StageLedger, groupFacts []DeclaredApplicantGroup) (HiringFunnelSnapshot, error) {
	if aggregate == nil || ledger == nil || ledger.agg != aggregate {
		return HiringFunnelSnapshot{}, fmt.Errorf("recruiting: snapshot requires its authoritative aggregate and stage ledger")
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	facts := make(map[string]DeclaredApplicantGroup, len(groupFacts))
	for _, fact := range groupFacts {
		id, label := strings.TrimSpace(fact.CandidacyID), strings.TrimSpace(fact.Group)
		if id == "" || label == "" || strings.TrimSpace(fact.Purpose) == "" || strings.TrimSpace(fact.AuthorizationRef) == "" {
			return HiringFunnelSnapshot{}, fmt.Errorf("recruiting: group fact requires candidacy, group, purpose, and authorization")
		}
		if _, exists := facts[id]; exists {
			return HiringFunnelSnapshot{}, fmt.Errorf("recruiting: duplicate applicant group fact")
		}
		facts[id] = fact
	}

	ids := make([]string, 0, len(aggregate.Candidacies))
	for id := range aggregate.Candidacies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	outcomes := make([]PinnedStageOutcome, 0, len(ids)*4)
	usedFacts := make(map[string]struct{}, len(facts))
	for _, id := range ids {
		c := aggregate.Candidacies[id]
		if id != c.CandidacyID || c.CanonicalDigest == "" || c.withDigest().CanonicalDigest != c.CanonicalDigest {
			return HiringFunnelSnapshot{}, fmt.Errorf("recruiting: candidacy source digest or identity is invalid")
		}
		app, ok := aggregate.Applications[c.ApplicationID]
		if !ok || app.CanonicalDigest == "" || app.withDigest().CanonicalDigest != app.CanonicalDigest || app.CandidateID != c.CandidateID {
			return HiringFunnelSnapshot{}, fmt.Errorf("recruiting: application source is missing or invalid")
		}
		if err := validateCandidacyEventChain(aggregate.Events, c); err != nil {
			return HiringFunnelSnapshot{}, err
		}
		fact, hasFact := facts[id]
		if hasFact && (strings.TrimSpace(fact.Purpose) != c.Purpose || strings.TrimSpace(fact.AuthorizationRef) != c.ConsentRef) {
			return HiringFunnelSnapshot{}, fmt.Errorf("recruiting: group fact is not authorized for the candidacy purpose")
		}
		if !hasFact {
			return HiringFunnelSnapshot{}, fmt.Errorf("recruiting: group fact is not authorized for the candidacy purpose")
		}
		usedFacts[id] = struct{}{}
		lastStage, err := verifiedLastStage(aggregate, ledger, c)
		if err != nil {
			return HiringFunnelSnapshot{}, err
		}
		path := []CandidacyStage{StageApplied, StageScreening, StageInterview, StageOffer}
		for _, from := range path {
			if stageRank(from) > stageRank(lastStage) {
				break
			}
			kind := FunnelAdvanced
			if c.Stage == StageRejected && from == lastStage {
				kind = FunnelRejected
			}
			if c.Stage != StageHired && c.Stage != StageRejected && from == lastStage {
				break
			} // undecided outcomes are not in the denominator
			outcomes = append(outcomes, PinnedStageOutcome{CandidacyID: id, FromStage: from, Outcome: kind})
		}
	}
	for id := range facts {
		if _, ok := usedFacts[id]; !ok {
			return HiringFunnelSnapshot{}, fmt.Errorf("recruiting: declared group fact does not bind an included candidacy")
		}
	}
	version := uint64(len(aggregate.Events))
	if version == 0 {
		return HiringFunnelSnapshot{}, fmt.Errorf("recruiting: empty source has no pin")
	}
	sourceDigest, err := sourceSnapshotDigest(aggregate, ledger.frontiers, ids)
	if err != nil {
		return HiringFunnelSnapshot{}, err
	}
	seal, err := snapshotSeal(version, sourceDigest, outcomes, groupFacts)
	if err != nil {
		return HiringFunnelSnapshot{}, err
	}
	return HiringFunnelSnapshot{SnapshotID: seal, Revision: version, Outcomes: outcomes, Groups: append([]DeclaredApplicantGroup(nil), groupFacts...), sourceDigest: sourceDigest, seal: seal}, nil
}

func verifiedLastStage(aggregate *Aggregate, ledger *StageLedger, c Candidacy) (CandidacyStage, error) {
	frontier, hasFrontier := ledger.frontiers[c.CandidacyID]
	last := StageApplied
	if hasFrontier {
		if frontier.CandidacyID != c.CandidacyID || frontier.Stage != StageHired && frontier.Stage != StageScreening && frontier.Stage != StageInterview && frontier.Stage != StageOffer || frontier.CandidacyRevision == 0 {
			return "", fmt.Errorf("recruiting: invalid governed stage frontier")
		}
		last = frontier.Stage
	}
	expected := lastStageRevision(last)
	if c.Stage == StageHired {
		if !hasFrontier || frontier.Stage != StageHired || frontier.CandidacyRevision != c.Revision {
			return "", fmt.Errorf("recruiting: hired candidacy is not pinned to its governed frontier")
		}
		return StageOffer, nil
	}
	if c.Stage == StageRejected {
		if c.Revision != expected+1 || hasFrontier && frontier.CandidacyRevision != expected {
			return "", fmt.Errorf("rejected candidacy is not pinned to its prior governed stage")
		}
		if !hasEventRevision(aggregate.Events, c.CandidacyID, c.Revision, "CANDIDACY_REJECTED") {
			return "", fmt.Errorf("recruiting: rejected outcome is absent from source events")
		}
		return last, nil
	}
	if c.Stage == StageWithdrawn {
		if c.Revision != expected+1 || hasFrontier && frontier.CandidacyRevision != expected {
			return "", fmt.Errorf("withdrawn candidacy is not pinned to its prior governed stage")
		}
		if !hasEventRevision(aggregate.Events, c.CandidacyID, c.Revision, "CANDIDACY_WITHDRAWN") {
			return "", fmt.Errorf("recruiting: withdrawal outcome is absent from source events")
		}
		return last, nil
	}
	if c.Stage != last || c.Revision != expected || hasFrontier != (last != StageApplied) || hasFrontier && frontier.CandidacyRevision != c.Revision {
		return "", fmt.Errorf("recruiting: candidacy is not pinned to its governed current stage")
	}
	return last, nil
}

func lastStageRevision(s CandidacyStage) uint64 { return uint64(stageRank(s) + 1) }
func stageRank(s CandidacyStage) int {
	switch s {
	case StageApplied:
		return 0
	case StageScreening:
		return 1
	case StageInterview:
		return 2
	case StageOffer:
		return 3
	case StageHired:
		return 4
	default:
		return -1
	}
}
func nextFunnelStage(from CandidacyStage) CandidacyStage {
	switch from {
	case StageApplied:
		return StageScreening
	case StageScreening:
		return StageInterview
	case StageInterview:
		return StageOffer
	case StageOffer:
		return StageHired
	default:
		return ""
	}
}
func hasEventRevision(events []RecruitingEvent, id string, revision uint64, kind string) bool {
	for _, event := range events {
		if event.AggregateID == id && event.Revision == revision && event.Kind == kind {
			return true
		}
	}
	return false
}

func validateCandidacyEventChain(events []RecruitingEvent, c Candidacy) error {
	matched := make([]RecruitingEvent, 0, c.Revision)
	for _, event := range events {
		if event.AggregateID == c.CandidacyID {
			matched = append(matched, event)
		}
	}
	if uint64(len(matched)) != c.Revision || len(matched) == 0 || matched[0].Revision != 1 || matched[0].Kind != "CANDIDACY_CREATED" {
		return fmt.Errorf("recruiting: candidacy event history is incomplete")
	}
	for i, event := range matched {
		if event.Revision != uint64(i+1) {
			return fmt.Errorf("recruiting: candidacy event revisions are not contiguous")
		}
		if i == 0 {
			continue
		}
		want := "CANDIDACY_ADVANCED"
		if uint64(i+1) == c.Revision {
			switch c.Stage {
			case StageHired:
				want = "CANDIDACY_HIRED"
			case StageRejected:
				want = "CANDIDACY_REJECTED"
			case StageWithdrawn:
				want = "CANDIDACY_WITHDRAWN"
			}
		}
		if event.Kind != want {
			return fmt.Errorf("recruiting: candidacy event outcome does not match its source revision")
		}
	}
	return nil
}

func sourceSnapshotDigest(aggregate *Aggregate, frontiers map[string]StageFrontier, candidacyIDs []string) (string, error) {
	w := canonicalbytes.New("hcmnext.domains.recruiting.HiringFunnelSource", schemaVersion).Int("events", int64(len(aggregate.Events)))
	for i, event := range aggregate.Events {
		w.String(fmt.Sprintf("event_%d", i), strings.Join([]string{event.Kind, event.AggregateID, fmt.Sprint(event.Revision)}, "\x00"))
	}
	for i, id := range candidacyIDs {
		c := aggregate.Candidacies[id]
		app := aggregate.Applications[c.ApplicationID]
		w.String(fmt.Sprintf("candidacy_%d", i), c.CanonicalDigest).String(fmt.Sprintf("application_%d", i), app.CanonicalDigest)
		if frontier, ok := frontiers[id]; ok {
			w.String(fmt.Sprintf("frontier_%d", i), canonicalbytes.Digest(frontier.body()))
		}
	}
	b, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(b), nil
}

func snapshotSeal(revision uint64, sourceDigest string, outcomes []PinnedStageOutcome, groups []DeclaredApplicantGroup) (string, error) {
	w := canonicalbytes.New("hcmnext.domains.recruiting.HiringFunnelSnapshot", schemaVersion).Int("revision", int64(revision)).String("source_digest", sourceDigest)
	for i, o := range outcomes {
		w.String(fmt.Sprintf("outcome_%d", i), strings.Join([]string{o.CandidacyID, string(o.FromStage), string(o.Outcome)}, "\x00"))
	}
	sorted := append([]DeclaredApplicantGroup(nil), groups...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].CandidacyID < sorted[j].CandidacyID })
	for i, g := range sorted {
		w.String(fmt.Sprintf("group_%d", i), strings.Join([]string{g.CandidacyID, g.Group, g.Purpose, g.AuthorizationRef}, "\x00"))
	}
	b, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(b), nil
}

// AnalyzeHiringFunnel computes the per-transition rates from an intact
// pinned snapshot. MinimumApplicants is an explicit reporting policy;
// no universal numeric EEOC cutoff is assumed.
func AnalyzeHiringFunnel(snapshot HiringFunnelSnapshot, policy HiringFunnelPolicy) (HiringFunnelAnalysis, error) {
	if snapshot.seal == "" || snapshot.SnapshotID != snapshot.seal || snapshot.Revision == 0 {
		return HiringFunnelAnalysis{}, fmt.Errorf("recruiting: untrusted or incomplete pinned snapshot")
	}
	seal, err := snapshotSeal(snapshot.Revision, snapshot.sourceDigest, snapshot.Outcomes, snapshot.Groups)
	if err != nil || seal != snapshot.seal {
		return HiringFunnelAnalysis{}, fmt.Errorf("recruiting: pinned snapshot integrity check failed")
	}
	if policy.MinimumApplicants < 1 {
		return HiringFunnelAnalysis{}, fmt.Errorf("recruiting: declared minimum applicant sample must be positive")
	}
	groupByID := make(map[string]string, len(snapshot.Groups))
	for _, fact := range snapshot.Groups {
		groupByID[fact.CandidacyID] = fact.Group
	}
	type tally struct{ applicants, selected int }
	tallies := make(map[CandidacyStage]map[string]tally)
	for _, outcome := range snapshot.Outcomes {
		group, ok := groupByID[outcome.CandidacyID]
		if !ok {
			return HiringFunnelAnalysis{}, fmt.Errorf("recruiting: pinned outcome lacks authorized group fact")
		}
		byGroup := tallies[outcome.FromStage]
		if byGroup == nil {
			byGroup = make(map[string]tally)
			tallies[outcome.FromStage] = byGroup
		}
		t := byGroup[group]
		t.applicants++
		if outcome.Outcome == FunnelAdvanced {
			t.selected++
		} else if outcome.Outcome != FunnelRejected {
			return HiringFunnelAnalysis{}, fmt.Errorf("recruiting: invalid pinned selection outcome")
		}
		byGroup[group] = t
	}
	result := HiringFunnelAnalysis{SnapshotID: snapshot.SnapshotID, Revision: snapshot.Revision}
	for _, from := range []CandidacyStage{StageApplied, StageScreening, StageInterview, StageOffer} {
		byGroup := tallies[from]
		if len(byGroup) == 0 {
			continue
		}
		names := make([]string, 0, len(byGroup))
		for name := range byGroup {
			names = append(names, name)
		}
		sort.Strings(names)
		tr := HiringTransitionAnalysis{FromStage: from, ToStage: nextFunnelStage(from)}
		for _, name := range names {
			t := byGroup[name]
			rate := HiringGroupRate{Group: name, Applicants: t.applicants, Selected: t.selected, Status: SelectionRateUnknown}
			if t.applicants >= policy.MinimumApplicants {
				rate.Status = SelectionRateKnown
				rate.Rate = float64(t.selected) / float64(t.applicants)
			}
			tr.Groups = append(tr.Groups, rate)
		}
		var reference *HiringGroupRate
		for i := range tr.Groups {
			candidate := &tr.Groups[i]
			if candidate.Status == SelectionRateKnown && (reference == nil || candidate.Rate > reference.Rate) {
				reference = candidate
			}
		}
		if reference != nil {
			for i := range tr.Groups {
				candidate := &tr.Groups[i]
				if candidate.Status != SelectionRateKnown || candidate.Group == reference.Group {
					continue
				}
				candidate.ComparedTo = reference.Group
				if reference.Rate == 0 {
					candidate.Ratio = 1
				} else {
					candidate.Ratio = candidate.Rate / reference.Rate
				}
				candidate.FourFifths = candidate.Ratio < 0.8
			}
		}
		result.Transitions = append(result.Transitions, tr)
	}
	return result, nil
}
