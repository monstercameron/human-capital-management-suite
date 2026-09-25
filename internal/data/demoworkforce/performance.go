package demoworkforce

// The HarborCare demo tenant's performance record.
//
// migrations/00108 defines the seven performance tables and
// internal/data/performancestore persists them, but nothing ever wrote a
// row for the demo tenant: every worker had an empty performance history, so
// the promotion routing predicate that reads a calibrated rating
// (internal/intent/app.PinsHighPerformerVariant) had nothing to read.
//
// PlanPerformance builds the record the way the domain says it is built,
// rather than inventing table rows: a performance cycle is opened, a
// participant/reviewer graph is frozen over it, every participant collects a
// manager review and a peer review, internal/domains/performance calculates
// each participant's proposed rating from those reviews under a published
// weighting rule, one calibration session per organization unit finalizes the
// cohort, and the finalized FinalCalibratedRating is what the seed stores.
// Nothing here asserts a rating directly; a rating is the arithmetic of the
// reviews the plan writes, so the stored rating, its proposed rating, its
// contributions and its digests all agree by construction.
//
// Two facts are named rather than hidden. The calibration facilitator is a
// service principal, not a worker: every worker in a cohort is either a
// participant or one of its reviewers, and the domain refuses a facilitator
// who is also a reviewer. And the chief executive's manager reviewer is the
// board reference their workforce row already carries, because no worker sits
// above them in the recorded chain.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/performancestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	// CalibrationFacilitatorID is the people-operations service principal
	// that chairs every demo calibration session. It is deliberately not a
	// worker: the domain refuses a facilitator who is also one of the
	// session's reviewers, and every HarborCare worker reviews somebody.
	CalibrationFacilitatorID = "hcmnext.demo.calibration-facilitator"

	// PerformanceRatingRuleID and PerformanceCalibrationRuleID name the two
	// published rules the demo record is calculated under.
	PerformanceRatingRuleID      = "harborcare.performance.rating"
	PerformanceCalibrationRuleID = "harborcare.performance.calibration"

	// PerformanceRatingScale is the decimal scale every demo rating carries.
	PerformanceRatingScale = 2
)

// PerformanceCycleSpec declares one review cycle of the demo record: its id,
// the calendar year it reviews, and whether it has been calibrated and
// closed. The current cycle is open, so it carries no finalized rating.
type PerformanceCycleSpec struct {
	CycleID  string
	Year     int
	Closed   bool
	OpenedAt time.Time
	ClosedAt time.Time
}

// PerformanceCycles is the demo record's review history: two completed annual
// cycles and the one now in flight.
var PerformanceCycles = []PerformanceCycleSpec{
	{
		CycleID: "harborcare-2024-annual", Year: 2024, Closed: true,
		OpenedAt: time.Date(2024, time.November, 1, 9, 0, 0, 0, time.UTC),
		ClosedAt: time.Date(2025, time.February, 14, 17, 0, 0, 0, time.UTC),
	},
	{
		CycleID: "harborcare-2025-annual", Year: 2025, Closed: true,
		OpenedAt: time.Date(2025, time.November, 3, 9, 0, 0, 0, time.UTC),
		ClosedAt: time.Date(2026, time.February, 13, 17, 0, 0, 0, time.UTC),
	},
	{
		CycleID: "harborcare-2026-annual", Year: 2026, Closed: false,
		OpenedAt: time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC),
	},
}

// ratingBand is one worker's standing: the manager and peer review scores
// that produce their proposed rating, and the optional calibrated value the
// session moves them to. The scores are the only rating input; the resulting
// decimal is the domain's weighted average of them, never a literal.
type ratingBand struct {
	Manager     string
	Peer        string
	CalibrateTo string
}

var (
	bandTop        = ratingBand{Manager: "5", Peer: "5"} // 5.00
	bandStrong     = ratingBand{Manager: "5", Peer: "4"} // 4.67
	bandSolidHigh  = ratingBand{Manager: "4", Peer: "4"} // 4.00
	bandCalibrated = ratingBand{Manager: "4", Peer: "4", CalibrateTo: "4.25"}
	bandSolid      = ratingBand{Manager: "4", Peer: "3"} // 3.67
	bandSolidLow   = ratingBand{Manager: "3", Peer: "4"} // 3.33
	bandBelow      = ratingBand{Manager: "3", Peer: "2"} // 2.67
)

