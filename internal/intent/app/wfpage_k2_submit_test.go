package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/forms"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func wfpageK2Definition() WorkflowPageDefinition {
	return WorkflowPageDefinition{TenantID: "tenant-a", Version: "page-3", Published: true, Rules: []workflow.PageRule{{Code: "AMOUNT_LIMIT", Message: "Amount is too high.", Expression: `amount <= 100`, Inputs: map[string]workflow.ValueType{"amount": {Kind: workflow.KindInteger}}}}, Computed: []forms.ComputedField{{Path: "double", Type: workflow.ValueType{Kind: workflow.KindInteger}, Expression: `amount`, Inputs: map[string]workflow.ValueType{"amount": {Kind: workflow.KindInteger}}}}}
}

func TestTodo_WFPAGE_016(t *testing.T) {
	definition := wfpageK2Definition()
	input := workflow.PageRuleInput{Fields: map[string]any{"amount": int64(50)}}
	result, err := EnforceWorkflowPageSubmit(context.Background(), definition, WorkflowPageSubmission{TenantID: "tenant-a", ActorID: "hr-1", PageVersion: "page-3", Values: input, ComputedValues: map[string]forms.ComputedValue{"double": {Path: "double", Type: workflow.ValueType{Kind: workflow.KindInteger}, Value: int64(50)}}, ClientSkipped: true}, []PageAsyncCheck{PageAsyncCheckFunc(func(context.Context, PageAsyncCheckRequest) (PageAsyncCheckOutcome, error) {
		return PageAsyncCheckOutcome{Code: "DUPLICATE_PERSON_CLEAR", Field: "person", Severity: PageCheckAdvisory, Message: "No duplicate found", Authorized: true}, nil
	})})
	if err != nil || !result.Accepted || len(result.Advisories) != 1 {
		t.Fatalf("accepted submit = %+v, %v", result, err)
	}
	input.Fields["amount"] = int64(101)
	result, err = EnforceWorkflowPageSubmit(context.Background(), definition, WorkflowPageSubmission{TenantID: "tenant-a", ActorID: "hr-1", PageVersion: "page-3", Values: input, ComputedValues: map[string]forms.ComputedValue{"double": {Path: "double", Type: workflow.ValueType{Kind: workflow.KindInteger}, Value: int64(101)}}}, nil)
	if !errors.Is(err, ErrPageSubmitRejected) || result.Accepted || len(result.Findings) == 0 || result.Findings[0].Code != "AMOUNT_LIMIT" {
		t.Fatalf("server rule result = %+v, %v", result, err)
	}
}

func TestTodo_WFPAGE_016_Security(t *testing.T) {
	definition := wfpageK2Definition()
	input := workflow.PageRuleInput{Fields: map[string]any{"amount": int64(50)}}
	forged := WorkflowPageSubmission{TenantID: "tenant-a", ActorID: "hr-1", PageVersion: "page-3", Values: input, ComputedValues: map[string]forms.ComputedValue{"double": {Path: "double", Type: workflow.ValueType{Kind: workflow.KindInteger}, Value: int64(999)}}}
	if _, err := EnforceWorkflowPageSubmit(context.Background(), definition, forged, nil); !errors.Is(err, ErrPageSubmitRejected) {
		t.Fatalf("tampered computed value accepted: %v", err)
	}
	_, err := EnforceWorkflowPageSubmit(context.Background(), definition, WorkflowPageSubmission{TenantID: "tenant-a", ActorID: "hr-1", PageVersion: "page-3", Values: input, ComputedValues: map[string]forms.ComputedValue{"double": {Path: "double", Type: workflow.ValueType{Kind: workflow.KindInteger}, Value: int64(50)}}}, []PageAsyncCheck{PageAsyncCheckFunc(func(context.Context, PageAsyncCheckRequest) (PageAsyncCheckOutcome, error) {
		return PageAsyncCheckOutcome{Code: "PAY_BAND_HIDDEN", Field: "salary", Severity: PageCheckAdvisory, Message: "Not available for this viewer", Authorized: false}, nil
	})})
	if err != nil {
		t.Fatalf("safe unauthorized outcome leaked/refused: %v", err)
	}
	_, err = EnforceWorkflowPageSubmit(context.Background(), definition, WorkflowPageSubmission{TenantID: "tenant-a", ActorID: "hr-1", PageVersion: "page-3", Values: input, ComputedValues: map[string]forms.ComputedValue{"double": {Path: "double", Type: workflow.ValueType{Kind: workflow.KindInteger}, Value: int64(50)}}}, []PageAsyncCheck{PageAsyncCheckFunc(func(context.Context, PageAsyncCheckRequest) (PageAsyncCheckOutcome, error) {
		return PageAsyncCheckOutcome{Code: "PAY_BAND_HIDDEN", Field: "salary", Severity: PageCheckAdvisory, Message: "salary is 90000", Authorized: false}, nil
	})})
	if !errors.Is(err, ErrPageSubmitRejected) {
		t.Fatalf("unauthorized value disclosure accepted: %v", err)
	}
}

func TestTodo_WFPAGE_016_Integration(t *testing.T) {
	definition := wfpageK2Definition()
	input := workflow.PageRuleInput{Fields: map[string]any{"amount": int64(50)}}
	called := false
	result, err := EnforceWorkflowPageSubmit(context.Background(), definition, WorkflowPageSubmission{TenantID: "tenant-a", ActorID: "hr-1", PageVersion: "page-3", Values: input, ComputedValues: map[string]forms.ComputedValue{"double": {Path: "double", Type: workflow.ValueType{Kind: workflow.KindInteger}, Value: int64(50)}}}, []PageAsyncCheck{PageAsyncCheckFunc(func(_ context.Context, request PageAsyncCheckRequest) (PageAsyncCheckOutcome, error) {
		called = request.TenantID == "tenant-a" && request.PageVersion == "page-3"
		return PageAsyncCheckOutcome{Code: "BUDGET_AVAILABLE", Field: "salary", Severity: PageCheckBlocking, Message: "Budget is available", Authorized: true}, nil
	})})
	if !errors.Is(err, ErrPageSubmitRejected) || !called || result.Accepted {
		t.Fatalf("governed async check = %+v, %v", result, err)
	}
}
