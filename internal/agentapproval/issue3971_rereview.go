package agentapproval

import (
	"sort"
	"strings"
	"time"
)

// Issue #3971: "What changed since I last reviewed?". A deterministic,
// viewer-filtered comparison of the approval revision the reviewer last saw
// against the current one. It explains; it never approves, executes, notifies
// or claims that an earlier approval covers the new revision. Any new approval
// stays an explicit human decision on the current digest in the product task
// view (AGENT2-006).

// ReReviewState says what the comparison could establish.
type ReReviewState string

const (
	// ReReviewChanged means the exact digest differs from the one last reviewed.
	ReReviewChanged ReReviewState = "changed"
	// ReReviewUnchanged means the same digest is still current.
	ReReviewUnchanged ReReviewState = "unchanged"
	// ReReviewIncomplete means a revision is unavailable or access changed; no
	// details are produced.
	ReReviewIncomplete ReReviewState = "incomplete"
)

// ReReviewChangeKind says how one detail differs.
type ReReviewChangeKind string

const (
	ReReviewChangedField ReReviewChangeKind = "changed"
	ReReviewAddedField   ReReviewChangeKind = "added"
	ReReviewRemovedField ReReviewChangeKind = "removed"
)

// Field names that are not material paths. Material paths are used verbatim.
const (
	ReReviewFieldItem        = "item"
	ReReviewFieldTier        = "tier"
	ReReviewFieldRisk        = "risk"
	ReReviewFieldDefinition  = "definition"
	ReReviewFieldOperation   = "operation"
	ReReviewFieldUncertainty = "uncertainty"
	ReReviewFieldAgent       = "agent"
	ReReviewFieldExpires     = "expires"
	ReReviewFieldTaskExpires = "task_expires"
	// ReReviewFieldBasePrefix marks the current-record value a proposed
	// change starts from ("base:job.level"), so a changed underlying record is
	// distinguishable from a changed proposal.
	ReReviewFieldBasePrefix = "base:"
	// ReReviewFieldSourcePrefix and ReReviewFieldSubjectPrefix name evidence
	// and affected records by their own reference.
	ReReviewFieldSourcePrefix  = "source:"
	ReReviewFieldSubjectPrefix = "subject:"
)

// ReReviewInvalidation names a check or approval the change voids.
type ReReviewInvalidation string

const (
	// ReReviewInvalidApprovalDigest: an approval is bound to one exact digest.
	ReReviewInvalidApprovalDigest ReReviewInvalidation = "approval_digest"
	// ReReviewInvalidStepUp: the current revision is high risk and needs a new
	// step-up in the task view.
	ReReviewInvalidStepUp ReReviewInvalidation = "step_up"
)

// ReReviewUnknown names evidence that could not be established.
type ReReviewUnknown string

const (
	ReReviewUnknownPrevious ReReviewUnknown = "previous_revision"
	ReReviewUnknownCurrent  ReReviewUnknown = "current_revision"
	ReReviewUnknownAccess   ReReviewUnknown = "access"
)

// ReReviewChange is one visible difference. Before is what the reviewer
// last saw, After is what is proposed now.
type ReReviewChange struct {
	ItemID string
	Field  string
	Kind   ReReviewChangeKind
	Before string
	After  string
}

// Source message states.
const (
	SourceMessageUnchanged   = "unchanged"
	SourceMessageEdited      = "edited"
	SourceMessageDeleted     = "deleted"
	SourceMessageUnavailable = "unavailable"
)

// SourceMessageInput is the authorized view of the message the proposal was
// captured from. Text fields are filled by the caller only for revisions the
// reviewer may still open.
type SourceMessageInput struct {
	CapturedRevision int
	CurrentRevision  int
	Deleted          bool
	Accessible       bool
	CapturedText     string
	CurrentText      string
}

// SourceReview is the comparison outcome for the source message. Deleted and
// inaccessible messages carry no text. ConfirmIntent asks the person to check
// the proposal still represents their intent; it is never an approval.
type SourceReview struct {
	Status        string
	CapturedText  string
	CurrentText   string
	ConfirmIntent bool
}

