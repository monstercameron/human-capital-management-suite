package promotionexec

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// AuthorityClass is the business authority represented by an approval. A
// principal identity is not an authority class: one person must not satisfy
// two independent classes merely because they happen to hold both roles.
type AuthorityClass string

const (
	AuthorityClassFinancePartner AuthorityClass = "FINANCE_PARTNER"
	AuthorityClassCurrentManager AuthorityClass = "CURRENT_MANAGER"
	// Short aliases keep call sites readable while the long names remain the
	// canonical authority vocabulary.
	AuthorityClassFinance = AuthorityClassFinancePartner
	AuthorityClassManager = AuthorityClassCurrentManager
)

var (
	ErrApprovalAuthorityIncomplete  = errors.New("promotionexec: approval authority is incomplete")
	ErrApprovalAuthorityNotDistinct = errors.New("promotionexec: approval authorities are not distinct")
	ErrApprovalAuthorityStale       = errors.New("promotionexec: approval authority is no longer current")
	ErrPromotionLifecycleAction     = errors.New("promotionexec: lifecycle action is unavailable")
	ErrPromotionWaitMetadata        = errors.New("promotionexec: passive wait metadata is not truthful")
)

// ApprovalAuthority is the server-resolved authority held by one approver.
// AuthorityRef is evidence, not a caller-selected role string. Active and the
// optional validity window are checked again at decision time.
type ApprovalAuthority struct {
	PrincipalID  string
	Class        AuthorityClass
	AuthorityRef string
	Scope        string
	Active       bool
	ValidFrom    time.Time
	ValidUntil   time.Time
}

// ApprovalAuthorityBinding is the immutable authority captured when a
// proposal's approval was routed.
type ApprovalAuthorityBinding struct {
	ProposalRevisionID string
	MaterialDigest     string
	RequirementID      string
	Authority          ApprovalAuthority
}

// CurrentApprovalAuthority is the fresh directory answer used at decision or
// execution time. It deliberately has the same shape as the pinned authority
// so comparison cannot silently drop class, scope or authority evidence.
type CurrentApprovalAuthority = ApprovalAuthority

// ValidateDistinctApprovers proves the approval set is genuinely split by
// authority class and principal. It accepts one class when a policy chooses a
// single approval route, but every pair in the set must be distinct.
func ValidateDistinctApprovers(approvers []ApprovalAuthority) error {
	if len(approvers) == 0 {
		return ErrApprovalAuthorityIncomplete
	}
	byPrincipal := make(map[string]AuthorityClass, len(approvers))
	byClass := make(map[AuthorityClass]string, len(approvers))
	for _, a := range approvers {
		if strings.TrimSpace(a.PrincipalID) == "" || a.Class == "" || strings.TrimSpace(a.AuthorityRef) == "" || strings.TrimSpace(a.Scope) == "" {
			return fmt.Errorf("%w: principal, class, scope and authority reference are required", ErrApprovalAuthorityIncomplete)
		}
		if !a.ValidFrom.IsZero() && !a.ValidUntil.IsZero() && !a.ValidUntil.After(a.ValidFrom) {
			return fmt.Errorf("%w: authority validity window is inverted", ErrApprovalAuthorityIncomplete)
		}
		if prior, ok := byPrincipal[a.PrincipalID]; ok {
			return fmt.Errorf("%w: principal %q is repeated across %s and %s", ErrApprovalAuthorityNotDistinct, a.PrincipalID, prior, a.Class)
		}
		if prior, ok := byClass[a.Class]; ok {
			return fmt.Errorf("%w: authority class %s has principals %q and %q", ErrApprovalAuthorityNotDistinct, a.Class, prior, a.PrincipalID)
		}
		byPrincipal[a.PrincipalID] = a.Class
		byClass[a.Class] = a.PrincipalID
	}
	return nil
}

// ValidatePromotionApprovers validates the two independent authority classes
// compiled by the promotion workflow. The under-threshold route may contain
// only the manager requirement, so this checks class integrity rather than
// forcing a finance vote where the definition does not require one.
func ValidatePromotionApprovers(approvers []ApprovalAuthority) error {
	if err := ValidateDistinctApprovers(approvers); err != nil {
		return err
	}
	seen := map[AuthorityClass]bool{}
	for _, a := range approvers {
		if a.Class != AuthorityClassFinancePartner && a.Class != AuthorityClassCurrentManager {
			return fmt.Errorf("%w: unknown authority class %q", ErrApprovalAuthorityIncomplete, a.Class)
		}
		if seen[a.Class] {
			return fmt.Errorf("%w: authority class %s is repeated", ErrApprovalAuthorityNotDistinct, a.Class)
		}
		seen[a.Class] = true
	}
	return nil
}

