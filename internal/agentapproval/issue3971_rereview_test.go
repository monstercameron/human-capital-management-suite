package agentapproval

import (
	"strings"
	"testing"
	"time"
)

func issue3971Pair(t *testing.T, mutate func(*AgentActionApproval)) (AgentActionApproval, AgentActionApproval) {
	t.Helper()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	service, err := New(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	previous := b2CreateID(t, service, "approval-1", b2Item("item-1", TierSubmitGoverned, "low"))
	current := cloneApproval(previous)
	current.ID = "approval-2"
	mutate(&current)
	current.Digest = Digest(current)
	return previous, current
}

func issue3971SeeAll(string, string) bool { return true }

func TestIssue3971_ReReviewListsChangedFieldsAndInvalidation(t *testing.T) {
	previous, current := issue3971Pair(t, func(a *AgentActionApproval) {
		a.Items[0].MaterialFields = []MaterialField{{Path: "job.level", Before: "L4", After: "L6"}, {Path: "job.effective_date", Before: "", After: "2026-12-01"}}
	})
	got := CompareApprovalRevisions(ReReviewInput{ViewerID: "user-1", Previous: &previous, Current: &current, CanSee: issue3971SeeAll})
	if got.State != ReReviewChanged || got.PreviousDigest != previous.Digest || got.CurrentDigest != current.Digest {
		t.Fatalf("comparison = %+v", got)
	}
	want := map[string]ReReviewChange{
		"job.level":          {ItemID: "item-1", Field: "job.level", Kind: ReReviewChangedField, Before: "L5", After: "L6"},
		"job.effective_date": {ItemID: "item-1", Field: "job.effective_date", Kind: ReReviewAddedField, After: "2026-12-01"},
	}
	if len(got.Changes) != len(want) {
		t.Fatalf("changes = %+v", got.Changes)
	}
	for _, change := range got.Changes {
		if want[change.Field] != change {
			t.Fatalf("change %+v, want %+v", change, want[change.Field])
		}
	}
	if !got.Invalidates(ReReviewInvalidApprovalDigest) {
		t.Fatalf("a changed revision must void the earlier approval: %+v", got.Invalidated)
	}
}

func TestIssue3971_ReReviewUnchangedDoesNotClaimInvalidation(t *testing.T) {
	previous, current := issue3971Pair(t, func(a *AgentActionApproval) {})
	got := CompareApprovalRevisions(ReReviewInput{ViewerID: "user-1", Previous: &previous, Current: &current, CanSee: issue3971SeeAll})
	if got.State != ReReviewUnchanged || len(got.Changes) != 0 || len(got.Invalidated) != 0 {
		t.Fatalf("identical revisions = %+v", got)
	}
}

func TestIssue3971_ReReviewAddedRemovedItemsAndStepUp(t *testing.T) {
	previous, current := issue3971Pair(t, func(a *AgentActionApproval) {
		a.Items = []ActionItem{b2Item("item-2", TierSubmitGoverned, "high")}
	})
	got := CompareApprovalRevisions(ReReviewInput{ViewerID: "user-1", Previous: &previous, Current: &current, CanSee: issue3971SeeAll})
	kinds := map[string]ReReviewChangeKind{}
	for _, change := range got.Changes {
		if change.Field == ReReviewFieldItem {
			kinds[change.ItemID] = change.Kind
		}
	}
	if kinds["item-1"] != ReReviewRemovedField || kinds["item-2"] != ReReviewAddedField {
		t.Fatalf("item changes = %+v", got.Changes)
	}
	if !got.Invalidates(ReReviewInvalidStepUp) {
		t.Fatalf("a high-risk revision needs a fresh step-up: %+v", got.Invalidated)
	}
}

// Security: a field the viewer may not see is counted, never named or valued.
func TestIssue3971_ReReviewHidesFieldsTheViewerCannotSee_Security(t *testing.T) {
	previous, current := issue3971Pair(t, func(a *AgentActionApproval) {
		a.Items[0].MaterialFields = []MaterialField{{Path: "job.level", Before: "L4", After: "L5"}, {Path: "pay.base_salary", Before: "100", After: "999000"}}
		a.Items[0].Uncertainty = "changed note"
	})
	previous.Items[0].MaterialFields = []MaterialField{{Path: "job.level", Before: "L4", After: "L5"}, {Path: "pay.base_salary", Before: "100", After: "120"}}
	previous.Digest = Digest(previous)
	got := CompareApprovalRevisions(ReReviewInput{ViewerID: "user-1", Previous: &previous, Current: &current, CanSee: func(_ string, field string) bool { return !strings.HasPrefix(field, "pay.") }})
	if got.HiddenChanges != 1 {
		t.Fatalf("hidden = %d, changes=%+v", got.HiddenChanges, got.Changes)
	}
	raw := strings.Join([]string{got.PreviousDigest}, "")
	for _, change := range got.Changes {
		raw += change.Field + change.Before + change.After
	}
	for _, leak := range []string{"pay.", "999000", "120"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("hidden field leaked %q: %+v", leak, got.Changes)
		}
	}
	// With no visibility callback nothing is shown at all.
	none := CompareApprovalRevisions(ReReviewInput{ViewerID: "user-1", Previous: &previous, Current: &current})
	if len(none.Changes) != 0 || none.HiddenChanges == 0 {
		t.Fatalf("nil visibility must default to hidden: %+v", none)
	}
}

