package performance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrStoreRefused is the common classification for persistence-boundary
// refusals. The storage adapter must preserve the more specific cause too.
var ErrStoreRefused = errors.New("performance: store operation refused")

var (
	ErrDuplicateRevision   = errors.New("PERFORMANCE_DUPLICATE_REVISION")
	ErrStaleRevision       = errors.New("PERFORMANCE_STALE_REVISION")
	ErrDuplicateEvent      = errors.New("PERFORMANCE_DUPLICATE_EVENT")
	ErrNotFound            = errors.New("PERFORMANCE_NOT_FOUND")
	ErrFinalRatingNotFinal = errors.New("PERFORMANCE_RATING_NOT_FINALIZED")
)

// RefusalError carries a stable code and field without exposing a payload.
type RefusalError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *RefusalError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("performance: %s: %s", e.Code, e.Reason)
	}
	return fmt.Sprintf("performance: %s: %s: %s", e.Code, e.Field, e.Reason)
}

func (e *RefusalError) Unwrap() error { return e.Cause }

func (e *RefusalError) Is(target error) bool {
	return target == ErrStoreRefused || target == e.Cause
}

// TenantID keeps the domain port independent of UUID and database packages.
type TenantID interface{ String() string }

// CycleRevision is the durable, immutable portion of a PerformanceCycle. The
// migration intentionally stores the canonical digest rather than duplicating
// the cycle's referenced population, calendar, and rating-scale documents.
type CycleRevision struct {
	CycleID            string
	Revision           uint64
	State              PerformanceCycleState
	SupersedesRevision uint64
	CanonicalDigest    string
}

// RatingCaseRecord is the current-state row used to guard finalization.
type RatingCaseRecord struct {
	CaseID          string
	ParticipantID   string
	Finalized       bool
	CanonicalDigest string
}

// RatingEventRecord is one append-only rating-case event. EventSequence is
// supplied by the caller and is unique within a tenant and case.
type RatingEventRecord struct {
	Kind             RatingEventKind
	ActorID          string
	At               values.Instant
	ContestDigest    string
	CorrectionDigest string
	Digest           string
}

// FinalRatingRecord is the durable final-rating projection. RatingRef is a
// UUID string because UUID is a storage concern and is validated by the data
// adapter at the boundary.
type FinalRatingRecord struct {
	RatingRef       string
	ParticipantID   string
	CycleID         string
	FinalRating     values.Decimal
	FinalizedAt     values.Instant
	CanonicalDigest string
}

// CalibrationSessionRecord stores the exact JSON documents represented by
// graph and adjustments. The database validates that both are JSON; domain
// validation remains the caller's responsibility.
type CalibrationSessionRecord struct {
	SessionID       string
	CycleID         string
	CycleRevision   uint64
	Graph           []byte
	Adjustments     []byte
	CanonicalDigest string
}

// ReviewRecord is the immutable revision identity persisted for a submitted
// review. Its narrative and rating-scale values remain content-addressed in
// the domain and are represented by the digest fields available in the table.
type ReviewRecord struct {
	ReviewID                 string
	ReviewerID               string
	ParticipantID            string
	CycleID                  string
	CycleRevision            uint64
	ReviewRevision           uint64
	SupersedesReviewRevision uint64
	SubmittedAt              values.Instant
}

// OutcomeLinkRecord is one durable link or unlink observation.
type OutcomeLinkRecord struct {
	RatingRef      string
	RatingDigest   string
	RatingRevision uint64
	Action         OutcomeLinkAction
	EffectiveAt    values.Instant
	Digest         string
}