// ReReviewInput is everything the comparison needs. CanSee is the field-level
// visibility decision for the viewer; nil means nothing may be shown.
type ReReviewInput struct {
	ViewerID string
	Previous *AgentActionApproval
	Current  *AgentActionApproval
	CanSee   func(itemID, field string) bool
	Source   *SourceMessageInput
}

// ReReview is the comparison result.
type ReReview struct {
	State          ReReviewState
	ViewerID       string
	PreviousDigest string
	CurrentDigest  string
	Changes        []ReReviewChange
	// HiddenChanges counts differences the viewer may not see. They are never
	// named or valued.
	HiddenChanges int
	Invalidated   []ReReviewInvalidation
	Unknown       []ReReviewUnknown
	Source        *SourceReview
}

// Invalidates reports whether the comparison voids the named check.
func (r ReReview) Invalidates(check ReReviewInvalidation) bool {
	for _, got := range r.Invalidated {
		if got == check {
			return true
		}
	}
	return false
}

// NeedsAttention reports whether the reviewer should be offered the comparison.
func (r ReReview) NeedsAttention() bool {
	return r.State != ReReviewUnchanged || (r.Source != nil && r.Source.ConfirmIntent)
}

func incompleteReReview(viewerID string, reason ReReviewUnknown) ReReview {
	return ReReview{State: ReReviewIncomplete, ViewerID: viewerID, Unknown: []ReReviewUnknown{reason}}
}

// CompareApprovalRevisions compares the revision last reviewed with the
// current one for one viewer. Authorization and digest integrity are checked
// before any detail is read, and an incomplete result carries no digests,
// fields or counts.
func CompareApprovalRevisions(in ReReviewInput) ReReview {
	viewer := strings.TrimSpace(in.ViewerID)
	prev, cur := in.Previous, in.Current
	if viewer == "" || (prev != nil && prev.AssignedUserID != viewer) || (cur != nil && cur.AssignedUserID != viewer) {
		return incompleteReReview("", ReReviewUnknownAccess)
	}
	if prev == nil || prev.ID == "" || prev.Digest == "" || Digest(*prev) != prev.Digest {
		return incompleteReReview(viewer, ReReviewUnknownPrevious)
	}
	if cur == nil || cur.ID == "" || cur.Digest == "" || Digest(*cur) != cur.Digest || cur.State != StatePending {
		return incompleteReReview(viewer, ReReviewUnknownCurrent)
	}
	if prev.TenantID != cur.TenantID || prev.TaskID != cur.TaskID {
		return incompleteReReview("", ReReviewUnknownAccess)
	}
	out := ReReview{State: ReReviewUnchanged, ViewerID: viewer, PreviousDigest: prev.Digest, CurrentDigest: cur.Digest, Source: reviewSource(in.Source)}
	if prev.Digest == cur.Digest {
		return out
	}
	out.State = ReReviewChanged
	out.Invalidated = []ReReviewInvalidation{ReReviewInvalidApprovalDigest}
	if approvalNeedsStepUp(*cur) {
		out.Invalidated = append(out.Invalidated, ReReviewInvalidStepUp)
	}
	var changes []ReReviewChange
	add := func(change ReReviewChange) { changes = append(changes, change) }
	if prev.AgentID != cur.AgentID {
		add(ReReviewChange{Field: ReReviewFieldAgent, Kind: ReReviewChangedField, Before: prev.AgentID, After: cur.AgentID})
	}
	if !prev.ExpiresAt.Equal(cur.ExpiresAt) {
		add(ReReviewChange{Field: ReReviewFieldExpires, Kind: ReReviewChangedField, Before: formatReReviewTime(prev.ExpiresAt), After: formatReReviewTime(cur.ExpiresAt)})
	}
	if !prev.TaskExpiresAt.Equal(cur.TaskExpiresAt) {
		add(ReReviewChange{Field: ReReviewFieldTaskExpires, Kind: ReReviewChangedField, Before: formatReReviewTime(prev.TaskExpiresAt), After: formatReReviewTime(cur.TaskExpiresAt)})
	}
	before, after := itemsByID(prev.Items), itemsByID(cur.Items)
	for id, item := range before {
		if _, ok := after[id]; !ok {
			add(ReReviewChange{ItemID: id, Field: ReReviewFieldItem, Kind: ReReviewRemovedField, Before: itemSummary(item)})
		}
	}
	for id, item := range after {
		old, ok := before[id]
		if !ok {
			add(ReReviewChange{ItemID: id, Field: ReReviewFieldItem, Kind: ReReviewAddedField, After: itemSummary(item)})
			continue
		}
		changes = append(changes, diffItem(id, old, item)...)
	}
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].ItemID != changes[j].ItemID {
			return changes[i].ItemID < changes[j].ItemID
		}
		return changes[i].Field < changes[j].Field
	})
	for _, change := range changes {
		if in.CanSee == nil || !in.CanSee(change.ItemID, change.Field) {
			out.HiddenChanges++
			continue
		}
		out.Changes = append(out.Changes, change)
	}
	return out
}

func formatReReviewTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func itemsByID(items []ActionItem) map[string]ActionItem {
	out := make(map[string]ActionItem, len(items))
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}

func itemSummary(item ActionItem) string {
	if item.Kind == ItemGovernedIntent {
		return strings.TrimSpace(item.IntentDefinitionID + " " + item.IntentDefinitionVersion)
	}
	return strings.TrimSpace(item.ConnectionID + " / " + item.ConnectionOperation)
}

func diffItem(id string, old, cur ActionItem) []ReReviewChange {
	var out []ReReviewChange
	changed := func(field, before, after string) {
		if before != after {
			out = append(out, ReReviewChange{ItemID: id, Field: field, Kind: ReReviewChangedField, Before: before, After: after})
		}
	}
	changed(ReReviewFieldTier, tierLabel(old.Tier), tierLabel(cur.Tier))
	changed(ReReviewFieldRisk, old.RiskClass, cur.RiskClass)
	changed(ReReviewFieldDefinition, strings.TrimSpace(old.IntentDefinitionID+" "+old.IntentDefinitionVersion), strings.TrimSpace(cur.IntentDefinitionID+" "+cur.IntentDefinitionVersion))
	changed(ReReviewFieldOperation, strings.TrimSpace(old.ConnectionID+" / "+old.ConnectionOperation), strings.TrimSpace(cur.ConnectionID+" / "+cur.ConnectionOperation))
	changed(ReReviewFieldUncertainty, old.Uncertainty, cur.Uncertainty)

	oldFields, curFields := map[string]MaterialField{}, map[string]MaterialField{}
	for _, f := range old.MaterialFields {
		oldFields[f.Path] = f
	}
	for _, f := range cur.MaterialFields {
		curFields[f.Path] = f
	}
	for path, f := range oldFields {
		next, ok := curFields[path]
		switch {
		case !ok:
			out = append(out, ReReviewChange{ItemID: id, Field: path, Kind: ReReviewRemovedField, Before: f.After})
		default:
			changed(path, f.After, next.After)
			changed(ReReviewFieldBasePrefix+path, f.Before, next.Before)
		}
	}
	for path, f := range curFields {
		if _, ok := oldFields[path]; !ok {
			out = append(out, ReReviewChange{ItemID: id, Field: path, Kind: ReReviewAddedField, After: f.After})
		}
	}

	oldSources, curSources := map[string]string{}, map[string]string{}
	for _, s := range old.Sources {
		oldSources[s.Ref] = s.Taint
	}
	for _, s := range cur.Sources {
		curSources[s.Ref] = s.Taint
	}
	for ref, taint := range oldSources {
		next, ok := curSources[ref]
		if !ok {
			out = append(out, ReReviewChange{ItemID: id, Field: ReReviewFieldSourcePrefix + ref, Kind: ReReviewRemovedField, Before: taint})
		} else {
			changed(ReReviewFieldSourcePrefix+ref, taint, next)
		}
	}
	for ref, taint := range curSources {
		if _, ok := oldSources[ref]; !ok {
			out = append(out, ReReviewChange{ItemID: id, Field: ReReviewFieldSourcePrefix + ref, Kind: ReReviewAddedField, After: taint})
		}
	}

	oldSubjects, curSubjects := map[string]string{}, map[string]string{}
	for _, s := range old.Subjects {
		oldSubjects[s.ID] = s.Kind
	}
	for _, s := range cur.Subjects {
		curSubjects[s.ID] = s.Kind
	}
	for sid, kind := range oldSubjects {
		next, ok := curSubjects[sid]
		if !ok {
			out = append(out, ReReviewChange{ItemID: id, Field: ReReviewFieldSubjectPrefix + sid, Kind: ReReviewRemovedField, Before: kind})
		} else {
			changed(ReReviewFieldSubjectPrefix+sid, kind, next)
		}
	}
	for sid, kind := range curSubjects {
		if _, ok := oldSubjects[sid]; !ok {
			out = append(out, ReReviewChange{ItemID: id, Field: ReReviewFieldSubjectPrefix + sid, Kind: ReReviewAddedField, After: kind})
		}
	}
	return out
}

