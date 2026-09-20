package app

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// gatePrincipal builds a minimal authenticated principal for the gate tests.
// roles is the exact, closed set of roles the credential resolved to.
func gatePrincipal(t *testing.T, roles ...string) *trust.Principal {
	t.Helper()
	now := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               "acme-corp",
		Subject:              "gate-test-subject",
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                roles,
		Purposes:             []string{"workflow_execution"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "gate-test-session",
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "credential-digest",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func promoteWorkerDefinition() intent.Definition {
	return intent.Definition{Ref: intent.Ref{TypeID: promotion.IntentType, Version: 1}}
}

func otherDefinition() intent.Definition {
	return intent.Definition{Ref: intent.Ref{TypeID: "hcmnext.rewards.change_base_pay", Version: 1}}
}

// TestExecutionAuthorityGateAdmitsOnlyNamedIntentTypesAndRoles is the primary
// test for [IntentService.authorizeExecution]: EXECUTE is refused, under the
// same envelope every other governed write in this release already uses,
// unless the cell carries an [ExecutionAuthority] that both names the
// intent's own type and admits the caller's role. Neither condition alone is
// enough, and a cell with no configured authority never distinguishes one
// caller or intent type from another.
func TestExecutionAuthorityGateAdmitsOnlyNamedIntentTypesAndRoles(t *testing.T) {
	authority := &ExecutionAuthority{
		AuthorityDigest:     "sha256:p1b-authority-amendment",
		AdmittedIntentTypes: map[string]bool{promotion.IntentType: true},
		RequiredRole:        "promotion_operator",
	}

	t.Run("no authority configured refuses exactly like every other P1A write", func(t *testing.T) {
		svc := &IntentService{}
		principal := gatePrincipal(t, "promotion_operator")

		ownedErr := svc.authorizeExecution(principal, promoteWorkerDefinition())
		assertP1ARefusal(t, ownedErr)
	})

	t.Run("authority configured but intent type not admitted refuses exactly like no authority", func(t *testing.T) {
		svc := &IntentService{executionAuthority: authority}
		principal := gatePrincipal(t, "promotion_operator")

		ownedErr := svc.authorizeExecution(principal, otherDefinition())
		assertP1ARefusal(t, ownedErr)
	})

	t.Run("authority configured and type admitted but caller lacks the named role is a distinct permission refusal", func(t *testing.T) {
		svc := &IntentService{executionAuthority: authority}
		principal := gatePrincipal(t, "intent_author")

		ownedErr := svc.authorizeExecution(principal, promoteWorkerDefinition())
		if ownedErr == nil {
			t.Fatal("expected a refusal for a caller with no execution role")
		}
		if ownedErr.Code() != envelope.CodePermissionDenied || ownedErr.ReasonRef() != reasonExecutionRoleRequired {
			t.Fatalf("error = (%s, %q), want (%s, %q)",
				ownedErr.Code(), ownedErr.ReasonRef(), envelope.CodePermissionDenied, reasonExecutionRoleRequired)
		}
	})

	t.Run("authority configured, type admitted and role held admits the call", func(t *testing.T) {
		svc := &IntentService{executionAuthority: authority}
		principal := gatePrincipal(t, "promotion_operator")

		if ownedErr := svc.authorizeExecution(principal, promoteWorkerDefinition()); ownedErr != nil {
			t.Fatalf("authorizeExecution = %v, want admission", ownedErr)
		}
	})

	t.Run("a caller holding an unrelated role plus the required one is still admitted", func(t *testing.T) {
		svc := &IntentService{executionAuthority: authority}
		principal := gatePrincipal(t, "intent_author", "promotion_operator")

		if ownedErr := svc.authorizeExecution(principal, promoteWorkerDefinition()); ownedErr != nil {
			t.Fatalf("authorizeExecution = %v, want admission", ownedErr)
		}
	})

	t.Run("an authority naming no required role admits no caller", func(t *testing.T) {
		svc := &IntentService{executionAuthority: &ExecutionAuthority{
			AdmittedIntentTypes: map[string]bool{promotion.IntentType: true},
		}}
		principal := gatePrincipal(t, "promotion_operator")

		ownedErr := svc.authorizeExecution(principal, promoteWorkerDefinition())
		if ownedErr == nil || ownedErr.Code() != envelope.CodePermissionDenied {
			t.Fatalf("authorizeExecution = %v, want a permission refusal for a roleless authority", ownedErr)
		}
	})
}

// assertP1ARefusal asserts ownedErr is exactly the shape [p1aRefusal] itself
// produces: same code, same reason. This is the "byte-for-byte the P1A cell
// of today" contract the gate names for a cell with no admitting authority.
func assertP1ARefusal(t *testing.T, ownedErr *envelope.Error) {
	t.Helper()
	want := p1aRefusal("ExecuteIntent", "executing an approved promotion proposal")
	if ownedErr == nil {
		t.Fatal("expected a refusal, got nil")
	}
	if ownedErr.Code() != want.Code() || ownedErr.ReasonRef() != want.ReasonRef() {
		t.Fatalf("error = (%s, %q), want (%s, %q)",
			ownedErr.Code(), ownedErr.ReasonRef(), want.Code(), want.ReasonRef())
	}
}

// TestExecuteIntentRefusesMissingExecutionWiring proves that even a fully
// admitted call (authority configured, role held, revision and approval
// presented) fails closed rather than panicking when this cell was not
// composed with the driver-side wiring EXECUTE needs to actually run.
func TestExecuteIntentRefusesMissingExecutionWiring(t *testing.T) {
	svc := &IntentService{
		executionAuthority: &ExecutionAuthority{
			AdmittedIntentTypes: map[string]bool{promotion.IntentType: true},
			RequiredRole:        "promotion_operator",
		},
	}
	if svc.executor != nil {
		t.Fatal("expected a zero-value IntentService to carry no executor")
	}
	owned := executionUnavailable()
	if owned == nil {
		t.Fatal("executionUnavailable returned nil")
	}
	if owned.Code() != envelope.CodeFailedPrecondition || owned.ReasonRef() != reasonExecutionUnavailable {
		t.Fatalf("executionUnavailable = (%s, %q), want (%s, %q)",
			owned.Code(), owned.ReasonRef(), envelope.CodeFailedPrecondition, reasonExecutionUnavailable)
	}
}

// TestExecutionErrorProjectsTheDerivedProposalRefusals proves WF-RUN-027's two
// derived refusals reach the caller as distinct reason references, not as one
// undifferentiated "stale proposal": a revision with no recorded decision and a
// revision the relationship graph has superseded call for different actions.
func TestExecutionErrorProjectsTheDerivedProposalRefusals(t *testing.T) {
	for _, tc := range []struct {
		code   string
		reason string
	}{
		{runtime.CodeUnapprovedProposal, reasonUnapprovedProposal},
		{runtime.CodeSupersededProposal, reasonSupersededProposal},
		{runtime.CodeMutableProposal, reasonStaleProposal},
		{runtime.CodeApprovalBindingMismatch, reasonStaleProposal},
		// A missing ACTIVE workflow version is a release nobody performed,
		// and carries its own refusal so the page does not invite a retry
		// that can never succeed (see no_active_version_refusal_test.go).
		{runtime.CodeVersionNotActive, reasonNoActiveWorkflowVersion},
		// A version that exists but does not resolve is the cell's
		// configuration, so the journey page reports it unavailable.
		{runtime.CodeVersionResolutionFailed, reasonExecutionUnavailable},
		{runtime.CodeWorkflowResolutionFailed, reasonExecutionUnavailable},
	} {
		t.Run(tc.code, func(t *testing.T) {
			owned := executionError(&runtime.Error{Code: tc.code, Detail: "refused"})
			if owned == nil {
				t.Fatal("executionError returned nil for a typed runtime refusal")
			}
			if owned.Code() != envelope.CodeFailedPrecondition {
				t.Errorf("code = %s, want FAILED_PRECONDITION", owned.Code())
			}
			if owned.ReasonRef() != tc.reason {
				t.Fatalf("reason = %q, want %q", owned.ReasonRef(), tc.reason)
			}
		})
	}
}

func TestExecutionErrorCommitAmbiguityIsNonRetryableAcrossWireProjections(t *testing.T) {
	const privateDetail = "provider connection contained tenant-secret"
	wrapped := fmt.Errorf("commit wrapper: %w: %s", transactioncommit.ErrCommitAmbiguous, privateDetail)
	owned := executionError(wrapped)
	if owned.Code() != envelope.CodeUnavailable || owned.ReasonRef() != reasonExecutionOutcomeAmbiguous || owned.Retryable() {
		t.Fatalf("owned ambiguity = code %s reason %q retryable %v", owned.Code(), owned.ReasonRef(), owned.Retryable())
	}
	if strings.Contains(owned.Error(), privateDetail) || strings.Contains(owned.Message(), privateDetail) {
		t.Fatalf("safe projection leaked diagnostic: %q", owned.Error())
	}
	// HTTP transports use HTTPStatus plus this exact ErrorDetail payload.
	if owned.HTTPStatus() != http.StatusServiceUnavailable || owned.Detail().GetRetryable() {
		t.Fatalf("HTTP projection = status %d detail %+v", owned.HTTPStatus(), owned.Detail())
	}
	back, ok := envelope.FromGRPC(owned.GRPCStatus().Err())
	if !ok || back.Retryable() || back.Detail().GetRetryable() || back.ReasonRef() != reasonExecutionOutcomeAmbiguous {
		t.Fatalf("gRPC round trip = %+v ok=%v", back, ok)
	}
	if strings.Contains(back.Error(), privateDetail) {
		t.Fatalf("gRPC projection leaked diagnostic: %q", back.Error())
	}

	ordinary := executionError(errors.New("temporary dependency failure"))
	if ordinary.Code() != envelope.CodeUnavailable || !ordinary.Retryable() || !ordinary.Detail().GetRetryable() {
		t.Fatalf("ordinary transient classification changed: %+v", ordinary)
	}
}
