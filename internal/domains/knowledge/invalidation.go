package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const invalidateSchemaVersion = "knowledge-invalidate/v1"

var (
	// ErrInvalidateRejected is the KNOW-005 seeded-defect sentinel. Uncited,
	// stale, injected or invalidated knowledge presented for invalidation or
	// rebuild must fail with this error carrying field, state and version.
	ErrInvalidateRejected = errors.New("KNOW_005_REJECTED")
)

// AnswerState is the closed KNOW-005 derived-answer vocabulary. Answers are
// marked STALE before any rebuild, and retired answers never rebuild.
type AnswerState string

const (
	AnswerCurrent AnswerState = "CURRENT"
	AnswerStale   AnswerState = "STALE"
	AnswerRetired AnswerState = "RETIRED"
)

// ChangeKind is the closed content-change vocabulary.
type ChangeKind string

const (
	ChangePublish    ChangeKind = "PUBLISH"
	ChangeRetire     ChangeKind = "RETIRE"
	ChangeCorrection ChangeKind = "CORRECTION"
)

// Valid reports whether k is a declared content-change kind.
func (k ChangeKind) Valid() bool {
	switch k {
	case ChangePublish, ChangeRetire, ChangeCorrection:
		return true
	default:
		return false
	}
}

// DerivedAnswer is one answer derived from a source article revision.
// Injected marks pipeline-detected injection; Invalidated marks source
// retraction. Invalidation is pure: answers are values.
type DerivedAnswer struct {
	AnswerRef   string
	TenantID    string
	ArticleID   string
	Revision    uint64
	ChunkRefs   []string
	Citations   []SourceRef
	State       AnswerState
	At          time.Time
	Injected    bool
	Invalidated bool
}

// ContentChange is the publish/retire/correction event that triggers
// invalidation. For PUBLISH and CORRECTION, Revision is the superseding
// revision and Citations carry its cited sources; RETIRE carries no new
// revision. All instants are caller-supplied.
type ContentChange struct {
	ArticleID   string
	Kind        ChangeKind
	Revision    uint64
	Citations   []SourceRef
	At          time.Time
	Injected    bool
	Invalidated bool
}

// AnswerConsumer binds one derived answer to the agents and workflows that
// serve it, so a content change can name every affected consumer.
type AnswerConsumer struct {
	AnswerRef  string
	AgentID    string
	WorkflowID string
}

// InvalidateRequest scopes one content change to its tenant, the derived
// answers that may depend on the article, and their consumers.
type InvalidateRequest struct {
	TenantID  string
	Change    ContentChange
	Answers   []DerivedAnswer
	Consumers []AnswerConsumer
	At        time.Time
}

// Invalidation is the KNOW-005 result: affected answers marked stale (or
// retired) before rebuild, the affected agents/workflows, the preserved
// pre-change cited answers as historical evidence, and the binding digest.
type Invalidation struct {
	AffectedAnswers   []DerivedAnswer
	AffectedAgents    []string
	AffectedWorkflows []string
	Preserved         []DerivedAnswer
	Digest            string
}

// InvalidationRejection is the stable KNOW-005 failure shape.
type InvalidationRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *InvalidationRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrInvalidateRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the KNOW_005_REJECTED sentinel to errors.Is.
func (r *InvalidationRejection) Unwrap() error { return ErrInvalidateRejected }

func invalidateReject(field, state, version, reason string) error {
	return &InvalidationRejection{Field: field, State: state, Version: version, Reason: reason}
}