// topPerformerOrdinals, strongPerformerOrdinals, calibratedOrdinals and
// belowExpectationOrdinals pin the shape of the distribution by worker
// ordinal (the N in HC-21{NNN}), so a worker's standing is the same in every
// cycle and the same on every replay. HC-21051 is the demo's promotion
// subject (the individual-contributor persona), and they are one of the top
// performers so the high-performer routing predicate has a subject it
// actually fires for.
var (
	topPerformerOrdinals     = []int{12, 26, 51}
	strongPerformerOrdinals  = []int{5, 20, 38, 55}
	calibratedOrdinals       = []int{7, 29, 46}
	belowExpectationOrdinals = []int{17, 44}
)

func containsOrdinal(ordinals []int, ordinal int) bool {
	for _, candidate := range ordinals {
		if candidate == ordinal {
			return true
		}
	}
	return false
}

// bandFor returns the standing of the worker at 1-based ordinal.
func (p *Pack) bandFor(ordinal int) ratingBand {
	switch {
	case containsOrdinal(p.topPerformers, ordinal):
		return bandTop
	case containsOrdinal(p.strongPerformers, ordinal):
		return bandStrong
	case containsOrdinal(p.calibrated, ordinal):
		return bandCalibrated
	case containsOrdinal(p.belowExpectation, ordinal):
		return bandBelow
	case ordinal%6 == 0:
		return bandSolidLow
	case ordinal%2 == 0:
		return bandSolidHigh
	default:
		return bandSolid
	}
}

// PlannedCalibrationSession is one organization unit's calibration of one
// cycle, together with the finalized ratings it produced.
type PlannedCalibrationSession struct {
	OrgUnit string
	Session performance.CalibrationSession
	Ratings []performance.FinalCalibratedRating
}

// PlannedPerformanceCycle is one cycle of the demo record: its full immutable
// revision chain, the frozen reviewer graph, the reviews collected against it
// and — for a closed cycle — the calibration sessions that finalized it.
type PlannedPerformanceCycle struct {
	Spec      PerformanceCycleSpec
	Revisions []performance.PerformanceCycle
	Graph     performance.FrozenParticipantReviewerGraph
	Reviews   []performance.Review
	Sessions  []PlannedCalibrationSession
}

// PerformancePlan is the whole deterministic performance record.
type PerformancePlan struct {
	Cycles []PlannedPerformanceCycle
}

// PerformanceSummary counts what one [SeedPerformance] call wrote.
type PerformanceSummary struct {
	CycleRevisions int
	Reviews        int
	Sessions       int
	RatingCases    int
	RatingEvents   int
	FinalRatings   int
	Skipped        int
}

// PlanPerformance derives the demo tenant's whole performance record. It is a
// pure function of the workforce plan: no clock, no randomness, and every
// identity is derived from the cycle and the worker.
func PlanPerformance(tenant uuid.UUID) (PerformancePlan, error) {
	return HarborCarePack.PlanPerformance(tenant)
}

// PlanPerformance derives this company's performance record.
func (p *Pack) PlanPerformance(tenant uuid.UUID) (PerformancePlan, error) {
	employees, err := p.Plan(tenant)
	if err != nil {
		return PerformancePlan{}, err
	}
	workerIDByKey := make(map[string]string, len(employees))
	for _, employee := range employees {
		workerIDByKey[employee.Row.WorkerKey] = employee.Row.WorkerID.String()
	}

	ratingRule, err := p.demoRatingRule()
	if err != nil {
		return PerformancePlan{}, err
	}
	calibrationRule, err := performance.NewCalibrationRule(p.policyPrefix+".performance.calibration", "v1",
		values.MustDecimal("0.50", 2, values.RoundingExactRequired),
		[]performance.CalibrationReasonCode{performance.CalibrationReasonConsistency, performance.CalibrationReasonEvidence})
	if err != nil {
		return PerformancePlan{}, fmt.Errorf("demoworkforce: calibration rule: %w", err)
	}

	plan := PerformancePlan{Cycles: make([]PlannedPerformanceCycle, 0, len(p.performanceCycles))}
	for _, spec := range p.performanceCycles {
		planned, err := p.planPerformanceCycle(spec, employees, workerIDByKey, ratingRule, calibrationRule)
		if err != nil {
			return PerformancePlan{}, err
		}
		plan.Cycles = append(plan.Cycles, planned)
	}
	return plan, nil
}