// Store is the tenant-aware persistence port for the seven performance tables.
// Immutable rows are added as new records; only a rating case may move from
// unfinalized to finalized, and that transition is compare-and-swap guarded.
type Store interface {
	SaveCycle(context.Context, TenantID, CycleRevision) error
	LoadCycle(context.Context, TenantID, string, uint64) (CycleRevision, error)
	SaveParticipantReviewerGraph(context.Context, TenantID, FrozenParticipantReviewerGraph) error
	ListOpenParticipantReviewerGraphsForMember(context.Context, TenantID, string) ([]FrozenParticipantReviewerGraph, error)
	SaveRatingCase(context.Context, TenantID, RatingCaseRecord) error
	LoadRatingCase(context.Context, TenantID, string) (RatingCaseRecord, error)
	FinalizeRatingCase(context.Context, TenantID, string, string) error
	AppendRatingEvent(context.Context, TenantID, string, uint64, RatingEventRecord) error
	ListRatingEvents(context.Context, TenantID, string) ([]RatingEventRecord, error)
	SaveFinalRating(context.Context, TenantID, string, FinalRatingRecord) error
	LoadFinalRating(context.Context, TenantID, string, string) (FinalRatingRecord, error)
	SaveCalibrationSession(context.Context, TenantID, CalibrationSessionRecord) error
	LoadCalibrationSession(context.Context, TenantID, string) (CalibrationSessionRecord, error)
	SaveReview(context.Context, TenantID, ReviewRecord) error
	LoadReview(context.Context, TenantID, string, uint64) (ReviewRecord, error)
	AppendOutcomeLink(context.Context, TenantID, OutcomeLinkRecord) error
	ListOutcomeLinks(context.Context, TenantID, string) ([]OutcomeLinkRecord, error)
}

// MemoryStore is the concurrency-safe reference implementation of Store.
// It is useful to domain callers and tests without introducing a database into
// the kernel package.
type MemoryStore struct {
	mu       sync.RWMutex
	cycles   map[string]map[uint64]CycleRevision
	graphs   map[string]map[string]map[uint64]map[uint64]FrozenParticipantReviewerGraph
	cases    map[string]RatingCaseRecord
	events   map[string]map[uint64]RatingEventRecord
	finals   map[string]FinalRatingRecord
	sessions map[string]CalibrationSessionRecord
	reviews  map[string]map[uint64]ReviewRecord
	outcomes map[string][]OutcomeLinkRecord
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		cycles: make(map[string]map[uint64]CycleRevision),
		graphs: make(map[string]map[string]map[uint64]map[uint64]FrozenParticipantReviewerGraph),
		cases:  make(map[string]RatingCaseRecord), events: make(map[string]map[uint64]RatingEventRecord),
		finals: make(map[string]FinalRatingRecord), sessions: make(map[string]CalibrationSessionRecord),
		reviews: make(map[string]map[uint64]ReviewRecord), outcomes: make(map[string][]OutcomeLinkRecord),
	}
}

func tenantKey(tenant TenantID) (string, error) {
	if tenant == nil || strings.TrimSpace(tenant.String()) == "" {
		return "", &RefusalError{Code: "PERFORMANCE_INVALID_TENANT", Field: "tenant_id", Reason: "tenant id is required", Cause: ErrStoreRefused}
	}
	return tenant.String(), nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return &RefusalError{Code: "PERFORMANCE_INVALID_CONTEXT", Field: "context", Reason: "context is required", Cause: ErrStoreRefused}
	}
	return ctx.Err()
}

func key(tenant, id string) string { return tenant + "\x00" + id }

func refuse(code, field, reason string, cause error) error {
	return &RefusalError{Code: code, Field: field, Reason: reason, Cause: cause}
}

func validateCycle(c CycleRevision) error {
	if strings.TrimSpace(c.CycleID) == "" || c.Revision == 0 || !c.State.Valid() || c.CanonicalDigest == "" {
		return refuse("PERFORMANCE_INVALID_CYCLE", "cycle", "cycle revision is incomplete", ErrStoreRefused)
	}
	if c.SupersedesRevision >= c.Revision {
		return refuse("PERFORMANCE_INVALID_CYCLE", "supersedes_revision", "superseded revision must precede revision", ErrStoreRefused)
	}
	return nil
}

func validateCase(c RatingCaseRecord) error {
	if strings.TrimSpace(c.CaseID) == "" || strings.TrimSpace(c.ParticipantID) == "" || strings.TrimSpace(c.CanonicalDigest) == "" {
		return refuse("PERFORMANCE_INVALID_CASE", "case", "rating case is incomplete", ErrStoreRefused)
	}
	return nil
}

