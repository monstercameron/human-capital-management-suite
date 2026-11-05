package retrieval

import (
	"context"
	"testing"
)

// recordingChatOwner and recordingDocumentOwner are independent source-owner
// adapters: each records the admitted request it received so the integration
// leg can prove both owners were reached with the same admitted scope.
type recordingChatOwner struct {
	records  []Record
	requests []OwnerRequest
}

func (o *recordingChatOwner) RetrieveChat(_ context.Context, request OwnerRequest) ([]Record, error) {
	o.requests = append(o.requests, request)
	return cloneRecords(o.records), nil
}

type recordingDocumentOwner struct {
	records  []Record
	requests []OwnerRequest
}

func (o *recordingDocumentOwner) RetrieveDocuments(_ context.Context, request OwnerRequest) ([]Record, error) {
	o.requests = append(o.requests, request)
	return cloneRecords(o.records), nil
}

// registryGrants is a stateful current-grant checker: Revoke flips a source
// between Retrieve and Finalize the way a real grant store would.
type registryGrants struct {
	revoked map[string]bool
	calls   int
}

func (g *registryGrants) CheckCurrent(_ context.Context, _ Access, envelope Envelope) error {
	g.calls++
	if g.revoked[envelope.RevocationToken] {
		return ErrGrantRevoked
	}
	return nil
}

func (g *registryGrants) Revoke(token string) {
	if g.revoked == nil {
		g.revoked = map[string]bool{}
	}
	g.revoked[token] = true
}

// TestTodo_AGENT_018_Integration runs the full retrieval lifecycle across both
// source owners and a stateful grant registry: an empty source list reaches
// both owners with the admitted scope, a document from another conversation
// discloses while chat stays conversation-scoped, a revocation between
// Retrieve and Finalize fails only its own source explicitly, and rotated
// grant evidence no longer matches the batch.
func TestTodo_AGENT_018_Integration(t *testing.T) {
	ctx := context.Background()
	access := retrievalAccess()
	chatRecord := retrievalRecord(Chat)
	docRecord := retrievalRecord(Document)
	// Documents are scoped by their own grants, not by the conversation the
	// question was asked in.
	docRecord.Envelope.ConversationID = "archive-channel"
	docRecord.Envelope.ThreadID = ""
	docRecord.Envelope.SourceID = "doc-9"
	docRecord.Envelope.Version = "v9"
	docRecord.Envelope.Citation = Citation{SourceID: "doc-9", Version: "v9", Target: "source://doc-9/v9"}
	docRecord.Envelope.RevocationToken = "doc-revoke-9"
	chat := &recordingChatOwner{records: []Record{chatRecord}}
	docs := &recordingDocumentOwner{records: []Record{docRecord}}
	grants := &registryGrants{}
	service := retrievalService(t, chat, docs, grants)

	query := retrievalQuery()
	query.Sources = nil
	query.Limit = 10
	batch, err := service.Retrieve(ctx, access, query)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(chat.requests) != 1 || len(docs.requests) != 1 {
		t.Fatalf("an empty source list reached chat %d and documents %d times, want both once", len(chat.requests), len(docs.requests))
	}
	for name, request := range map[string]OwnerRequest{"chat": chat.requests[0], "documents": docs.requests[0]} {
		if request.Access.PrincipalID != access.PrincipalID || request.Access.Purpose != access.Purpose || request.Access.ConversationID != access.ConversationID || len(request.Access.GrantIDs) != 1 || request.Query.ThreadID != "thread-a" {
			t.Fatalf("%s owner did not receive the admitted scope: %+v", name, request)
		}
	}
	// The admitted scope handed to the owners is detached: later caller
	// mutation must not rewrite what the owners were asked.
	access.GrantIDs[0] = "mutated-after-retrieve"
	if chat.requests[0].Access.GrantIDs[0] != "grant-a" || docs.requests[0].Access.GrantIDs[0] != "grant-a" {
		t.Fatalf("owner requests alias the caller access: %+v %+v", chat.requests[0].Access, docs.requests[0].Access)
	}
	access.GrantIDs[0] = "grant-a"
	if batch.Count() != 2 {
		t.Fatalf("batch holds %d records, want one per source", batch.Count())
	}
	want := []SourceStatus{{Kind: Chat, State: Complete}, {Kind: Document, State: Complete}}
	if got := batch.Statuses(); !equalStatuses(got, want) {
		t.Fatalf("statuses = %+v, want %+v", got, want)
	}

	// A revocation that lands between retrieval and disclosure fails only its
	// own source, explicitly; the other source still discloses its content.
	grants.Revoke("doc-revoke-9")
	final, err := service.Finalize(ctx, access, batch)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	items := final.Items()
	if len(items) != 1 || items[0].Envelope.Kind != Chat || items[0].Content == "" {
		t.Fatalf("final items = %+v, want the one still-authorized chat record with content", items)
	}
	want = []SourceStatus{{Kind: Chat, State: Complete}, {Kind: Document, State: Failed, Code: FailureRevoked}}
	if got := final.Statuses(); !equalStatuses(got, want) {
		t.Fatalf("final statuses = %+v, want %+v", got, want)
	}

	// Rotated grant evidence no longer matches the retrieved batch.
	rotated := access
	rotated.RevocationTokens = []string{"revoke-b"}
	if _, err := service.Finalize(ctx, rotated, batch); err == nil {
		t.Fatal("a batch finalized under rotated revocation evidence")
	}

	// Status order follows the requested source order, not a fixed one.
	reordered := retrievalQuery()
	reordered.Sources = []Kind{Document, Chat}
	fresh, err := retrievalService(t, chat, docs, &registryGrants{}).Retrieve(ctx, access, reordered)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	want = []SourceStatus{{Kind: Document, State: Complete}, {Kind: Chat, State: Complete}}
	if got := fresh.Statuses(); !equalStatuses(got, want) {
		t.Fatalf("reordered statuses = %+v, want %+v", got, want)
	}
}
