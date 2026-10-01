package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

func bindPersonaRunModelStep(request *AgentModelExecutorRequest, runID string, ordinal uint32) error {
	if request == nil || runID == "" || ordinal == 0 || request.Task.TaskID != runID {
		return ErrAgentModelExecutorBinding
	}
	step := personaRunModelStepID(runID, ordinal)
	request.StepID = step
	request.Route.TraceID = step
	request.Model.TraceID = step
	return nil
}

func personaRunModelStepID(runID string, ordinal uint32) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("persona-model-step\x00%s\x00%d", runID, ordinal)))
	return "persona-model-" + hex.EncodeToString(sum[:16])
}

func appendPersonaRunToolContinuation(request *AgentModelExecutorRequest, proposal agentmodel.ToolProposal, result []byte) error {
	if request == nil || request.ToolResultClass == "" || strings.TrimSpace(proposal.ID) == "" || strings.TrimSpace(proposal.Name) == "" || !personaRunJSONObject(proposal.Arguments) || len(result) == 0 {
		return ErrPersonaRunOutputRejected
	}
	model := &request.Model
	model.Messages = append(model.Messages,
		agentmodel.ModelMessage{Role: agentmodel.RoleAssistant, ToolCallID: proposal.ID, ToolName: proposal.Name, ToolArguments: append([]byte(nil), proposal.Arguments...)},
		agentmodel.ModelMessage{Role: agentmodel.RoleTool, ToolCallID: proposal.ID, Content: string(result)},
	)
	fieldsByName := make(map[string]agentegress.Field, len(request.Outbound.Fields))
	for _, field := range request.Outbound.Fields {
		fieldsByName[field.Name] = field
	}
	newFields := make([]agentegress.Field, 0, len(model.Messages)+len(model.ContextRefs))
	newSources := make(map[string]string, len(model.Messages)+len(model.ContextRefs))
	declared := make([]string, 0, len(model.Messages)+len(model.ContextRefs))
	for i, message := range model.Messages {
		name := fmt.Sprintf("model.message.%d", i)
		class := request.ToolResultClass
		value := any(message.Content)
		source := "persona-untrusted-tool-result"
		taint := []string{"UNTRUSTED_TOOL_RESULT"}
		provenance := []string{"persona-run:" + model.TraceID, "tool-call:" + proposal.ID}
		switch {
		case message.Role == agentmodel.RoleAssistant && message.ToolCallID != "":
			value = string(message.ToolArguments)
			source = "persona-model-tool-proposal"
			taint = []string{"MODEL_TOOL_PROPOSAL"}
			if original, ok := fieldsByName[previousInvokerFieldName(model.Messages[:i])]; ok {
				class = original.Class
			}
		case message.Role == agentmodel.RoleTool:
			class = request.ToolResultClass
		default:
			original, ok := fieldsByName[name]
			if !ok || request.FieldSources[name] == "" {
				return ErrPersonaRunOutputRejected
			}
			class = original.Class
			taint = append([]string(nil), original.Taint...)
			provenance = append([]string(nil), original.Provenance...)
			source = request.FieldSources[name]
		}
		newFields = append(newFields, agentegress.Field{Name: name, Value: value, Class: class, Taint: taint, Provenance: provenance})
		declared = append(declared, name)
		newSources[name] = source
	}
	for i := range model.ContextRefs {
		name := fmt.Sprintf("model.context.%d", i)
		field, ok := fieldsByName[name]
		if !ok || request.FieldSources[name] == "" {
			return ErrPersonaRunOutputRejected
		}
		newFields = append(newFields, field)
		declared = append(declared, name)
		newSources[name] = request.FieldSources[name]
	}
	request.Outbound.Fields = newFields
	request.Outbound.DeclaredFields = declared
	request.FieldSources = newSources
	if err := agentmodel.ValidateModelRequest(*model); err != nil {
		return ErrPersonaRunOutputRejected
	}
	if err := validateExecutorRequest(*request); err != nil {
		return err
	}
	return nil
}

func personaRunJSONObject(raw []byte) bool {
	var value map[string]json.RawMessage
	return json.Unmarshal(raw, &value) == nil && value != nil
}

func previousInvokerFieldName(messages []agentmodel.ModelMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == agentmodel.RoleUser {
			return fmt.Sprintf("model.message.%d", i)
		}
	}
	return ""
}

func personaRunBytesDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func personaRunToolEffectIdentity(runID, stepID, proposalID string) (string, string) {
	sum := sha256.Sum256([]byte(runID + "\x00" + stepID + "\x00" + proposalID))
	id := hex.EncodeToString(sum[:16])
	return "persona-tool-" + id, "persona-tool-idem-" + id
}