func invalidationDigest(tenant string, change ContentChange, affected []string, at time.Time) string {
	refs := append([]string(nil), affected...)
	sort.Strings(refs)
	sum := sha256.Sum256([]byte(strings.Join(append([]string{
		tenant, change.ArticleID, string(change.Kind),
		fmt.Sprintf("%d", change.Revision), at.UTC().Format(time.RFC3339Nano),
	}, refs...), "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// InvalidateDerived identifies every derived answer, agent and workflow
// affected by a publish/retire/correction, marks affected answers stale
// before any rebuild, and preserves the pre-change cited answers as
// historical evidence. Seeded defects — uncited, stale, injected or
// invalidated knowledge — fail with KNOW_005_REJECTED and mark nothing.
func InvalidateDerived(req InvalidateRequest) (Invalidation, error) {
	const version = invalidateSchemaVersion
	if strings.TrimSpace(req.TenantID) == "" {
		return Invalidation{}, invalidateReject("invalidate.tenant", "MISSING", version, "invalidation needs a tenant")
	}
	change := req.Change
	if strings.TrimSpace(change.ArticleID) == "" {
		return Invalidation{}, invalidateReject("change.article", "MISSING", version, "content change needs an article")
	}
	if !change.Kind.Valid() {
		return Invalidation{}, invalidateReject("change.kind", "INVALID", version, fmt.Sprintf("change kind %q is not declared", change.Kind))
	}
	if change.At.IsZero() || req.At.IsZero() {
		return Invalidation{}, invalidateReject("change.at", "MISSING", version, "change and invalidation times are required")
	}
	if change.Kind == ChangeRetire {
		if change.Revision != 0 {
			return Invalidation{}, invalidateReject("change.revision", "INVALID", version, "retire carries no new revision")
		}
	} else if change.Revision == 0 {
		return Invalidation{}, invalidateReject("change.revision", "MISSING", version, fmt.Sprintf("article %q needs a superseding revision", change.ArticleID))
	}
	if change.Kind != ChangeRetire && len(change.Citations) == 0 {
		return Invalidation{}, invalidateReject("change.citations", "UNCITED", version, fmt.Sprintf("article %q revision %d has no citations", change.ArticleID, change.Revision))
	}
	if change.Injected {
		return Invalidation{}, invalidateReject("change.provenance", "INJECTED", version, fmt.Sprintf("article %q arrived from an untrusted source", change.ArticleID))
	}
	if change.Invalidated {
		return Invalidation{}, invalidateReject("change.validity", "INVALIDATED", version, fmt.Sprintf("article %q was invalidated by its source", change.ArticleID))
	}
	var scoped []DerivedAnswer
	var maxRevision uint64
	for _, a := range req.Answers {
		if a.TenantID != req.TenantID || a.ArticleID != change.ArticleID {
			continue
		}
		scoped = append(scoped, a)
		if a.Revision > maxRevision {
			maxRevision = a.Revision
		}
	}
	if change.Kind != ChangeRetire && change.Revision <= maxRevision {
		return Invalidation{}, invalidateReject("change.revision", "STALE", version, fmt.Sprintf("revision %d does not supersede revision %d", change.Revision, maxRevision))
	}
	var affected, preserved []DerivedAnswer
	for _, a := range scoped {
		if change.Kind != ChangeRetire && a.Revision >= change.Revision {
			continue
		}
		switch {
		case len(a.Citations) == 0:
			return Invalidation{}, invalidateReject("answer.citations", "UNCITED", version, fmt.Sprintf("answer %q has no citations", a.AnswerRef))
		case a.Injected:
			return Invalidation{}, invalidateReject("answer.provenance", "INJECTED", version, fmt.Sprintf("answer %q arrived from an untrusted source", a.AnswerRef))
		case a.Invalidated:
			return Invalidation{}, invalidateReject("answer.validity", "INVALIDATED", version, fmt.Sprintf("answer %q was invalidated by its source", a.AnswerRef))
		}
		preserved = append(preserved, a)
		marked := a
		marked.State = AnswerStale
		if change.Kind == ChangeRetire {
			marked.State = AnswerRetired
		}
		affected = append(affected, marked)
	}
	sort.Slice(affected, func(i, j int) bool { return affected[i].AnswerRef < affected[j].AnswerRef })
	sort.Slice(preserved, func(i, j int) bool { return preserved[i].AnswerRef < preserved[j].AnswerRef })
	hit := map[string]bool{}
	for _, a := range affected {
		hit[a.AnswerRef] = true
	}
	var agents, workflows []string
	seenAgents, seenWorkflows := map[string]bool{}, map[string]bool{}
	for _, c := range req.Consumers {
		if !hit[c.AnswerRef] {
			continue
		}
		if c.AgentID != "" && !seenAgents[c.AgentID] {
			seenAgents[c.AgentID] = true
			agents = append(agents, c.AgentID)
		}
		if c.WorkflowID != "" && !seenWorkflows[c.WorkflowID] {
			seenWorkflows[c.WorkflowID] = true
			workflows = append(workflows, c.WorkflowID)
		}
	}
	sort.Strings(agents)
	sort.Strings(workflows)
	refs := make([]string, 0, len(affected))
	for _, a := range affected {
		refs = append(refs, a.AnswerRef)
	}
	return Invalidation{
		AffectedAnswers: affected, AffectedAgents: agents,
		AffectedWorkflows: workflows, Preserved: preserved,
		Digest: invalidationDigest(req.TenantID, change, refs, req.At),
	}, nil
}

// RebuildAnswer rebuilds one stale-marked answer at a new cited revision.
// Answers that were never marked stale — current answers, already-rebuilt
// answers and retired answers — cannot rebuild: staleness precedes rebuild
// and retirement is terminal.
func RebuildAnswer(stale DerivedAnswer, newRevision uint64, citations []SourceRef, at time.Time) (DerivedAnswer, error) {
	const version = invalidateSchemaVersion
	if stale.State != AnswerStale {
		return DerivedAnswer{}, invalidateReject("answer.state", "NOT_STALE", version, fmt.Sprintf("answer %q must be marked stale before rebuild", stale.AnswerRef))
	}
	if newRevision <= stale.Revision {
		return DerivedAnswer{}, invalidateReject("answer.revision", "STALE", version, fmt.Sprintf("rebuild revision %d does not supersede revision %d", newRevision, stale.Revision))
	}
	if len(citations) == 0 {
		return DerivedAnswer{}, invalidateReject("answer.citations", "UNCITED", version, fmt.Sprintf("answer %q rebuild has no citations", stale.AnswerRef))
	}
	if at.IsZero() {
		return DerivedAnswer{}, invalidateReject("answer.at", "MISSING", version, "rebuild time is required")
	}
	return DerivedAnswer{
		AnswerRef: stale.AnswerRef, TenantID: stale.TenantID,
		ArticleID: stale.ArticleID, Revision: newRevision,
		ChunkRefs: append([]string(nil), stale.ChunkRefs...),
		Citations: append([]SourceRef(nil), citations...),
		State:     AnswerCurrent, At: at,
	}, nil
}