func validEventKind(kind RatingEventKind) bool {
	return kind == RatingEventContestRaised || kind == RatingEventCorrectionDecided || kind == RatingEventFinalized
}

func validateEvent(e RatingEventRecord, sequence uint64) error {
	if sequence == 0 || !validEventKind(e.Kind) || strings.TrimSpace(e.ActorID) == "" || !e.At.IsSet() || strings.TrimSpace(e.Digest) == "" {
		return refuse("PERFORMANCE_INVALID_EVENT", "event", "rating event is incomplete", ErrStoreRefused)
	}
	return nil
}

func validateSession(s CalibrationSessionRecord) error {
	if strings.TrimSpace(s.SessionID) == "" || strings.TrimSpace(s.CycleID) == "" || s.CycleRevision == 0 || len(s.Graph) == 0 || len(s.Adjustments) == 0 || strings.TrimSpace(s.CanonicalDigest) == "" {
		return refuse("PERFORMANCE_INVALID_SESSION", "session", "calibration session is incomplete", ErrStoreRefused)
	}
	return nil
}

func validateReview(r ReviewRecord) error {
	if strings.TrimSpace(r.ReviewID) == "" || strings.TrimSpace(r.ReviewerID) == "" || strings.TrimSpace(r.ParticipantID) == "" || strings.TrimSpace(r.CycleID) == "" || r.CycleRevision == 0 || r.ReviewRevision == 0 || !r.SubmittedAt.IsSet() {
		return refuse("PERFORMANCE_INVALID_REVIEW", "review", "review is incomplete", ErrStoreRefused)
	}
	return nil
}

func (s *MemoryStore) SaveCycle(ctx context.Context, tenant TenantID, c CycleRevision) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return err
	}
	if err := validateCycle(c); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(t, c.CycleID)
	if s.cycles[k] == nil {
		s.cycles[k] = make(map[uint64]CycleRevision)
	}
	if _, ok := s.cycles[k][c.Revision]; ok {
		return refuse(ErrDuplicateRevision.Error(), "revision", "cycle revision is already stored", ErrDuplicateRevision)
	}
	if len(s.cycles[k]) == 0 && (c.Revision != 1 || c.SupersedesRevision != 0) {
		return refuse(ErrStaleRevision.Error(), "revision", "first cycle revision must be one", ErrStaleRevision)
	}
	if len(s.cycles[k]) > 0 {
		latest := latestCycle(s.cycles[k])
		if c.Revision != latest.Revision+1 || c.SupersedesRevision != latest.Revision {
			return refuse(ErrStaleRevision.Error(), "supersedes_revision", "cycle revision does not extend the current chain", ErrStaleRevision)
		}
	}
	s.cycles[k][c.Revision] = c
	return nil
}

func latestCycle(m map[uint64]CycleRevision) CycleRevision {
	var out CycleRevision
	for _, c := range m {
		if c.Revision > out.Revision {
			out = c
		}
	}
	return out
}

