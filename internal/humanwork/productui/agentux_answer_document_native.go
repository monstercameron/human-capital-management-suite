//go:build !(js && wasm)

package productui

func agentAnswerDocumentRoute() AgentAnswerDocumentCitation { return AgentAnswerDocumentCitation{} }
func agentAnswerRevealSection(string) func()                { return nil }
