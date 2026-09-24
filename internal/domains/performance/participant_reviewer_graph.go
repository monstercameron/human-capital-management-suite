package performance

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidParticipantReviewerGraph = errors.New("performance: invalid participant and reviewer graph")
	ErrSelfReviewRejected              = errors.New("performance: self-review is not permitted")
	ErrReviewerParticipantRejected     = errors.New("performance: reviewer is also a participant")
	ErrLateAssignmentRejected          = errors.New("performance: late reviewer assignment requires a new frozen revision")
	ErrReassignmentRejected            = errors.New("performance: reviewer reassignment requires a new frozen revision")
)

// ReviewerRelationshipKind is the closed relationship vocabulary captured at
// freeze time. A relationship is evidence for routing, not an authorization
// decision.
type ReviewerRelationshipKind string

const (
	ReviewerRelationshipManager      ReviewerRelationshipKind = "MANAGER"
	ReviewerRelationshipPeer         ReviewerRelationshipKind = "PEER"
	ReviewerRelationshipDirectReport ReviewerRelationshipKind = "DIRECT_REPORT"
	ReviewerRelationshipProject      ReviewerRelationshipKind = "PROJECT_PARTNER"
	ReviewerRelationshipHRPartner    ReviewerRelationshipKind = "HR_PARTNER"
)

func (k ReviewerRelationshipKind) Valid() bool {
	switch k {
	case ReviewerRelationshipManager, ReviewerRelationshipPeer, ReviewerRelationshipDirectReport,
		ReviewerRelationshipProject, ReviewerRelationshipHRPartner:
		return true
	default:
		return false
	}
}

// GraphRevisionPolicy makes late assignment and reassignment behavior
// explicit. A frozen graph is never edited in place.
type GraphRevisionPolicy string

const (
	GraphPolicyReject      GraphRevisionPolicy = "REJECT"
	GraphPolicyNewRevision GraphRevisionPolicy = "NEW_FROZEN_REVISION"
)

func (p GraphRevisionPolicy) Valid() bool {
	return p == "" || p == GraphPolicyReject || p == GraphPolicyNewRevision
}

// ReviewerGraphRules govern only graph construction and amendment. Empty
// policies are accepted for compatibility and mean new frozen revisions.
type ReviewerGraphRules struct {
	RejectSelfReview            bool
	RejectReviewerIsParticipant bool
	AllowedRelationships        []ReviewerRelationshipKind
	LateAssignmentPolicy        GraphRevisionPolicy
	ReassignmentPolicy          GraphRevisionPolicy
}

func DefaultReviewerGraphRules() ReviewerGraphRules {
	return ReviewerGraphRules{
		RejectSelfReview: true, RejectReviewerIsParticipant: true,
		LateAssignmentPolicy: GraphPolicyNewRevision, ReassignmentPolicy: GraphPolicyNewRevision,
	}
}

func (r ReviewerGraphRules) Validate() error {
	if !r.LateAssignmentPolicy.Valid() || !r.ReassignmentPolicy.Valid() {
		return fmt.Errorf("%w: unknown amendment policy", ErrInvalidParticipantReviewerGraph)
	}
	seen := make(map[ReviewerRelationshipKind]struct{}, len(r.AllowedRelationships))
	for _, relationship := range r.AllowedRelationships {
		if !relationship.Valid() {
			return fmt.Errorf("%w: unknown relationship %q", ErrInvalidParticipantReviewerGraph, relationship)
		}
		if _, exists := seen[relationship]; exists {
			return fmt.Errorf("%w: duplicate relationship %q", ErrInvalidParticipantReviewerGraph, relationship)
		}
		seen[relationship] = struct{}{}
	}
	return nil
}

func (r ReviewerGraphRules) latePolicy() GraphRevisionPolicy {
	if r.LateAssignmentPolicy == "" {
		return GraphPolicyNewRevision
	}
	return r.LateAssignmentPolicy
}

func (r ReviewerGraphRules) reassignmentPolicy() GraphRevisionPolicy {
	if r.ReassignmentPolicy == "" {
		return GraphPolicyNewRevision
	}
	return r.ReassignmentPolicy
}

func (r ReviewerGraphRules) allows(relationship ReviewerRelationshipKind) bool {
	if len(r.AllowedRelationships) == 0 {
		return true
	}
	for _, allowed := range r.AllowedRelationships {
		if allowed == relationship {
			return true
		}
	}
	return false
}

// ParticipantRef is an opaque member reference resolved from the cycle's
// population binding. The graph stores no copied population attributes.
type ParticipantRef struct {
	ID string `json:"id"`
}

