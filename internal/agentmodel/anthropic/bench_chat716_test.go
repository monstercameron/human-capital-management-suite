package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

func BenchmarkMakeRequestPlain(b *testing.B) {
	req := requestFixture()
	req.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := makeRequest("claude-test", req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMakeRequest(b *testing.B) {
	req := requestFixture()
	req.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
	req.Messages = append(req.Messages,
		agentmodel.ModelMessage{Role: agentmodel.RoleAssistant, ToolCallID: "call-a", ToolName: "lookup_policy", ToolArguments: json.RawMessage(`{"query":"leave"}`)},
		agentmodel.ModelMessage{Role: agentmodel.RoleTool, ToolCallID: "call-a", Content: `{"hits":[]}`},
		agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: "Thanks, and what about sick leave?"},
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := makeRequest("claude-test", req); err != nil {
			b.Fatal(err)
		}
	}
}