// ValidateCurrentAuthority compares the pinned approval authority with the
// fresh authority answer. Any missing, inactive, expired, changed class,
// scope, or grant is a stale approval and must route to reapproval.
func ValidateCurrentAuthority(binding ApprovalAuthorityBinding, current CurrentApprovalAuthority, at time.Time) error {
	if strings.TrimSpace(binding.ProposalRevisionID) == "" || strings.TrimSpace(binding.MaterialDigest) == "" ||
		strings.TrimSpace(binding.RequirementID) == "" {
		return fmt.Errorf("%w: proposal and requirement binding are required", ErrApprovalAuthorityIncomplete)
	}
	if !isPromotionAuthorityClass(binding.Authority.Class) || !isPromotionAuthorityClass(current.Class) {
		return fmt.Errorf("%w: authority class is not a promotion approval class", ErrApprovalAuthorityIncomplete)
	}
	if err := ValidateDistinctApprovers([]ApprovalAuthority{binding.Authority}); err != nil {
		return err
	}
	if err := ValidateDistinctApprovers([]ApprovalAuthority{current}); err != nil {
		return err
	}
	if at.IsZero() || !binding.Authority.Active || !current.Active || current.PrincipalID != binding.Authority.PrincipalID ||
		current.Class != binding.Authority.Class || current.AuthorityRef != binding.Authority.AuthorityRef ||
		current.Scope != binding.Authority.Scope || !current.ValidFrom.Equal(binding.Authority.ValidFrom) ||
		!current.ValidUntil.Equal(binding.Authority.ValidUntil) {
		return fmt.Errorf("%w: current authority does not reproduce the pinned authority", ErrApprovalAuthorityStale)
	}
	if !binding.Authority.ValidFrom.IsZero() && at.Before(binding.Authority.ValidFrom) {
		return fmt.Errorf("%w: decision precedes the pinned authority window", ErrApprovalAuthorityStale)
	}
	if !binding.Authority.ValidUntil.IsZero() && !at.Before(binding.Authority.ValidUntil) {
		return fmt.Errorf("%w: pinned authority expired", ErrApprovalAuthorityStale)
	}
	if !current.ValidFrom.IsZero() && at.Before(current.ValidFrom) {
		return fmt.Errorf("%w: current authority is not active yet", ErrApprovalAuthorityStale)
	}
	if !current.ValidUntil.IsZero() && !at.Before(current.ValidUntil) {
		return fmt.Errorf("%w: current authority expired", ErrApprovalAuthorityStale)
	}
	return nil
}

func isPromotionAuthorityClass(class AuthorityClass) bool {
	return class == AuthorityClassFinancePartner || class == AuthorityClassCurrentManager
}

// ApprovalAuthorityClass maps the published approval node to its authority
// class. Unknown nodes return the empty class and are not silently treated as
// either finance or manager authority.
func ApprovalAuthorityClass(nodeID string) AuthorityClass {
	switch nodeID {
	case NodeApproveFinance:
		return AuthorityClassFinancePartner
	case NodeApproveManager, NodeReapproval:
		return AuthorityClassCurrentManager
	default:
		return ""
	}
}

// PromotionLifecycleState is the request/execution projection relevant to
// operator actions. Proposal revisions themselves remain immutable; Edit is a
// convenience for creating a new revision and is never an in-place mutation.
type PromotionLifecycleState string

const (
	PromotionDraft       PromotionLifecycleState = "DRAFT"
	PromotionPreflighted PromotionLifecycleState = "PREFLIGHTED"
	PromotionSimulated   PromotionLifecycleState = "SIMULATED"
	PromotionSubmitted   PromotionLifecycleState = "SUBMITTED"
	PromotionApproved    PromotionLifecycleState = "APPROVED"
	PromotionExecuting   PromotionLifecycleState = "EXECUTING"
	PromotionCommitted   PromotionLifecycleState = "COMMITTED"
	PromotionRejected    PromotionLifecycleState = "REJECTED"
	PromotionWithdrawn   PromotionLifecycleState = "WITHDRAWN"
	PromotionCancelled   PromotionLifecycleState = "CANCELLED"
	PromotionSuperseded  PromotionLifecycleState = "SUPERSEDED"
	PromotionClosed      PromotionLifecycleState = "CLOSED"
	PromotionReopened    PromotionLifecycleState = "REOPENED"

	// Execution dimension values are kept in the same wire type so a
	// LifecycleSnapshot can carry request and execution states without a
	// second enum package. They intentionally remain distinct from request
	// states even where the spelling overlaps (for example COMMITTED).
	PromotionExecutionNotPlanned     PromotionLifecycleState = "NOT_PLANNED"
	PromotionExecutionScheduled      PromotionLifecycleState = "SCHEDULED"
	PromotionExecutionRevalidating   PromotionLifecycleState = "REVALIDATING"
	PromotionExecutionExecuting      PromotionLifecycleState = "EXECUTING"
	PromotionExecutionCommitted      PromotionLifecycleState = "COMMITTED"
	PromotionExecutionBlocked        PromotionLifecycleState = "BLOCKED"
	PromotionExecutionRepairRequired PromotionLifecycleState = "REPAIR_REQUIRED"
)

