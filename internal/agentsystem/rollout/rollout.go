package agentrollout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	// ErrInvalid reports a malformed preview or reconciliation request.
	ErrInvalid = errors.New("agent rollout: invalid input")
	// ErrNotManager reports that the actor lacks manager authority.
	ErrNotManager = errors.New("agent rollout: actor is not a conversation manager")
	// ErrApprovalRequired reports that an exact preview has no valid approval.
	ErrApprovalRequired = errors.New("agent rollout: preview approval required")
	// ErrPreviewStale reports changed preview inputs or installation revision.
	ErrPreviewStale = errors.New("agent rollout: preview no longer matches selector")
	// ErrReviewRequired reports an existing installation needing a new review.
	ErrReviewRequired = errors.New("agent rollout: installation change requires a new review")
	// ErrNotFound reports that the requested installation does not exist.
	ErrNotFound = errors.New("agent rollout: installation not found")
)

// Selector is deliberately declarative and tenant-bound. When both lists are
// present, a conversation must match both; neither field is an implicit grant.
type Selector struct {
	TenantID        string   `json:"tenant_id"`
	ConversationIDs []string `json:"conversation_ids,omitempty"`
	Classes         []string `json:"classes,omitempty"`
}

// Matches reports whether a conversation is within the selector's explicit
// tenant, ID, and class bounds.
func (s Selector) Matches(c Conversation) bool {
	if s.TenantID == "" || c.TenantID != s.TenantID || c.ID == "" || c.Class == "" {
		return false
	}
	if len(s.ConversationIDs) == 0 && len(s.Classes) == 0 {
		return false
	}
	if len(s.ConversationIDs) > 0 && !contains(s.ConversationIDs, c.ID) {
		return false
	}
	return len(s.Classes) == 0 || contains(s.Classes, c.Class)
}

// Conversation is a chat-owned snapshot. Both revisions are included in the
// preview digest and passed back to the write boundary as compare-and-set data.
type Conversation struct {
	TenantID               string `json:"tenant_id"`
	ID                     string `json:"id"`
	Class                  string `json:"class"`
	MembershipRevision     uint64 `json:"membership_revision"`
	ClassificationRevision uint64 `json:"classification_revision"`
	Eligible               bool   `json:"eligible"`
}

// DesiredInstallation pins one published agent version and its exact grant.
type DesiredInstallation struct {
	AgentID string   `json:"agent_id"`
	Version uint64   `json:"version"`
	Grant   []string `json:"grant"`
}

// Request defines a previewed rollout. BatchLimit bounds each reconciliation
// call; it does not change the approved candidate set.
type Request struct {
	ID         string
	Selector   Selector
	Desired    DesiredInstallation
	BatchLimit int
}

// Plan is a preview snapshot plus an optional approval bound to its digest.
// Cursor and stage are operational state and are excluded from Digest.
type Plan struct {
	ID             string
	Selector       Selector
	Desired        DesiredInstallation
	Candidates     []Conversation
	Digest         string
	ApproverID     string
	ApprovalRef    string
	ApprovalDigest string
	BatchLimit     int
	Cursor         int
	Stage          Stage
}

// Stage reports the rollout plan's review and reconciliation state.
type Stage string

const (
	StagePreviewed   Stage = "PREVIEWED"
	StageApproved    Stage = "APPROVED"
	StageReconciling Stage = "RECONCILING"
	StageComplete    Stage = "COMPLETE"
)

// Installation is the minimal record needed to reconcile a single grant.
type Installation struct {
	ID             string
	TenantID       string
	ConversationID string
	AgentID        string
	Version        uint64
	Grant          []string
	Revision       uint64
	Active         bool
}

// Catalog returns matching conversations and fresh chat-owned snapshots.
type Catalog interface {
	Matching(context.Context, Selector) ([]Conversation, error)
	Current(context.Context, string, string) (Conversation, error)
}

// ManagerAuthorizer proves the approver's authority in each conversation.
type ManagerAuthorizer interface {
	IsConversationManager(context.Context, string, string, string) (bool, error)
	// RecordRolloutApproval stores an immutable actor/plan/digest approval and
	// returns its opaque reference; the reference must not be caller-forgeable.
	RecordRolloutApproval(context.Context, string, string, string, []Conversation) (string, error)
	// ApprovedRolloutActor verifies the stored reference against the exact plan
	// and digest and returns its approving manager, or an empty actor if invalid.
	ApprovedRolloutActor(context.Context, string, string, string) (string, error)
}

