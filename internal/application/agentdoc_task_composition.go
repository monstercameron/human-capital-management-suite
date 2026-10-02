package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// bindAgentTaskDocumentRuntime creates the one resolver shared by admission
// and task-model request construction. A workspace without a document store
// deliberately returns nil; reference-free task starts remain available.
func bindAgentTaskDocumentRuntime(runtime *agentRuntime, store *documenthubstore.Store) (agentdocref.Resolver, error) {
	if store == nil {
		return nil, nil
	}
	resolver, err := NewAgentDocumentResolver(store)
	if err != nil {
		return nil, err
	}
	if runtime != nil && runtime.Starter != nil {
		runtime.Starter.documents = resolver
	}
	return resolver, nil
}