type PromotionAction string

const (
	PromotionActionEdit      PromotionAction = "EDIT"
	PromotionActionSupersede PromotionAction = "SUPERSEDE"
	PromotionActionWithdraw  PromotionAction = "WITHDRAW"
	PromotionActionCancel    PromotionAction = "CANCEL"
)

// LifecycleSnapshot contains only durable facts needed to answer whether an
// action may be offered. IrreversibleEffect must come from the runtime, never
// from a button or a caller's assertion.
type LifecycleSnapshot struct {
	RequestState       PromotionLifecycleState
	ExecutionState     PromotionLifecycleState
	RevisionImmutable  bool
	IrreversibleEffect bool
	AtSafePoint        bool
}

type ActionAvailability struct {
	Action  PromotionAction
	Allowed bool
	Reason  string
}

func (a ActionAvailability) Validate() error {
	if a.Action == "" || strings.TrimSpace(a.Reason) == "" {
		return ErrPromotionLifecycleAction
	}
	return nil
}

// PromotionActionAvailability returns all four actions, including disabled
// ones, so a passive consumer cannot infer availability from missing metadata.
func PromotionActionAvailability(s LifecycleSnapshot) []ActionAvailability {
	knownRequest := s.RequestState == PromotionDraft || s.RequestState == PromotionPreflighted ||
		s.RequestState == PromotionSimulated || s.RequestState == PromotionSubmitted ||
		s.RequestState == PromotionApproved || s.RequestState == PromotionExecuting ||
		s.RequestState == PromotionCommitted || s.RequestState == PromotionRejected ||
		s.RequestState == PromotionWithdrawn || s.RequestState == PromotionCancelled ||
		s.RequestState == PromotionSuperseded || s.RequestState == PromotionClosed ||
		s.RequestState == PromotionReopened
	terminal := s.RequestState == PromotionCommitted || s.RequestState == PromotionRejected ||
		s.RequestState == PromotionWithdrawn || s.RequestState == PromotionCancelled ||
		s.RequestState == PromotionSuperseded || s.RequestState == PromotionClosed
	// RequestState and ExecutionState are independent dimensions. A runtime
	// commit therefore closes these actions even if a lagging request
	// projection still says APPROVED.
	committed := s.ExecutionState == PromotionCommitted || s.IrreversibleEffect
	terminal = terminal || committed
	executing := s.RequestState == PromotionExecuting || s.ExecutionState == PromotionExecuting
	knownExecution := s.ExecutionState == "" || s.ExecutionState == PromotionExecutionNotPlanned ||
		s.ExecutionState == PromotionExecutionScheduled || s.ExecutionState == PromotionExecutionRevalidating ||
		s.ExecutionState == PromotionExecutionExecuting || s.ExecutionState == PromotionExecutionCommitted ||
		s.ExecutionState == PromotionExecutionBlocked || s.ExecutionState == PromotionExecutionRepairRequired
	unknownExecution := !knownExecution
	executionPending := s.ExecutionState == PromotionExecutionScheduled || s.ExecutionState == PromotionExecutionRevalidating
	executionRepair := s.ExecutionState == PromotionExecutionRepairRequired
	canEdit := !s.RevisionImmutable && !terminal && !executing && !executionPending && !executionRepair && !unknownExecution && s.RequestState == PromotionDraft
	canSupersede := knownRequest && !terminal && !executing && !executionPending && !executionRepair && !unknownExecution
	canWithdraw := knownRequest && !terminal && !executing && !executionPending && !executionRepair && !unknownExecution && (s.RequestState == PromotionSubmitted || s.RequestState == PromotionApproved)
	canCancel := knownRequest && !terminal && !executionRepair && !unknownExecution && ((!executing && (s.RequestState == PromotionDraft || s.RequestState == PromotionPreflighted || s.RequestState == PromotionSimulated || s.RequestState == PromotionSubmitted || s.RequestState == PromotionApproved)) || (executing && s.AtSafePoint))
	return []ActionAvailability{
		{Action: PromotionActionEdit, Allowed: canEdit, Reason: actionReason(canEdit, "draft is editable", "proposal revisions are immutable; create a successor")},
		{Action: PromotionActionSupersede, Allowed: canSupersede, Reason: actionReason(canSupersede, "a successor revision may replace this request", "terminal or irreversible execution prevents supersession")},
		{Action: PromotionActionWithdraw, Allowed: canWithdraw, Reason: actionReason(canWithdraw, "withdrawal is allowed before execution", "withdrawal is unavailable after execution or irreversible effects")},
		{Action: PromotionActionCancel, Allowed: canCancel, Reason: actionReason(canCancel, "cancellation is allowed at the current safe point", "cancellation is unavailable after an irreversible effect or terminal state")},
	}
}

