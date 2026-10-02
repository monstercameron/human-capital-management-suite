package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaInvocationServedPorts = errors.New("application: served persona invocation ports unavailable")

type personaInvocationLogger interface {
	Error(string, ...any)
}

type personaInvocationServedPorts struct {
	chat          personaChatPostWriter
	conversations personaClassifiedHumanConversationReader
	// directory reads conversations and members for the direct-conversation
	// question. The post writer above is deliberately narrower, so it cannot
	// answer those reads itself (AGENTUX-075).
	directory  personaDirectConversationReader
	references personaReferenceResolver
	failures   personaInvocationFailureSink
}

// composePersonaInvocationServedPorts adapts the already-composed chat and
// persona services to the post-commit invocation boundary. The post writer is
// the routed/audited service beneath streamingChatService: SendPost is already
// holding the outer streaming admission lease when the invocation calls it.
func composePersonaInvocationServedPorts(
	served *streamingChatService,
	personas *personaServeWiring,
	logger personaInvocationLogger,
) (personaInvocationServedPorts, error) {
	if served == nil || served.ConversationService == nil || personas == nil || personas.refs == nil || personas.refs.source == nil || logger == nil {
		return personaInvocationServedPorts{}, errPersonaInvocationServedPorts
	}
	if _, recursive := served.ConversationService.(*streamingChatService); recursive {
		return personaInvocationServedPorts{}, fmt.Errorf("%w: streaming chat cannot be its own post writer", errPersonaInvocationServedPorts)
	}
	references, err := newPersonaChatReferenceResolver(personas.refs, personas.now)
	if err != nil {
		return personaInvocationServedPorts{}, fmt.Errorf("%w: canonical persona resolver: %v", errPersonaInvocationServedPorts, err)
	}
	return personaInvocationServedPorts{
		chat:          servedPersonaPostWriter{service: served.ConversationService},
		conversations: served.ConversationService,
		directory:     served.ConversationService,
		references:    references,
		failures:      personaInvocationFailureLogger{logger: logger},
	}, nil
}

// ClassifiedHumanWriter adds source classification to these immutable served
// ports. The original post writer remains unchanged for other consumers.
func (p personaInvocationServedPorts) ClassifiedHumanWriter(classifier PersonaAcceptedHumanPostClassifier) (personaChatPostWriter, error) {
	return NewClassifiedPersonaHumanPostWriter(p.chat, p.conversations, classifier)
}

// servedPersonaPostWriter delegates one authenticated human post to the
// routed/audited chat service. It validates the trusted identity before the
// write; post-commit identity and revision checks remain at the handoff.
type servedPersonaPostWriter struct {
	service chatcore.ConversationService
}

func (w servedPersonaPostWriter) SendPost(ctx context.Context, request chatcore.SendPostRequest) (chatcore.Post, error) {
	if ctx == nil || w.service == nil {
		return chatcore.Post{}, chatcore.ErrPermissionDenied
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal.SubjectKind() != trust.SubjectKindHuman ||
		request.TenantID == "" || request.ConversationID == "" ||
		request.Principal.TenantID != request.TenantID || request.Principal.SubjectID == "" ||
		request.Principal.SubjectID != principal.Subject() || request.TenantID != principal.Tenant().String() {
		return chatcore.Post{}, chatcore.ErrPermissionDenied
	}
	return w.service.SendPost(ctx, request)
}

// personaInvocationFailureLogger is the production best-effort sink for
// failures discovered after a successful chat commit. It emits only the
// authenticated tenant, committed post id, failure type and stable failure
// code to the server logger; the sink never changes the already-committed
// SendPost result or logs possibly sensitive admission text.
type personaInvocationFailureLogger struct {
	logger personaInvocationLogger
}

func (s personaInvocationFailureLogger) RecordPersonaInvocationFailure(ctx context.Context, postID string, err error) {
	if s.logger == nil || err == nil || strings.TrimSpace(postID) == "" {
		return
	}
	attributes := []any{"post_id", postID, "error_type", fmt.Sprintf("%T", err), "error_code", "PERSONA_INVOCATION_POST_COMMIT_FAILED"}
	if ctx != nil {
		if principal, ok := trust.FromContext(ctx); ok {
			attributes = append(attributes, "tenant_id", principal.Tenant().String())
		}
	}
	s.logger.Error("hcmnext.persona_invocation_post_commit_failed", attributes...)
	// The cause may quote admission detail, so it is written only in an
	// explicitly opted-in local diagnostic session and never to the sink.
	if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
		slog.WarnContext(ctx, "hcmnext.persona_invocation_post_commit_cause", "post_id", postID, "cause", err.Error())
	}
	slog.ErrorContext(ctx, "hcmnext.persona_invocation_post_commit_failed", "post_id", postID, "error_type", fmt.Sprintf("%T", err), "error_code", "PERSONA_INVOCATION_POST_COMMIT_FAILED")
}

var _ personaChatPostWriter = servedPersonaPostWriter{}
var _ personaInvocationFailureSink = personaInvocationFailureLogger{}