func (s *MemoryStore) LoadCycle(ctx context.Context, tenant TenantID, cycleID string, revision uint64) (CycleRevision, error) {
	if err := checkContext(ctx); err != nil {
		return CycleRevision{}, err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return CycleRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.cycles[key(t, cycleID)][revision]
	if !ok {
		return CycleRevision{}, refuse(ErrNotFound.Error(), "cycle_id", "cycle revision was not found", ErrNotFound)
	}
	return c, nil
}

// SaveParticipantReviewerGraph appends one validated, frozen graph revision
// for the current OPEN cycle revision. A later amendment is a new immutable
// graph revision and can never replace an existing snapshot.
func (s *MemoryStore) SaveParticipantReviewerGraph(ctx context.Context, tenant TenantID, graph FrozenParticipantReviewerGraph) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return err
	}
	if err := graph.Validate(); err != nil {
		return refuse("PERFORMANCE_INVALID_GRAPH", "graph", "frozen participant/reviewer graph is invalid", err)
	}
	if graph.GraphRevision == 0 || graph.SupersedesGraphRevision+1 != graph.GraphRevision {
		return refuse(ErrStaleRevision.Error(), "graph_revision", "graph revision must extend its frozen predecessor", ErrStaleRevision)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cycleRevisions := s.cycles[key(t, graph.CycleID)]
	cycle := cycleRevisions[graph.CycleRevision]
	if cycle.Revision == 0 || cycle.State != PerformanceCycleOpen || latestCycle(cycleRevisions).Revision != graph.CycleRevision {
		return refuse(ErrStaleRevision.Error(), "cycle_revision", "graph must bind the current open cycle revision", ErrStaleRevision)
	}
	if s.graphs[t] == nil {
		s.graphs[t] = make(map[string]map[uint64]map[uint64]FrozenParticipantReviewerGraph)
	}
	if s.graphs[t][graph.CycleID] == nil {
		s.graphs[t][graph.CycleID] = make(map[uint64]map[uint64]FrozenParticipantReviewerGraph)
	}
	if s.graphs[t][graph.CycleID][graph.CycleRevision] == nil {
		s.graphs[t][graph.CycleID][graph.CycleRevision] = make(map[uint64]FrozenParticipantReviewerGraph)
	}
	graphs := s.graphs[t][graph.CycleID][graph.CycleRevision]
	if _, exists := graphs[graph.GraphRevision]; exists {
		return refuse(ErrDuplicateRevision.Error(), "graph_revision", "participant/reviewer graph revision is already stored", ErrDuplicateRevision)
	}
	if uint64(len(graphs))+1 != graph.GraphRevision {
		return refuse(ErrStaleRevision.Error(), "graph_revision", "participant/reviewer graph revision is not the next revision", ErrStaleRevision)
	}
	graphs[graph.GraphRevision] = cloneFrozenParticipantReviewerGraph(graph)
	return nil
}

// ListOpenParticipantReviewerGraphsForMember returns only the latest frozen
// graph snapshot for each currently OPEN cycle in which memberID is a
// participant or assigned reviewer. Results are detached and deterministically
// ordered by cycle id.
func (s *MemoryStore) ListOpenParticipantReviewerGraphsForMember(ctx context.Context, tenant TenantID, memberID string) ([]FrozenParticipantReviewerGraph, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(memberID) == "" {
		return nil, refuse("PERFORMANCE_INVALID_MEMBER", "member_id", "member reference is required", ErrStoreRefused)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]FrozenParticipantReviewerGraph, 0)
	for cycleID, byCycle := range s.graphs[t] {
		revisions := s.cycles[key(t, cycleID)]
		if len(revisions) == 0 {
			continue
		}
		cycle, ok := revisions[latestCycle(revisions).Revision]
		if !ok || cycle.State != PerformanceCycleOpen {
			continue
		}
		byGraphRevision := byCycle[cycle.Revision]
		graph, ok := byGraphRevision[uint64(len(byGraphRevision))]
		if !ok || !frozenGraphContainsMember(graph, memberID) {
			continue
		}
		result = append(result, cloneFrozenParticipantReviewerGraph(graph))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CycleID < result[j].CycleID })
	return result, nil
}

func frozenGraphContainsMember(graph FrozenParticipantReviewerGraph, memberID string) bool {
	for _, participant := range graph.Participants {
		if participant.ID == memberID {
			return true
		}
	}
	for _, assignment := range graph.Reviewers {
		if assignment.ReviewerID == memberID {
			return true
		}
	}
	return false
}

func (s *MemoryStore) SaveRatingCase(ctx context.Context, tenant TenantID, c RatingCaseRecord) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return err
	}
	if err := validateCase(c); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(t, c.CaseID)
	old, ok := s.cases[k]
	if !ok {
		s.cases[k] = c
		return nil
	}
	if old.Finalized && !c.Finalized {
		return refuse(ErrStaleRevision.Error(), "finalized", "a finalized case cannot be reopened", ErrStaleRevision)
	}
	if old.Finalized == c.Finalized && old.CanonicalDigest == c.CanonicalDigest {
		return refuse(ErrDuplicateRevision.Error(), "case_id", "rating case is already stored", ErrDuplicateRevision)
	}
	if c.Finalized && !old.Finalized {
		s.cases[k] = c
		return nil
	}
	return refuse(ErrStaleRevision.Error(), "finalized", "rating case update is not a finalization CAS", ErrStaleRevision)
}