func actionReason(allowed bool, yes, no string) string {
	if allowed {
		return yes
	}
	return no
}

func CanEditProposal(s LifecycleSnapshot) bool { return availability(s, PromotionActionEdit).Allowed }
func CanSupersedePromotion(s LifecycleSnapshot) bool {
	return availability(s, PromotionActionSupersede).Allowed
}
func CanWithdrawPromotion(s LifecycleSnapshot) bool {
	return availability(s, PromotionActionWithdraw).Allowed
}
func CanCancelPromotion(s LifecycleSnapshot) bool {
	return availability(s, PromotionActionCancel).Allowed
}

func availability(s LifecycleSnapshot, action PromotionAction) ActionAvailability {
	for _, a := range PromotionActionAvailability(s) {
		if a.Action == action {
			return a
		}
	}
	return ActionAvailability{Action: action, Reason: "unknown action"}
}

// PassiveWaitMetadata is honest metadata for a durable wait. It describes a
// wake-up condition and does not imply polling, mutation, approval, or an
// available action.
type PassiveWaitMetadata struct {
	Waiting     bool
	Passive     bool
	WakeKind    string
	WakeAt      time.Time
	ZoneID      string
	CalendarRef string
	Reason      string
	PollAfter   time.Duration
}

func (m PassiveWaitMetadata) Validate() error {
	if !m.Waiting {
		if m.Passive || m.WakeKind != "" || !m.WakeAt.IsZero() || m.ZoneID != "" || m.CalendarRef != "" || m.Reason != "" || m.PollAfter != 0 {
			return fmt.Errorf("%w: non-wait state carries wake metadata", ErrPromotionWaitMetadata)
		}
		return nil
	}
	if !m.Passive || strings.TrimSpace(m.WakeKind) == "" || m.WakeAt.IsZero() ||
		strings.TrimSpace(m.ZoneID) == "" || strings.TrimSpace(m.CalendarRef) == "" ||
		strings.TrimSpace(m.Reason) == "" || m.PollAfter < 0 {
		return fmt.Errorf("%w: waiting state must identify a passive wake condition and pinned datasets", ErrPromotionWaitMetadata)
	}
	if m.PollAfter > 0 && m.PollAfter < time.Second {
		return fmt.Errorf("%w: polling interval is too aggressive for a passive wait", ErrPromotionWaitMetadata)
	}
	return nil
}

// NewPassiveWaitMetadata creates metadata for a scheduled wait. A zero wake
// instant is rejected instead of being rendered as an apparently actionable
// wait with an unknown deadline.
func NewPassiveWaitMetadata(wakeKind string, wakeAt time.Time, zoneID, calendarRef, reason string) (PassiveWaitMetadata, error) {
	m := PassiveWaitMetadata{Waiting: true, Passive: true, WakeKind: strings.TrimSpace(wakeKind), WakeAt: wakeAt.UTC(), ZoneID: strings.TrimSpace(zoneID), CalendarRef: strings.TrimSpace(calendarRef), Reason: strings.TrimSpace(reason), PollAfter: time.Minute}
	if err := m.Validate(); err != nil {
		return PassiveWaitMetadata{}, err
	}
	return m, nil
}

// SortedActions is useful to render deterministic action metadata without
// letting map iteration or transport ordering become authority.
func SortedActions(in []ActionAvailability) []ActionAvailability {
	out := append([]ActionAvailability(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Action < out[j].Action })
	return out
}
