package schedopt

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const ShiftSelfServiceVersion = "schedopt-self-service/v1"

var ErrShiftSelfServiceRejected = errors.New("SCHED_OPT_045_03_REJECTED")

// ShiftSelfServiceRejection identifies a stale or invalid worker action.
type ShiftSelfServiceRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *ShiftSelfServiceRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrShiftSelfServiceRejected, r.Field, r.State, r.Version, r.Reason)
}

func (r *ShiftSelfServiceRejection) Unwrap() error { return ErrShiftSelfServiceRejected }

func shiftReject(field, state, reason string) error {
	return &ShiftSelfServiceRejection{Field: field, State: state, Version: ShiftSelfServiceVersion, Reason: reason}
}

type ShiftOfferKind string

const (
	ShiftOfferOpenClaim ShiftOfferKind = "OPEN_CLAIM"
	ShiftOfferTrade     ShiftOfferKind = "TRADE"
)

type ShiftOfferState string

const (
	ShiftOfferActive  ShiftOfferState = "OPEN"
	ShiftOfferClaimed ShiftOfferState = "CLAIMED"
	ShiftOfferTraded  ShiftOfferState = "TRADED"
	ShiftOfferClosed  ShiftOfferState = "CLOSED"
)

// ShiftOffer is bound to one published digest and one fencing token. The
// offered worker must own AssignmentID; a trade also names the requested
// assignment and its current owner.
type ShiftOffer struct {
	ID                    string
	Kind                  ShiftOfferKind
	AssignmentID          string
	WorkerRef             string
	RequestedAssignmentID string
	RequestedWorkerRef    string
	PublicationDigest     string
	FencingToken          uint64
	Reason                string
	State                 ShiftOfferState
}

// ShiftSelfServiceRepository is the durable compare-and-swap boundary for
// offers. Implementations must tenant-scope every operation and advance the
// schedule fence only when the expected published digest still matches.
type ShiftSelfServiceRepository interface {
	LoadShiftScheduleFence(context.Context, values.TenantId, string, string) (uint64, error)
	CreateShiftOffer(context.Context, values.TenantId, string, uint64, ShiftOffer) (ShiftOffer, error)
	LoadShiftOffer(context.Context, values.TenantId, string, string) (ShiftOffer, error)
	ClaimShiftOffer(context.Context, values.TenantId, string, string, uint64, string) (uint64, error)
}

type PublishedReviewPolicy struct {
	Jurisdiction string
	Rules        map[string]FairWorkweekRule
	RegularRates map[string]values.Rate
}

type ShiftSelfServiceConfig struct {
	Approved    ApprovedSchedule
	Publication Publication
	Rules       []ReviewRule
	Problem     WorkforceOptimizationProblem
	Review      PublishedReviewPolicy
	Repository  ShiftSelfServiceRepository
	ScheduleID  string
}

// ShiftSelfService serializes claim and trade acceptance for one published
// schedule revision. When Repository is supplied, offer creation and
// acceptance also advance the tenant-scoped durable schedule fence.
type ShiftSelfService struct {
	mu          sync.Mutex
	approved    ApprovedSchedule
	publication Publication
	rules       []ReviewRule
	problem     WorkforceOptimizationProblem
	review      PublishedReviewPolicy
	fence       uint64
	seen        map[string]string
	offers      map[string]ShiftOffer
	activeOn    map[string]string
	repository  ShiftSelfServiceRepository
	scheduleID  string
}

type ShiftSelfServiceResult struct {
	Approved     ApprovedSchedule
	Publication  Publication
	FencingToken uint64
}