// ReviewerAssignment resolves one reviewer for one participant at freeze
// time, retaining the declared relationship kind.
type ReviewerAssignment struct {
	ParticipantID string                   `json:"participant_id"`
	ReviewerID    string                   `json:"reviewer_id"`
	Relationship  ReviewerRelationshipKind `json:"relationship"`
}

func sortParticipants(participants []ParticipantRef) {
	sort.Slice(participants, func(i, j int) bool { return participants[i].ID < participants[j].ID })
}

func sortAssignments(assignments []ReviewerAssignment) {
	sort.Slice(assignments, func(i, j int) bool {
		if assignments[i].ParticipantID != assignments[j].ParticipantID {
			return assignments[i].ParticipantID < assignments[j].ParticipantID
		}
		if assignments[i].ReviewerID != assignments[j].ReviewerID {
			return assignments[i].ReviewerID < assignments[j].ReviewerID
		}
		return assignments[i].Relationship < assignments[j].Relationship
	})
}

// FrozenParticipantReviewerGraph is an immutable-by-value graph snapshot for
// one performance cycle revision. Amendments return a new graph revision with
// lineage; no method mutates an existing snapshot.
type FrozenParticipantReviewerGraph struct {
	CycleID                 string               `json:"cycle_id"`
	CycleRevision           uint64               `json:"cycle_revision"`
	Population              PopulationBindingRef `json:"population"`
	GraphRevision           uint64               `json:"graph_revision"`
	SupersedesGraphRevision uint64               `json:"supersedes_graph_revision"`
	Participants            []ParticipantRef     `json:"participants"`
	Reviewers               []ReviewerAssignment `json:"reviewers"`
	Rules                   ReviewerGraphRules   `json:"rules"`
	FrozenAt                values.Instant       `json:"frozen_at"`
	Digest                  string               `json:"digest"`
}

func cloneFrozenParticipantReviewerGraph(g FrozenParticipantReviewerGraph) FrozenParticipantReviewerGraph {
	g.Participants = append([]ParticipantRef(nil), g.Participants...)
	g.Reviewers = append([]ReviewerAssignment(nil), g.Reviewers...)
	g.Rules.AllowedRelationships = append([]ReviewerRelationshipKind(nil), g.Rules.AllowedRelationships...)
	return g
}

func (g FrozenParticipantReviewerGraph) Validate() error {
	if strings.TrimSpace(g.CycleID) == "" || g.CycleRevision == 0 || g.GraphRevision == 0 || g.Population.Validate() != nil {
		return ErrInvalidParticipantReviewerGraph
	}
	if g.SupersedesGraphRevision >= g.GraphRevision {
		return ErrInvalidParticipantReviewerGraph
	}
	if err := g.Rules.Validate(); err != nil {
		return err
	}
	if err := g.FrozenAt.Validate(); err != nil {
		return fmt.Errorf("%w: frozen_at: %v", ErrInvalidParticipantReviewerGraph, err)
	}
	if len(g.Participants) == 0 || len(g.Reviewers) == 0 {
		return fmt.Errorf("%w: participants and reviewers are required", ErrInvalidParticipantReviewerGraph)
	}
	participantSet := make(map[string]struct{}, len(g.Participants))
	for _, participant := range g.Participants {
		if strings.TrimSpace(participant.ID) == "" {
			return fmt.Errorf("%w: participant id is required", ErrInvalidParticipantReviewerGraph)
		}
		if _, exists := participantSet[participant.ID]; exists {
			return fmt.Errorf("%w: duplicate participant %q", ErrInvalidParticipantReviewerGraph, participant.ID)
		}
		participantSet[participant.ID] = struct{}{}
	}
	assigned := make(map[string]struct{}, len(g.Reviewers))
	for _, reviewer := range g.Reviewers {
		if _, exists := participantSet[reviewer.ParticipantID]; !exists || strings.TrimSpace(reviewer.ReviewerID) == "" || !reviewer.Relationship.Valid() {
			return fmt.Errorf("%w: reviewer assignment is invalid", ErrInvalidParticipantReviewerGraph)
		}
		if !g.Rules.allows(reviewer.Relationship) {
			return fmt.Errorf("%w: relationship %q is not allowed", ErrInvalidParticipantReviewerGraph, reviewer.Relationship)
		}
		if reviewer.ParticipantID == reviewer.ReviewerID && g.Rules.RejectSelfReview {
			return ErrSelfReviewRejected
		}
		if _, isParticipant := participantSet[reviewer.ReviewerID]; isParticipant && g.Rules.RejectReviewerIsParticipant {
			return ErrReviewerParticipantRejected
		}
		key := reviewer.ParticipantID + "\x00" + reviewer.ReviewerID + "\x00" + string(reviewer.Relationship)
		if _, exists := assigned[key]; exists {
			return fmt.Errorf("%w: duplicate reviewer assignment", ErrInvalidParticipantReviewerGraph)
		}
		assigned[key] = struct{}{}
	}
	for participant := range participantSet {
		found := false
		for _, reviewer := range g.Reviewers {
			if reviewer.ParticipantID == participant {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: participant %q has no reviewer", ErrInvalidParticipantReviewerGraph, participant)
		}
	}
	if g.Digest == "" || g.Digest != g.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidParticipantReviewerGraph)
	}
	return nil
}

