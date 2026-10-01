package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestPersonaRunExecutor_DigestsNormalizedOutcomes(t *testing.T) {
	result := agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Text: "A governed answer", Finish: agentmodel.FinishComplete}
	first, err := personaRunResultDigest(result)
	if err != nil || len(first) != 71 || first[:7] != "sha256:" {
		t.Fatalf("result digest=%q err=%v, want sha256 digest", first, err)
	}
	second, err := personaRunResultDigest(result)
	if err != nil || second != first {
		t.Fatalf("same result digest=%q err=%v, want %q", second, err, first)
	}
	if _, err := personaRunResultDigest(agentmodel.ModelResult{}); !errors.Is(err, ErrPersonaRunModelFailure) {
		t.Fatalf("empty result error=%v, want typed model failure", err)
	}
	receipt := PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: "ephemeral-1"}
	got, err := personaRunDeliveryDigest(receipt)
	if err != nil || len(got) != 71 {
		t.Fatalf("delivery digest=%q err=%v, want sha256 digest", got, err)
	}
}

func TestPersonaRunExecutor_FailureIsTypedWithoutLeakingCause(t *testing.T) {
	failure := &PersonaRunFailure{Code: "MODEL_UNAVAILABLE", Retryable: true, kind: ErrPersonaRunModelFailure}
	if !errors.Is(failure, ErrPersonaRunModelFailure) || !failure.Retryable || failure.Code != "MODEL_UNAVAILABLE" {
		t.Fatalf("typed failure=%+v, want retryable model-unavailable classification", failure)
	}
	if strings.Contains(failure.Error(), "provider-secret") {
		t.Fatalf("failure exposed sensitive cause: %v", failure)
	}
}

func TestPersonaRunExecutor_TerminalFailuresAreNotRetryable(t *testing.T) {
	for _, code := range []string{"CONTEXT_UNAVAILABLE", "MODEL_UNAVAILABLE", "OUTPUT_REJECTED", "DELIVERY_FAILED"} {
		t.Run(code, func(t *testing.T) {
			failure := personaRunStoredFailure(runstate.Run{State: runstate.StateFailed, TerminalCode: code, Retryable: false})
			var typed *PersonaRunFailure
			if !errors.As(failure, &typed) || typed.Retryable {
				t.Fatalf("stored terminal failure=%+v, want typed non-retryable outcome", failure)
			}
		})
	}
}

func TestPersonaRunExecutor_ReplyReceiptMustIdentifyOneDelivery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		receipt PersonaReplyDeliveryReceipt
		want    bool
	}{
		{name: "public post", receipt: PersonaReplyDeliveryReceipt{Public: true, PublicPostID: "post-1"}, want: true},
		{name: "private ephemeral", receipt: PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: "ephemeral-1"}, want: true},
		{name: "empty", receipt: PersonaReplyDeliveryReceipt{}},
		{name: "both routes", receipt: PersonaReplyDeliveryReceipt{Public: true, PublicPostID: "post-1", Private: true, EphemeralPostID: "ephemeral-1"}},
		{name: "missing durable identity", receipt: PersonaReplyDeliveryReceipt{Private: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validPersonaRunReplyReceipt(tc.receipt); got != tc.want {
				t.Fatalf("validPersonaRunReplyReceipt(%+v)=%v, want %v", tc.receipt, got, tc.want)
			}
		})
	}
}

func TestPersonaRunExecutor_ChatPrincipalRequiresMatchingHumanContext(t *testing.T) {
	now := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "persona-run-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	got, ok := personaRunChatPrincipal(ctx, "tenant-a", "alice")
	if !ok || got.TenantID != "tenant-a" || got.SubjectID != "alice" {
		t.Fatalf("chat principal=%+v ok=%v, want authenticated invoker", got, ok)
	}
	for _, tc := range []struct{ tenant, subject string }{{"tenant-b", "alice"}, {"tenant-a", "mallory"}} {
		if _, ok := personaRunChatPrincipal(ctx, tc.tenant, tc.subject); ok {
			t.Fatalf("accepted mismatched principal tenant=%q subject=%q", tc.tenant, tc.subject)
		}
	}
	if _, ok := personaRunChatPrincipal(context.Background(), "tenant-a", "alice"); ok {
		t.Fatal("accepted missing authenticated principal")
	}
}

func TestPersonaRunExecutor_RequiresCompleteProductionComposition(t *testing.T) {
	if _, err := NewPersonaRunStarter(PersonaRunStarterConfig{}); !errors.Is(err, ErrPersonaRunExecutorUnavailable) {
		t.Fatalf("empty production composition error=%v, want unavailable", err)
	}
	var executor *personaAdmittedRunExecutor
	if _, err := executor.Start(context.Background(), agentrun.Record{}); !errors.Is(err, ErrPersonaRunExecutorUnavailable) {
		t.Fatalf("nil executor error=%v, want unavailable", err)
	}
}