func (p *Pack) demoRatingRule() (performance.ProposedRatingRule, error) {
	rule, err := performance.NewProposedRatingRule(p.policyPrefix+".performance.rating", "v1",
		PerformanceRatingScale, PerformanceRatingScale, values.RoundingHalfEven, 2,
		map[performance.ReviewerRelationshipKind]values.Decimal{
			performance.ReviewerRelationshipManager: values.MustDecimal("2.00", 2, values.RoundingExactRequired),
			performance.ReviewerRelationshipPeer:    values.MustDecimal("1.00", 2, values.RoundingExactRequired),
		}, performance.OutlierHandlingNone)
	if err != nil {
		return performance.ProposedRatingRule{}, fmt.Errorf("demoworkforce: proposed rating rule: %w", err)
	}
	return rule, nil
}

// cycleReferences are the three governed documents a cycle binds. They are
// derived from the cycle id so a cycle's population, calendar and rating
// scale cannot silently belong to another cycle.
func (p *Pack) cycleReferences(spec PerformanceCycleSpec) (performance.PopulationBindingRef, performance.CalendarBindingRef, performance.RatingScaleVersionRef) {
	version := fmt.Sprintf("%d", spec.Year)
	return performance.PopulationBindingRef{
			DefinitionID:    p.policyPrefix + ".performance.population",
			RevisionVersion: version,
			Digest:          demoDigest("performance-population", spec.CycleID),
		},
		performance.CalendarBindingRef{
			Ref:     p.policyPrefix + ".performance.calendar",
			Version: version,
			Digest:  demoDigest("performance-calendar", spec.CycleID),
		},
		performance.RatingScaleVersionRef{
			ID:      p.policyPrefix + ".performance.scale.five-point",
			Version: "1",
			Digest:  demoDigest("performance-rating-scale", "five-point/1"),
		}
}

// demoDigest is a stable content digest for a named demo document. The demo
// does not ship the documents themselves; it ships references to them, and a
// reference needs a digest that is the same on every run.
func demoDigest(kind, key string) string {
	return canonicalbytes.Digest([]byte("hcmnext.demo." + kind + "\x00" + key))
}

// reviewerPanel resolves one participant's two reviewers: their recorded
// manager, and a peer. The peer is chosen by walking the workforce from a
// fixed offset until it lands on somebody who is neither the participant nor
// their manager, so a two-person unit whose only colleague is the manager
// still gets a real second reviewer.
func reviewerPanel(employees []Employee, index int, workerIDByKey map[string]string) (manager, peer string) {
	participant := employees[index].Row.WorkerID.String()
	manager = employees[index].Row.ManagerRelationshipRef
	if resolved, ok := workerIDByKey[manager]; ok {
		manager = resolved
	}
	for step := 0; step < len(employees); step++ {
		candidate := employees[(index+7+step)%len(employees)].Row.WorkerID.String()
		if candidate != participant && candidate != manager {
			return manager, candidate
		}
	}
	return manager, ""
}