func NewShiftSelfService(config ShiftSelfServiceConfig) (*ShiftSelfService, error) {
	approved := config.Approved
	publication := config.Publication
	if approved.BoundDigest == "" || approved.BoundDigest != approved.computedDigest() ||
		publication.Digest == "" || publication.Digest != publication.computedDigest() ||
		publication.BoundDigest != approved.BoundDigest || publication.Tenant != approved.Tenant ||
		publication.Revision != approved.Revision || !sameReviewAssignments(approved.Assignments, publication.Assignments) {
		return nil, shiftReject("publication", "UNBOUND", "approved and published assignments must match")
	}
	if err := config.Problem.Validate(); err != nil || config.Problem.CanonicalDigest == "" {
		return nil, shiftReject("hard_constraints", "UNBOUND", "a sealed SCHED-OPT-003 problem is required")
	}
	for _, assignment := range publication.Assignments {
		worker, err := parseShiftWorker(assignment.WorkerRef, publication.Tenant)
		if err != nil || !problemHasVariable(config.Problem, worker, assignment.WindowRef) {
			return nil, shiftReject("publication.assignment", "OUTSIDE_PROBLEM", "published worker and window must be in the sealed problem")
		}
	}
	seen := map[string]string{publication.IdempotencyKey: publication.BoundDigest}
	scheduleID := strings.TrimSpace(config.ScheduleID)
	if config.Repository != nil && scheduleID == "" {
		return nil, shiftReject("schedule_id", "INCOMPLETE", "durable self-service requires a stable schedule identifier")
	}
	if scheduleID == "" {
		scheduleID = publication.Revision
	}
	fence := uint64(1)
	if config.Repository != nil {
		var err error
		fence, err = config.Repository.LoadShiftScheduleFence(context.Background(), values.TenantId(publication.Tenant), scheduleID, publication.Digest)
		if err != nil || fence == 0 {
			return nil, shiftReject("fencing_token", "STALE", "durable schedule state does not match the published revision")
		}
	}
	return &ShiftSelfService{
		approved: cloneApprovedSchedule(approved), publication: clonePublication(publication),
		rules: append([]ReviewRule(nil), config.Rules...), problem: cloneOptimizationProblem(config.Problem),
		review: clonePublishedReviewPolicy(config.Review), fence: fence, seen: seen,
		offers: make(map[string]ShiftOffer), activeOn: make(map[string]string),
		repository: config.Repository, scheduleID: scheduleID,
	}, nil
}

func (s *ShiftSelfService) FencingToken() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fence
}

func (s *ShiftSelfService) OfferOpenShift(offerID, assignmentID string, owner values.EntityRef, expectedFence uint64, reason string) (ShiftOffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkScheduleFence(expectedFence); err != nil {
		return ShiftOffer{}, err
	}
	assignment, ok := findReviewAssignment(s.publication.Assignments, assignmentID)
	if !ok {
		return ShiftOffer{}, shiftReject("assignment", "NOT_FOUND", "published assignment was not found")
	}
	if err := validateOfferOwner(owner, assignment.WorkerRef, s.publication.Tenant); err != nil {
		return ShiftOffer{}, err
	}
	return s.createOffer(ShiftOffer{ID: offerID, Kind: ShiftOfferOpenClaim, AssignmentID: assignmentID, WorkerRef: assignment.WorkerRef, Reason: reason})
}

func (s *ShiftSelfService) OfferTrade(offerID, assignmentID, requestedAssignmentID string, owner values.EntityRef, expectedFence uint64, reason string) (ShiftOffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkScheduleFence(expectedFence); err != nil {
		return ShiftOffer{}, err
	}
	if assignmentID == requestedAssignmentID {
		return ShiftOffer{}, shiftReject("requested_assignment", "SAME_ASSIGNMENT", "a trade must name two different assignments")
	}
	offered, ok := findReviewAssignment(s.publication.Assignments, assignmentID)
	if !ok {
		return ShiftOffer{}, shiftReject("assignment", "NOT_FOUND", "published assignment was not found")
	}
	requested, ok := findReviewAssignment(s.publication.Assignments, requestedAssignmentID)
	if !ok {
		return ShiftOffer{}, shiftReject("requested_assignment", "NOT_FOUND", "requested published assignment was not found")
	}
	if err := validateOfferOwner(owner, offered.WorkerRef, s.publication.Tenant); err != nil {
		return ShiftOffer{}, err
	}
	if offered.WorkerRef == requested.WorkerRef {
		return ShiftOffer{}, shiftReject("requested_worker", "SAME_WORKER", "a worker cannot trade with themself")
	}
	return s.createOffer(ShiftOffer{
		ID: offerID, Kind: ShiftOfferTrade, AssignmentID: assignmentID, WorkerRef: offered.WorkerRef,
		RequestedAssignmentID: requestedAssignmentID, RequestedWorkerRef: requested.WorkerRef, Reason: reason,
	})
}