// Boundary applies one installation transition. Every method must atomically
// recheck manager authority and the supplied current membership/classification
// revisions before writing. Install creates a fresh independent installation
// and must be idempotent for the rollout/conversation pair.
type Boundary interface {
	Find(context.Context, string, string, string) (Installation, bool, error)
	FindByID(context.Context, string) (Installation, error)
	Install(context.Context, string, string, Conversation, DesiredInstallation) (Installation, error)
	Suspend(context.Context, string, Installation, Conversation, string) (Installation, error)
	Remove(context.Context, string, Installation, Conversation) (Installation, error)
}

// Service coordinates previews and their narrowly scoped reconciliation.
type Service struct {
	Catalog  Catalog
	Managers ManagerAuthorizer
	Boundary Boundary
}

// Preview resolves the current selector once and pins the exact candidate
// identities and revisions. A selector never acts as a standing grant.
func (s Service) Preview(ctx context.Context, req Request) (Plan, error) {
	if s.Catalog == nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Desired.AgentID) == "" || req.Desired.Version == 0 || req.BatchLimit < 1 || !validSelector(req.Selector) || !validGrant(req.Desired.Grant) {
		return Plan{}, ErrInvalid
	}
	candidates, err := s.Catalog.Matching(ctx, req.Selector)
	if err != nil {
		return Plan{}, err
	}
	candidates, err = canonicalCandidates(req.Selector, candidates)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{ID: req.ID, Selector: cloneSelector(req.Selector), Desired: cloneDesired(req.Desired), Candidates: candidates, BatchLimit: req.BatchLimit, Stage: StagePreviewed}
	plan.Digest, err = digest(plan)
	if err != nil {
		return Plan{}, fmt.Errorf("agent rollout: encode preview: %w", err)
	}
	return plan, nil
}

// Approve binds a manager's approval to the exact preview digest. Authority is
// checked in every conversation before the plan becomes actionable.
func (s Service) Approve(ctx context.Context, plan Plan, actor string) (Plan, error) {
	if s.Managers == nil || actor == "" || len(plan.Candidates) == 0 || plan.Stage != StagePreviewed || !validPlan(plan) {
		return Plan{}, ErrInvalid
	}
	for _, candidate := range plan.Candidates {
		ok, err := s.Managers.IsConversationManager(ctx, actor, candidate.TenantID, candidate.ID)
		if err != nil {
			return Plan{}, err
		}
		if !ok {
			return Plan{}, ErrNotManager
		}
	}
	approvalRef, err := s.Managers.RecordRolloutApproval(ctx, actor, plan.ID, plan.Digest, append([]Conversation(nil), plan.Candidates...))
	if err != nil {
		return Plan{}, err
	}
	if strings.TrimSpace(approvalRef) == "" {
		return Plan{}, ErrApprovalRequired
	}
	plan.ApproverID = actor
	plan.ApprovalRef = approvalRef
	plan.ApprovalDigest = plan.Digest
	plan.Stage = StageApproved
	return plan, nil
}

// Result describes one candidate's reconciliation outcome.
type Result struct {
	ConversationID string
	InstallationID string
	Action         string
}

