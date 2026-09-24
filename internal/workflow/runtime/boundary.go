package runtime

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Stable refusal codes for the Promotion pre-execution boundaries.
const (
	// CodeHardConflict is the common prefix for a refusal caused by an
	// overlapping, unordered write footprint. The more specific codes below
	// retain the competing workflow kind without making callers parse prose.
	CodeHardConflict                     = "HARD_CONFLICT"
	CodeHardConflictCompetingTransfer    = "HARD_CONFLICT_COMPETING_TRANSFER"
	CodeHardConflictCompetingTermination = "HARD_CONFLICT_COMPETING_TERMINATION"
	CodeHardConflictCompetingLeave       = "HARD_CONFLICT_COMPETING_LEAVE"
	CodeHardConflictCompetingPromotion   = "HARD_CONFLICT_COMPETING_PROMOTION"

	// CodeApprovalBindingStale reports that approval was made under a
	// relationship graph that is no longer current.
	CodeApprovalBindingStale = "APPROVAL_BINDING_STALE"
	// CodePolicyVersionChanged reports a policy/rule-pack version movement at
	// the pre-execution boundary.
	CodePolicyVersionChanged = "POLICY_VERSION_CHANGED"
	// CodeReapprovalRequired is the route a non-blocking material change must
	// take before the workflow may execute again.
	CodeReapprovalRequired = "REAPPROVAL_REQUIRED"
)

// ConflictKind identifies the competing registered workflow family in a
// hard-conflict refusal. Known Promotion-era values remain named constants;
// other registrations may use their workflow id.
type ConflictKind string

const (
	ConflictKindTransfer    ConflictKind = "TRANSFER"
	ConflictKindTermination ConflictKind = "TERMINATION"
	ConflictKindLeave       ConflictKind = "LEAVE"
	ConflictKindPromotion   ConflictKind = "PROMOTION"
)

// RevalidationEvidence contains the pinned and current values for the keys a
// workflow registration declared as material at its revalidation boundary.
type RevalidationEvidence struct {
	Pinned  map[string]string
	Current map[string]string
}

