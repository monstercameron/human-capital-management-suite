package workorder

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func fixture(t *testing.T) (*WorkOrder, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	w, err := NewWorkOrder(CreateInput{ID: "wo-1", TenantID: "tenant-1", ProjectID: "project-1", TemplateID: "construction", TemplateVersion: "v1", TemplateDigest: "digest-v1", ActorID: "initiator", InitiatorID: "initiator", IdempotencyKey: "create-1", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return w, now
}

func dec(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCreateRestoreAndPhaseTransition(t *testing.T) {
	w, now := fixture(t)
	if err := w.RequestPhaseTransition(TransitionInput{ExpectedRevision: 1, Target: PhaseAuthorization, Reason: "submit scope", ActorID: "initiator", IdempotencyKey: "phase-1", Now: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := w.RequestPhaseTransition(TransitionInput{ExpectedRevision: 1, Target: PhaseAuthorization, Reason: "submit scope", ActorID: "initiator", IdempotencyKey: "phase-1", Now: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if got := w.Snapshot(); got.Revision != 2 || got.Phase != PhaseAuthorization || len(got.Journal) != 2 {
		t.Fatalf("transition/replay state = %#v", got)
	}
	if err := w.RequestPhaseTransition(TransitionInput{ExpectedRevision: 1, Target: PhaseReady, Reason: "stale", ActorID: "initiator", IdempotencyKey: "phase-2", Now: now}); !errors.Is(err, ErrStale) {
		t.Fatalf("stale revision error = %v", err)
	}
	restored, err := Restore(w.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.RequestPhaseTransition(TransitionInput{ExpectedRevision: 1, Target: PhaseAuthorization, Reason: "submit scope", ActorID: "initiator", IdempotencyKey: "phase-1", Now: now}); err != nil {
		t.Fatalf("restored replay: %v", err)
	}
	bad := w.Snapshot()
	bad.Journal[1].Revision = 4
	if _, err := Restore(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("broken journal restore error = %v", err)
	}
}

func TestRequestApprovalNotesAssignmentAndJournal(t *testing.T) {
	w, now := fixture(t)
	request := InitiatorRequestInput{ID: "budget-1", DefinitionID: "budget-request", Kind: RequestBudget, Subject: "Stringer redesign", Rationale: "CO-03 scope", ActorID: "initiator", Amount: dec(t, "1250.00"), Currency: "USD", CostCategory: "steel", FundingSource: "project", BaselineRevision: "base-1", ExpectedRevision: 1, IdempotencyKey: "request-1", Now: now}
	if err := w.SubmitRequest(request); err != nil {
		t.Fatal(err)
	}
	request.Amount = dec(t, "9999.00")
	if err := w.SubmitRequest(request); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
	if err := w.DecideRequest(RequestDecisionInput{RequestID: "budget-1", ExpectedRevision: 2, Decision: RequestApproved, ActorID: "initiator", Reason: "self approval", IdempotencyKey: "decision-self", Now: now.Add(time.Minute)}); !errors.Is(err, ErrTransition) {
		t.Fatalf("self approval error = %v", err)
	}
	if err := w.DecideRequest(RequestDecisionInput{RequestID: "budget-1", ExpectedRevision: 2, Decision: RequestApproved, ActorID: "finance", Reason: "within policy", IdempotencyKey: "decision-1", Now: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := w.AddNote(NoteInput{ID: "note-1", AuthorID: "crew-lead", Body: "Delivered to site", Visibility: "PARTICIPANTS", Classification: "OPERATIONS", AttachmentRefs: []string{"doc-1"}, ExpectedRevision: 3, IdempotencyKey: "note-1", Now: now.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := w.Assign(AssignmentInput{ID: "assign-1", WorkerID: "worker-1", Role: "ironworker", Location: "Riverside", ActorID: "supervisor", ExpectedRevision: 4, IdempotencyKey: "assign-1", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour), Now: now.Add(3 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	s := w.Snapshot()
	if s.Phase != PhaseDraft || len(s.Requests) != 1 || s.Requests[0].Status != RequestApproved || len(s.Notes) != 1 || s.Notes[0].Body != "Delivered to site" || len(s.Assignments) != 1 || len(s.Journal) != 5 {
		t.Fatalf("unexpected aggregate state: %#v", s)
	}
}

func TestProgressSpendAndCorrectionAreAppendOnly(t *testing.T) {
	w, now := fixture(t)
	if err := w.RecordProgress(ProgressInput{ID: "progress-1", LineID: "line-1", Unit: "ft", ActorID: "foreman", Quantity: dec(t, "12.50"), ExpectedRevision: 1, IdempotencyKey: "progress-1", Now: now}); err != nil {
		t.Fatal(err)
	}
	if err := w.RecordSpend(SpendInput{ID: "spend-1", Category: "material", Description: "Steel", Currency: "USD", ActorID: "buyer", SourceRef: "receipt-1", Disposition: SpendIncurred, Amount: dec(t, "90.25"), ExpectedRevision: 2, IdempotencyKey: "spend-1", Now: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	replacement := dec(t, "85.25")
	if err := w.CorrectRecord(CorrectionInput{ID: "correction-1", TargetRecordID: "spend-1", Reason: "receipt adjustment", ActorID: "finance", ReplacementAmount: &replacement, ExpectedRevision: 3, IdempotencyKey: "correction-1", Now: now.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	s := w.Snapshot()
	if len(s.Progress) != 1 || len(s.Spending) != 1 || s.Spending[0].Amount.String() != "90.25" || len(s.Corrections) != 1 || s.Corrections[0].ReplacementAmount.String() != "85.25" {
		t.Fatalf("correction mutated source or was lost: %#v", s)
	}
	if err := w.RecordSpend(SpendInput{ID: "bad", Category: "material", Currency: "usd", ActorID: "buyer", SourceRef: "receipt", Amount: dec(t, "1.00"), ExpectedRevision: 4, IdempotencyKey: "bad", Now: now}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("lowercase currency error = %v", err)
	}
}

func TestValidationAndConfigurableTransitions(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	w, err := NewWorkOrder(CreateInput{ID: "wo-custom", TenantID: "tenant", ProjectID: "project", TemplateID: "service", TemplateVersion: "3", TemplateDigest: "digest-v3", ActorID: "initiator", InitialPhase: "INTAKE", Phases: []Phase{"INTAKE", "WORK", "DONE"}, Transitions: []PhaseTransition{{From: "INTAKE", To: "WORK"}, {From: "WORK", To: "DONE"}}, IdempotencyKey: "create", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.RequestPhaseTransition(TransitionInput{ExpectedRevision: 1, Target: "DONE", Reason: "skip", ActorID: "lead", IdempotencyKey: "skip", Now: now}); !errors.Is(err, ErrTransition) {
		t.Fatalf("unconfigured edge error = %v", err)
	}
	if _, err := NewWorkOrder(CreateInput{ID: "", TenantID: "tenant", ProjectID: "project", TemplateID: "t", TemplateVersion: "1", TemplateDigest: "digest", IdempotencyKey: "key", Now: now}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid create error = %v", err)
	}
}

func TestWorkEntryRevisionCorrectionAndCopySafety(t *testing.T) {
	w, now := fixture(t)
	input := WorkEntryInput{ID: "labor-1", WorkerID: "worker-1", Kind: "LABOR", WorkDate: "2026-09-25", TimeZone: "America/New_York", LineID: "line-1", Description: "stringer install", ActorID: "foreman", DurationMinutes: 90, ExpectedRevision: 1, IdempotencyKey: "entry-1", Now: now}
	if err := w.RecordWorkEntry(input); err != nil {
		t.Fatal(err)
	}
	if err := w.RecordWorkEntry(input); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	input.ID = "labor-2"
	input.CorrectionOfEntryID = "labor-1"
	input.ExpectedRevision = 2
	input.IdempotencyKey = "entry-2"
	input.DurationMinutes = 60
	if err := w.RecordWorkEntry(input); err != nil {
		t.Fatal(err)
	}
	bad := input
	bad.ID = "labor-3"
	bad.CorrectionOfEntryID = "missing"
	bad.ExpectedRevision = 3
	bad.IdempotencyKey = "entry-3"
	if err := w.RecordWorkEntry(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing correction target = %v", err)
	}
	s := w.Snapshot()
	if len(s.WorkEntries) != 2 || s.WorkEntries[1].CorrectionOfEntryID != "labor-1" || s.Revision != 3 {
		t.Fatalf("entries/replay state = %#v", s)
	}
	restored, err := Restore(s)
	if err != nil {
		t.Fatal(err)
	}
	s.WorkEntries[0].ID = "mutated"
	if restored.Snapshot().WorkEntries[0].ID != "labor-1" {
		t.Fatal("restore aliases input snapshot")
	}
	amount := dec(t, "1.00")
	s.Corrections = []Correction{{ID: "c1", TargetRecordID: "labor-1", ReplacementAmount: &amount}}
	restored, err = Restore(s)
	if err != nil {
		t.Fatal(err)
	}
	amount = dec(t, "2.00")
	if restored.Snapshot().Corrections[0].ReplacementAmount.String() != "1.00" {
		t.Fatal("restore aliases correction amount")
	}
}

func TestIdempotencyKeyIsScopedToActor(t *testing.T) {
	w, now := fixture(t)
	first := NoteInput{ID: "note-a", AuthorID: "author-a", Body: "first", Visibility: "PARTICIPANTS", Classification: "OPERATIONS", ExpectedRevision: 1, IdempotencyKey: "shared-key", Now: now}
	if err := w.AddNote(first); err != nil {
		t.Fatal(err)
	}
	second := NoteInput{ID: "note-b", AuthorID: "author-b", Body: "second", Visibility: "PARTICIPANTS", Classification: "OPERATIONS", ExpectedRevision: 2, IdempotencyKey: "shared-key", Now: now.Add(time.Minute)}
	if err := w.AddNote(second); err != nil {
		t.Fatalf("same key from another actor: %v", err)
	}
	restored, err := Restore(w.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Snapshot().Notes) != 2 {
		t.Fatalf("records=%d, want 2", len(restored.Snapshot().Notes))
	}
}

func TestOptionalDecimalsRoundTripForRequestsAndLabor(t *testing.T) {
	w, now := fixture(t)
	approval := InitiatorRequestInput{ID: "approval-1", DefinitionID: "approval", Kind: RequestApproval, Subject: "Lift plan", Rationale: "Review before work", ActorID: "initiator", Policy: "safety", ApproverClass: "supervisor", ProposedAction: "release", ExpectedRevision: 1, IdempotencyKey: "approval", Now: now}
	if err := w.SubmitRequest(approval); err != nil {
		t.Fatal(err)
	}
	document := InitiatorRequestInput{ID: "document-1", DefinitionID: "document", Kind: RequestDocument, Subject: "As-built", Rationale: "Closeout", ActorID: "initiator", ExpectedRevision: 2, IdempotencyKey: "document", Now: now.Add(time.Minute)}
	if err := w.SubmitRequest(document); err != nil {
		t.Fatal(err)
	}
	crew := InitiatorRequestInput{ID: "crew-1", DefinitionID: "crew", Kind: RequestCrew, Subject: "Ironworkers", Rationale: "Install", ActorID: "initiator", Quantity: dec(t, "2.00"), Unit: "workers", RoleOrItem: "ironworker", Location: "Riverside", NeededUntil: now.Add(24 * time.Hour).Format(time.RFC3339), EstimatedCost: dec(t, "500.00"), EstimatedCostCurrency: "USD", EvidenceRefs: []string{"evidence-a", "evidence-b"}, ExpectedRevision: 3, IdempotencyKey: "crew", Now: now.Add(2 * time.Minute)}
	if err := w.SubmitRequest(crew); err != nil {
		t.Fatal(err)
	}
	change := InitiatorRequestInput{ID: "co-1", DefinitionID: "change", Kind: RequestChangeOrder, Subject: "CO-03", Rationale: "Design revision", ActorID: "initiator", ScopeDelta: "Install redesigned stringers", BaselineRevision: "3", QuantityDelta: dec(t, "-2.00"), QuantityDeltaUnit: "ft", PriceDelta: dec(t, "-125.00"), PriceDeltaCurrency: "USD", ScheduleDelta: "3600", EvidenceRefs: []string{"rfi-14", "drawing-3"}, ExpectedRevision: 4, IdempotencyKey: "change", Now: now.Add(3 * time.Minute)}
	if err := w.SubmitRequest(change); err != nil {
		t.Fatal(err)
	}
	progress := ProgressInput{ID: "progress-1", LineID: "line-1", Unit: "ft", ActorID: "foreman", Quantity: dec(t, "4.00"), EvidenceRefs: []string{"photo-1", "photo-2"}, ExpectedRevision: 5, IdempotencyKey: "progress", Now: now.Add(4 * time.Minute)}
	if err := w.RecordProgress(progress); err != nil {
		t.Fatal(err)
	}
	spend := SpendInput{ID: "spend-1", Category: "steel", ActorID: "buyer", Currency: "USD", Amount: dec(t, "40.00"), Quantity: dec(t, "2.00"), Unit: "pcs", Disposition: SpendCommitment, SourceRef: "po-1", ExpectedRevision: 6, IdempotencyKey: "spend", Now: now.Add(5 * time.Minute)}
	if err := w.RecordSpend(spend); err != nil {
		t.Fatal(err)
	}
	entry := WorkEntryInput{ID: "labor-1", WorkerID: "worker", Kind: "LABOR", WorkDate: "2026-09-25", TimeZone: "America/New_York", LineID: "line", ActorID: "foreman", DurationMinutes: 60, ExpectedRevision: 7, IdempotencyKey: "labor", Now: now.Add(6 * time.Minute)}
	if err := w.RecordWorkEntry(entry); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(w.Snapshot())
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var restoredSnapshot Snapshot
	if err = json.Unmarshal(encoded, &restoredSnapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	restored, err := Restore(restoredSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	state := restored.Snapshot()
	if len(state.Requests) != 4 || len(state.WorkEntries) != 1 || state.WorkEntries[0].Quantity.Validate() != nil || state.Requests[2].Unit != "workers" || len(state.Requests[2].EvidenceRefs) != 2 || state.Requests[3].QuantityDelta.String() != "-2.00" || state.Progress[0].EvidenceRefs[1] != "photo-2" || state.Spending[0].Disposition != SpendCommitment || state.Spending[0].Quantity.String() != "2.00" {
		t.Fatalf("round-trip did not preserve optional decimals: %#v", restored.Snapshot())
	}
}

func TestInvalidCommandsLeaveAggregateUnchanged(t *testing.T) {
	w, now := fixture(t)
	invalidRequest := InitiatorRequestInput{ID: "bad-budget", DefinitionID: "budget", Kind: RequestBudget, Subject: "Budget", Rationale: "Need", ActorID: "initiator", Amount: dec(t, "10.00"), Currency: "USD", BaselineRevision: "base", ExpectedRevision: 1, IdempotencyKey: "bad-budget", Now: now}
	if err := w.SubmitRequest(invalidRequest); !errors.Is(err, ErrInvalid) {
		t.Fatalf("incomplete budget request: %v", err)
	}
	if w.Snapshot().Revision != 1 || len(w.Snapshot().Requests) != 0 {
		t.Fatal("invalid request changed state")
	}
	if err := w.Assign(AssignmentInput{ID: "bad-assignment", WorkerID: "worker", Role: "crew", ActorID: "supervisor", ExpectedRevision: 1, IdempotencyKey: "bad-assignment", Start: now.Add(time.Hour), End: now, Now: now}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reversed assignment window: %v", err)
	}
	partialPin := CreateInput{ID: "partial", TenantID: "tenant", ProjectID: "project", TemplateID: "template", TemplateVersion: "1", TemplateDigest: "digest", ActorID: "actor", WorkflowID: "workflow", IdempotencyKey: "create", Now: now}
	if _, err := NewWorkOrder(partialPin); !errors.Is(err, ErrInvalid) {
		t.Fatalf("partial workflow pin: %v", err)
	}
	entry := WorkEntryInput{ID: "other-1", WorkerID: "worker", Kind: "OTHER", WorkDate: "2026-09-25", TimeZone: "America/New_York", LineID: "line", Description: "site support", ActorID: "foreman", DurationMinutes: 30, ExpectedRevision: 1, IdempotencyKey: "other-1", Now: now}
	if err := w.RecordWorkEntry(entry); err != nil {
		t.Fatalf("OTHER entry: %v", err)
	}
}

func TestRestrictedNotesFailClosedOnWriteAndRestore(t *testing.T) {
	w, now := fixture(t)
	for _, visibility := range []string{"SUPERVISORS", "FINANCE"} {
		err := w.AddNote(NoteInput{ID: "restricted-" + visibility, AuthorID: "author", Body: "private", Visibility: visibility, Classification: "FINANCIAL", ExpectedRevision: 1, IdempotencyKey: "restricted-" + visibility, Now: now})
		if !errors.Is(err, ErrNoteVisibilityUnsupported) {
			t.Fatalf("visibility %s error = %v", visibility, err)
		}
	}
	if w.Snapshot().Revision != 1 || len(w.Snapshot().Notes) != 0 {
		t.Fatal("restricted note write changed aggregate")
	}
	malformed := w.Snapshot()
	malformed.Notes = []Note{{ID: "old-restricted", AuthorID: "author", Body: "sensitive", Visibility: "FINANCE", Classification: "FINANCIAL", At: now}}
	if _, err := Restore(malformed); !errors.Is(err, ErrNoteVisibilityUnsupported) {
		t.Fatalf("restricted snapshot restore error = %v", err)
	}
}