func (p *Pack) planPerformanceCycle(spec PerformanceCycleSpec, employees []Employee, workerIDByKey map[string]string,
	ratingRule performance.ProposedRatingRule, calibrationRule performance.CalibrationRule) (PlannedPerformanceCycle, error) {
	population, calendar, scale := p.cycleReferences(spec)
	planned, err := performance.NewPerformanceCycle(spec.CycleID, population, calendar, scale)
	if err != nil {
		return PlannedPerformanceCycle{}, fmt.Errorf("demoworkforce: cycle %s: %w", spec.CycleID, err)
	}
	revisions := []performance.PerformanceCycle{planned}
	opened, err := planned.Open()
	if err != nil {
		return PlannedPerformanceCycle{}, fmt.Errorf("demoworkforce: open cycle %s: %w", spec.CycleID, err)
	}
	revisions = append(revisions, opened)

	participants := make([]performance.ParticipantRef, 0, len(employees))
	assignments := make([]performance.ReviewerAssignment, 0, 2*len(employees))
	for index := range employees {
		participant := employees[index].Row.WorkerID.String()
		manager, peer := reviewerPanel(employees, index, workerIDByKey)
		if manager == "" || peer == "" {
			return PlannedPerformanceCycle{}, fmt.Errorf("demoworkforce: %s has no reviewer panel", employees[index].Row.WorkerKey)
		}
		participants = append(participants, performance.ParticipantRef{ID: participant})
		assignments = append(assignments,
			performance.ReviewerAssignment{ParticipantID: participant, ReviewerID: manager, Relationship: performance.ReviewerRelationshipManager},
			performance.ReviewerAssignment{ParticipantID: participant, ReviewerID: peer, Relationship: performance.ReviewerRelationshipPeer})
	}
	// Colleagues review one another, so a reviewer is normally a participant
	// too; only self-review is refused.
	rules := performance.ReviewerGraphRules{
		RejectSelfReview:     true,
		LateAssignmentPolicy: performance.GraphPolicyNewRevision,
		ReassignmentPolicy:   performance.GraphPolicyNewRevision,
	}
	graph, err := performance.FreezeParticipantReviewerGraph(opened, participants, assignments, rules, values.NewInstant(spec.OpenedAt))
	if err != nil {
		return PlannedPerformanceCycle{}, fmt.Errorf("demoworkforce: freeze %s graph: %w", spec.CycleID, err)
	}

	submittedAt := spec.OpenedAt.Add(21 * 24 * time.Hour)
	collection, err := performance.NewReviewCollection(graph, values.NewInstant(submittedAt.Add(14*24*time.Hour)),
		performance.ReviewCollectionPolicy{RatingScale: scale})
	if err != nil {
		return PlannedPerformanceCycle{}, fmt.Errorf("demoworkforce: %s review collection: %w", spec.CycleID, err)
	}
	for index := range employees {
		band := p.bandFor(index + 1)
		participant := employees[index].Row.WorkerID.String()
		manager, peer := reviewerPanel(employees, index, workerIDByKey)
		for _, item := range []struct {
			reviewer, rating, label string
		}{{manager, band.Manager, "manager"}, {peer, band.Peer, "peer"}} {
			review := performance.Review{
				ID:            fmt.Sprintf("%s:%s:%s", spec.CycleID, employees[index].Row.WorkerKey, item.label),
				Kind:          performance.ReviewKindReview,
				State:         performance.ReviewStateSubmitted,
				ReviewerID:    item.reviewer,
				ParticipantID: participant,
				CycleID:       graph.CycleID,
				CycleRevision: graph.CycleRevision,
				GraphRevision: graph.GraphRevision,
				GraphDigest:   graph.Digest,
				RatingScale:   scale,
				Rating:        item.rating,
				// A review's narrative is private; only its digest is durable.
				NarrativeDigest: demoDigest("performance-review-narrative", fmt.Sprintf("%s/%s/%s", spec.CycleID, employees[index].Row.WorkerKey, item.label)),
				SubmittedAt:     values.NewInstant(submittedAt.Add(time.Duration(index) * time.Minute)),
				ReviewRevision:  1,
			}
			collection, err = collection.Submit(review)
			if err != nil {
				return PlannedPerformanceCycle{}, fmt.Errorf("demoworkforce: submit %s: %w", review.ID, err)
			}
		}
	}
	cycle := PlannedPerformanceCycle{Spec: spec, Revisions: revisions, Graph: graph, Reviews: collection.Reviews}
	if !spec.Closed {
		return cycle, nil
	}

	// A closed cycle carries the calibration evidence its transitions
	// require, and one calibration session per organization unit.
	evidence := performance.EvidenceRef{
		ID: p.policyPrefix + ".performance.calibration-pack", Version: fmt.Sprintf("%d", spec.Year),
		Digest: demoDigest("performance-calibration-pack", spec.CycleID),
	}
	calibrating, err := opened.BeginCalibration(evidence)
	if err != nil {
		return PlannedPerformanceCycle{}, fmt.Errorf("demoworkforce: calibrate %s: %w", spec.CycleID, err)
	}
	closed, err := calibrating.Close(evidence)
	if err != nil {
		return PlannedPerformanceCycle{}, fmt.Errorf("demoworkforce: close %s: %w", spec.CycleID, err)
	}
	cycle.Revisions = append(cycle.Revisions, calibrating, closed)

	sessions, err := p.planCalibrationSessions(spec, employees, collection, graph, ratingRule, calibrationRule)
	if err != nil {
		return PlannedPerformanceCycle{}, err
	}
	cycle.Sessions = sessions
	return cycle, nil
}

