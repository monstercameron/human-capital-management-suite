package main

import (
	"testing"
)

// TestTodo_CHATBUG_095_ReferenceDimensions proves the stored picture
// dimensions travel from the upload answer onto the posted reference: the
// draft keeps them and begin sends them, so the post carries the footprint
// its tile renders before the bytes arrive.
func TestTodo_CHATBUG_095_ReferenceDimensions(t *testing.T) {
	s := &chatattach001State{}
	key, ok := s.add("room", "photo.png", "image/png", "blob:photo", 8)
	if !ok {
		t.Fatal("draft refused")
	}
	if s.complete("room", key, "artifact", "image/png", 8, 200) != chatattach001Uploaded {
		t.Fatal("completion lost")
	}
	s.setDims("room", key, 640, 480)
	refs, _, ok := s.begin("room", "tenant", "hello", nil)
	if !ok || len(refs) != 1 {
		t.Fatalf("send refs=%+v ready=%v", refs, ok)
	}
	if refs[0].Width != 640 || refs[0].Height != 480 {
		t.Fatalf("posted reference dimensions=%dx%d", refs[0].Width, refs[0].Height)
	}
}

// TestTodo_CHATBUG_095_ProbeRetry proves the upload-availability probe gets
// one second chance when the request never completed (an abort on load
// reports status zero), and none after that or after a real answer.
func TestTodo_CHATBUG_095_ProbeRetry(t *testing.T) {
	for _, tc := range []struct {
		status, attempt int
		want            bool
	}{
		{0, 0, true},
		{0, 1, false},
		{200, 0, false},
		{403, 0, false},
		{503, 0, false},
	} {
		if got := chatattach001ProbeRetry(tc.status, tc.attempt); got != tc.want {
			t.Errorf("status=%d attempt=%d retry=%v", tc.status, tc.attempt, got)
		}
	}
}