func (s *ShiftSelfService) ClaimOpenShift(offerID string, worker values.EntityRef, offerFence uint64, standings []WorkerStanding, changedAt time.Time) (ShiftSelfServiceResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	offer, err := s.activeOffer(offerID, ShiftOfferOpenClaim, offerFence)
	if err != nil {
		return ShiftSelfServiceResult{}, err
	}
	if err := validateClaimant(worker, s.publication.Tenant, offer.WorkerRef); err != nil {
		return ShiftSelfServiceResult{}, err
	}
	assignment, _ := findReviewAssignment(s.publication.Assignments, offer.AssignmentID)
	window, err := s.checkHardAssignable(worker, assignment.WindowRef, standings, []string{offer.AssignmentID})
	if err != nil {
		return ShiftSelfServiceResult{}, err
	}
	change := ManualChange{
		Kind: ChangeMove, AssignmentID: assignment.AssignmentID, WorkerRef: worker.String(),
		WindowRef: assignment.WindowRef, Reason: offer.Reason, AuthorityRef: worker.String(), ChangedAt: changedAt,
	}
	if err := s.checkNoOverlap(worker, window, []string{assignment.AssignmentID}); err != nil {
		return ShiftSelfServiceResult{}, err
	}
	return s.applyOffer(offer, []ManualChange{change}, worker, changedAt, ShiftOfferClaimed)
}

func (s *ShiftSelfService) AcceptTrade(offerID string, worker values.EntityRef, offerFence uint64, standings []WorkerStanding, changedAt time.Time) (ShiftSelfServiceResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	offer, err := s.activeOffer(offerID, ShiftOfferTrade, offerFence)
	if err != nil {
		return ShiftSelfServiceResult{}, err
	}
	if err := validateOfferOwner(worker, offer.RequestedWorkerRef, s.publication.Tenant); err != nil {
		return ShiftSelfServiceResult{}, err
	}
	first, okFirst := findReviewAssignment(s.publication.Assignments, offer.AssignmentID)
	second, okSecond := findReviewAssignment(s.publication.Assignments, offer.RequestedAssignmentID)
	if !okFirst || !okSecond || first.WorkerRef != offer.WorkerRef || second.WorkerRef != offer.RequestedWorkerRef {
		return ShiftSelfServiceResult{}, shiftReject("offer", "STALE_ASSIGNMENT", "trade assignments changed after the offer")
	}
	firstWorker, err := parseShiftWorker(first.WorkerRef, s.publication.Tenant)
	if err != nil {
		return ShiftSelfServiceResult{}, err
	}
	firstWindow, err := s.checkHardAssignable(worker, first.WindowRef, standings, []string{first.AssignmentID, second.AssignmentID})
	if err != nil {
		return ShiftSelfServiceResult{}, err
	}
	secondWindow, err := s.checkHardAssignable(firstWorker, second.WindowRef, standings, []string{first.AssignmentID, second.AssignmentID})
	if err != nil {
		return ShiftSelfServiceResult{}, err
	}
	if intervalsOverlap(firstWindow.Work, secondWindow.Work) {
		return ShiftSelfServiceResult{}, shiftReject("trade.windows", "OVERLAP", "a swap between overlapping windows cannot be applied safely")
	}
	for _, item := range []struct {
		worker values.EntityRef
		window DemandWindow
	}{{worker, firstWindow}, {firstWorker, secondWindow}} {
		if err := s.checkNoOverlap(item.worker, item.window, []string{first.AssignmentID, second.AssignmentID}); err != nil {
			return ShiftSelfServiceResult{}, err
		}
	}
	changes := []ManualChange{
		{Kind: ChangeMove, AssignmentID: first.AssignmentID, WorkerRef: worker.String(), WindowRef: first.WindowRef, Reason: offer.Reason, AuthorityRef: worker.String(), ChangedAt: changedAt},
		{Kind: ChangeMove, AssignmentID: second.AssignmentID, WorkerRef: firstWorker.String(), WindowRef: second.WindowRef, Reason: "accepted trade " + offer.ID, AuthorityRef: worker.String(), ChangedAt: changedAt},
	}
	return s.applyOffer(offer, changes, worker, changedAt, ShiftOfferTraded)
}

// Reconcile applies SCHED-OPT-007 reconciliation to the latest published
// self-service revision. Observations must come from the downstream roster or
// integration read path; this method does not manufacture coverage evidence.
func (s *ShiftSelfService) Reconcile(observed []ObservedCoverage) (ScheduleReconResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ReconcileSchedule(clonePublication(s.publication), observed)
}

