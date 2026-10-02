package chat

import (
	"context"
	"strings"
	"testing"
)

// countingSourceAccess resolves like the hub does and counts how often it was
// asked.
type countingSourceAccess struct {
	allowed bool
	asked   []AgentDocumentSource
}

func (a *countingSourceAccess) ResolveAgentDocumentSource(_ context.Context, reader Principal, tenant, conversation string, source AgentDocumentSource) (AgentDocumentSource, error) {
	a.asked = append(a.asked, source)
	if reader.SubjectID != "u1" || tenant != "t1" || conversation != "c1" || !a.allowed {
		return source, ErrPermissionDenied
	}
	source.Readable, source.Href = true, "/workspace/app/docs?document="+source.DocumentID+"&version=v1#resolved"
	return source, nil
}

// TestTodo_CHATBUG_014_SourcesResolvedOncePerRead: one history read asks the
// document hub about each distinct source once, however many answers on the
// page cite it, and the next read asks again.
func TestTodo_CHATBUG_014_SourcesResolvedOncePerRead(t *testing.T) {
	s := projectionService()
	access := &countingSourceAccess{allowed: true}
	s.SetAgentSourceAccess(access)
	pto := "\n\nSources\n- [PTO](/workspace/app/docs?document=pto)"
	both := pto + "\n- [Holidays](/workspace/app/docs?document=holidays)"
	template := s.store.(*referenceProjectionStore).posts[0]
	posts := make([]Post, 0, 6)
	for i, body := range []string{"One." + pto, "Two." + pto, "Three." + both, "No sources here.", "Four." + both, "Five." + pto} {
		post := template
		post.ID = "answer-" + string(rune('a'+i))
		post.Body = body
		posts = append(posts, post)
	}

	read := func() []Post { return s.projectConversationReferences(context.Background(), principal(), posts) }
	projected := read()
	if len(access.asked) != 2 {
		t.Fatalf("five answers citing two documents asked the hub %d times, want once per document", len(access.asked))
	}
	for i, post := range projected {
		cites := strings.Count(posts[i].Body, "\n- ")
		if got := strings.Count(post.Body, "readable:true"); got != cites {
			t.Fatalf("answer %d has %d readable sources of %d: %s", i, got, cites, post.Body)
		}
		if cites > 0 && !strings.Contains(post.Body, "#resolved") {
			t.Fatalf("answer %d kept its stored link: %s", i, post.Body)
		}
	}

	// Nothing is kept between reads: access is resolved again, and a revocation
	// takes effect on the very next one.
	access.allowed = false
	projected = read()
	if len(access.asked) != 4 {
		t.Fatalf("the second read asked the hub %d more times, want 2", len(access.asked)-2)
	}
	for i, post := range projected {
		if strings.Contains(post.Body, "readable:true") || strings.Contains(post.Body, "/workspace/app/docs") {
			t.Fatalf("answer %d is still readable after the revocation: %s", i, post.Body)
		}
	}

	// The answer is per reader and per conversation, not only per source.
	memo := newAgentSourceMemo(&countingSourceAccess{allowed: true})
	source := AgentDocumentSource{Title: "PTO", DocumentID: "pto"}
	ctx := context.Background()
	mine, err := memo.ResolveAgentDocumentSource(ctx, principal(), "t1", "c1", source)
	if err != nil || !mine.Readable {
		t.Fatalf("the reader's own source: %+v %v", mine, err)
	}
	for name, ask := range map[string]func() (AgentDocumentSource, error){
		"another reader": func() (AgentDocumentSource, error) {
			return memo.ResolveAgentDocumentSource(ctx, Principal{TenantID: "t1", SubjectID: "u2"}, "t1", "c1", source)
		},
		"another conversation": func() (AgentDocumentSource, error) {
			return memo.ResolveAgentDocumentSource(ctx, principal(), "t1", "c2", source)
		},
	} {
		if got, err := ask(); err == nil || got.Readable {
			t.Errorf("%s was answered from the first reader's answer: %+v", name, got)
		}
	}
	if again, err := memo.ResolveAgentDocumentSource(ctx, principal(), "t1", "c1", source); err != nil || again != mine {
		t.Fatalf("the same question changed its answer within one read: %+v %v", again, err)
	}

	// A read that was abandoned does not leave its interrupted answer behind,
	// and a service with no hub access trusts nothing.
	inner := &countingSourceAccess{allowed: true}
	memo = newAgentSourceMemo(inner)
	gone, cancel := context.WithCancel(ctx)
	cancel()
	_, _ = memo.ResolveAgentDocumentSource(gone, principal(), "t1", "c1", source)
	_, _ = memo.ResolveAgentDocumentSource(ctx, principal(), "t1", "c1", source)
	if len(inner.asked) != 2 {
		t.Fatalf("an answer from a cancelled read was kept: asked %d times", len(inner.asked))
	}
	if newAgentSourceMemo(nil) != nil {
		t.Fatal("a service with no source access was given one")
	}
}
