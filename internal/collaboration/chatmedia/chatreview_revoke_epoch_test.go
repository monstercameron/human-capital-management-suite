package chatmedia

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

// Revoke invalidates the grants that are outstanding. A grant issued after the
// revocation, to a caller the authority still admits, opens: revocation is an
// epoch, not a tombstone on the artifact.
func TestChatReview_GrantAfterRevokeOpens(t *testing.T) {
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return nil })
	ctx := context.Background()
	ref, err := s.Upload(ctx, UploadRequest{TenantID: "t1", ConversationID: "c1", PrincipalID: "p1", DeclaredType: "image/png", Content: pngBytes()})
	if err != nil {
		t.Fatal(err)
	}
	open := func(token string) error {
		b, _, err := s.Open(ctx, AccessRequest{TenantID: "t1", ConversationID: "c1", PrincipalID: "p1", ArtifactID: ref.ArtifactID, Grant: token}, 0, nil)
		if err == nil {
			b.Close()
		}
		return err
	}
	before, err := s.Authorize(ctx, AccessRequest{TenantID: "t1", ConversationID: "c1", PrincipalID: "p1", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := open(before.Token); err != nil {
		t.Fatalf("grant before revoke did not open: %v", err)
	}
	s.Revoke(ref.ArtifactID)
	if err := open(before.Token); err != ErrRevoked {
		t.Fatalf("grant outstanding at revoke opened: %v", err)
	}
	after, err := s.Authorize(ctx, AccessRequest{TenantID: "t1", ConversationID: "c1", PrincipalID: "p1", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := open(after.Token); err != nil {
		t.Fatalf("grant issued after revoke did not open: %v", err)
	}
	// And a second revocation retires that one too.
	s.Revoke(ref.ArtifactID)
	if err := open(after.Token); err != ErrRevoked {
		t.Fatalf("grant outstanding at second revoke opened: %v", err)
	}
}