func TestIssue3971_ReReviewIncompleteWhenEvidenceUnavailable_Security(t *testing.T) {
	previous, current := issue3971Pair(t, func(a *AgentActionApproval) { a.Items[0].Uncertainty = "now different" })
	tampered := previous
	tampered.Items = cloneItems(previous.Items)
	tampered.Items[0].Uncertainty = "edited after the digest"
	otherTask := previous
	otherTask.TaskID = "task-9"
	otherTask.Digest = Digest(otherTask)
	otherTenant := previous
	otherTenant.TenantID = "tenant-2"
	otherTenant.Digest = Digest(otherTenant)
	closed := current
	closed.State = StateRevoked
	cases := []struct {
		name  string
		input ReReviewInput
		want  ReReviewUnknown
	}{
		{"previous missing", ReReviewInput{ViewerID: "user-1", Current: &current}, ReReviewUnknownPrevious},
		{"previous tampered", ReReviewInput{ViewerID: "user-1", Previous: &tampered, Current: &current}, ReReviewUnknownPrevious},
		{"current missing", ReReviewInput{ViewerID: "user-1", Previous: &previous}, ReReviewUnknownCurrent},
		{"current no longer open", ReReviewInput{ViewerID: "user-1", Previous: &previous, Current: &closed}, ReReviewUnknownCurrent},
		{"viewer not assigned", ReReviewInput{ViewerID: "mallory", Previous: &previous, Current: &current}, ReReviewUnknownAccess},
		{"different task", ReReviewInput{ViewerID: "user-1", Previous: &otherTask, Current: &current}, ReReviewUnknownAccess},
		{"different tenant", ReReviewInput{ViewerID: "user-1", Previous: &otherTenant, Current: &current}, ReReviewUnknownAccess},
	}
	for _, tc := range cases {
		tc.input.CanSee = issue3971SeeAll
		got := CompareApprovalRevisions(tc.input)
		if got.State != ReReviewIncomplete || len(got.Changes) != 0 || got.HiddenChanges != 0 || got.PreviousDigest != "" || len(got.Invalidated) != 0 {
			t.Errorf("%s: comparison must be incomplete and empty, got %+v", tc.name, got)
			continue
		}
		if len(got.Unknown) != 1 || got.Unknown[0] != tc.want {
			t.Errorf("%s: unknown = %v, want %v", tc.name, got.Unknown, tc.want)
		}
	}
}