func (s *ShiftSelfService) checkScheduleFence(expected uint64) error {
	if expected == 0 || expected != s.fence {
		return shiftReject("fencing_token", "STALE", "the published schedule changed; refresh before offering a shift")
	}
	return nil
}

func (s *ShiftSelfService) createOffer(offer ShiftOffer) (ShiftOffer, error) {
	if strings.TrimSpace(offer.ID) == "" || strings.TrimSpace(offer.Reason) == "" {
		return ShiftOffer{}, shiftReject("offer", "INCOMPLETE", "offer id and reason are required")
	}
	if _, exists := s.offers[offer.ID]; exists {
		return ShiftOffer{}, shiftReject("offer_id", "DUPLICATE", "offer id has already been used")
	}
	assignments := []string{offer.AssignmentID}
	if offer.Kind == ShiftOfferTrade {
		assignments = append(assignments, offer.RequestedAssignmentID)
	}
	for _, assignmentID := range assignments {
		if s.activeOn[assignmentID] != "" {
			return ShiftOffer{}, shiftReject("assignment", "ALREADY_OFFERED", "a published assignment already has an active offer")
		}
	}
	offer.PublicationDigest = s.publication.Digest
	offer.State = ShiftOfferActive
	if s.repository != nil {
		persisted, err := s.repository.CreateShiftOffer(context.Background(), values.TenantId(s.publication.Tenant), s.scheduleID, s.fence, offer)
		if err != nil {
			return ShiftOffer{}, shiftReject("fencing_token", "STALE", "durable schedule fence refused this offer")
		}
		offer = persisted
		s.fence = persisted.FencingToken
	} else {
		s.fence++
		offer.FencingToken = s.fence
	}
	s.offers[offer.ID] = offer
	for _, assignmentID := range assignments {
		s.activeOn[assignmentID] = offer.ID
	}
	return offer, nil
}

func (s *ShiftSelfService) activeOffer(id string, kind ShiftOfferKind, fence uint64) (ShiftOffer, error) {
	offer, ok := s.offers[id]
	if !ok && s.repository != nil {
		loaded, err := s.repository.LoadShiftOffer(context.Background(), values.TenantId(s.publication.Tenant), s.scheduleID, id)
		if err == nil {
			offer = loaded
			s.offers[id] = loaded
			ok = true
		}
	}
	if !ok {
		if s.repository == nil {
			return ShiftOffer{}, shiftReject("offer", "NOT_FOUND", "shift offer was not found")
		}
		return ShiftOffer{}, shiftReject("offer", "STALE", "shift offer is absent from the durable published schedule")
	}
	if offer.State != ShiftOfferActive || offer.Kind != kind || offer.FencingToken != fence || offer.PublicationDigest != s.publication.Digest {
		return ShiftOffer{}, shiftReject("fencing_token", "STALE", "offer is closed or its published revision changed")
	}
	return offer, nil
}

func (s *ShiftSelfService) checkHardAssignable(worker values.EntityRef, windowID string, standings []WorkerStanding, excluded []string) (DemandWindow, error) {
	var window *DemandWindow
	for i := range s.problem.DemandWindows {
		if s.problem.DemandWindows[i].SignalID == windowID {
			copy := s.problem.DemandWindows[i]
			window = &copy
			break
		}
	}
	if window == nil {
		return DemandWindow{}, shiftReject("window", "NOT_FOUND", "shift window is absent from the sealed hard-constraint problem")
	}
	evaluation, err := s.problem.EvaluateHardConstraints(standings)
	if err != nil {
		return DemandWindow{}, shiftReject("hard_constraints", "INVALID_EVIDENCE", "current worker standings failed SCHED-OPT-003 evaluation")
	}
	for _, verdict := range evaluation.Evaluations {
		if verdict.CandidateRef != worker || verdict.DemandWindowID != windowID {
			continue
		}
		if verdict.Verdict != VerdictAssignable {
			kinds := make([]string, len(verdict.BlockingKinds))
			for i, kind := range verdict.BlockingKinds {
				kinds[i] = string(kind)
			}
			return DemandWindow{}, shiftReject("hard_constraints", "BLOCKED", "claimant fails: "+strings.Join(kinds, ","))
		}
		return *window, nil
	}
	return DemandWindow{}, shiftReject("hard_constraints", "OUTSIDE_PROBLEM", "claimant and shift window are not a decision variable in the sealed problem")
}