// planCalibrationSessions calibrates one cycle, one organization unit at a
// time, the way a real calibration runs. The panel is the unit's own head and
// the chief people officer; the facilitator chairs but never adjusts.
func (p *Pack) planCalibrationSessions(spec PerformanceCycleSpec, employees []Employee, collection performance.ReviewCollection,
	graph performance.FrozenParticipantReviewerGraph, ratingRule performance.ProposedRatingRule,
	calibrationRule performance.CalibrationRule) ([]PlannedCalibrationSession, error) {
	unitOrder := make([]string, 0, len(p.Company.Units))
	membersByUnit := make(map[string][]int, len(p.Company.Units))
	for index := range employees {
		unit := employees[index].Organization.Code
		if _, seen := membersByUnit[unit]; !seen {
			unitOrder = append(unitOrder, unit)
		}
		membersByUnit[unit] = append(membersByUnit[unit], index)
	}
	// The chief people officer sits on every panel; the domain refuses an
	// adjustment whose only adjuster is the participant's own manager, so the
	// one worker they manage directly is never calibrated by them alone.
	peopleOfficer := employees[p.peopleOfficerIndex].Row.WorkerID.String()

	sessions := make([]PlannedCalibrationSession, 0, len(unitOrder))
	for _, unit := range unitOrder {
		members := membersByUnit[unit]
		cohort := make([]performance.ProposedRating, 0, len(members))
		for _, index := range members {
			proposed, err := performance.CalculateProposedRating(collection, employees[index].Row.WorkerID.String(), ratingRule)
			if err != nil {
				return nil, fmt.Errorf("demoworkforce: propose %s in %s: %w", employees[index].Row.WorkerKey, spec.CycleID, err)
			}
			cohort = append(cohort, proposed)
		}
		unitHead := employees[members[0]].Row.WorkerID.String()
		panel := []string{unitHead}
		if peopleOfficer != unitHead {
			panel = append(panel, peopleOfficer)
		}
		session, err := performance.NewCalibrationSession(
			fmt.Sprintf("%s:%s", spec.CycleID, unit), graph, cohort, CalibrationFacilitatorID, panel, calibrationRule)
		if err != nil {
			return nil, fmt.Errorf("demoworkforce: calibration session %s/%s: %w", spec.CycleID, unit, err)
		}
		for _, index := range members {
			band := p.bandFor(index + 1)
			if band.CalibrateTo == "" {
				continue
			}
			participant := employees[index].Row.WorkerID.String()
			adjuster := calibrationAdjuster(graph, participant, panel)
			if adjuster == "" {
				continue
			}
			from := cohortRating(cohort, participant)
			to, err := values.NewDecimal(band.CalibrateTo, PerformanceRatingScale, values.RoundingExactRequired)
			if err != nil {
				return nil, fmt.Errorf("demoworkforce: calibrated rating %s: %w", band.CalibrateTo, err)
			}
			adjustment, err := performance.NewCalibrationAdjustment(participant, from, to, performance.CalibrationReasonConsistency, adjuster)
			if err != nil {
				return nil, fmt.Errorf("demoworkforce: adjustment for %s: %w", employees[index].Row.WorkerKey, err)
			}
			session, err = session.AddAdjustment(adjustment)
			if err != nil {
				return nil, fmt.Errorf("demoworkforce: apply adjustment for %s: %w", employees[index].Row.WorkerKey, err)
			}
		}
		ratings, err := session.Finalize()
		if err != nil {
			return nil, fmt.Errorf("demoworkforce: finalize %s/%s: %w", spec.CycleID, unit, err)
		}
		sessions = append(sessions, PlannedCalibrationSession{OrgUnit: unit, Session: session, Ratings: ratings})
	}
	return sessions, nil
}