func TestIssue3971_ReReviewSourceMessage(t *testing.T) {
	previous, current := issue3971Pair(t, func(a *AgentActionApproval) { a.Items[0].Uncertainty = "x" })
	base := ReReviewInput{ViewerID: "user-1", Previous: &previous, Current: &current, CanSee: issue3971SeeAll}
	base.Source = &SourceMessageInput{CapturedRevision: 1, CurrentRevision: 2, Accessible: true, CapturedText: "promote Sam to L5", CurrentText: "promote Sam to L6"}
	edited := CompareApprovalRevisions(base).Source
	if edited == nil || edited.Status != SourceMessageEdited || edited.CapturedText != "promote Sam to L5" || edited.CurrentText != "promote Sam to L6" || !edited.ConfirmIntent {
		t.Fatalf("edited source = %+v", edited)
	}
	base.Source = &SourceMessageInput{CapturedRevision: 1, CurrentRevision: 3, Accessible: true, Deleted: true, CapturedText: "secret instruction", CurrentText: "secret instruction"}
	deleted := CompareApprovalRevisions(base).Source
	if deleted == nil || deleted.Status != SourceMessageDeleted || deleted.CapturedText != "" || deleted.CurrentText != "" || !deleted.ConfirmIntent {
		t.Fatalf("deleted source must not expose retained content: %+v", deleted)
	}
	base.Source = &SourceMessageInput{CapturedRevision: 1, CurrentRevision: 2, Accessible: false, CapturedText: "private", CurrentText: "private"}
	lost := CompareApprovalRevisions(base).Source
	if lost == nil || lost.Status != SourceMessageUnavailable || lost.CapturedText != "" || lost.CurrentText != "" || !lost.ConfirmIntent {
		t.Fatalf("lost access must not expose content: %+v", lost)
	}
	base.Source = &SourceMessageInput{CapturedRevision: 2, CurrentRevision: 2, Accessible: true, CapturedText: "same", CurrentText: "same"}
	same := CompareApprovalRevisions(base).Source
	if same == nil || same.Status != SourceMessageUnchanged || same.ConfirmIntent || same.CapturedText != "" {
		t.Fatalf("unchanged source = %+v", same)
	}
	// An edit on its own never counts as an approval and never voids a matching digest.
	unchangedApproval := CompareApprovalRevisions(ReReviewInput{ViewerID: "user-1", Previous: &previous, Current: &previous, CanSee: issue3971SeeAll,
		Source: &SourceMessageInput{CapturedRevision: 1, CurrentRevision: 2, Accessible: true, CapturedText: "a", CurrentText: "b"}})
	if unchangedApproval.State != ReReviewUnchanged || len(unchangedApproval.Invalidated) != 0 || unchangedApproval.Source == nil || !unchangedApproval.Source.ConfirmIntent {
		t.Fatalf("edit-only comparison = %+v", unchangedApproval)
	}
}

func TestIssue3971_ReReviewCardCarriesComparisonWithoutApprovalAuthority(t *testing.T) {
	previous, current := issue3971Pair(t, func(a *AgentActionApproval) { a.Items[0].MaterialFields[0].After = "L6" })
	now := time.Date(2026, 9, 28, 12, 1, 0, 0, time.UTC)
	card, err := BuildPersonaChatCardWithReReview(current, "invocation-1", "user-1", now, ReReviewInput{ViewerID: "user-1", Previous: &previous, CanSee: issue3971SeeAll})
	if err != nil {
		t.Fatal(err)
	}
	if card.ReReview == nil || card.ReReview.State != ReReviewChanged || card.ReReview.CurrentDigest != card.Digest {
		t.Fatalf("card comparison = %+v", card.ReReview)
	}
	// The comparison is bound to the invoker: a different viewer gets a card with no comparison data.
	other, err := BuildPersonaChatCardWithReReview(current, "invocation-1", "user-1", now, ReReviewInput{ViewerID: "mallory", Previous: &previous, CanSee: issue3971SeeAll})
	if err != nil || other.ReReview == nil || other.ReReview.State != ReReviewIncomplete || len(other.ReReview.Changes) != 0 {
		t.Fatalf("another viewer's comparison = %+v, %v", other.ReReview, err)
	}
	// Chat still cannot decide: callbacks keep refusing approve.
	if _, err := ResolvePersonaChatCardCallback(card, PersonaChatCardCallback{Action: "approve", ActorID: "user-1", InvocationID: "invocation-1", Now: now, Current: current}); err != ErrChatApprovalSurface {
		t.Fatalf("approve from chat = %v", err)
	}
	// The re-review data does not change the exact-digest binding or the task-view callback.
	if href, err := ResolvePersonaChatCardCallback(card, PersonaChatCardCallback{Action: "open_task_view", ActorID: "user-1", InvocationID: "invocation-1", Now: now, Current: current}); err != nil || !strings.Contains(href, "task=task-1") {
		t.Fatalf("open task view = %q, %v", href, err)
	}
}