func (g FrozenParticipantReviewerGraph) computedDigest() string {
	participants := append([]ParticipantRef(nil), g.Participants...)
	reviewers := append([]ReviewerAssignment(nil), g.Reviewers...)
	sortParticipants(participants)
	sortAssignments(reviewers)
	allowed := append([]ReviewerRelationshipKind(nil), g.Rules.AllowedRelationships...)
	sort.Slice(allowed, func(i, j int) bool { return allowed[i] < allowed[j] })
	w := canonicalbytes.New("hcmnext.domains.performance.FrozenParticipantReviewerGraph", 1).
		String("cycle_id", g.CycleID).
		Int("cycle_revision", int64(g.CycleRevision)).
		Value("population", g.Population).
		Int("graph_revision", int64(g.GraphRevision)).
		Int("supersedes_graph_revision", int64(g.SupersedesGraphRevision)).
		Bool("reject_self_review", g.Rules.RejectSelfReview).
		Bool("reject_reviewer_is_participant", g.Rules.RejectReviewerIsParticipant).
		String("late_assignment_policy", string(g.Rules.latePolicy())).
		String("reassignment_policy", string(g.Rules.reassignmentPolicy())).
		Value("frozen_at", g.FrozenAt).
		Count("allowed_relationships", len(allowed))
	for _, relationship := range allowed {
		w.String("allowed_relationship", string(relationship))
	}
	w.Count("participants", len(participants))
	for _, participant := range participants {
		w.String("participant", participant.ID)
	}
	w.Count("reviewers", len(reviewers))
	for _, reviewer := range reviewers {
		w.String("participant_id", reviewer.ParticipantID).
			String("reviewer_id", reviewer.ReviewerID).
			String("relationship", string(reviewer.Relationship))
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func freezeParticipantReviewerGraph(cycle PerformanceCycle, participants []ParticipantRef, reviewers []ReviewerAssignment, rules ReviewerGraphRules, frozenAt values.Instant, graphRevision, supersedes uint64) (FrozenParticipantReviewerGraph, error) {
	if err := cycle.Validate(); err != nil {
		return FrozenParticipantReviewerGraph{}, err
	}
	if err := rules.Validate(); err != nil {
		return FrozenParticipantReviewerGraph{}, err
	}
	graph := FrozenParticipantReviewerGraph{
		CycleID: cycle.CycleID, CycleRevision: cycle.Revision, Population: cycle.Population,
		GraphRevision: graphRevision, SupersedesGraphRevision: supersedes,
		Participants: append([]ParticipantRef(nil), participants...), Reviewers: append([]ReviewerAssignment(nil), reviewers...),
		Rules: rules, FrozenAt: frozenAt,
	}
	sortParticipants(graph.Participants)
	sortAssignments(graph.Reviewers)
	graph.Digest = graph.computedDigest()
	if err := graph.Validate(); err != nil {
		return FrozenParticipantReviewerGraph{}, err
	}
	return graph, nil
}

// FreezeParticipantReviewerGraph resolves and freezes the graph for the
// exact cycle revision and population binding supplied by cycle.
func FreezeParticipantReviewerGraph(cycle PerformanceCycle, participants []ParticipantRef, reviewers []ReviewerAssignment, rules ReviewerGraphRules, frozenAt values.Instant) (FrozenParticipantReviewerGraph, error) {
	return freezeParticipantReviewerGraph(cycle, participants, reviewers, rules, frozenAt, 1, 0)
}

// FreezeReviewerGraph is a concise constructor alias.
func FreezeReviewerGraph(cycle PerformanceCycle, participants []ParticipantRef, reviewers []ReviewerAssignment, rules ReviewerGraphRules, frozenAt values.Instant) (FrozenParticipantReviewerGraph, error) {
	return FreezeParticipantReviewerGraph(cycle, participants, reviewers, rules, frozenAt)
}

func assignmentSet(reviewers []ReviewerAssignment) map[string]map[string]struct{} {
	set := make(map[string]map[string]struct{})
	for _, reviewer := range reviewers {
		if set[reviewer.ParticipantID] == nil {
			set[reviewer.ParticipantID] = make(map[string]struct{})
		}
		set[reviewer.ParticipantID][reviewer.ReviewerID+"\x00"+string(reviewer.Relationship)] = struct{}{}
	}
	return set
}

func requiresLateOrReassignment(previous FrozenParticipantReviewerGraph, participants []ParticipantRef, reviewers []ReviewerAssignment) (late, reassigned bool) {
	oldParticipants := make(map[string]struct{}, len(previous.Participants))
	for _, participant := range previous.Participants {
		oldParticipants[participant.ID] = struct{}{}
	}
	oldAssignments := assignmentSet(previous.Reviewers)
	newAssignments := assignmentSet(reviewers)
	for _, participant := range participants {
		if _, exists := oldParticipants[participant.ID]; !exists {
			late = true
		}
	}
	for participant, next := range newAssignments {
		previousSet, existed := oldAssignments[participant]
		if !existed {
			late = true
			continue
		}
		if len(next) != len(previousSet) {
			reassigned = true
			continue
		}
		for key := range next {
			if _, exists := previousSet[key]; !exists {
				reassigned = true
				break
			}
		}
	}
	return late, reassigned
}

// Amend creates a new frozen graph revision. It refuses in-place changes and
// applies the previous snapshot's explicit late/reassignment policies.
func (g FrozenParticipantReviewerGraph) Amend(participants []ParticipantRef, reviewers []ReviewerAssignment, frozenAt values.Instant) (FrozenParticipantReviewerGraph, error) {
	if err := g.Validate(); err != nil {
		return FrozenParticipantReviewerGraph{}, err
	}
	late, reassigned := requiresLateOrReassignment(g, participants, reviewers)
	if late && g.Rules.latePolicy() == GraphPolicyReject {
		return FrozenParticipantReviewerGraph{}, ErrLateAssignmentRejected
	}
	if reassigned && g.Rules.reassignmentPolicy() == GraphPolicyReject {
		return FrozenParticipantReviewerGraph{}, ErrReassignmentRejected
	}
	cycle := PerformanceCycle{
		CycleID: g.CycleID, Revision: g.CycleRevision, State: PerformanceCyclePlanned,
		Population:  g.Population,
		Calendar:    CalendarBindingRef{Ref: "amendment-placeholder", Version: "1", Digest: "amendment-placeholder"},
		RatingScale: RatingScaleVersionRef{ID: "amendment-placeholder", Version: "1", Digest: "amendment-placeholder"},
	}
	// The original cycle is not retained in the graph, so amendment validates
	// the lineage fields directly and reconstructs only the cycle binding.
	cycle.CanonicalDigest = cycle.computedDigest()
	return freezeParticipantReviewerGraph(cycle, participants, reviewers, g.Rules, frozenAt, g.GraphRevision+1, g.GraphRevision)
}

// AmendParticipantReviewerGraph is the package-level amendment form.
func AmendParticipantReviewerGraph(g FrozenParticipantReviewerGraph, participants []ParticipantRef, reviewers []ReviewerAssignment, frozenAt values.Instant) (FrozenParticipantReviewerGraph, error) {
	return g.Amend(participants, reviewers, frozenAt)
}

// ParticipantIDs returns a detached participant list.
func (g FrozenParticipantReviewerGraph) ParticipantIDs() []string {
	ids := make([]string, 0, len(g.Participants))
	for _, participant := range g.Participants {
		ids = append(ids, participant.ID)
	}
	return ids
}

// ReviewerAssignments returns a detached assignment list.
func (g FrozenParticipantReviewerGraph) ReviewerAssignments() []ReviewerAssignment {
	return append([]ReviewerAssignment(nil), g.Reviewers...)
}

type ParticipantReviewerGraphExplanation struct {
	CycleID                 string
	CycleRevision           uint64
	GraphRevision           uint64
	SupersedesGraphRevision uint64
	Population              PopulationBindingRef
	ParticipantIDs          []string
	ReviewerCount           int
	LateAssignmentPolicy    GraphRevisionPolicy
	ReassignmentPolicy      GraphRevisionPolicy
	Digest                  string
}

func (g FrozenParticipantReviewerGraph) Explain() (ParticipantReviewerGraphExplanation, error) {
	if err := g.Validate(); err != nil {
		return ParticipantReviewerGraphExplanation{}, err
	}
	return ParticipantReviewerGraphExplanation{
		CycleID: g.CycleID, CycleRevision: g.CycleRevision, GraphRevision: g.GraphRevision,
		SupersedesGraphRevision: g.SupersedesGraphRevision, Population: g.Population,
		ParticipantIDs: g.ParticipantIDs(), ReviewerCount: len(g.Reviewers),
		LateAssignmentPolicy: g.Rules.latePolicy(), ReassignmentPolicy: g.Rules.reassignmentPolicy(), Digest: g.Digest,
	}, nil
}

func ExplainParticipantReviewerGraph(g FrozenParticipantReviewerGraph) (ParticipantReviewerGraphExplanation, error) {
	return g.Explain()
}
