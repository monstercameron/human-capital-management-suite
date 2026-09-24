package productui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_REV_074_01(t *testing.T) {
	view := testView(PagePaySummary)
	view.PaySummary = PaySummaryProjection{State: PayReadReady,
		Amount:            PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "125000.00"},
		Currency:          PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "USD"},
		PayBasis:          PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "ANNUAL_SALARY"},
		Frequency:         PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "ANNUAL"},
		EffectiveInterval: PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "2026-09-01 onward"},
		KnownAt:           PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "2026-09-23T12:00:00Z"},
	}
	markup, err := ui.RenderToString(paySummaryPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Base pay", "USD", "ANNUAL_SALARY", "2026-09-01 onward", "2026-09-23T12:00:00Z"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("authorized summary markup does not contain %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Return to live workspace") {
		t.Fatal("a ready compensation projection rendered its unavailable fallback")
	}
	errorView := testView(PagePaySummary)
	errorView.PaySummary = PaySummaryFromCompensationRead(compensation.Result{}, context.DeadlineExceeded)
	errorMarkup, err := ui.RenderToString(paySummaryPage(errorView))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errorMarkup, "Pay details could not be loaded") || strings.Contains(errorMarkup, "not published yet") {
		t.Fatalf("read failure was conflated with an unpublished service: %s", errorMarkup)
	}

	statements := testView(PagePayStatements)
	statements.PayStatements = PayStatementsProjection{State: PayReadReady, Statements: []PayStatementRow{{StatementRef: "statement-1", Period: "1–15 Sep 2026", PayDate: "2026-09-18", NetAmount: PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "2400.00"}, Currency: PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "USD"}}}}
	statementMarkup, err := ui.RenderToString(payStatementsPage(statements))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1–15 Sep 2026", "2026-09-18", "USD"} {
		if !strings.Contains(statementMarkup, want) {
			t.Fatalf("authorized statement markup does not contain %q: %s", want, statementMarkup)
		}
	}
	if strings.Contains(statementMarkup, "statement-1") {
		t.Fatal("internal statement reference was exposed in employee markup")
	}
	noStatements := testView(PagePayStatements)
	noStatements.PayStatements.State = PayReadNoRecord
	noStatementsMarkup, err := ui.RenderToString(payStatementsPage(noStatements))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(noStatementsMarkup, "No pay statements are available") || strings.Contains(noStatementsMarkup, "not published yet") {
		t.Fatalf("no-record statement result was conflated with an unpublished service: %s", noStatementsMarkup)
	}

	discrepancy := testView(PagePayDiscrepancy)
	markup, err = ui.RenderToString(payDiscrepancyPage(discrepancy))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "pay-discrepancy-form") || !strings.Contains(markup, "Pay discrepancy unavailable") {
		t.Fatal("intake without a governed submit service must remain an explicit unavailable state")
	}
}

func TestTodo_REV_074_01_Golden(t *testing.T) {
	view := testView(PagePaySummary)
	view.PaySummary = PaySummaryProjection{State: PayReadReady,
		Amount:            PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "125000.00"},
		Currency:          PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "USD"},
		PayBasis:          PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "ANNUAL_SALARY"},
		Frequency:         PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "ANNUAL"},
		EffectiveInterval: PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "2026-09-01 onward"},
		KnownAt:           PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "2026-09-23T12:00:00Z"},
	}
	markup, err := ui.RenderToString(paySummaryPage(view))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(markup))
	got := hex.EncodeToString(digest[:])
	const want = "0b8c95436378aeaa01542fdc1a633cddcb44999e6ce4d97ba201a81d9380a9b2"
	if got != want {
		t.Fatalf("employee pay summary projection digest = %s, want %s", got, want)
	}
}