func TestIssue3971_ReReviewCardOmitsHiddenMaterialFields_Security(t *testing.T) {
	previous, current := issue3971Pair(t, func(a *AgentActionApproval) {
		a.Items[0].MaterialFields = []MaterialField{{Path: "job.level", Before: "L4", After: "L5"}, {Path: "pay.base_salary", Before: "1", After: "987654"}}
	})
	now := time.Date(2026, 9, 28, 12, 1, 0, 0, time.UTC)
	card, err := BuildPersonaChatCardWithReReview(current, "invocation-1", "user-1", now, ReReviewInput{ViewerID: "user-1", Previous: &previous,
		CanSee: func(_ string, field string) bool { return !strings.HasPrefix(field, "pay.") }})
	if err != nil {
		t.Fatal(err)
	}
	if got := card.Card.Values["material_changes"]; strings.Contains(got, "987654") || strings.Contains(got, "pay.") || !strings.Contains(got, "job.level") {
		t.Fatalf("material_changes = %q", got)
	}
	if card.Digest != current.Digest {
		t.Fatalf("digest binding must still cover every field: %s vs %s", card.Digest, current.Digest)
	}
}

// A card whose material list was redacted for the viewer must still open the
// governed task view, and a tampered list must not.
func TestIssue3971_ReReviewRedactedCardStillResolvesTaskView_Security(t *testing.T) {
	previous, current := issue3971Pair(t, func(a *AgentActionApproval) {
		a.Items[0].MaterialFields = []MaterialField{{Path: "job.level", Before: "L4", After: "L5"}, {Path: "pay.base_salary", Before: "1", After: "987654"}}
	})
	now := time.Date(2026, 9, 28, 12, 1, 0, 0, time.UTC)
	card, err := BuildPersonaChatCardWithReReview(current, "invocation-1", "user-1", now, ReReviewInput{ViewerID: "user-1", Previous: &previous,
		CanSee: func(_ string, field string) bool { return !strings.HasPrefix(field, "pay.") }})
	if err != nil {
		t.Fatal(err)
	}
	callback := PersonaChatCardCallback{Action: "open_task_view", ActorID: "user-1", InvocationID: "invocation-1", Now: now, Current: current}
	if href, err := ResolvePersonaChatCardCallback(card, callback); err != nil || !strings.Contains(href, "task=task-1") {
		t.Fatalf("redacted card must still open the task view: %q, %v", href, err)
	}
	forged := card
	forged.Card.Values = map[string]string{}
	for k, v := range card.Card.Values {
		forged.Card.Values[k] = v
	}
	forged.Card.Values["material_changes"] += "; item-1: job.level | L1 → L9"
	if _, err := ResolvePersonaChatCardCallback(forged, callback); err != ErrChatCardInvalid {
		t.Fatalf("a material entry the approval does not hold must be refused: %v", err)
	}
	forged.Card.Values["material_changes"] = card.Card.Values["material_changes"]
	forged.Card.Values["uncertainty"] = "edited"
	if _, err := ResolvePersonaChatCardCallback(forged, callback); err != ErrChatCardInvalid {
		t.Fatalf("other card values stay bound: %v", err)
	}
}

// Narrative card values follow the same field-level visibility as the diff.
func TestIssue3971_ReReviewCardOmitsHiddenNarrative_Security(t *testing.T) {
	previous, current := issue3971Pair(t, func(a *AgentActionApproval) {
		a.Items[0].Uncertainty = "salary question for 987654"
	})
	now := time.Date(2026, 9, 28, 12, 1, 0, 0, time.UTC)
	card, err := BuildPersonaChatCardWithReReview(current, "invocation-1", "user-1", now, ReReviewInput{ViewerID: "user-1", Previous: &previous,
		CanSee: func(_ string, field string) bool { return field != ReReviewFieldUncertainty }})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(card.Card.Values["uncertainty"], "987654") {
		t.Fatalf("hidden uncertainty leaked into the card: %q", card.Card.Values["uncertainty"])
	}
	callback := PersonaChatCardCallback{Action: "open_task_view", ActorID: "user-1", InvocationID: "invocation-1", Now: now, Current: current}
	if _, err := ResolvePersonaChatCardCallback(card, callback); err != nil {
		t.Fatalf("redacted card must still open the task view: %v", err)
	}
}
