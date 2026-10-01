package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/forms"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var (
	ErrPageSubmitRejected  = errors.New("workflow page submit: rejected")
	ErrPageCheckDisclosure = errors.New("workflow page submit: unauthorized check disclosure")
)

type PageCheckSeverity string

const (
	PageCheckAdvisory PageCheckSeverity = "ADVISORY"
	PageCheckBlocking PageCheckSeverity = "BLOCKING"
)

type WorkflowPageDefinition struct {
	TenantID  string
	Version   string
	Published bool
	Rules     []workflow.PageRule
	Computed  []forms.ComputedField
}

type WorkflowPageSubmission struct {
	TenantID       string
	ActorID        string
	PageVersion    string
	Values         workflow.PageRuleInput
	ComputedValues map[string]forms.ComputedValue
	ClientSkipped  bool
}

type PageAsyncCheckRequest struct {
	TenantID    string
	ActorID     string
	PageVersion string
	Values      workflow.PageRuleInput
}

type PageAsyncCheckOutcome struct {
	Code          string
	Field         string
	Severity      PageCheckSeverity
	Message       string
	Authorized    bool
	DisclosedData string
}

type PageAsyncCheck interface {
	Check(context.Context, PageAsyncCheckRequest) (PageAsyncCheckOutcome, error)
}

type PageAsyncCheckFunc func(context.Context, PageAsyncCheckRequest) (PageAsyncCheckOutcome, error)

func (f PageAsyncCheckFunc) Check(ctx context.Context, req PageAsyncCheckRequest) (PageAsyncCheckOutcome, error) {
	return f(ctx, req)
}

type PageSubmitResult struct {
	Accepted   bool
	Findings   []workflow.PageRuleFinding
	Advisories []PageAsyncCheckOutcome
	Computed   map[string]forms.ComputedValue
}

// EnforceWorkflowPageSubmit is the server boundary. It ignores client rule
// findings and client computed values, recompiles the published definition,
// recomputes every read-only value, then performs governed async checks.
func EnforceWorkflowPageSubmit(ctx context.Context, definition WorkflowPageDefinition, submission WorkflowPageSubmission, checks []PageAsyncCheck) (PageSubmitResult, error) {
	if !definition.Published || strings.TrimSpace(definition.TenantID) == "" || definition.TenantID != submission.TenantID || definition.Version == "" || definition.Version != submission.PageVersion || submission.ActorID == "" {
		return PageSubmitResult{}, fmt.Errorf("%w: published page identity is invalid", ErrPageSubmitRejected)
	}
	result := PageSubmitResult{Accepted: true, Computed: make(map[string]forms.ComputedValue, len(definition.Computed))}
	for _, field := range definition.Computed {
		value, err := field.Recompute(submission.Values)
		if err != nil {
			return PageSubmitResult{}, fmt.Errorf("%w: recompute %s: %v", ErrPageSubmitRejected, field.Path, err)
		}
		expected := forms.ComputedValue{Path: field.Path, Type: field.Type, Value: value}
		result.Computed[field.Path] = expected
		if supplied, ok := submission.ComputedValues[field.Path]; !ok || forms.VerifyComputedValue(expected, supplied) != nil {
			result.Accepted = false
			result.Findings = append(result.Findings, workflow.PageRuleFinding{Code: "COMPUTED_VALUE_MISMATCH", Field: field.Path, Message: "This value was recalculated on the server."})
		}
	}
	for _, rule := range definition.Rules {
		compiled, err := rule.Compile()
		if err != nil {
			return PageSubmitResult{}, fmt.Errorf("%w: rule %s: %v", ErrPageSubmitRejected, rule.Code, err)
		}
		check, err := compiled.Evaluate(submission.Values)
		if err != nil {
			return PageSubmitResult{}, fmt.Errorf("%w: rule %s: %v", ErrPageSubmitRejected, rule.Code, err)
		}
		if !check.Valid {
			result.Accepted = false
			result.Findings = append(result.Findings, check.Findings...)
		}
	}
	for _, check := range checks {
		if check == nil {
			return PageSubmitResult{}, fmt.Errorf("%w: nil asynchronous check", ErrPageSubmitRejected)
		}
		outcome, err := check.Check(ctx, PageAsyncCheckRequest{TenantID: submission.TenantID, ActorID: submission.ActorID, PageVersion: submission.PageVersion, Values: submission.Values})
		if err != nil {
			return PageSubmitResult{}, fmt.Errorf("%w: asynchronous check: %v", ErrPageSubmitRejected, err)
		}
		if !outcome.Authorized && (outcome.DisclosedData != "" || strings.Contains(strings.ToLower(outcome.Message), "salary") || strings.Contains(strings.ToLower(outcome.Message), "budget")) {
			return PageSubmitResult{}, fmt.Errorf("%w: %v", ErrPageSubmitRejected, ErrPageCheckDisclosure)
		}
		if outcome.Code == "" || (outcome.Severity != PageCheckAdvisory && outcome.Severity != PageCheckBlocking) {
			return PageSubmitResult{}, fmt.Errorf("%w: asynchronous check returned an invalid coded outcome", ErrPageSubmitRejected)
		}
		if outcome.Severity == PageCheckBlocking {
			result.Accepted = false
		} else {
			result.Advisories = append(result.Advisories, outcome)
		}
	}
	if !result.Accepted {
		return result, ErrPageSubmitRejected
	}
	return result, nil
}
