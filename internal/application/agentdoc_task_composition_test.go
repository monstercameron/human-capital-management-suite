package application

import "testing"

func TestTodo_AGENTDOC_004_Composition(t *testing.T) {
	runtime := &agentRuntime{Starter: &agentStarter{}}
	resolver, err := bindAgentTaskDocumentRuntime(runtime, documentServiceFixture(t).store)
	if err != nil || resolver == nil || runtime.Starter.documents != resolver {
		t.Fatalf("shared resolver = %v starter=%v err=%v", resolver, runtime.Starter.documents, err)
	}
	withoutDocuments, err := bindAgentTaskDocumentRuntime(runtime, nil)
	if err != nil || withoutDocuments != nil {
		t.Fatalf("absent documents = %v, %v", withoutDocuments, err)
	}
}