func (s *MemoryStore) LoadRatingCase(ctx context.Context, tenant TenantID, id string) (RatingCaseRecord, error) {
	if err := checkContext(ctx); err != nil {
		return RatingCaseRecord{}, err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return RatingCaseRecord{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.cases[key(t, id)]
	if !ok {
		return RatingCaseRecord{}, refuse(ErrNotFound.Error(), "case_id", "rating case was not found", ErrNotFound)
	}
	return c, nil
}

func (s *MemoryStore) FinalizeRatingCase(ctx context.Context, tenant TenantID, id, digest string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(t, id)
	c, ok := s.cases[k]
	if !ok {
		return refuse(ErrNotFound.Error(), "case_id", "rating case was not found", ErrNotFound)
	}
	if c.Finalized {
		return refuse(ErrStaleRevision.Error(), "finalized", "rating case was already finalized", ErrStaleRevision)
	}
	if digest == "" {
		return refuse("PERFORMANCE_INVALID_DIGEST", "canonical_digest", "canonical digest is required", ErrStoreRefused)
	}
	c.Finalized = true
	c.CanonicalDigest = digest
	s.cases[k] = c
	return nil
}

func (s *MemoryStore) AppendRatingEvent(ctx context.Context, tenant TenantID, id string, sequence uint64, e RatingEventRecord) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return err
	}
	if err := validateEvent(e, sequence); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(t, id)
	if s.events[k] == nil {
		s.events[k] = make(map[uint64]RatingEventRecord)
	}
	if _, ok := s.events[k][sequence]; ok {
		return refuse(ErrDuplicateEvent.Error(), "event_sequence", "rating event sequence is already stored", ErrDuplicateEvent)
	}
	s.events[k][sequence] = e
	return nil
}

func (s *MemoryStore) ListRatingEvents(ctx context.Context, tenant TenantID, id string) ([]RatingEventRecord, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	events := s.events[key(t, id)]
	seq := make([]int, 0, len(events))
	for n := range events {
		seq = append(seq, int(n))
	}
	sort.Ints(seq)
	out := make([]RatingEventRecord, 0, len(seq))
	for _, n := range seq {
		out = append(out, events[uint64(n)])
	}
	return out, nil
}

func (s *MemoryStore) SaveFinalRating(ctx context.Context, tenant TenantID, caseID string, r FinalRatingRecord) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return err
	}
	if caseID == "" || r.RatingRef == "" || r.ParticipantID == "" || r.CycleID == "" || !r.FinalizedAt.IsSet() || r.CanonicalDigest == "" {
		return refuse("PERFORMANCE_INVALID_FINAL_RATING", "final_rating", "final rating is incomplete", ErrStoreRefused)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cases[key(t, caseID)]
	if !ok {
		return refuse(ErrNotFound.Error(), "case_id", "rating case was not found", ErrNotFound)
	}
	if !c.Finalized {
		return refuse(ErrFinalRatingNotFinal.Error(), "finalized", "final rating requires a finalized case", ErrFinalRatingNotFinal)
	}
	k := key(t, r.ParticipantID+"\x00"+r.CycleID)
	if _, exists := s.finals[k]; exists {
		return refuse(ErrDuplicateRevision.Error(), "participant_id", "final rating is already stored", ErrDuplicateRevision)
	}
	s.finals[k] = r
	return nil
}

