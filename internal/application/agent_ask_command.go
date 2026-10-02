package application

import (
	"context"
	"fmt"
	"strings"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The "/ask" command (AGENT-027) is a second way to say what a typed mention
// says. The server turns "/ask @Agent question" into the message the mention
// would have sent and then treats it as that message: it is committed first,
// and the run it starts goes through the same admission, the same limits and
// the same worker. Nothing about the command starts a run on its own.

// Reasons an ask command is refused before any message is written.
const (
	PersonaAskNeedsAgent    = "ask_needs_agent"    // no agent named, and the conversation is not one with an agent
	PersonaAskNeedsQuestion = "ask_needs_question" // nothing was asked
	PersonaAskOneAgent      = "ask_one_agent"      // more than one agent named
	PersonaAskNotOwnWords   = "ask_not_own_words"  // forwarded or attributed text is not a command
)

// PersonaAskCommandError says why an ask command was not sent. The command
// line is never posted as text when it is refused, so the person can fix it.
type PersonaAskCommandError struct{ Reason string }

func (e *PersonaAskCommandError) Error() string {
	return fmt.Sprintf("application: ask command refused: %s", e.Reason)
}

// Unwrap makes the refusal an invalid argument to Chat's transport.
func (e *PersonaAskCommandError) Unwrap() error { return chatcore.ErrInvalidArgument }

// personaAskCommands is the registry the server reads the command from: Chat's
// own commands with "/ask" registered by the agent platform.
func personaAskCommands() chatcore.Chatcmd001Registry {
	return chatcore.Chatcmd001WithAsk(chatcore.Chatcmd001Defaults())
}

// askCommand rewrites an ask command into the message a typed mention sends.
// Any other message, including any other command, is returned unchanged. The
// question is free text: it is not read by the argument grammar, so a question
// with a quote or an equals sign in it is still a question.
func (c *personaChatInvocation) askCommand(ctx context.Context, request chatcore.SendPostRequest) (chatcore.SendPostRequest, error) {
	line := chatcore.Chatcmd001ParseLine(request.Body)
	if !line.Command || line.Name != chatcore.Chatcmd001AskName {
		return request, nil
	}
	if _, registered := personaAskCommands().Lookup(line.Name); !registered {
		return request, nil
	}
	if request.SourceAttribution != nil {
		return request, &PersonaAskCommandError{Reason: PersonaAskNotOwnWords}
	}
	question := strings.TrimSpace(line.Raw)
	var named []chatcore.Reference
	for _, reference := range request.References {
		if reference.Kind == chatcore.AgentMention {
			named = append(named, reference)
		}
	}
	switch {
	case len(named) > 1:
		return request, &PersonaAskCommandError{Reason: PersonaAskOneAgent}
	case len(named) == 1:
		// What is left after the agent's name is the question. The name is
		// kept in the message, as a typed mention keeps it.
		asked := question
		if display := strings.TrimSpace(named[0].Display); display != "" {
			asked = strings.TrimSpace(strings.Replace(asked, "@"+display, "", 1))
		}
		if asked == "" || strings.TrimLeft(asked, "@") == "" {
			return request, &PersonaAskCommandError{Reason: PersonaAskNeedsQuestion}
		}
	default:
		// No agent is named. That is only an ask in the person's own direct
		// conversation with one agent, which is read before anything is sent.
		principal, ok := trust.FromContext(ctx)
		if !ok || principal == nil {
			return request, &PersonaAskCommandError{Reason: PersonaAskNeedsAgent}
		}
		reference, direct := c.directAgentReference(ctx, principal, chatcore.Post{TenantID: request.TenantID, ConversationID: request.ConversationID})
		if !direct || !validPersonaReferenceID(reference.ID) {
			return request, &PersonaAskCommandError{Reason: PersonaAskNeedsAgent}
		}
		if question == "" {
			return request, &PersonaAskCommandError{Reason: PersonaAskNeedsQuestion}
		}
	}
	request.Body = question
	return request, nil
}