// Reconcile applies at most BatchLimit candidates from the approved preview.
// Newly matching conversations invalidate the preview and are never installed.
func (s Service) Reconcile(ctx context.Context, plan Plan) (Plan, []Result, error) {
	if s.Catalog == nil || s.Managers == nil || s.Boundary == nil || !validPlan(plan) {
		return Plan{}, nil, ErrInvalid
	}
	if plan.ApproverID == "" || plan.ApprovalRef == "" || plan.ApprovalDigest != plan.Digest || (plan.Stage != StageApproved && plan.Stage != StageReconciling && plan.Stage != StageComplete) {
		return Plan{}, nil, ErrApprovalRequired
	}
	approvedBy, err := s.Managers.ApprovedRolloutActor(ctx, plan.ApprovalRef, plan.ID, plan.Digest)
	if err != nil {
		return Plan{}, nil, err
	}
	if approvedBy == "" || approvedBy != plan.ApproverID {
		return Plan{}, nil, ErrApprovalRequired
	}
	matching, err := s.Catalog.Matching(ctx, plan.Selector)
	if err != nil {
		return Plan{}, nil, err
	}
	matching, err = canonicalCandidates(plan.Selector, matching)
	if err != nil {
		return Plan{}, nil, err
	}
	if hasNewCandidates(plan.Candidates, matching) {
		return Plan{}, nil, ErrPreviewStale
	}
	end := plan.Cursor + plan.BatchLimit
	if end > len(plan.Candidates) {
		end = len(plan.Candidates)
	}
	results := make([]Result, 0, end-plan.Cursor)
	for _, preview := range plan.Candidates[plan.Cursor:end] {
		result, err := s.reconcileOne(ctx, plan, preview)
		if err != nil {
			return plan, results, err
		}
		results = append(results, result)
		plan.Cursor++
	}
	if plan.Cursor == len(plan.Candidates) {
		plan.Stage = StageComplete
	} else {
		plan.Stage = StageReconciling
	}
	return plan, results, nil
}

func (s Service) reconcileOne(ctx context.Context, plan Plan, preview Conversation) (Result, error) {
	current, err := s.Catalog.Current(ctx, preview.TenantID, preview.ID)
	if err != nil {
		return Result{}, err
	}
	if current.TenantID != preview.TenantID || current.ID != preview.ID {
		return Result{}, ErrPreviewStale
	}
	installed, exists, err := s.Boundary.Find(ctx, current.TenantID, plan.Desired.AgentID, current.ID)
	if err != nil {
		return Result{}, err
	}
	if !sameSnapshot(preview, current) || !current.Eligible || !plan.Selector.Matches(current) {
		if !exists || !installed.Active {
			return Result{ConversationID: preview.ID, Action: "PENDING_REVIEW"}, nil
		}
		if err := s.authorize(ctx, plan.ApproverID, current); err != nil {
			return Result{}, err
		}
		updated, err := s.Boundary.Suspend(ctx, plan.ApproverID, installed, current, "ROLLOUT_PREVIEW_STALE")
		if err != nil {
			return Result{}, err
		}
		if updated.ID != installed.ID || updated.Revision != installed.Revision+1 || updated.Active {
			return Result{}, ErrPreviewStale
		}
		return Result{ConversationID: preview.ID, InstallationID: updated.ID, Action: "SUSPENDED"}, nil
	}
	if err := s.authorize(ctx, plan.ApproverID, current); err != nil {
		return Result{}, err
	}
	if exists {
		if installed.TenantID != current.TenantID || installed.ConversationID != current.ID || installed.AgentID != plan.Desired.AgentID {
			return Result{}, ErrPreviewStale
		}
		if installed.Active && installed.Version == plan.Desired.Version && sameStrings(installed.Grant, plan.Desired.Grant) {
			return Result{ConversationID: preview.ID, InstallationID: installed.ID, Action: "UNCHANGED"}, nil
		}
		return Result{}, ErrReviewRequired
	}
	created, err := s.Boundary.Install(ctx, plan.ApproverID, plan.ID, current, plan.Desired)
	if err != nil {
		return Result{}, err
	}
	if created.ID == "" || created.Revision == 0 || created.TenantID != current.TenantID || created.ConversationID != current.ID || created.AgentID != plan.Desired.AgentID || created.Version != plan.Desired.Version || !sameStrings(created.Grant, plan.Desired.Grant) {
		return Result{}, ErrPreviewStale
	}
	return Result{ConversationID: preview.ID, InstallationID: created.ID, Action: "INSTALLED"}, nil
}

