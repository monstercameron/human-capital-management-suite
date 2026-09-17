package knowledge

import (
	"errors"
	"testing"
	"time"
)

func know005Answers(t *testing.T) []DerivedAnswer {
	t.Helper()
	cited := []SourceRef{
		{System: "hr-policy", Identifier: "pto-2026", Authority: "authority:hr-policy-board"},
	}
	at := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	return []DerivedAnswer{
		{
			AnswerRef: "answer:pto-1", TenantID: "acme",
			ArticleID: "art:pto-policy", Revision: 2,
			ChunkRefs: []string{"chunk:pto-1"}, Citations: cited,
			State: AnswerCurrent, At: at,
		},
		{
			AnswerRef: "answer:pto-2", TenantID: "acme",
			ArticleID: "art:pto-policy", Revision: 2,
			ChunkRefs: []string{"chunk:pto-2"}, Citations: cited,
			State: AnswerCurrent, At: at,
		},
	}
}

func know005Request(t *testing.T) InvalidateRequest {
	t.Helper()
	return InvalidateRequest{
		TenantID: "acme",
		Change: ContentChange{
			ArticleID: "art:pto-policy", Kind: ChangePublish, Revision: 3,
			Citations: []SourceRef{
				{System: "hr-policy", Identifier: "pto-2026-r3", Authority: "authority:hr-policy-board"},
			},
			At: time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC),
		},
		Answers: know005Answers(t),
		Consumers: []AnswerConsumer{
			{AnswerRef: "answer:pto-1", AgentID: "agent:hr-help", WorkflowID: "workflow:onboarding"},
			{AnswerRef: "answer:pto-2", AgentID: "agent:hr-help", WorkflowID: "workflow:leave-request"},
		},
		At: time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC),
	}
}

// TestTodo_KNOW_005 is the primary KNOW-005 contract test. Seeded defects —
// uncited, stale, injected or invalidated knowledge left publishable — must
// return KNOW_005_REJECTED with the offending field/state/version and
// persist nothing: invalidation is pure.
func TestTodo_KNOW_005(t *testing.T) {
	t.Run("publish change marks affected stale before rebuild", func(t *testing.T) {
		inv, err := InvalidateDerived(know005Request(t))
		if err != nil {
			t.Fatalf("InvalidateDerived: %v", err)
		}
		if len(inv.AffectedAnswers) != 2 {
			t.Fatalf("both derived answers are affected: %+v", inv.AffectedAnswers)
		}
		for _, a := range inv.AffectedAnswers {
			if a.State != AnswerStale {
				t.Fatalf("affected answer must be STALE before rebuild: %+v", a)
			}
			if len(a.Citations) == 0 {
				t.Fatalf("stale mark must preserve citations: %+v", a)
			}
		}
		if len(inv.AffectedAgents) != 1 || inv.AffectedAgents[0] != "agent:hr-help" {
			t.Fatalf("affected agents must be identified: %+v", inv.AffectedAgents)
		}
		if len(inv.AffectedWorkflows) != 2 {
			t.Fatalf("affected workflows must be identified: %+v", inv.AffectedWorkflows)
		}
		if inv.Digest == "" {
			t.Fatal("invalidation must carry a digest")
		}
		rebuilt, err := RebuildAnswer(inv.AffectedAnswers[0], 3, inv.Preserved[0].Citations,
			time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("RebuildAnswer: %v", err)
		}
		if rebuilt.State != AnswerCurrent || rebuilt.Revision != 3 {
			t.Fatalf("rebuild must return a current answer at the new revision: %+v", rebuilt)
		}
	})

	t.Run("seeded defects are rejected with field state version", func(t *testing.T) {
		base := know005Request(t)
		uncitedChange := base
		uncitedChange.Change.Citations = nil
		staleChange := base
		staleChange.Change.Revision = 1
		injectedChange := base
		injectedChange.Change.Injected = true
		invalidatedChange := base
		invalidatedChange.Change.Invalidated = true
		for name, req := range map[string]InvalidateRequest{
			"uncited": uncitedChange, "stale": staleChange,
			"injected": injectedChange, "invalidated": invalidatedChange,
		} {
			_, err := InvalidateDerived(req)
			var rej *InvalidationRejection
			if !errors.As(err, &rej) {
				t.Fatalf("%s: expected *InvalidationRejection, got %v", name, err)
			}
			if !errors.Is(err, ErrInvalidateRejected) {
				t.Fatalf("%s: expected KNOW_005_REJECTED, got %v", name, err)
			}
			if rej.Field == "" || rej.State == "" || rej.Version == "" {
				t.Fatalf("%s: rejection must name field/state/version: %+v", name, rej)
			}
		}
		uncitedAnswer := base
		uncitedAnswer.Answers = know005Answers(t)
		uncitedAnswer.Answers[0].Citations = nil
		if _, err := InvalidateDerived(uncitedAnswer); !errors.Is(err, ErrInvalidateRejected) {
			t.Fatalf("uncited derived answer must be rejected, got %v", err)
		}
		taintedAnswer := base
		taintedAnswer.Answers = know005Answers(t)
		taintedAnswer.Answers[1].Injected = true
		if _, err := InvalidateDerived(taintedAnswer); !errors.Is(err, ErrInvalidateRejected) {
			t.Fatalf("injected derived answer must be rejected, got %v", err)
		}
	})

	t.Run("invalidation is pure and leaves inputs unmutated", func(t *testing.T) {
		req := know005Request(t)
		if _, err := InvalidateDerived(req); err != nil {
			t.Fatalf("InvalidateDerived: %v", err)
		}
		for _, a := range req.Answers {
			if a.State != AnswerCurrent {
				t.Fatalf("input answers must stay CURRENT: %+v", a)
			}
		}
		if req.Change.Revision != 3 {
			t.Fatalf("input change must be unmutated: %+v", req.Change)
		}
	})

	t.Run("retire and correction identify affected answers", func(t *testing.T) {
		retire := know005Request(t)
		retire.Change.Kind = ChangeRetire
		retire.Change.Revision = 0
		inv, err := InvalidateDerived(retire)
		if err != nil {
			t.Fatalf("InvalidateDerived: %v", err)
		}
		for _, a := range inv.AffectedAnswers {
			if a.State != AnswerRetired {
				t.Fatalf("retired answers must be RETIRED: %+v", a)
			}
		}
		correction := know005Request(t)
		correction.Change.Kind = ChangeCorrection
		inv, err = InvalidateDerived(correction)
		if err != nil {
			t.Fatalf("InvalidateDerived: %v", err)
		}
		if len(inv.AffectedAnswers) != 2 {
			t.Fatalf("correction must identify affected answers: %+v", inv.AffectedAnswers)
		}
	})
}

