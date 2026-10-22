package chat

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_CHATBUG_095_ReferenceDimensions proves the commit-time snapshot
// check covers the picture's dimensions: a reference that disagrees with the
// media service about them is refused, like a content-type or size mismatch.
func TestTodo_CHATBUG_095_ReferenceDimensions(t *testing.T) {
	directory := &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: MediaFacts{ContentType: "image/png", ByteSize: 4096, Width: 460, Height: 260, Admitted: true}}
	s, _ := mediaService(t, directory)
	if _, err := s.SendPost(context.Background(), mediaSend(mediaRef())); err != nil {
		t.Fatalf("send with matching dimensions = %v", err)
	}
	wrong := mediaRef()
	wrong.Width = 640
	if _, err := s.SendPost(context.Background(), mediaSend(wrong)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("send with mismatched width = %v", err)
	}
}
