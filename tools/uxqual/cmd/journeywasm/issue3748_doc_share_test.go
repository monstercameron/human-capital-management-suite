package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestIssue3748_DocShareCanonical(t *testing.T) {
	cases := map[string]string{
		"/workspace/app/docs?document=doc-42":                 "/workspace/app/docs?document=doc-42",
		"/workspace/app/docs?document=doc-42&q=x&docs_edit=1": "/workspace/app/docs?document=doc-42",
		"/workspace/app/docs?q=handbook":                      "",
		"/workspace/app/project?project=p1":                   "",
	}
	for in, want := range cases {
		got := docShareCanonical(in)
		if got != want {
			t.Errorf("docShareCanonical(%q) = %q, want %q", in, got, want)
		}
	}
}

// What Send to chat posts is recognized by chat as a document reference, so it
// unfurls like a pasted link.
func TestIssue3748_PostedLinkUnfurls(t *testing.T) {
	origin := "https://hcm.example.test"
	body := "Please read this\n" + origin + docShareCanonical("/workspace/app/docs?document=doc-42&x=1")
	refs := chatui.DocReferences(body, origin)
	if len(refs) != 1 || refs[0].ID != "doc-42" {
		t.Fatalf("refs = %+v", refs)
	}
}