func TestTodo_REV_074_01_Security(t *testing.T) {
	view := testView(PagePaySummary)
	view.PaySummary = PaySummaryFromCompensationRead(compensation.Result{
		Disclosure: "PARTIAL", Presence: "PRESENT",
		Fields: []compensation.DisclosedFact{
			{Field: compensation.FieldAmount, Access: compensation.EffectDeny, DenialReason: "policy", Value: values.Value("SECRET-125000")},
			{Field: compensation.FieldCurrency, Access: compensation.EffectAllow, Value: values.Value("USD")},
			{Field: compensation.FieldPayBasis, Access: compensation.EffectAllow, Value: values.Unknown[string]("not_asserted_at_requested_coordinate")},
		},
	}, nil)
	markup, err := ui.RenderToString(paySummaryPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "SECRET-125000") || strings.Contains(markup, "USD") || !strings.Contains(markup, "Restricted") || !strings.Contains(markup, "Not available") {
		t.Fatalf("field-level denial/unknown semantics were not kept fail-closed: %s", markup)
	}
	duplicate := PaySummaryFromCompensationRead(compensation.Result{Disclosure: "PARTIAL", Presence: "PRESENT", Fields: []compensation.DisclosedFact{
		{Field: compensation.FieldAmount, Access: compensation.EffectAllow, Value: values.Value("100.00")},
		{Field: compensation.FieldAmount, Access: compensation.EffectDeny, Value: values.Redacted[string]("policy")},
	}}, nil)
	if duplicate.State != PayReadError || duplicate.Amount.Value != "" {
		t.Fatalf("duplicate domain fields were not rejected: %+v", duplicate)
	}
	statements := testView(PagePayStatements)
	statements.PayStatements = PayStatementsProjection{State: PayReadReady, Statements: []PayStatementRow{{
		StatementRef: "statement-self", Period: "September", PayDate: "2026-09-18",
		NetAmount: PayValueProjection{Included: true, Access: "DENY", Presence: "REDACTED", Value: "SECRET-NET-PAY"},
		Currency:  PayValueProjection{Included: true, Access: "ALLOW", Presence: "VALUE", Value: "USD"},
	}}}
	statementMarkup, err := ui.RenderToString(payStatementsPage(statements))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statementMarkup, "SECRET-NET-PAY") || strings.Contains(statementMarkup, "USD") || !strings.Contains(statementMarkup, "Restricted") {
		t.Fatalf("statement row exposed a denied net amount: %s", statementMarkup)
	}

	called := 0
	var received PayDiscrepancyRequest
	projection := PayDiscrepancyProjection{
		Statements: []PayStatementOption{{StatementRef: "statement-self", PeriodRef: "period-self", PeriodLabel: "September"}},
		Categories: []PayDiscrepancyCategory{{Code: "incorrect_amount", Label: "Incorrect amount"}},
		Submit:     func(request PayDiscrepancyRequest, done func(error)) { called++; received = request; done(nil) },
	}
	for _, request := range []PayDiscrepancyRequest{
		{StatementRef: "statement-other", PeriodRef: "period-self", Category: "incorrect_amount", Reason: "Wrong amount"},
		{StatementRef: "statement-self", PeriodRef: "period-self", Category: "arbitrary", Reason: "Wrong amount"},
	} {
		if submitPayDiscrepancy(projection, request, nil) {
			t.Fatalf("unbound intake request accepted: %+v", request)
		}
	}
	if called != 0 {
		t.Fatalf("invalid intake requests reached governed service %d times", called)
	}
	want := PayDiscrepancyRequest{StatementRef: "statement-self", PeriodRef: "period-self", Category: "incorrect_amount", Reason: "Wrong amount"}
	if !submitPayDiscrepancy(projection, want, func(error) {}) || called != 1 || received != want {
		t.Fatalf("valid intake request = submitted:%t calls:%d value:%+v", called == 1, called, received)
	}
}

type rev07401Reader struct{}

func (rev07401Reader) CompensationAt(_ context.Context, query compensation.Query) (compensation.FactSet, error) {
	return compensation.FactSet{Worker: query.Worker}, nil
}

func TestTodo_REV_074_01_Integration(t *testing.T) {
	tenant := values.TenantId("tenant-rev07401")
	worker := values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: uuid.NewString()}
	instant := values.NewInstant(time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
	knownAt, err := values.NewKnownAt(instant)
	if err != nil {
		t.Fatal(err)
	}
	effectiveOn, err := values.NewLocalDate(2026, time.September, 23)
	if err != nil {
		t.Fatal(err)
	}
	result, err := compensation.Read(context.Background(), rev07401Reader{}, compensation.Request{
		Tenant: tenant, Worker: worker, AsOf: people.AsOf{EffectiveOn: effectiveOn, KnownAt: knownAt},
		Fields: []compensation.FieldID{compensation.FieldAmount},
		Authorization: compensation.AuthorizationDecision{PolicyVersion: "self-service/1", Purpose: "pay-self-read", SubjectDisclosable: true,
			Fields: map[compensation.FieldID]compensation.FieldRuling{compensation.FieldAmount: {Effect: compensation.EffectAllow}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	projection := PaySummaryFromCompensationRead(result, nil)
	if projection.State != PayReadNoRecord {
		t.Fatalf("domain absent response mapped to page state %q", projection.State)
	}
	view := testView(PagePaySummary)
	view.PaySummary = projection
	markup, err := ui.RenderToString(paySummaryPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "No pay details are available") || strings.Contains(markup, "USD") || strings.Contains(markup, "$0.00") {
		t.Fatalf("no-record domain read did not reach the truthful empty state: %s", markup)
	}
}