// TestTodo_KNOW_005_Recovery proves stale-before-rebuild ordering and
// historical evidence preservation: rebuilds of current answers fail,
// retired answers never rebuild, and the pre-change cited answer survives.
func TestTodo_KNOW_005_Recovery(t *testing.T) {
	inv, err := InvalidateDerived(know005Request(t))
	if err != nil {
		t.Fatalf("InvalidateDerived: %v", err)
	}
	current := know005Answers(t)[0]
	if _, err := RebuildAnswer(current, 3, current.Citations, time.Now().UTC()); !errors.Is(err, ErrInvalidateRejected) {
		t.Fatalf("rebuild of a CURRENT answer must fail, got %v", err)
	}
	if len(inv.Preserved) != 2 {
		t.Fatalf("historical cited answers must be preserved: %+v", inv.Preserved)
	}
	for _, p := range inv.Preserved {
		if p.State != AnswerCurrent || len(p.Citations) == 0 {
			t.Fatalf("preserved evidence must stay cited and current: %+v", p)
		}
	}
	rebuilt, err := RebuildAnswer(inv.AffectedAnswers[0], 3, inv.Preserved[0].Citations,
		time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("RebuildAnswer: %v", err)
	}
	if rebuilt.AnswerRef != inv.AffectedAnswers[0].AnswerRef {
		t.Fatalf("rebuild must preserve answer identity: %+v", rebuilt)
	}
	if _, err := RebuildAnswer(rebuilt, 4, rebuilt.Citations, time.Now().UTC()); !errors.Is(err, ErrInvalidateRejected) {
		t.Fatalf("second rebuild without a new stale mark must fail, got %v", err)
	}
	retire := know005Request(t)
	retire.Change.Kind = ChangeRetire
	retire.Change.Revision = 0
	retiredInv, err := InvalidateDerived(retire)
	if err != nil {
		t.Fatalf("InvalidateDerived: %v", err)
	}
	if _, err := RebuildAnswer(retiredInv.AffectedAnswers[0], 4,
		retiredInv.Preserved[0].Citations, time.Now().UTC()); !errors.Is(err, ErrInvalidateRejected) {
		t.Fatalf("retired answers must never rebuild, got %v", err)
	}
}

// TestTodo_KNOW_005_Mutation kills the guard-removal mutants: dropping the
// citation, injection, invalidation or stale-ordering check must fail.
func TestTodo_KNOW_005_Mutation(t *testing.T) {
	base := know005Request(t)
	uncited := base
	uncited.Answers = know005Answers(t)
	uncited.Answers[0].Citations = nil
	invalidated := base
	invalidated.Answers = know005Answers(t)
	invalidated.Answers[0].Invalidated = true
	outOfOrder := know005Answers(t)[0]
	for name, fn := range map[string]func() error{
		"citation check":     func() error { _, err := InvalidateDerived(uncited); return err },
		"invalidation check": func() error { _, err := InvalidateDerived(invalidated); return err },
		"stale-order check": func() error {
			_, err := RebuildAnswer(outOfOrder, 3, outOfOrder.Citations, time.Now().UTC())
			return err
		},
	} {
		if err := fn(); !errors.Is(err, ErrInvalidateRejected) {
			t.Fatalf("%s mutant survived: expected KNOW_005_REJECTED", name)
		}
	}
}