// calibrationAdjuster picks the panel member who may move a participant's
// rating: never the participant's own manager, because a manager acting alone
// is exactly the separation the domain refuses.
func calibrationAdjuster(graph performance.FrozenParticipantReviewerGraph, participant string, panel []string) string {
	manager := ""
	for _, assignment := range graph.Reviewers {
		if assignment.ParticipantID == participant && assignment.Relationship == performance.ReviewerRelationshipManager {
			manager = assignment.ReviewerID
			break
		}
	}
	for _, member := range panel {
		if member != manager && member != participant {
			return member
		}
	}
	return ""
}

func cohortRating(cohort []performance.ProposedRating, participant string) values.Decimal {
	for _, proposed := range cohort {
		if proposed.ParticipantID == participant {
			return proposed.Rating
		}
	}
	return values.Decimal{}
}

// SeedPerformance records the planned performance history into migration
// 00108's tables inside tx, which the caller owns. Every row identity is
// derived deterministically from the cycle and the participant, so a replay
// finds the rows it wrote instead of inserting a second copy: the tables are
// append-only, and an ON CONFLICT DO NOTHING insert on a derived primary key
// is the only replay these tables can have.
func SeedPerformance(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) (PerformanceSummary, error) {
	return HarborCarePack.SeedPerformance(ctx, tx, tenant)
}

// SeedPerformance records this company's performance history inside tx.
func (p *Pack) SeedPerformance(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) (PerformanceSummary, error) {
	if tx == nil || tenant == uuid.Nil {
		return PerformanceSummary{}, fmt.Errorf("demoworkforce: seed performance: a transaction and tenant are required")
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return PerformanceSummary{}, err
	}
	plan, err := p.PlanPerformance(tenant)
	if err != nil {
		return PerformanceSummary{}, err
	}
	var summary PerformanceSummary
	for _, cycle := range plan.Cycles {
		if err := seedPerformanceCycle(ctx, tx, tenant, cycle, &summary); err != nil {
			return PerformanceSummary{}, err
		}
	}
	return summary, nil
}

