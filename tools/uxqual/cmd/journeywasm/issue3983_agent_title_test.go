package main

import "testing"

func TestIssue3983_AgentTaskTitleReplacesInsteadOfStacking(t *testing.T) {
	const page = "Agents · Harborcare Demo"
	const q1, q2 = "What is the parental leave policy?", "How many vacation days carry over to next year?"
	document, last, base := page, "", ""
	apply := func(prefix string) string {
		next, kept := agentTaskTabTitle(document, last, base, prefix)
		document, last, base = next, next, kept
		return document
	}
	for _, step := range []struct{ prefix, want string }{
		{q1, q1 + " · " + page},
		{q2, q2 + " · " + page},
		{q1, q1 + " · " + page},
		{q1, q1 + " · " + page},
		{"", page},
		{"", page},
	} {
		if got := apply(step.prefix); got != step.want {
			t.Fatalf("title for %q = %q, want %q", step.prefix, got, step.want)
		}
	}
	// A title the shell sets in between becomes the new base.
	document = "Chat · Other"
	if got := apply(q2); got != q2+" · Chat · Other" {
		t.Fatalf("title after the shell changed it = %q", got)
	}
}
