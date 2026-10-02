package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type agentDocForwardingInner struct {
	basic, withDocuments, withResult int
}

func (i *agentDocForwardingInner) ClientForRequest(context.Context) productui.PersonaAdminClient {
	return nil
}

func (i *agentDocForwardingInner) ExecutePersonaAdminCommand(context.Context, productui.PersonaAdminCommandRequest) error {
	i.basic++
	return nil
}

func (i *agentDocForwardingInner) ExecutePersonaAdminCommandWithDocumentReferences(context.Context, productui.PersonaAdminCommandRequest, []agentdocref.Reference) error {
	i.withDocuments++
	return nil
}

func (i *agentDocForwardingInner) ExecutePersonaAdminCommandWithResult(context.Context, productui.PersonaAdminCommandRequest) (productui.PersonaAdminEvaluationResult, error) {
	i.withResult++
	return productui.PersonaAdminEvaluationResult{}, nil
}

// The served local-dev cell refused every command that carried document
// references, and Run evaluation, because the decorator around the command
// surface did not expose the two extension methods the transport looks for.
func TestLocalDevPersonaAdminDecoratorForwardsCommandExtensions(t *testing.T) {
	inner := &agentDocForwardingInner{}
	decorated := withLocalDevPersonaBootstrapAvailability(inner, true)
	documents, ok := decorated.(interface {
		ExecutePersonaAdminCommandWithDocumentReferences(context.Context, productui.PersonaAdminCommandRequest, []agentdocref.Reference) error
	})
	if !ok {
		t.Fatal("decorated surface does not accept commands with document references")
	}
	if err := documents.ExecutePersonaAdminCommandWithDocumentReferences(context.Background(), productui.PersonaAdminCommandRequest{}, []agentdocref.Reference{}); err != nil || inner.withDocuments != 1 {
		t.Fatalf("document command not forwarded: err=%v calls=%d", err, inner.withDocuments)
	}
	results, ok := decorated.(interface {
		ExecutePersonaAdminCommandWithResult(context.Context, productui.PersonaAdminCommandRequest) (productui.PersonaAdminEvaluationResult, error)
	})
	if !ok {
		t.Fatal("decorated surface does not accept commands that answer with a result")
	}
	if _, err := results.ExecutePersonaAdminCommandWithResult(context.Background(), productui.PersonaAdminCommandRequest{}); err != nil || inner.withResult != 1 {
		t.Fatalf("result command not forwarded: err=%v calls=%d", err, inner.withResult)
	}
	if inner.basic != 0 {
		t.Fatalf("an extension was forwarded as a basic command: %d", inner.basic)
	}
}
