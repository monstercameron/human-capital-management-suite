package application

import (
	"context"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_AGENTUX_078: the served chat writer panics on a conversation read,
// and one failure code cannot be stored. A served writer with no reader behind
// it leaves a post that names no agent alone through the real post-commit
// question path, never dereferencing nil; and the failure a model-less server
// writes for a question classifies to the storable code whose run-row card is
// one plain sentence with no retry.
func TestTodo_AGENTUX_078(t *testing.T) {
	// The served assembly's writer with no reader behind it: the question path
	// the post-commit goroutine runs finds no agent instead of dereferencing
	// the nil service the writer embeds.
	invocation := &personaChatInvocation{chat: servedPortChatWriter{}}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "agentux078-primary", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "test-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	post := chatcore.Post{ID: "post-1", TenantID: "tenant-a", ConversationID: "dm-1", Body: "hello"}
	if references := invocation.askedAgentReferences(context.Background(), principal, post); references != nil {
		t.Fatalf("a post naming no agent through a reader-absent writer asked %+v", references)
	}

	// The failure a server with no model writes for a question classifies to
	// the storable code, never offered a retry, and the run row renders it as
	// one plain sentence that names no code and keeps no placeholder.
	code, retryable := personaPostFailureClassification(errPersonaModelNotConfigured)
	if code != "SERVER_HAS_NO_MODEL" || retryable {
		t.Fatalf("no-model failure classified as %q retry=%v", code, retryable)
	}
	card := chatcore.AgentAnswerFailureFor("en-US", "Assistant", code)
	if card.Class != "no_model" || card.Retryable || card.Sentence == "" ||
		strings.Contains(card.Sentence, "SERVER_HAS_NO_MODEL") || strings.Contains(card.Sentence, "{") {
		t.Fatalf("no-model run row reads %+v", card)
	}
}
