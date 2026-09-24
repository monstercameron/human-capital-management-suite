package productclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

func TestTodo_REV_077_02_ProductClientUsesAuthorizedJourneySearch(t *testing.T) {
	state, err := ParseState("/workspace/app/help/knowledge-search", "q=leave+policy&locale=de-DE")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
		SearchKnowledge: func(_ context.Context, request *journeyv1.SearchKnowledgeRequest) (*journeyv1.SearchKnowledgeResponse, error) {
			called = true
			if request.GetQuery() != "leave policy" || request.GetLocale() != "de-DE" {
				t.Fatalf("RPC request = %+v", request)
			}
			return &journeyv1.SearchKnowledgeResponse{Matches: []*journeyv1.KnowledgeArticleMatch{{ArticleId: "leave-4", Revision: 4, Locale: "de-DE", Title: "Urlaubsrichtlinie", Summary: "Urlaub beantragen"}}}, nil
		},
	}
	view, err := Load(context.Background(), service, Session{Roles: []string{"worker_self"}}, state)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !called {
		t.Fatal("knowledge search RPC was not called")
	}
	results, err := view.SearchKnowledge("leave policy")
	if err != nil || len(results) != 1 || results[0].Title != "Urlaubsrichtlinie" || results[0].Revision != 4 {
		t.Fatalf("view results = %+v, error=%v", results, err)
	}
	wrongQuery, err := view.SearchKnowledge("another query")
	if err != nil || len(wrongQuery) != 0 {
		t.Fatalf("request-mismatched callback results=%+v error=%v", wrongQuery, err)
	}
}
