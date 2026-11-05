package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// A user follow-up after a tool result must join the tool result as a text
// block in the same message: roles keep alternating and neither the result
// nor the follow-up may be dropped.
func TestMakeRequest_PreservesToolResultBeforeUserFollowUp(t *testing.T) {
	req := requestFixture()
	req.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
	req.Messages = append(req.Messages,
		agentmodel.ModelMessage{Role: agentmodel.RoleAssistant, ToolCallID: "call-a", ToolName: "lookup_policy", ToolArguments: json.RawMessage(`{"query":"leave"}`)},
		agentmodel.ModelMessage{Role: agentmodel.RoleTool, ToolCallID: "call-a", Content: `{"hits":[]}`},
		agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: "Thanks, and what about sick leave?"},
	)
	if err := agentmodel.ValidateModelRequest(req); err != nil {
		t.Fatalf("ValidateModelRequest() error = %v", err)
	}
	mapped, err := makeRequest("claude-test", req)
	if err != nil {
		t.Fatalf("makeRequest() error = %v", err)
	}
	if len(mapped.Messages) != 3 || mapped.Messages[0].Role != "user" || mapped.Messages[1].Role != "assistant" || mapped.Messages[2].Role != "user" {
		t.Fatalf("tool follow-up roles = %+v", mapped.Messages)
	}
	var blocks []contentBlock
	if err := json.Unmarshal(mapped.Messages[2].Content, &blocks); err != nil {
		t.Fatalf("user follow-up is not content blocks: %v", err)
	}
	var sawResult, sawText bool
	for _, block := range blocks {
		switch block.Type {
		case "tool_result":
			sawResult = sawResult || (block.ToolUseID == "call-a" && block.Content == `{"hits":[]}`)
		case "text":
			sawText = sawText || (block.Text == "Thanks, and what about sick leave?")
		}
	}
	if !sawResult || !sawText {
		t.Fatalf("tool result or follow-up text lost: %+v", blocks)
	}
}

// Assistant text immediately before a tool call is a valid transcript; the
// tool call joins the text as blocks in one assistant message rather than
// breaking the provider's role alternation.
func TestMakeRequest_MergesToolCallAfterAssistantText(t *testing.T) {
	req := requestFixture()
	req.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
	req.Messages = []agentmodel.ModelMessage{
		{Role: agentmodel.RoleUser, Content: "What is the leave policy?"},
		{Role: agentmodel.RoleAssistant, Content: "Let me look that up."},
		{Role: agentmodel.RoleAssistant, ToolCallID: "call-a", ToolName: "lookup_policy", ToolArguments: json.RawMessage(`{"query":"leave"}`)},
		{Role: agentmodel.RoleTool, ToolCallID: "call-a", Content: `{"hits":[]}`},
	}
	if err := agentmodel.ValidateModelRequest(req); err != nil {
		t.Fatalf("ValidateModelRequest() error = %v", err)
	}
	mapped, err := makeRequest("claude-test", req)
	if err != nil {
		t.Fatalf("makeRequest() error = %v", err)
	}
	if len(mapped.Messages) != 3 || mapped.Messages[0].Role != "user" || mapped.Messages[1].Role != "assistant" || mapped.Messages[2].Role != "user" {
		t.Fatalf("assistant tool-call roles = %+v", mapped.Messages)
	}
	var blocks []contentBlock
	if err := json.Unmarshal(mapped.Messages[1].Content, &blocks); err != nil {
		t.Fatalf("assistant message is not content blocks: %v", err)
	}
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[0].Text != "Let me look that up." ||
		blocks[1].Type != "tool_use" || blocks[1].ID != "call-a" || blocks[1].Name != "lookup_policy" {
		t.Fatalf("assistant text or tool call lost: %+v", blocks)
	}
}
