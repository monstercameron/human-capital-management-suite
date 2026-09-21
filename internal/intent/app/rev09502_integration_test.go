package app_test

import (
	"context"
	"errors"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

// rev09502Refused asserts err is the typed business-reason refusal naming
// field, and nothing else.
func rev09502Refused(t *testing.T, err error, field string) {
	t.Helper()
	var typed *workspace.JourneyInputError
	if !errors.As(err, &typed) {
		t.Fatalf("err = %v, want a typed *workspace.JourneyInputError", err)
	}
	if typed.FieldPath != field || typed.ReasonRef != workspace.JourneyReasonNotProse {
		t.Fatalf("refusal = %s/%s, want %s/%s", typed.FieldPath, typed.ReasonRef, field, workspace.JourneyReasonNotProse)
	}
	if !errors.Is(err, workspace.ErrJourneyInput) {
		t.Fatalf("refusal %v does not unwrap to ErrJourneyInput", err)
	}
}

func rev09502Count(t *testing.T, engine workspace.JourneyEngine, ctx context.Context) int {
	t.Helper()
	listed, err := engine.ListJourneys(ctx)
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	return len(listed)
}

// TestTodo_REV_095_02 drives every proposal write path through the
// production cell over real PostgreSQL: a token-shaped business reason is
// refused with a typed error naming the field, and no intent is recorded; a
// prose reason on the same form is recorded unchanged.
func TestTodo_REV_095_02(t *testing.T) {
	engine, principal := promoux012Engine(t)
	author := principal("principal:rev09502-author")
	form := workspace.ProposalInput{
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01",
	}

	// Propose: the page form.
	for _, token := range []string{"promotion_into_senior_hrbp", "promotion-into-senior-hrbp", "  retention_adjustment  "} {
		refused := form
		refused.BusinessReason = token
		_, err := engine.Propose(author, refused)
		rev09502Refused(t, err, "business_reason")
	}
	if got := rev09502Count(t, engine, author); got != 0 {
		t.Fatalf("a refused proposal left %d journeys recorded", got)
	}

	// ProposePromotion: the intent-only contract names the field "reason".
	contract, ok := engine.(interface {
		ProposePromotion(context.Context, *journeyv1.ProposePromotionRequest) (*journeyv1.ProposePromotionResponse, error)
	})
	if !ok {
		t.Fatal("the composed journey engine does not serve ProposePromotion")
	}
	_, err := contract.ProposePromotion(author, &journeyv1.ProposePromotionRequest{
		SubjectWorkerRef: "omar-reyes", DesiredJobCode: "OPS-HRBP3", DesiredGrade: "P3",
		DesiredBasePay: "98000.00", EffectiveDate: "2026-06-01", Reason: "promotion_into_senior_hrbp",
		ExpectedSubjectRevision: app.PromotionSubjectRevision("omar-reyes"), ClientRequestId: "req-rev09502-token",
	})
	rev09502Refused(t, err, "reason")
	if got := rev09502Count(t, engine, author); got != 0 {
		t.Fatalf("a refused intent-only proposal left %d journeys recorded", got)
	}

	// A prose reason is unaffected and is recorded exactly as written.
	prose := form
	prose.BusinessReason = "Promotion into the senior HRBP role"
	proposed, err := engine.Propose(author, prose)
	if err != nil {
		t.Fatalf("Propose(prose): %v", err)
	}
	if proposed.BusinessReason != prose.BusinessReason {
		t.Fatalf("recorded reason = %q, want %q", proposed.BusinessReason, prose.BusinessReason)
	}

	// EditProposal: the correction path refuses the same token before any
	// successor is recorded or the original is superseded.
	_, superseded, err := engine.EditProposal(author, proposed.IntentID, proposed.GovernanceVersion,
		"idem:rev09502:edit", "correcting the reason", workspace.EditProposalInput{
			TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "98000.00",
			EffectiveDate: "2026-06-01", BusinessReason: "promotion_into_senior_hrbp_fix_verify",
		})
	rev09502Refused(t, err, "business_reason")
	if superseded != "" {
		t.Fatalf("a refused edit superseded %s", superseded)
	}
	listed, err := engine.ListJourneys(author)
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	if len(listed) != 1 || listed[0].IntentID != proposed.IntentID || listed[0].BusinessReason != prose.BusinessReason {
		t.Fatalf("after a refused edit the journeys are %+v, want only %s with its prose reason", listed, proposed.IntentID)
	}
}