func seedPerformanceCycle(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, cycle PlannedPerformanceCycle, summary *PerformanceSummary) error {
	for _, revision := range cycle.Revisions {
		rowID := deterministicID("performance-cycle", fmt.Sprintf("%s/%d", revision.CycleID, revision.Revision))
		written, err := insertOnce(ctx, tx, `
			INSERT INTO performance_cycle (tenant_id, row_id, cycle_id, revision, state, supersedes_revision, canonical_digest)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`,
			tenant, rowID, revision.CycleID, int64(revision.Revision), revision.State.String(),
			nullableRevision(revision.SupersedesRevision), storedDigest(revision.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("demoworkforce: seed cycle %s revision %d: %w", revision.CycleID, revision.Revision, err)
		}
		count(summary, &summary.CycleRevisions, written)
	}
	for _, review := range cycle.Reviews {
		rowID := deterministicID("performance-review", review.ID)
		written, err := insertOnce(ctx, tx, `
			INSERT INTO performance_review (tenant_id, row_id, review_id, reviewer_id, participant_id, cycle_id,
				cycle_revision, review_revision, supersedes_review_revision, submitted_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT DO NOTHING`,
			tenant, rowID, review.ID, review.ReviewerID, review.ParticipantID, review.CycleID,
			int64(review.CycleRevision), int64(review.ReviewRevision),
			nullableRevision(review.SupersedesReviewRevision), review.SubmittedAt.Time())
		if err != nil {
			return fmt.Errorf("demoworkforce: seed review %s: %w", review.ID, err)
		}
		count(summary, &summary.Reviews, written)
	}
	for _, planned := range cycle.Sessions {
		if err := seedCalibrationSession(ctx, tx, tenant, cycle, planned, summary); err != nil {
			return err
		}
	}
	return nil
}

func seedCalibrationSession(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, cycle PlannedPerformanceCycle,
	planned PlannedCalibrationSession, summary *PerformanceSummary) error {
	session := planned.Session
	graphJSON, err := performancestore.EncodeCalibrationGraph(session.Graph.CycleID, session.Graph.GraphRevision, session.Graph.Digest, planned.OrgUnit)
	if err != nil {
		return err
	}
	adjustmentsJSON, err := performancestore.EncodeCalibratedRatings(planned.Ratings)
	if err != nil {
		return err
	}
	rowID := deterministicID("performance-calibration-session", session.SessionID)
	written, err := insertOnce(ctx, tx, `
		INSERT INTO performance_calibration_session (tenant_id, row_id, session_id, cycle_id, cycle_revision, graph, adjustments, canonical_digest)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8) ON CONFLICT DO NOTHING`,
		tenant, rowID, session.SessionID, session.CycleID, int64(session.CycleRevision),
		string(graphJSON), string(adjustmentsJSON), storedDigest(session.CanonicalDigest))
	if err != nil {
		return fmt.Errorf("demoworkforce: seed calibration session %s: %w", session.SessionID, err)
	}
	count(summary, &summary.Sessions, written)

	finalizedAt := cycle.Spec.ClosedAt
	for _, rating := range planned.Ratings {
		caseID := fmt.Sprintf("%s:%s", session.CycleID, rating.ParticipantID)
		caseWritten, err := insertOnce(ctx, tx, `
			INSERT INTO performance_rating_case (tenant_id, row_id, case_id, participant_id, finalized, canonical_digest)
			VALUES ($1, $2, $3, $4, true, $5) ON CONFLICT DO NOTHING`,
			tenant, deterministicID("performance-rating-case", caseID), caseID, rating.ParticipantID,
			storedDigest(rating.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("demoworkforce: seed rating case %s: %w", caseID, err)
		}
		count(summary, &summary.RatingCases, caseWritten)

		eventWritten, err := insertOnce(ctx, tx, `
			INSERT INTO performance_rating_event (tenant_id, row_id, case_id, kind, actor_id, at, digest, event_sequence)
			VALUES ($1, $2, $3, 'FINALIZED', $4, $5, $6, 1) ON CONFLICT DO NOTHING`,
			tenant, deterministicID("performance-rating-event", caseID+"/1"), caseID, CalibrationFacilitatorID,
			finalizedAt, storedDigest(rating.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("demoworkforce: seed rating event %s: %w", caseID, err)
		}
		count(summary, &summary.RatingEvents, eventWritten)

		ratingWritten, err := insertOnce(ctx, tx, `
			INSERT INTO performance_final_rating (tenant_id, row_id, rating_ref, participant_id, cycle_id, final_rating, finalized_at, canonical_digest)
			VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8) ON CONFLICT DO NOTHING`,
			tenant, deterministicID("performance-final-rating", caseID), FinalRatingRef(session.CycleID, rating.ParticipantID),
			rating.ParticipantID, session.CycleID, rating.FinalRating.String(), finalizedAt, storedDigest(rating.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("demoworkforce: seed final rating %s: %w", caseID, err)
		}
		count(summary, &summary.FinalRatings, ratingWritten)
	}
	return nil
}

// FinalRatingRef is the deterministic rating reference one participant's
// finalized rating for a cycle is recorded under.
func FinalRatingRef(cycleID, participantID string) uuid.UUID {
	return deterministicID("performance-final-rating-ref", cycleID+"/"+participantID)
}

// insertOnce runs an ON CONFLICT DO NOTHING insert and reports whether it
// wrote a row.
func insertOnce(ctx context.Context, tx dbport.Tx, sql string, args ...any) (bool, error) {
	affected, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func count(summary *PerformanceSummary, written *int, wrote bool) {
	if wrote {
		*written++
		return
	}
	summary.Skipped++
}

func nullableRevision(revision uint64) any {
	if revision == 0 {
		return nil
	}
	return int64(revision)
}

// storedDigest strips the algorithm prefix the domain carries: the
// content_digest domain stores bare hex.
func storedDigest(digest string) string {
	return strings.TrimPrefix(digest, canonicalbytes.DigestAlgorithm+":")
}