func (s *ShiftSelfService) checkNoOverlap(worker values.EntityRef, target DemandWindow, excluded []string) error {
	exclude := make(map[string]struct{}, len(excluded))
	for _, id := range excluded {
		exclude[id] = struct{}{}
	}
	for _, assignment := range s.publication.Assignments {
		if _, skip := exclude[assignment.AssignmentID]; skip || assignment.WorkerRef != worker.String() {
			continue
		}
		for _, window := range s.problem.DemandWindows {
			if window.SignalID == assignment.WindowRef && intervalsOverlap(window.Work, target.Work) {
				return shiftReject("worker", "OVERLAPPING_ASSIGNMENT", "claimant already has an overlapping published shift")
			}
		}
	}
	return nil
}

func (s *ShiftSelfService) applyOffer(offer ShiftOffer, changes []ManualChange, actor values.EntityRef, changedAt time.Time, resultState ShiftOfferState) (ShiftSelfServiceResult, error) {
	schedule := CandidateSchedule{
		Tenant: s.approved.Tenant, Revision: s.approved.Revision, RuleDigest: s.approved.RuleDigest,
		Assignments: append([]ReviewAssignment(nil), s.publication.Assignments...), Rules: append([]ReviewRule(nil), s.rules...),
		ReviewLifecycle: ReviewPublished,
	}
	approved, err := ApplyPublishedReview(schedule, s.publication, s.review.Jurisdiction, s.review.Rules, s.review.RegularRates, changes, actor.String(), changedAt)
	if err != nil {
		var rejection *ReviewRejection
		if errors.As(err, &rejection) {
			return ShiftSelfServiceResult{}, shiftReject(rejection.Field, rejection.State, rejection.Reason)
		}
		return ShiftSelfServiceResult{}, shiftReject("published_review", "REJECTED", "published review rejected the change")
	}
	nextFence := offer.FencingToken + 1
	approved.Revision = fmt.Sprintf("%s/self-service-%d", s.approved.Revision, nextFence)
	approved.BoundDigest = approved.computedDigest()
	idempotencyKey := fmt.Sprintf("shift-self-service:%s:%d", offer.ID, offer.FencingToken)
	publication, err := PublishSchedule(approved, idempotencyKey, s.seen, changedAt)
	if err != nil {
		return ShiftSelfServiceResult{}, shiftReject("publication", "REJECTED", "SCHED-OPT-007 refused the reviewed revision")
	}
	if s.repository != nil {
		nextFence, err = s.repository.ClaimShiftOffer(context.Background(), values.TenantId(s.publication.Tenant), s.scheduleID, offer.ID, offer.FencingToken, publication.Digest)
		if err != nil {
			return ShiftSelfServiceResult{}, shiftReject("fencing_token", "STALE", "durable offer or published schedule changed before acceptance")
		}
	}
	s.approved = cloneApprovedSchedule(approved)
	s.publication = clonePublication(publication)
	s.fence = nextFence
	s.seen[idempotencyKey] = approved.BoundDigest
	offer.State = resultState
	s.offers[offer.ID] = offer
	for assignmentID, activeID := range s.activeOn {
		if activeID == offer.ID || s.offers[activeID].PublicationDigest != publication.Digest {
			delete(s.activeOn, assignmentID)
			if activeID != offer.ID {
				other := s.offers[activeID]
				other.State = ShiftOfferClosed
				s.offers[activeID] = other
			}
		}
	}
	return ShiftSelfServiceResult{Approved: cloneApprovedSchedule(approved), Publication: clonePublication(publication), FencingToken: s.fence}, nil
}

func validateOfferOwner(owner values.EntityRef, assignedWorker string, tenant string) error {
	if err := owner.Validate(); err != nil || string(owner.Tenant) != tenant || owner.String() != assignedWorker {
		return shiftReject("worker", "NOT_OWNER", "only the published worker may create this offer")
	}
	return nil
}

func validateClaimant(worker values.EntityRef, tenant string, owner string) error {
	if err := worker.Validate(); err != nil || string(worker.Tenant) != tenant {
		return shiftReject("worker", "INVALID", "claimant must be a valid worker in the published tenant")
	}
	if worker.String() == owner {
		return shiftReject("worker", "ALREADY_ASSIGNED", "the current worker cannot claim their own open shift")
	}
	return nil
}

