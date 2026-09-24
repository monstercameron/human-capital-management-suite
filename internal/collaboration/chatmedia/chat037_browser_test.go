package chatmedia

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

// TestTodo_CHAT_037_Browser is the CHAT-037 BROWSER matrix test. It exercises
// the rendered playback surface: a verified voice message can be scrubbed
// with bounded reads the way an <audio> element seeks, the accessible
// transcript travels with every read (not just the first), and a container
// whose bytes do not match its declared audio type never reaches a playable
// state in the first place.
func TestTodo_CHAT_037_Browser(t *testing.T) {
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return nil })
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "audio/wav", Content: wavBytes(2), EvidenceID: "e", Transcript: "two seconds of room tone"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	full, fullRef, err := s.Open(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: g.Token}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	fullBytes, err := io.ReadAll(full)
	full.Close()
	if err != nil {
		t.Fatal(err)
	}
	if fullRef.MediaType != MediaWAV || fullRef.Transcript != "two seconds of room tone" {
		t.Fatalf("full playback reference = %+v", fullRef)
	}

	// A player scrubbing partway through the clip gets a bounded read of the
	// same playable container, and the accessible alternative is still
	// attached, so a caption track never falls out of sync with a seek.
	mid := int64(len(fullBytes) / 2)
	partial, partialRef, err := s.Open(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: g.Token}, mid, nil)
	if err != nil {
		t.Fatal(err)
	}
	partialBytes, err := io.ReadAll(partial)
	partial.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(partialBytes) != string(fullBytes[mid:]) {
		t.Fatal("seeking did not return the tail of the same playable bytes")
	}
	if partialRef.Transcript != "two seconds of room tone" {
		t.Fatal("seeking lost the accessible alternative")
	}

	// A container whose bytes don't match its declared audio type is refused
	// at admission, so it can never be opened for playback at all: the
	// player's data source is inspected before it is ever offered to serve.
	if _, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "audio/mpeg", Content: wavBytes(1), EvidenceID: "mismatch", Transcript: "wav bytes labeled mp3"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("mismatched container admitted for eventual playback: %v", err)
	}
}
