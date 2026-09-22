package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func strings_Contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// mediaDirectoryStub is the media authority the chat service consults at commit
// time. It answers for exactly one artifact in one conversation, so a reference
// to anything else is refused by the real rule rather than by the fake.
type mediaDirectoryStub struct {
	tenant, conversation, artifact string
	facts                          MediaFacts
	calls                          int
}

func (d *mediaDirectoryStub) MediaArtifact(_ context.Context, tenantID, conversationID, artifactID string) (MediaFacts, error) {
	d.calls++
	if tenantID != d.tenant || conversationID != d.conversation || artifactID != d.artifact {
		return MediaFacts{}, ErrNotFound
	}
	return d.facts, nil
}

func mediaService(t *testing.T, directory MediaDirectory) (*Service, *fakeStore) {
	t.Helper()
	f := &fakeStore{
		conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "u1", Revision: 1},
		membership:   Membership{ConversationID: "c1", TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", Role: Member, Revision: 1},
	}
	s := newTestService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	if directory != nil {
		s.SetMediaDirectory(directory)
	}
	return s, f
}

func mediaSend(ref Reference) SendPostRequest {
	return SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "see attached", IdempotencyKey: "k", References: []Reference{ref}}
}

func mediaRef() Reference {
	return Reference{Kind: MediaAttachment, TenantID: "t1", ID: "artifact-1", ConversationID: "c1", Display: "comp-bands.png", ContentType: "image/png", ByteSize: 4096, Width: 460, Height: 260}
}

// TestMediaAttachmentIsValidatedAgainstTheMediaService proves a post may attach an
// artifact the media service admits, and that the post carries only the
// reference: the bytes stay where scanning and per-read authorization live.
func TestMediaAttachmentIsValidatedAgainstTheMediaService(t *testing.T) {
	directory := &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: MediaFacts{ContentType: "image/png", ByteSize: 4096, Admitted: true}}
	s, f := mediaService(t, directory)
	post, err := s.SendPost(context.Background(), mediaSend(mediaRef()))
	if err != nil {
		t.Fatalf("send with an admitted attachment = %v", err)
	}
	if directory.calls != 1 {
		t.Fatalf("media authority consulted %d times", directory.calls)
	}
	_ = post
	if len(f.sent.References) != 1 || f.sent.References[0].Kind != MediaAttachment || f.sent.References[0].ID != "artifact-1" {
		t.Fatalf("persisted references = %+v", f.sent.References)
	}
	if f.sent.References[0].ContentType != "image/png" || f.sent.References[0].ByteSize != 4096 || f.sent.References[0].Width != 460 {
		t.Fatalf("attachment snapshot lost: %+v", f.sent.References[0])
	}
	if strings_Contains(f.sent.Body, "image/png") {
		t.Fatal("the attachment leaked into the body")
	}
	if f.mutations == 0 {
		t.Fatal("the post was not written")
	}
}

// TestMediaAttachmentFailsClosed walks every way an attachment can be wrong. Each
// one matters: a post that references an artifact it may not, or one that
// disagrees with the media service about what it is attaching, would render as a
// broken or mislabelled attachment for the life of the post.
func TestMediaAttachmentFailsClosed(t *testing.T) {
	admitted := MediaFacts{ContentType: "image/png", ByteSize: 4096, Admitted: true}
	for _, tc := range []struct {
		name      string
		directory MediaDirectory
		mutate    func(*Reference)
		want      error
	}{
		{
			name:      "no media service composed",
			directory: nil,
			want:      ErrUnavailable,
		},
		{
			name:      "artifact does not exist",
			directory: &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "other", facts: admitted},
			want:      ErrNotFound,
		},
		{
			name:      "artifact is not admitted",
			directory: &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: MediaFacts{ContentType: "image/png", ByteSize: 4096}},
			want:      ErrNotFound,
		},
		{
			name:      "another conversation's media",
			directory: &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: admitted},
			mutate:    func(r *Reference) { r.ConversationID = "c2" },
			want:      ErrPermissionDenied,
		},
		{
			name:      "another tenant's media",
			directory: &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: admitted},
			mutate:    func(r *Reference) { r.TenantID = "t2" },
			want:      ErrPermissionDenied,
		},
		{
			name:      "content type disagrees with the media service",
			directory: &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: admitted},
			mutate:    func(r *Reference) { r.ContentType = "image/gif" },
			want:      ErrInvalidArgument,
		},
		{
			name:      "byte size disagrees with the media service",
			directory: &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: admitted},
			mutate:    func(r *Reference) { r.ByteSize = 9 },
			want:      ErrInvalidArgument,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := mediaService(t, tc.directory)
			ref := mediaRef()
			if tc.mutate != nil {
				tc.mutate(&ref)
			}
			if _, err := s.SendPost(context.Background(), mediaSend(ref)); !errors.Is(err, tc.want) {
				t.Fatalf("send = %v, want %v", err, tc.want)
			}
			if f.mutations != 0 {
				t.Fatalf("a refused attachment still wrote the post: %d mutations", f.mutations)
			}
		})
	}
}