func (s *MemoryStore) LoadFinalRating(ctx context.Context, tenant TenantID, participantID, cycleID string) (FinalRatingRecord, error) {
	if err := checkContext(ctx); err != nil {
		return FinalRatingRecord{}, err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return FinalRatingRecord{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.finals[key(t, participantID+"\x00"+cycleID)]
	if !ok {
		return FinalRatingRecord{}, refuse(ErrNotFound.Error(), "participant_id", "final rating was not found", ErrNotFound)
	}
	return r, nil
}

func (s *MemoryStore) SaveCalibrationSession(ctx context.Context, tenant TenantID, r CalibrationSessionRecord) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return err
	}
	if err := validateSession(r); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(t, r.SessionID)
	if _, ok := s.sessions[k]; ok {
		return refuse(ErrDuplicateRevision.Error(), "session_id", "calibration session is already stored", ErrDuplicateRevision)
	}
	s.sessions[k] = r
	return nil
}
func (s *MemoryStore) LoadCalibrationSession(ctx context.Context, tenant TenantID, id string) (CalibrationSessionRecord, error) {
	if err := checkContext(ctx); err != nil {
		return CalibrationSessionRecord{}, err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return CalibrationSessionRecord{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.sessions[key(t, id)]
	if !ok {
		return CalibrationSessionRecord{}, refuse(ErrNotFound.Error(), "session_id", "calibration session was not found", ErrNotFound)
	}
	r.Graph = append([]byte(nil), r.Graph...)
	r.Adjustments = append([]byte(nil), r.Adjustments...)
	return r, nil
}

func (s *MemoryStore) SaveReview(ctx context.Context, tenant TenantID, r ReviewRecord) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return err
	}
	if err := validateReview(r); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(t, r.ReviewID)
	if s.reviews[k] == nil {
		s.reviews[k] = make(map[uint64]ReviewRecord)
	}
	if _, ok := s.reviews[k][r.ReviewRevision]; ok {
		return refuse(ErrDuplicateRevision.Error(), "review_revision", "review revision is already stored", ErrDuplicateRevision)
	}
	if len(s.reviews[k]) == 0 && r.SupersedesReviewRevision != 0 {
		return refuse(ErrStaleRevision.Error(), "supersedes_review_revision", "first review revision has no predecessor", ErrStaleRevision)
	}
	if len(s.reviews[k]) > 0 {
		latest := latestReview(s.reviews[k])
		if r.ReviewRevision != latest.ReviewRevision+1 || r.SupersedesReviewRevision != latest.ReviewRevision {
			return refuse(ErrStaleRevision.Error(), "supersedes_review_revision", "review revision does not extend the current chain", ErrStaleRevision)
		}
	}
	s.reviews[k][r.ReviewRevision] = r
	return nil
}
func latestReview(m map[uint64]ReviewRecord) ReviewRecord {
	var out ReviewRecord
	for _, r := range m {
		if r.ReviewRevision > out.ReviewRevision {
			out = r
		}
	}
	return out
}
func (s *MemoryStore) LoadReview(ctx context.Context, tenant TenantID, id string, revision uint64) (ReviewRecord, error) {
	if err := checkContext(ctx); err != nil {
		return ReviewRecord{}, err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return ReviewRecord{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.reviews[key(t, id)][revision]
	if !ok {
		return ReviewRecord{}, refuse(ErrNotFound.Error(), "review_id", "review revision was not found", ErrNotFound)
	}
	return r, nil
}

func (s *MemoryStore) AppendOutcomeLink(ctx context.Context, tenant TenantID, r OutcomeLinkRecord) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return err
	}
	if r.RatingRef == "" || r.RatingDigest == "" || r.RatingRevision == 0 || (r.Action != OutcomeLinkActionLink && r.Action != OutcomeLinkActionUnlink) || !r.EffectiveAt.IsSet() || r.Digest == "" {
		return refuse("PERFORMANCE_INVALID_OUTCOME_LINK", "outcome_link", "outcome link is incomplete", ErrStoreRefused)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(t, r.RatingRef)
	for _, old := range s.outcomes[k] {
		if old.Digest == r.Digest {
			return refuse(ErrDuplicateRevision.Error(), "digest", "outcome link is already stored", ErrDuplicateRevision)
		}
	}
	s.outcomes[k] = append(s.outcomes[k], r)
	return nil
}
func (s *MemoryStore) ListOutcomeLinks(ctx context.Context, tenant TenantID, ratingRef string) ([]OutcomeLinkRecord, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	t, err := tenantKey(tenant)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]OutcomeLinkRecord(nil), s.outcomes[key(t, ratingRef)]...)
	return out, nil
}

var _ Store = (*MemoryStore)(nil)