func findReviewAssignment(assignments []ReviewAssignment, id string) (ReviewAssignment, bool) {
	for _, assignment := range assignments {
		if assignment.AssignmentID == id {
			return assignment, true
		}
	}
	return ReviewAssignment{}, false
}

func parseShiftWorker(text string, tenant string) (values.EntityRef, error) {
	var worker values.EntityRef
	if err := worker.UnmarshalText([]byte(text)); err != nil || string(worker.Tenant) != tenant || worker.Kind != "candidate" {
		return values.EntityRef{}, shiftReject("worker", "INVALID", "published worker reference is invalid or crosses tenant")
	}
	return worker, nil
}

func problemHasVariable(problem WorkforceOptimizationProblem, worker values.EntityRef, window string) bool {
	for _, variable := range problem.DecisionVariables {
		if variable.CandidateRef == worker && variable.DemandWindowID == window {
			return true
		}
	}
	return false
}

func intervalsOverlap(a, b values.EffectiveInterval) bool {
	aStart, hasAStart := a.StartInstant()
	aEnd, hasAEnd := a.EndInstant()
	bStart, hasBStart := b.StartInstant()
	bEnd, hasBEnd := b.EndInstant()
	if !hasAStart || !hasBStart {
		return true
	}
	if hasAEnd && !aEnd.After(bStart) || hasBEnd && !bEnd.After(aStart) {
		return false
	}
	return true
}

func sameReviewAssignments(a, b []ReviewAssignment) bool {
	parts := func(assignments []ReviewAssignment) []string {
		out := make([]string, len(assignments))
		for i, assignment := range assignments {
			out[i] = strings.Join([]string{assignment.AssignmentID, assignment.WorkerRef, assignment.DemandRef, assignment.WindowRef}, "\x00")
		}
		sort.Strings(out)
		return out
	}
	left, right := parts(a), parts(b)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func clonePublication(publication Publication) Publication {
	publication.Assignments = append([]ReviewAssignment(nil), publication.Assignments...)
	publication.Messages = append([]string(nil), publication.Messages...)
	publication.IntegrationEffects = append([]string(nil), publication.IntegrationEffects...)
	return publication
}

func cloneApprovedSchedule(approved ApprovedSchedule) ApprovedSchedule {
	approved.Assignments = append([]ReviewAssignment(nil), approved.Assignments...)
	approved.Changes = append([]ManualChange(nil), approved.Changes...)
	approved.FairWorkweekAssessments = append([]FairWorkweekAssessment(nil), approved.FairWorkweekAssessments...)
	for i := range approved.FairWorkweekAssessments {
		if approved.FairWorkweekAssessments[i].Obligation != nil {
			copy := *approved.FairWorkweekAssessments[i].Obligation
			approved.FairWorkweekAssessments[i].Obligation = &copy
		}
	}
	approved.PremiumObligations = append([]PremiumObligation(nil), approved.PremiumObligations...)
	return approved
}

func cloneOptimizationProblem(problem WorkforceOptimizationProblem) WorkforceOptimizationProblem {
	problem.Population.Candidates = problem.Population.CandidateFactsList()
	problem.Population.ExclusionCounts = problem.Population.Exclusions()
	problem.DemandWindows = append([]DemandWindow(nil), problem.DemandWindows...)
	problem.HardConstraints = cloneConstraints(problem.HardConstraints)
	problem.SoftConstraints = cloneConstraints(problem.SoftConstraints)
	problem.DecisionVariables = append([]DecisionVariable(nil), problem.DecisionVariables...)
	return problem
}

func cloneConstraints(constraints []Constraint) []Constraint {
	out := append([]Constraint(nil), constraints...)
	for i := range out {
		out[i].QualificationRefs = append([]values.EntityRef(nil), constraints[i].QualificationRefs...)
		out[i].AuthorizationRefs = append([]values.EntityRef(nil), constraints[i].AuthorizationRefs...)
	}
	return out
}

func clonePublishedReviewPolicy(policy PublishedReviewPolicy) PublishedReviewPolicy {
	rules := policy.Rules
	policy.Rules = make(map[string]FairWorkweekRule, len(rules))
	for key, rule := range rules {
		policy.Rules[key] = rule
	}
	rates := policy.RegularRates
	policy.RegularRates = make(map[string]values.Rate, len(rates))
	for key, rate := range rates {
		policy.RegularRates[key] = rate
	}
	return policy
}
