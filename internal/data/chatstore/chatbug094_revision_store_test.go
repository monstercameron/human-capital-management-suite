package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// TestTodo_CHATBUG_094_StoreReads: a request for a rendering never answers a
// raw database miss. A post that predates detection keeps no revision row: the
// request is queued with the source detected from the body, and the language
// read reports not-found so the caller detects instead. A revision that is not
// there is the typed not-found error, which the reader leaves out of the batch
// instead of failing it 503.
func TestTodo_CHATBUG_094_StoreReads(t *testing.T) {
	s, scope, p := chatrenderDB(t)
	ctx := context.Background()
	const legacyBody = "The quarterly report is ready for review"
	if err := s.Store.execTenant(ctx, scope.Tenant, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,revision,body) VALUES('legacy-post','tenant-a','render-room','alice','tenant-a',50,1,'`+legacyBody+`')`); err != nil {
		t.Fatal(err)
	}
	target := chatrender.Rendering{Tenant: scope.Tenant, Message: "legacy-post", Revision: 1, Tone: chatrender.AsWritten, Language: "de"}
	if err := s.RequestRendering(ctx, scope, target); err != nil {
		t.Fatalf("a post with no revision row was not queued: %v", err)
	}
	job, err := s.ClaimRendering(ctx, scope.Tenant, time.Minute)
	if err != nil || job.Request.Message != "legacy-post" || job.Request.Text != legacyBody || job.Request.SourceLanguage != chatrender.Detect(legacyBody).Language {
		t.Fatalf("the queued request carries %+v, %v", job.Request, err)
	}
	stale := target
	stale.Message, stale.Revision = p.ID, p.Revision+10
	if err := s.RequestRendering(ctx, scope, stale); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("a revision that is not there answered %v, want not found", err)
	}
	if _, err := s.RevisionLanguage(ctx, scope, "legacy-post", 1); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("a post with no revision row answered %v, want not found", err)
	}
}
