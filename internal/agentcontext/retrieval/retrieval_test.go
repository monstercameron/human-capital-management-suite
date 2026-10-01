package retrieval

import (
	"context"
	"errors"
	"testing"
	"time"
)

var retrievalNow = time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)

type ownerStub struct {
	records []Record
	err     error
	request OwnerRequest
}

func (o *ownerStub) RetrieveChat(_ context.Context, request OwnerRequest) ([]Record, error) {
	o.request = request
	return cloneRecords(o.records), o.err
}

func (o *ownerStub) RetrieveDocuments(_ context.Context, request OwnerRequest) ([]Record, error) {
	o.request = request
	return cloneRecords(o.records), o.err
}

type grantStub struct {
	calls  int
	failAt int
	err    error
}

func (g *grantStub) CheckCurrent(_ context.Context, _ Access, _ Envelope) error {
	g.calls++
	if g.failAt == g.calls {
		return g.err
	}
	return nil
}

func retrievalAccess() Access {
	return Access{
		TenantID: "tenant-a", LegalEntityID: "entity-a", PrincipalID: "person-a",
		InvocationID: "invoke-a", ConversationID: "channel-a", Audience: "person-a",
		Purpose: "answer-policy-question", AuthorizationVersion: "authz-17",
		GrantIDs: []string{"grant-a"}, RevocationTokens: []string{"revoke-a"},
	}
}

func retrievalQuery() Query {
	return Query{Text: "leave policy", ConversationID: "channel-a", ThreadID: "thread-a", Limit: 10, Sources: []Kind{Chat}}
}

func retrievalRecord(kind Kind) Record {
	owner := "company-chat"
	if kind == Document {
		owner = "knowledge-hub"
	}
	return Record{Envelope: Envelope{
		Kind: kind, Owner: owner, TenantID: "tenant-a", LegalEntityID: "entity-a",
		SourceID: "source-1", Version: "v3", Classification: "INTERNAL",
		Audience: []string{"person-a"}, Purpose: "answer-policy-question",
		ConversationID: "channel-a", ThreadID: "thread-a",
		ObservedAt: retrievalNow.Add(-time.Minute), RetrievedAt: retrievalNow,
		FreshUntil:      retrievalNow.Add(time.Minute),
		Citation:        Citation{SourceID: "source-1", Version: "v3", Target: "source://source-1/v3"},
		RevocationToken: "source-revoke-3",
	}, Content: "The approved leave policy applies."}
}

