package documenthubstore

import (
	"context"
	"strings"
	"testing"
)

func TestAgentUXSearch_Keyword_Security_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	create := func(tenant, title, markdown string, share bool) (string, string) {
		t.Helper()
		id, v, err := s.CreatePersonalDocument(ctx, tenant, "owner", title, markdown)
		if err != nil {
			t.Fatal(err)
		}
		if share {
			if err := s.SharePersonalDocumentRole(ctx, tenant, id, "owner", "reader", RoleViewer); err != nil {
				t.Fatal(err)
			}
		}
		return id, v.ID
	}
	guide, version := create("workspace", "2026 holiday guide", "# 2026 holiday guide\n\nOffices are closed on each listed day.\n\n| Thanksgiving Day | Nov 26 | Thursday |\n| Christmas Day | Dec 25 | Friday |\n", true)
	create("workspace", "Paid time off policy", "# Paid time off policy\n\n## Carryover\n\nEmployees may carry over 40 hours.\n\n## Requests\n\nAsk your manager.\n", true)
	secret, _ := create("workspace", "Executive holiday retreat 2026", "# Retreat\n\nHoliday retreat budget for 2026.\n", false)
	foreign, _ := create("foreign", "Foreign holiday guide 2026", "# Foreign\n\nHoliday 2026.\n", true)
	members := []string{"owner", "reader"}
	hits, err := s.SearchWorkspaceKeyword(ctx, "workspace", "reader", "which company holidays are coming up in the rest of 2026", members)
	if err != nil || len(hits) != 1 || hits[0].DocumentID != guide || hits[0].VersionID != version || !strings.Contains(hits[0].Text, "Thanksgiving Day") || hits[0].SectionAnchor != "2026-holiday-guide" {
		t.Fatalf("holiday question: %+v %v", hits, err)
	}
	for _, hit := range hits {
		if hit.DocumentID == secret || hit.DocumentID == foreign {
			t.Fatal("unreadable or foreign document returned")
		}
	}
	hits, err = s.SearchWorkspaceKeyword(ctx, "workspace", "reader", "how much paid time off can I carry over", members)
	if err != nil || len(hits) != 1 || hits[0].Title != "Paid time off policy" || hits[0].SectionAnchor != "carryover" || strings.Contains(hits[0].Text, "Ask your manager") {
		t.Fatalf("section selection: %+v %v", hits, err)
	}
	if hits, err := s.SearchWorkspaceKeyword(ctx, "workspace", "reader", "company policies and guides", members); err != nil || len(hits) != 2 {
		t.Fatalf("plural words did not match: %+v %v", hits, err)
	}
	if hits, err := s.SearchWorkspaceKeyword(ctx, "workspace", "reader", "what is the", members); err != nil || len(hits) != 0 {
		t.Fatalf("question with no content words matched: %+v %v", hits, err)
	}
	if hits, err := s.SearchWorkspaceKeyword(ctx, "workspace", "reader", "holiday 2026", []string{"owner", "reader", "new-member"}); err != nil || len(hits) != 0 {
		t.Fatalf("a member without a read grant did not make the document private: %+v %v", hits, err)
	}
	if _, err := s.SearchWorkspaceKeyword(ctx, "workspace", "reader", "holiday 2026", nil); err == nil {
		t.Fatal("empty directory allowed")
	}
	deny, err := s.GrantAction(ctx, "workspace", GrantInput{DocumentID: guide, SubjectKind: "person", SubjectID: "reader", Action: ActionRead, Effect: EffectDeny, Issuer: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if hits, err := s.SearchWorkspaceKeyword(ctx, "workspace", "reader", "holiday 2026", members); err != nil || len(hits) != 0 {
		t.Fatalf("restricted document still returned: %+v %v", hits, err)
	}
	if err := s.RevokeGrant(ctx, "workspace", deny.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if hits, err := s.SearchWorkspaceKeyword(ctx, "workspace", "reader", "holiday 2026", members); err != nil || len(hits) != 1 {
		t.Fatalf("revoked deny still blocked a live allow: %+v %v", hits, err)
	}
}
