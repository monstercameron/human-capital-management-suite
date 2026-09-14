package workspace

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_UXAUDIT_013_HelpServiceBoundary(t *testing.T) {
	var service HelpService = helpServiceStub{}
	response, err := service.SearchKnowledge(context.Background(), KnowledgeSearchRequest{Query: "leave", Locale: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	projected := ProjectKnowledgeSearchResponse(response)
	if len(projected.Results) != 1 || projected.Results[0].ID != "article-authorized" {
		t.Fatalf("projected authorized results = %+v", projected.Results)
	}
	if projected.Results[0].Summary != "safe summary" {
		t.Fatalf("summary = %q", projected.Results[0].Summary)
	}
	receipt, err := service.CreateSupportRequest(context.Background(), SupportRequest{Category: "benefits", Details: "Need help"})
	if err != nil {
		t.Fatal(err)
	}
	if err := receipt.Validate(); err != nil || receipt.RequestID != "req-service-issued" {
		t.Fatalf("receipt = %+v, err = %v", receipt, err)
	}
}

func TestTodo_UXAUDIT_013_HelpServiceSecurity(t *testing.T) {
	response := ProjectKnowledgeSearchResponse(KnowledgeSearchResponse{Results: []KnowledgeArticle{
		{ID: "denied", Title: "Private", Authorized: false},
		{ID: "", Title: "Malformed", Authorized: true},
		{ID: "ok", Title: "Public", Authorized: true},
	}})
	if len(response.Results) != 1 || response.Results[0].ID != "ok" {
		t.Fatalf("non-disclosing projection = %+v", response.Results)
	}
	for name, request := range map[string]KnowledgeSearchRequest{
		"empty":          {Query: " ", Locale: "en-US"},
		"missing locale": {Query: "leave"},
	} {
		if err := request.Validate(); !errors.Is(err, ErrHelpInvalid) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	if err := (SupportRequest{Category: "benefits"}).Validate(); !errors.Is(err, ErrHelpInvalid) {
		t.Fatalf("empty details error = %v", err)
	}
	if err := (SupportRequestReceipt{}).Validate(); !errors.Is(err, ErrHelpUnready) {
		t.Fatalf("empty receipt error = %v", err)
	}
}

func TestTodo_UXAUDIT_013_HelpServiceHrefSecurity(t *testing.T) {
	for _, href := range []string{
		"javascript:alert(1)", "//evil.example/article", "https://evil.example/article",
		"/workspace/app/home", "/workspace/app/help/../admin", "/workspace/app/help/%2e%2e/admin",
	} {
		response := ProjectKnowledgeSearchResponse(KnowledgeSearchResponse{Results: []KnowledgeArticle{{
			ID: "article", Title: "Authorized", Href: href, Authorized: true,
		}}})
		if got := response.Results[0].Href; got != "" {
			t.Fatalf("href %q escaped the same-origin Help boundary as %q", href, got)
		}
	}
	if got := normalizeHelpHref("/workspace/app/help/article-1?locale=en-US#summary"); got != "/workspace/app/help/article-1?locale=en-US" {
		t.Fatalf("normalized same-origin href = %q", got)
	}
}

type helpServiceStub struct{}

func (helpServiceStub) SearchKnowledge(context.Context, KnowledgeSearchRequest) (KnowledgeSearchResponse, error) {
	return KnowledgeSearchResponse{PolicyVersion: "help-policy-v1", Results: []KnowledgeArticle{
		{ID: "article-authorized", Title: "Leave guidance", Summary: "safe summary", Authorized: true},
		{ID: "article-denied", Title: "Restricted", Authorized: false},
	}}, nil
}

func (helpServiceStub) CreateSupportRequest(context.Context, SupportRequest) (SupportRequestReceipt, error) {
	return SupportRequestReceipt{RequestID: "req-service-issued", Status: "accepted", PolicyVersion: "help-policy-v1"}, nil
}