// ValidateRevalidationKeys checks a registration's immutable ordered key set.
func ValidateRevalidationKeys(keys []string) error {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			return refuse(CodeInvalidRecord, "", "", "revalidation key is empty")
		}
		if _, ok := seen[key]; ok {
			return refuse(CodeInvalidRecord, "", "", "revalidation key %q is duplicated", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// RevalidationResult is the evidence from one generic boundary check.
type RevalidationResult struct {
	Confirmed   bool
	Requirement RevalidationRequirement
	ChangedFact string
	Explanation string
}

// EvaluateRevalidation applies a registration's ordered key declaration. A
// missing side is malformed evidence; the first changed key determines the
// route and therefore remains stable across retries.
func EvaluateRevalidation(keys []string, evidence RevalidationEvidence) (RevalidationResult, error) {
	if err := ValidateRevalidationKeys(keys); err != nil {
		return RevalidationResult{}, err
	}
	for _, key := range keys {
		pinned, hasPinned := evidence.Pinned[key]
		current, hasCurrent := evidence.Current[key]
		if !hasPinned || !hasCurrent {
			return RevalidationResult{}, refuse(CodeInvalidRecord, "", "", "revalidation key %q requires pinned and current values", key)
		}
		if pinned != current {
			return RevalidationResult{
				Confirmed: false, Requirement: RevalidationReapprovalRequired, ChangedFact: key,
				Explanation: fmt.Sprintf("%s changed from %q to %q; reapproval is required", key, pinned, current),
			}, refuse(CodeReapprovalRequired, "", "", "%s changed from %q to %q; reapproval is required", key, pinned, current)
		}
	}
	return RevalidationResult{Confirmed: true, Requirement: RevalidationConfirmed,
		Explanation: "registered revalidation keys reproduce the pinned values"}, nil
}

// ConflictObservation is one current competing proposal returned by the
// caller-owned conflict authority. The runtime performs the classification;
// the port only supplies current candidates and never supplies an allow/deny
// boolean.
type ConflictObservation struct {
	Candidate conflict.Candidate
	Kind      ConflictKind
}

// ConflictFacts is the runtime-owned read port for current proposal conflict
// candidates. A durable adapter may take a transaction-level lock while
// reading its authority so the read and the later Start write share one
// serialization boundary.
type ConflictFacts interface {
	Candidates(context.Context, Executor, ConflictCheckRequest) ([]ConflictObservation, error)
}

// ConflictCheckRequest is the exact candidate and worker scope being checked
// before Start is allowed to write a workflow instance.
type ConflictCheckRequest struct {
	TenantID    uuid.UUID
	SubjectRefs []string
	Candidate   conflict.Candidate
}

// ConflictResult is the deterministic classification returned when no hard
// conflict prevents the operation. A hard conflict is returned as a typed
// runtime error so callers cannot accidentally continue after inspecting a
// result.
type ConflictResult struct {
	Decision            conflict.Decision
	CompetingRevisionID string
	CompetingKind       ConflictKind
	EvidenceRef         string
	Explanation         string
}

// CheckConflict classifies the candidate against all current observations and
// refuses HARD_CONFLICT before any runtime state can be written.
func CheckConflict(ctx context.Context, ex Executor, req ConflictCheckRequest, facts ConflictFacts) (ret0 ConflictResult, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.check_conflict", req, facts)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if req.TenantID == uuid.Nil {
		return ConflictResult{}, refuse(CodeInvalidRecord, "", "", "conflict check requires a tenant id")
	}
	if len(req.SubjectRefs) == 0 {
		return ConflictResult{}, refuse(CodeInvalidRecord, "", "", "conflict check names no business subject")
	}
	if facts == nil {
		return ConflictResult{}, refuse(CodeInvalidRecord, "", "", "conflict check requires ConflictFacts")
	}
	if err := req.Candidate.Validate(); err != nil {
		return ConflictResult{}, wrap(CodeInvalidRecord, "", "", err, "validate conflict candidate")
	}

	observations, err := facts.Candidates(ctx, ex, req)
	if err != nil {
		return ConflictResult{}, wrap(CodeStorageFailed, "", "", err, "read current conflict candidates")
	}
	sort.SliceStable(observations, func(i, j int) bool {
		if observations[i].Candidate.ProposalRevisionID != observations[j].Candidate.ProposalRevisionID {
			return observations[i].Candidate.ProposalRevisionID < observations[j].Candidate.ProposalRevisionID
		}
		return observations[i].Kind < observations[j].Kind
	})

	for _, observation := range observations {
		if observation.Candidate.ProposalRevisionID == req.Candidate.ProposalRevisionID {
			continue
		}
		classification, classifyErr := conflict.ClassifyConflict(req.Candidate, observation.Candidate, nil)
		if classifyErr != nil {
			return ConflictResult{}, wrap(CodeInvalidRecord, "", "", classifyErr,
				"classify proposal conflict against %s", observation.Candidate.ProposalRevisionID)
		}
		result := ConflictResult{
			Decision:            classification.Decision,
			CompetingRevisionID: observation.Candidate.ProposalRevisionID,
			CompetingKind:       observation.Kind,
			EvidenceRef:         classification.EvidenceRef,
			Explanation:         classification.Explanation,
		}
		if classification.Decision != conflict.DecisionHardConflict {
			continue
		}
		return result, refuse(hardConflictCode(observation.Kind), "", "",
			"%s: proposal %s loses to competing %s %s (%s); evidence=%s",
			CodeHardConflict, req.Candidate.ProposalRevisionID, observation.Kind,
			observation.Candidate.ProposalRevisionID, classification.Explanation,
			classification.EvidenceRef)
	}
	return ConflictResult{}, nil
}

func hardConflictCode(kind ConflictKind) string {
	switch kind {
	case ConflictKindTransfer:
		return CodeHardConflictCompetingTransfer
	case ConflictKindTermination:
		return CodeHardConflictCompetingTermination
	case ConflictKindLeave:
		return CodeHardConflictCompetingLeave
	case ConflictKindPromotion:
		return CodeHardConflictCompetingPromotion
	default:
		return CodeHardConflict
	}
}

// RevalidationRequirement is the route selected by the material pre-execution
// check. Confirmed is represented by the empty value.
type RevalidationRequirement string

const (
	RevalidationConfirmed          RevalidationRequirement = ""
	RevalidationReapprovalRequired RevalidationRequirement = CodeReapprovalRequired
)

// PromotionRevalidation is the pinned/current identity set the runtime
// checks immediately before a material Promotion advancement. Empty pairs are
// not checked; a pair with only one side present is invalid rather than
// silently treated as changed.
type PromotionRevalidation struct {
	PinnedManagerRef       string
	CurrentManagerRef      string
	PinnedTeamRef          string
	CurrentTeamRef         string
	PinnedOrganizationRef  string
	CurrentOrganizationRef string
	PinnedPolicyVersion    string
	CurrentPolicyVersion   string
}

// PromotionRevalidationResult preserves the pre-generic source name.
type PromotionRevalidationResult = RevalidationResult

// EvaluatePromotionRevalidation compares pinned approval/policy identities to
// their current values without opening a transaction or reading a clock.
func EvaluatePromotionRevalidation(in PromotionRevalidation) (PromotionRevalidationResult, error) {
	pairs := []struct {
		name, pinned, current string
	}{
		{"manager_relationship", in.PinnedManagerRef, in.CurrentManagerRef},
		{"team_relationship", in.PinnedTeamRef, in.CurrentTeamRef},
		{"organization_relationship", in.PinnedOrganizationRef, in.CurrentOrganizationRef},
		{"policy_version", in.PinnedPolicyVersion, in.CurrentPolicyVersion},
	}
	for _, pair := range pairs {
		if (pair.pinned == "") != (pair.current == "") {
			return PromotionRevalidationResult{}, refuse(CodeInvalidRecord, "", "",
				"%s must provide both pinned and current values", pair.name)
		}
	}
	for _, pair := range pairs {
		if pair.pinned == "" || pair.pinned == pair.current {
			continue
		}
		if pair.name == "policy_version" {
			return PromotionRevalidationResult{
					Confirmed: false, Requirement: RevalidationReapprovalRequired,
					ChangedFact: pair.name,
					Explanation: fmt.Sprintf("%s changed from %q to %q; route %s",
						pair.name, pair.pinned, pair.current, CodeReapprovalRequired),
				}, refuse(CodePolicyVersionChanged, "", "",
					"%s changed from %q to %q; route %s",
					pair.name, pair.pinned, pair.current, CodeReapprovalRequired)
		}
		return PromotionRevalidationResult{
				Confirmed: false, Requirement: RevalidationReapprovalRequired,
				ChangedFact: pair.name,
				Explanation: fmt.Sprintf("%s changed from %q to %q; approval binding is stale",
					pair.name, pair.pinned, pair.current),
			}, refuse(CodeApprovalBindingStale, "", "",
				"%s changed from %q to %q; approval binding is stale and requires %s",
				pair.name, pair.pinned, pair.current, CodeReapprovalRequired)
	}
	return PromotionRevalidationResult{Confirmed: true, Requirement: RevalidationConfirmed,
		Explanation: "promotion pre-execution facts reproduce the pinned approval binding and policy version"}, nil
}

// ReapprovalParkRequest asks the runtime to move a live instance to BLOCKED
// while retaining its current frontier. The caller commits this transition
// even though the subsequent advancement is refused; this keeps the park
// durable and keeps business execution out of the refused call.
type ReapprovalParkRequest struct {
	TenantID                uuid.UUID
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	ReasonRef               string
}

// ParkForReapproval durably parks an instance without changing its frontier.
// It is intentionally separate from EvaluatePromotionRevalidation: the
// materiality result is pure evidence, while this function is the one
// explicit runtime state transition that records the operational route.
func ParkForReapproval(ctx context.Context, ex Executor, req ReapprovalParkRequest) (ret0 Instance, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.park_for_reapproval", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if req.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.ExpectedInstanceVersion < 1 || req.ReasonRef == "" {
		return Instance{}, refuse(CodeInvalidRecord, req.InstanceID.String(), "",
			"reapproval park requires tenant, instance, expected version and reason ref")
	}
	current, err := (Store{}).LoadInstance(ctx, ex, req.TenantID, req.InstanceID)
	if err != nil {
		return Instance{}, err
	}
	if current.RuntimeStatus == InstanceBlocked {
		return current, nil
	}
	return (Store{}).RecordInstanceState(ctx, ex, InstanceTransition{
		TenantID: req.TenantID, InstanceID: req.InstanceID,
		ExpectedVersion: req.ExpectedInstanceVersion, Status: InstanceBlocked,
		CurrentNodeIDs: current.CurrentNodeIDs, VariableRevisionHead: current.VariableRevisionHead,
		EffectiveContextRef: current.EffectiveContextRef, LastCheckpointRef: req.ReasonRef,
		CompletionDimensions: current.CompletionDimensions,
	})
}
