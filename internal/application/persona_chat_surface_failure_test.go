package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

type personaSurfaceFailuresFixture struct {
	failures []agentinvocationstore.PostFailure
	writes   int
}

func (s *personaSurfaceFailuresFixture) RecordPostFailure(ctx context.Context, failure agentinvocationstore.PostFailure) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.writes++
	s.failures = append(s.failures, failure)
	return nil
}
func (s *personaSurfaceFailuresFixture) ListPostFailures(context.Context, string, string, string) ([]agentinvocationstore.PostFailure, error) {
	return s.failures, nil
}
func (s *personaSurfaceFailuresFixture) LookupPostFailure(_ context.Context, tenant, invoker, post string) (agentinvocationstore.PostFailure, error) {
	for _, failure := range s.failures {
		if failure.TenantID == tenant && failure.InvokerID == invoker && failure.PostID == post {
			return failure, nil
		}
	}
	return agentinvocationstore.PostFailure{}, agentinvocationstore.ErrNotFound
}

func TestTodo_AGENTP_020_PostFailurePersistsSanitizedOutcomeAfterCancellation(t *testing.T) {
	_, ctx, _, _, _ := personaSurfaceFixture(t)
	store := &personaSurfaceFailuresFixture{}
	sink := &personaDurableInvocationFailureSink{store: store}
	invoker := &personaChatInvocation{failures: sink}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	post := chat.Post{ID: "post", ConversationID: "channel-a", TenantID: "tenant-a", AuthorID: "user-a", Revision: 1, References: []chat.Reference{{Kind: chat.AgentMention, TenantID: "tenant-a", ID: "agent:coach", ConversationID: "channel-a"}}}
	invoker.recordPostFailure(canceled, post, fmt.Errorf("raw provider credential detail: %w", ErrPersonaRunModelFailure))
	if store.writes != 1 || store.failures[0].Code != "MODEL_UNAVAILABLE" || !store.failures[0].Retryable || store.failures[0].ThreadID != "post" {
		t.Fatalf("durable failure=%+v", store)
	}
	post.AuthorID = "forged"
	invoker.recordPostFailure(ctx, post, errors.New("secret"))
	if store.writes != 1 {
		t.Fatal("forged author failure written")
	}
	for _, test := range []struct {
		err   error
		code  string
		retry bool
	}{{agentinvoke.ErrDenied, "ADMISSION_REFUSED", false}, {ErrPersonaRunOutputRejected, "OUTPUT_REJECTED", false}, {ErrPersonaRunDeliveryFailure, "DELIVERY_FAILED", false}, {ErrPersonaRunExecutorUnavailable, "EXECUTION_UNAVAILABLE", true}, {ErrAgentDirectoryUnavailable, "ADMISSION_UNAVAILABLE", true}, {errors.New("password"), "INVOCATION_FAILED", false}} {
		code, retry := personaPostFailureClassification(test.err)
		if code != test.code || retry != test.retry {
			t.Fatalf("classification=%s %v want=%s %v", code, retry, test.code, test.retry)
		}
	}
}

func TestTodo_AGENTP_020_DurableFailureClearsMissingAdmissionSpinnerAndRetries(t *testing.T) {
	s, ctx, room, invocations, execution := personaSurfaceFixture(t)
	execution.err = agentrunstate.ErrNotFound
	store := &personaSurfaceFailuresFixture{failures: []agentinvocationstore.PostFailure{{TenantID: "tenant-a", InvokerID: "user-a", PostID: "post-a", ConversationID: "channel-a", ThreadID: "post-a", Code: "EXECUTION_UNAVAILABLE", Retryable: true}}}
	s.Failures = store
	progress, err := s.Progress(ctx, "channel-a")
	if err != nil || len(progress.Invocations) != 1 || progress.Invocations[0].Status != "FAILED" || !progress.Invocations[0].Retryable {
		t.Fatalf("claimed failure=%+v %v", progress, err)
	}
	invocations.rows = nil
	progress, err = s.Progress(ctx, "channel-a")
	if err != nil || len(progress.Invocations) != 1 || progress.Invocations[0].InvocationID != "post-failure:post-a" {
		t.Fatalf("pre-claim failure=%+v %v", progress, err)
	}
	result, err := s.Retry(ctx, "post-failure:post-a", "unique-key")
	// The question was never admitted, so asking again is its first admission.
	if err != nil || result.PostID != "post-a" || len(room.sends) != 0 || len(room.reattempts) != 1 || room.reattempts[0].attempt != 0 {
		t.Fatalf("retry=%+v %v sends=%v reattempts=%+v", result, err, room.sends, room.reattempts)
	}
	store.failures[0].InvokerID = "other"
	if _, err = s.Progress(ctx, "channel-a"); !errors.Is(err, personachat.ErrUnavailable) {
		t.Fatalf("foreign failure leaked=%v", err)
	}
}