func retrievalService(t *testing.T, chat ChatOwner, docs DocumentOwner, grants CurrentGrantChecker) *Service {
	t.Helper()
	s, err := New(Config{Chat: chat, Documents: docs, Grants: grants, Now: func() time.Time { return retrievalNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestTodo_AGENT_018(t *testing.T) {
	ctx := context.Background()
	access := retrievalAccess()
	owner := &ownerStub{records: []Record{retrievalRecord(Chat)}}
	grants := &grantStub{}
	service := retrievalService(t, owner, nil, grants)
	batch, err := service.Retrieve(ctx, access, retrievalQuery())
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if batch.Count() != 1 || batch.Statuses()[0] != (SourceStatus{Kind: Chat, State: Complete}) {
		t.Fatalf("batch = count %d, statuses %+v", batch.Count(), batch.Statuses())
	}
	if owner.request.Access.PrincipalID != access.PrincipalID || owner.request.Query.ThreadID != "thread-a" {
		t.Fatalf("owner request lost admitted scope: %+v", owner.request)
	}
	if grants.calls != 1 {
		t.Fatalf("retrieval current-grant checks = %d, want 1", grants.calls)
	}
	final, err := service.Finalize(ctx, access, batch)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	items := final.Items()
	if len(items) != 1 || items[0].Envelope.Version != "v3" || items[0].Envelope.Citation.Target != "source://source-1/v3" {
		t.Fatalf("final items = %+v", items)
	}
	if grants.calls != 2 {
		t.Fatalf("late grant checks = %d, want 2", grants.calls)
	}
	items[0].Envelope.Audience[0] = "mutated"
	if final.Items()[0].Envelope.Audience[0] != "person-a" {
		t.Fatal("disclosure exposed mutable envelope state")
	}
}

func TestTodo_AGENT_018_Fault(t *testing.T) {
	ctx := context.Background()
	access := retrievalAccess()
	query := retrievalQuery()
	query.Sources = []Kind{Chat, Document}
	chat := &ownerStub{err: errors.New("chat database details must not escape")}
	docs := &ownerStub{records: []Record{retrievalRecord(Document)}}
	service := retrievalService(t, chat, docs, &grantStub{})
	batch, err := service.Retrieve(ctx, access, query)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	want := []SourceStatus{{Kind: Chat, State: Failed, Code: FailureUnavailable}, {Kind: Document, State: Complete}}
	if got := batch.Statuses(); !equalStatuses(got, want) {
		t.Fatalf("statuses = %+v, want %+v", got, want)
	}
	final, err := service.Finalize(ctx, access, batch)
	if err != nil || len(final.Items()) != 1 {
		t.Fatalf("Finalize = %d items, %v", len(final.Items()), err)
	}
	for _, status := range final.Statuses() {
		if status.Kind == Chat && (status.State != Failed || status.Code != FailureUnavailable) {
			t.Fatalf("chat failure status = %+v", status)
		}
	}
	if _, err := New(Config{Grants: &grantStub{}, Now: func() time.Time { return retrievalNow }}); err != nil {
		t.Fatalf("New permits missing sources so failure can be explicit: %v", err)
	}
	missing := retrievalService(t, nil, nil, &grantStub{})
	missingBatch, err := missing.Retrieve(ctx, access, query)
	if err != nil || len(missingBatch.Statuses()) != 2 || missingBatch.Statuses()[0].Code != FailureUnavailable {
		t.Fatalf("missing source result = %+v, %v", missingBatch.Statuses(), err)
	}
}

func TestTodo_AGENT_018_Security(t *testing.T) {
	ctx := context.Background()
	access := retrievalAccess()
	record := retrievalRecord(Chat)
	grants := &grantStub{failAt: 2, err: ErrGrantRevoked}
	service := retrievalService(t, &ownerStub{records: []Record{record}}, nil, grants)
	batch, err := service.Retrieve(ctx, access, retrievalQuery())
	if err != nil || batch.Count() != 1 {
		t.Fatalf("Retrieve = count %d, %v", batch.Count(), err)
	}
	final, err := service.Finalize(ctx, access, batch)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(final.Items()) != 0 || final.Statuses()[0] != (SourceStatus{Kind: Chat, State: Failed, Code: FailureRevoked}) {
		t.Fatalf("revoked source leaked or failure was implicit: items=%+v statuses=%+v", final.Items(), final.Statuses())
	}
	other := access
	other.Audience = "different-person"
	if _, err := service.Finalize(ctx, other, batch); !errors.Is(err, ErrBatchMismatch) {
		t.Fatalf("changed audience Finalize = %v, want ErrBatchMismatch", err)
	}
	denied := &grantStub{failAt: 1, err: ErrGrantRevoked}
	deniedService := retrievalService(t, &ownerStub{records: []Record{record}}, nil, denied)
	deniedBatch, err := deniedService.Retrieve(ctx, access, retrievalQuery())
	if err != nil || deniedBatch.Count() != 0 || deniedBatch.Statuses()[0].Code != FailureRevoked {
		t.Fatalf("retrieval revoked result = %+v, %v", deniedBatch.Statuses(), err)
	}
	partialRecord := retrievalRecord(Chat)
	secondRecord := retrievalRecord(Chat)
	secondRecord.Envelope.SourceID = "source-2"
	secondRecord.Envelope.Version = "v4"
	secondRecord.Envelope.Citation = Citation{SourceID: "source-2", Version: "v4", Target: "source://source-2/v4"}
	partialGrants := &grantStub{failAt: 3, err: ErrGrantRevoked}
	partialService := retrievalService(t, &ownerStub{records: []Record{partialRecord, secondRecord}}, nil, partialGrants)
	partialBatch, err := partialService.Retrieve(ctx, access, retrievalQuery())
	if err != nil || partialBatch.Count() != 2 {
		t.Fatalf("partial setup = count %d, %v", partialBatch.Count(), err)
	}
	partial, err := partialService.Finalize(ctx, access, partialBatch)
	if err != nil || len(partial.Items()) != 1 || partial.Statuses()[0] != (SourceStatus{Kind: Chat, State: Partial, Code: FailureRevoked}) {
		t.Fatalf("partial finalization = %+v, statuses %+v, err %v", partial.Items(), partial.Statuses(), err)
	}
	failedAuth := &grantStub{failAt: 1, err: errors.New("grant backend unavailable")}
	authService := retrievalService(t, &ownerStub{records: []Record{record}}, nil, failedAuth)
	authBatch, err := authService.Retrieve(ctx, access, retrievalQuery())
	if err != nil || authBatch.Count() != 0 || authBatch.Statuses()[0].Code != FailureAuthorization {
		t.Fatalf("authorization fault = %+v, %v", authBatch.Statuses(), err)
	}
}

func TestTodo_AGENT_018_Conformance(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Record)
	}{
		{"wrong owner kind", func(r *Record) { r.Envelope.Kind = Document }},
		{"wrong tenant", func(r *Record) { r.Envelope.TenantID = "tenant-b" }},
		{"wrong legal entity", func(r *Record) { r.Envelope.LegalEntityID = "entity-b" }},
		{"missing legal entity", func(r *Record) { r.Envelope.LegalEntityID = "" }},
		{"missing owner", func(r *Record) { r.Envelope.Owner = " " }},
		{"missing version", func(r *Record) { r.Envelope.Version = "" }},
		{"classification missing", func(r *Record) { r.Envelope.Classification = "" }},
		{"hidden audience", func(r *Record) { r.Envelope.Audience = []string{"someone-else"} }},
		{"blank audience entry", func(r *Record) { r.Envelope.Audience = []string{"person-a", " "} }},
		{"purpose mismatch", func(r *Record) { r.Envelope.Purpose = "other-purpose" }},
		{"stale", func(r *Record) { r.Envelope.FreshUntil = retrievalNow }},
		{"future observation", func(r *Record) { r.Envelope.ObservedAt = retrievalNow.Add(time.Second) }},
		{"future retrieval", func(r *Record) { r.Envelope.RetrievedAt = retrievalNow.Add(time.Second) }},
		{"deleted", func(r *Record) { r.Envelope.Deleted = true }},
		{"legal hold", func(r *Record) { r.Envelope.OnHold = true }},
		{"missing revocation", func(r *Record) { r.Envelope.RevocationToken = "" }},
		{"citation version mismatch", func(r *Record) { r.Envelope.Citation.Version = "v2" }},
		{"citation target missing", func(r *Record) { r.Envelope.Citation.Target = "" }},
		{"wrong conversation", func(r *Record) { r.Envelope.ConversationID = "private-channel" }},
		{"wrong thread", func(r *Record) { r.Envelope.ThreadID = "other-thread" }},
		{"empty content", func(r *Record) { r.Content = " " }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := retrievalRecord(Chat)
			tc.mutate(&record)
			service := retrievalService(t, &ownerStub{records: []Record{record}}, nil, &grantStub{})
			batch, err := service.Retrieve(context.Background(), retrievalAccess(), retrievalQuery())
			if err != nil || batch.Count() != 0 || batch.Statuses()[0] != (SourceStatus{Kind: Chat, State: Failed, Code: FailureInvalid}) {
				t.Fatalf("malformed owner result = count %d statuses %+v err %v", batch.Count(), batch.Statuses(), err)
			}
		})
	}
	access := retrievalAccess()
	if err := validateRequest(access, Query{Text: "q", ConversationID: "channel-a", Limit: 1, Sources: []Kind{Chat, Chat}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("duplicate source validation = %v", err)
	}
	if err := validateRequest(access, Query{Text: "q", ConversationID: "other-channel", Limit: 1, Sources: []Kind{Chat}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross-conversation validation = %v", err)
	}
	for _, mutate := range []func(*Access){
		func(a *Access) { a.GrantIDs = []string{"grant-a", " "} },
		func(a *Access) { a.RevocationTokens = []string{"revoke-a", "\t"} },
	} {
		invalidAccess := access
		mutate(&invalidAccess)
		if err := validateRequest(invalidAccess, retrievalQuery()); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("blank grant evidence validation = %v", err)
		}
	}
	query := retrievalQuery()
	query.Limit = 1
	if err := validateRecords([]Record{retrievalRecord(Chat), retrievalRecord(Chat)}, Chat, access, query, retrievalNow); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("limit validation = %v", err)
	}
	if _, err := New(Config{Chat: &ownerStub{}, Documents: &ownerStub{}, Now: func() time.Time { return retrievalNow }}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing grant checker New = %v", err)
	}
	if _, err := New(Config{Grants: &grantStub{}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing clock New = %v", err)
	}
	var nilService *Service
	if _, err := nilService.Retrieve(context.Background(), access, retrievalQuery()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil service Retrieve = %v", err)
	}
	if _, err := retrievalService(t, nil, nil, &grantStub{}).Finalize(context.Background(), access, Batch{}); !errors.Is(err, ErrBatchMismatch) {
		t.Fatalf("empty batch Finalize = %v", err)
	}
}

func equalStatuses(left, right []SourceStatus) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
