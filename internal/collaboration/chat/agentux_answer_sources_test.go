package chat

import (
	"context"
	"strings"
	"testing"
)

type qualitySourceAccess struct{ allowed bool }

func (a *qualitySourceAccess) ResolveAgentDocumentSource(_ context.Context, reader Principal, tenant, conversation string, source AgentDocumentSource) (AgentDocumentSource, error) {
	if reader.SubjectID != "u1" || tenant != "t1" || conversation != "c1" || !a.allowed {
		return source, ErrPermissionDenied
	}
	source.Readable, source.Href = true, "/workspace/app/docs?document=pto&version=v1#carryover"
	return source, nil
}

func TestAgentUXQuality_SourceProjection_Security(t *testing.T) {
	s := projectionService()
	access := &qualitySourceAccess{allowed: true}
	s.SetAgentSourceAccess(access)
	body := "See [PTO](/workspace/app/docs?document=pto).\n\nSources\n- [PTO](/workspace/app/docs?document=pto) <!--chat.agent.source.readable:true-->"
	s.store.(*referenceProjectionStore).posts[0].Body = body
	read := func() string {
		posts := s.projectConversationReferences(context.Background(), principal(), s.store.(*referenceProjectionStore).posts)
		return posts[0].Body
	}
	if got := read(); !strings.Contains(got, "&version=v1#carryover") || !strings.Contains(got, "readable:true") {
		t.Fatalf("readable projection: %s", got)
	}
	access.allowed = false
	if got := read(); strings.Contains(got, "/workspace/app/docs") || !strings.Contains(got, "readable:false") {
		t.Fatalf("revoked projection: %s", got)
	}
	if s.store.(*referenceProjectionStore).posts[0].Body != body {
		t.Fatal("reader projection mutated stored answer")
	}
	s.SetAgentSourceAccess(nil)
	if got := read(); strings.Contains(got, "/workspace/app/docs") {
		t.Fatalf("missing access authority trusted stored link: %s", got)
	}
}

func TestAgentUXQuality_SourceProjection(t *testing.T) {
	if section := agentLegacyCitationSection("40 hours (Paid time off policy, version 1, Carryover section).", "Paid time off policy (version 1)"); section != "Carryover" {
		t.Fatalf("legacy inline section lost: %q", section)
	}
	for _, href := range []string{"//evil/workspace/app/docs?document=pto", "https://evil/workspace/app/docs?document=pto", "/workspace/app/docs", "javascript:alert(1)"} {
		if validProjectedAgentSource(href) {
			t.Fatalf("invalid projected target: %q", href)
		}
	}
	source := parseAgentDocumentSource("[PTO](/workspace/app/docs?document=pto&version=one#carryover)")
	if source.DocumentID != "pto" || source.VersionID != "one" || source.SectionAnchor != "carryover" || source.Readable {
		t.Fatalf("source: %+v", source)
	}
}

func TestAgentUXQuality_EphemeralSourceProjection_Security(t *testing.T) {
	service := projectionService()
	service.SetAgentSourceAccess(&qualitySourceAccess{allowed: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan WatchEvent, 1)
	original := &EphemeralDelivery{ID: "private-answer", Body: "Answer\n\nSources\n- PTO"}
	input <- WatchEvent{EphemeralDelivery: original}
	close(input)
	events, _ := service.projectWatchEvents(ctx, principal(), "t1", "c1", input, nil)
	projected := <-events
	if projected.EphemeralDelivery == nil || !strings.Contains(projected.EphemeralDelivery.Body, "readable:true") || projected.EphemeralDelivery == original || strings.Contains(original.Body, "readable:") {
		t.Fatalf("ephemeral source not projected independently: %+v", projected)
	}
}
