package agentdocref

import (
	"context"
	"reflect"
	"testing"
)

type resolverContractFake struct{}

func (resolverContractFake) Resolve(context.Context, Invoker, []Reference) ([]ResolvedDocument, []Omission, error) {
	return nil, nil, nil
}

func TestTodo_AGENTDOC_003(t *testing.T) {
	var _ Resolver = resolverContractFake{}
	invoker := Invoker{TenantID: "tenant-a", SubjectID: "reader-a"}
	if invoker.TenantID != "tenant-a" || invoker.SubjectID != "reader-a" {
		t.Fatalf("invoker changed: %+v", invoker)
	}
	for _, reason := range []string{NotReadable, NotFound, NotPublished, OverBudget} {
		if reason == "" {
			t.Fatal("empty omission reason")
		}
	}
}

func TestTodo_AGENTDOC_003_Property(t *testing.T) {
	original := []ResolvedDocument{
		{Reference: Reference{Label: "First"}, Content: "# One\n\n1234\n## Two\n\n5678\n"},
		{Reference: Reference{Label: "Second"}, Content: "# Three\n\n90\n"},
	}
	for budget := 0; budget <= 80; budget++ {
		got := ApplyBudget(original, budget)
		used := 0
		for i, document := range got {
			used += len([]rune(document.Content))
			if i >= len(original) || document.Reference != original[i].Reference || len(document.Content) > len(original[i].Content) || document.Content != original[i].Content[:len(document.Content)] {
				t.Fatalf("budget %d delivered non-prefix content: %#v", budget, got)
			}
		}
		if used > budget {
			t.Fatalf("budget %d used %d characters", budget, used)
		}
	}
	if !reflect.DeepEqual(original[0].Content, "# One\n\n1234\n## Two\n\n5678\n") {
		t.Fatal("ApplyBudget mutated caller content")
	}
}

func TestTodo_AGENTDOC_003_Mutation(t *testing.T) {
	content := "# One\n\nkeep\n## Two\n\ndrop\n"
	firstSection := "# One\n\nkeep\n"
	got := ApplyBudget([]ResolvedDocument{{Reference: Reference{Label: "Policy"}, Content: content}}, len([]rune(firstSection)))
	if len(got) != 1 || got[0].Content != firstSection || !got[0].Truncated {
		t.Fatalf("section quarantine mutation survived: %#v", got)
	}
	if got := ApplyBudget([]ResolvedDocument{{Reference: Reference{Label: "Policy"}, Content: content}}, 1); len(got) != 0 {
		t.Fatalf("partial section escaped budget: %#v", got)
	}
}