func tierLabel(t Tier) string {
	if t == TierExternalWrite {
		return "T4"
	}
	return "T3"
}

func reviewSource(in *SourceMessageInput) *SourceReview {
	if in == nil {
		return nil
	}
	switch {
	case in.Deleted:
		return &SourceReview{Status: SourceMessageDeleted, ConfirmIntent: true}
	case !in.Accessible:
		return &SourceReview{Status: SourceMessageUnavailable, ConfirmIntent: true}
	case in.CurrentRevision != in.CapturedRevision:
		return &SourceReview{Status: SourceMessageEdited, CapturedText: in.CapturedText, CurrentText: in.CurrentText, ConfirmIntent: true}
	default:
		return &SourceReview{Status: SourceMessageUnchanged}
	}
}

// BuildPersonaChatCardWithReReview is BuildPersonaChatCard for a card whose
// proposal replaces one the invoker reviewed before. The comparison is
// computed for the invoker only; it adds no approval authority and does not
// change the exact-digest binding, expiry or task-view-only route.
func BuildPersonaChatCardWithReReview(current AgentActionApproval, invocationID, invokerID string, issuedAt time.Time, in ReReviewInput) (PersonaChatCard, error) {
	card, err := BuildPersonaChatCard(current, invocationID, invokerID, issuedAt)
	if err != nil {
		return PersonaChatCard{}, err
	}
	in.Current = &current
	comparison := CompareApprovalRevisions(in)
	card.ReReview = &comparison
	// The card's own lists follow the same field-level visibility as the
	// comparison, so a hidden detail is not named or valued there either. The
	// digest still covers every field.
	see := func(itemID, field string) bool { return in.CanSee != nil && in.CanSee(itemID, field) }
	var changes, sources, uncertainties []string
	for _, item := range current.Items {
		for _, field := range item.MaterialFields {
			if see(item.ID, field.Path) {
				changes = append(changes, item.ID+": "+field.Path+" | "+field.Before+" → "+field.After)
			}
		}
		for _, source := range item.Sources {
			if see(item.ID, ReReviewFieldSourcePrefix+source.Ref) {
				sources = append(sources, item.ID+": "+source.Ref+" ("+source.Taint+")")
			}
		}
		if see(item.ID, ReReviewFieldUncertainty) {
			uncertainties = append(uncertainties, item.ID+": "+item.Uncertainty)
		}
	}
	card.Card.Values["material_changes"] = strings.Join(changes, "; ")
	card.Card.Values["sources"] = strings.Join(sources, "; ")
	card.Card.Values["uncertainty"] = strings.Join(uncertainties, "; ")
	return card, nil
}

// issue3971RedactableKeys are the card values that list per-field detail and
// may therefore be shortened for a viewer who cannot see some fields.
var issue3971RedactableKeys = []string{"material_changes", "sources", "uncertainty"}

func issue3971Redactable(key string) bool {
	for _, k := range issue3971RedactableKeys {
		if k == key {
			return true
		}
	}
	return false
}

// entriesSubset reports whether every "; "-separated entry of got is also an
// entry of full.
func entriesSubset(got, full string) bool {
	if strings.TrimSpace(got) == "" {
		return true
	}
	have := map[string]bool{}
	for _, entry := range strings.Split(full, "; ") {
		have[entry] = true
	}
	for _, entry := range strings.Split(got, "; ") {
		if !have[entry] {
			return false
		}
	}
	return true
}