// RevokeOne revokes only the named installation at its expected revision.
// The boundary must recheck current manager, membership and classification.
func (s Service) RevokeOne(ctx context.Context, actor, installationID string, expectedRevision uint64) (Installation, error) {
	if s.Catalog == nil || s.Managers == nil || s.Boundary == nil || actor == "" || installationID == "" || expectedRevision == 0 {
		return Installation{}, ErrInvalid
	}
	installation, err := s.Boundary.FindByID(ctx, installationID)
	if err != nil {
		return Installation{}, err
	}
	if installation.ID != installationID || installation.Revision != expectedRevision || !installation.Active {
		return Installation{}, ErrPreviewStale
	}
	current, err := s.Catalog.Current(ctx, installation.TenantID, installation.ConversationID)
	if err != nil {
		return Installation{}, err
	}
	if current.TenantID != installation.TenantID || current.ID != installation.ConversationID || current.MembershipRevision == 0 || current.ClassificationRevision == 0 {
		return Installation{}, ErrPreviewStale
	}
	if err := s.authorize(ctx, actor, current); err != nil {
		return Installation{}, err
	}
	removed, err := s.Boundary.Remove(ctx, actor, installation, current)
	if err != nil {
		return Installation{}, err
	}
	if removed.ID != installation.ID || removed.Revision != installation.Revision+1 || removed.Active {
		return Installation{}, ErrPreviewStale
	}
	return removed, nil
}

func (s Service) authorize(ctx context.Context, actor string, c Conversation) error {
	ok, err := s.Managers.IsConversationManager(ctx, actor, c.TenantID, c.ID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotManager
	}
	return nil
}

func validPlan(plan Plan) bool {
	if strings.TrimSpace(plan.ID) == "" || !validSelector(plan.Selector) || strings.TrimSpace(plan.Desired.AgentID) == "" || plan.Desired.Version == 0 || plan.BatchLimit < 1 || !validGrant(plan.Desired.Grant) || plan.Cursor < 0 || plan.Cursor > len(plan.Candidates) {
		return false
	}
	switch plan.Stage {
	case StagePreviewed, StageApproved:
		if plan.Cursor != 0 {
			return false
		}
	case StageReconciling:
		if plan.Cursor == 0 || plan.Cursor == len(plan.Candidates) {
			return false
		}
	case StageComplete:
		if plan.Cursor != len(plan.Candidates) {
			return false
		}
	default:
		return false
	}
	d, err := digest(plan)
	return err == nil && d == plan.Digest
}

func validSelector(selector Selector) bool {
	return strings.TrimSpace(selector.TenantID) != "" && (len(selector.ConversationIDs) > 0 || len(selector.Classes) > 0) && uniqueNonempty(selector.ConversationIDs) && uniqueNonempty(selector.Classes)
}

func validGrant(grant []string) bool { return uniqueNonempty(grant) }

func uniqueNonempty(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func canonicalCandidates(selector Selector, candidates []Conversation) ([]Conversation, error) {
	items := make([]Conversation, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if !selector.Matches(candidate) || candidate.MembershipRevision == 0 || candidate.ClassificationRevision == 0 {
			return nil, ErrInvalid
		}
		if _, exists := seen[candidate.ID]; exists {
			return nil, ErrInvalid
		}
		seen[candidate.ID] = struct{}{}
		items = append(items, candidate)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func digest(plan Plan) (string, error) {
	body, err := json.Marshal(struct {
		ID         string              `json:"id"`
		Selector   Selector            `json:"selector"`
		Desired    DesiredInstallation `json:"desired"`
		Candidates []Conversation      `json:"candidates"`
		BatchLimit int                 `json:"batch_limit"`
	}{plan.ID, cloneSelector(plan.Selector), cloneDesired(plan.Desired), plan.Candidates, plan.BatchLimit})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func cloneSelector(selector Selector) Selector {
	selector.ConversationIDs = append([]string(nil), selector.ConversationIDs...)
	selector.Classes = append([]string(nil), selector.Classes...)
	sort.Strings(selector.ConversationIDs)
	sort.Strings(selector.Classes)
	return selector
}

func cloneDesired(desired DesiredInstallation) DesiredInstallation {
	desired.Grant = append([]string(nil), desired.Grant...)
	sort.Strings(desired.Grant)
	return desired
}

func sameSnapshot(a, b Conversation) bool {
	return a.TenantID == b.TenantID && a.ID == b.ID && a.Class == b.Class && a.MembershipRevision == b.MembershipRevision && a.ClassificationRevision == b.ClassificationRevision && a.Eligible == b.Eligible
}

func hasNewCandidates(preview, current []Conversation) bool {
	known := make(map[string]struct{}, len(preview))
	for _, candidate := range preview {
		known[candidate.ID] = struct{}{}
	}
	for _, candidate := range current {
		if _, exists := known[candidate.ID]; !exists {
			return true
		}
	}
	return false
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left, right := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
