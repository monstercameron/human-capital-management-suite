package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// CHATBUG-049: a reply that is only a document's title says nothing. When the run
// did search documents, the model is asked once more, inside the same run, to
// answer from what the documents say; if that reply still says nothing, the
// server's own sentence (personaComposedListReply) is delivered as before.

// personaRegenerationInstruction is the one instruction the second call adds. It
// carries no data of the person's: it names the problem and the way out.
const personaRegenerationInstruction = "Your previous reply only named a document and did not answer. " +
	"Answer the question again in at least one full sentence, using what the documents you read say. " +
	"If it asks for a list, list the items you found, each with a few words about it, and say how many you could read."

// personaRegenerationSuffix names the second call's model step, as ":retry" names a
// transient retry's: the call is admitted, budgeted and recorded as its own step.
const personaRegenerationSuffix = ":regen"

// regenerateTitleOnly asks the model once more for the reply that was only a
// title. ok is true when the new reply says something, and result is it; the
// run is the current state of the run either way. err is set only when the run
// was ended (a requested action outside the persona's scope) or its state could
// not be kept; the caller returns it. A failed or empty second call is not an
// error: the server's sentence stands in.
func (e *personaAdmittedRunExecutor) regenerateTitleOnly(ctx context.Context, admission agentrun.Record, run runstate.Run, work PersonaRunModelWork, attempt uint32, searched []personaQualitySearchedDocument) (result agentmodel.ModelResult, current runstate.Run, ok bool, err error) {
	if len(searched) == 0 {
		return agentmodel.ModelResult{}, run, false, nil
	}
	request := personaRegenerationRequest(work.Request)
	// The second call answers; it may not search again. The instruction is the
	// server's own text, which the outbound verifier knows (AGENTUX-076).
	request.Model.Tools = nil
	request.StepID += personaRegenerationSuffix
	request.Route.TraceID, request.Model.TraceID = request.StepID, request.StepID
	if appendPersonaServerInstruction(&request, personaRegenerationInstruction, "title-only") != nil {
		return agentmodel.ModelResult{}, run, false, nil
	}
	call, next, callErr := e.executeQualityModel(ctx, run, request, admission)
	if next.ID != "" {
		run = next
	}
	reply := call.Result
	if callErr != nil || reply.Failure != nil || reply.Refusal != nil || reply.Finish != agentmodel.FinishComplete ||
		strings.TrimSpace(reply.Text) == "" || len(reply.ToolProposals) != 0 {
		return agentmodel.ModelResult{}, run, false, nil
	}
	// The second reply is held to the same limits on what it may ask to do as the first.
	run, err = e.checkRequestedActions(ctx, admission, run, PersonaRunModelWork{Request: request}, reply, attempt)
	if err != nil {
		return agentmodel.ModelResult{}, run, false, err
	}
	if !personaReplyHasStatement(reply.Text, personaReplyStatementTitles(searched)...) {
		return agentmodel.ModelResult{}, run, false, nil
	}
	digest, digestErr := personaRunResultDigest(reply)
	if digestErr != nil {
		return agentmodel.ModelResult{}, run, false, nil
	}
	// The sealed answer is bound to the last model result the run recorded, so the
	// regenerated reply is recorded as it was the first.
	run, err = e.qualityCheckpoint(ctx, run.ID, e.workerID, run.Fence, run.Version, runstate.PhaseModelCall, attempt, request.StepID, digest, e.now().UTC())
	if err != nil {
		return agentmodel.ModelResult{}, run, false, fmt.Errorf("%w: record regenerated result: %v", ErrPersonaRunExecutorUnavailable, err)
	}
	return reply, run, true, nil
}

// personaRegenerationRequest copies what appendPersonaRunRegeneration changes, so
// the request the first answer was made from stays as it was.
func personaRegenerationRequest(request AgentModelExecutorRequest) AgentModelExecutorRequest {
	request.Model.Messages = append([]agentmodel.ModelMessage(nil), request.Model.Messages...)
	request.Outbound.Fields = append([]agentegress.Field(nil), request.Outbound.Fields...)
	request.Outbound.DeclaredFields = append([]string(nil), request.Outbound.DeclaredFields...)
	sources := make(map[string]string, len(request.FieldSources)+1)
	for name, source := range request.FieldSources {
		sources[name] = source
	}
	request.FieldSources = sources
	return request
}

// appendPersonaRunRegeneration adds the instruction as one more message from the
// side of the conversation the question came from, on the same step binding
// scheme (a step of its own, named for the one it follows), and checks the result
// as the continuation does.
func appendPersonaRunRegeneration(request *AgentModelExecutorRequest, instruction string) error {
	if request == nil || strings.TrimSpace(instruction) == "" {
		return ErrPersonaRunOutputRejected
	}
	model := &request.Model
	asked := previousInvokerFieldName(model.Messages)
	var original agentegress.Field
	found := false
	for _, field := range request.Outbound.Fields {
		if field.Name == asked {
			original, found = field, true
			break
		}
	}
	if asked == "" || !found || request.FieldSources[asked] == "" {
		return ErrPersonaRunOutputRejected
	}
	name := fmt.Sprintf("model.message.%d", len(model.Messages))
	model.Messages = append(model.Messages, agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: instruction})
	request.Outbound.Fields = append(request.Outbound.Fields, agentegress.Field{
		Name: name, Value: instruction, Class: original.Class,
		Taint:      append([]string(nil), original.Taint...),
		Provenance: append(append([]string(nil), original.Provenance...), "server-instruction:title-only"),
	})
	request.Outbound.DeclaredFields = append(request.Outbound.DeclaredFields, name)
	request.FieldSources[name] = request.FieldSources[asked]
	request.StepID += personaRegenerationSuffix
	request.Route.TraceID, request.Model.TraceID = request.StepID, request.StepID
	if err := agentmodel.ValidateModelRequest(*model); err != nil {
		return ErrPersonaRunOutputRejected
	}
	return validateExecutorRequest(*request)
}
