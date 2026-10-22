package chatmedia

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"io"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

// TestTodo_CHATBUG_095_StoredDimensions proves an upload records the picture's
// own pixel dimensions on its reference, for a static image and for an
// animated GIF alike, so a post can carry them and a tile can keep its
// footprint before the bytes arrive.
func TestTodo_CHATBUG_095_StoredDimensions(t *testing.T) {
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return nil })
	pngRef, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: validChatPNG(t, 640, 480, false)})
	if err != nil {
		t.Fatal(err)
	}
	if pngRef.Width != 640 || pngRef.Height != 480 {
		t.Fatalf("png reference dimensions=%dx%d", pngRef.Width, pngRef.Height)
	}
	frame := image.NewPaletted(image.Rect(0, 0, 480, 270), color.Palette{color.Black, color.White})
	var animation bytes.Buffer
	if err := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 20}}); err != nil {
		t.Fatal(err)
	}
	gifRef, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/gif", Content: animation.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	if gifRef.Width != 480 || gifRef.Height != 270 {
		t.Fatalf("gif reference dimensions=%dx%d", gifRef.Width, gifRef.Height)
	}
	described, err := s.Describe(context.Background(), "t", "c", pngRef.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if described.Width != 640 || described.Height != 480 {
		t.Fatalf("described dimensions=%dx%d", described.Width, described.Height)
	}
	// The GIF still serves its original animated bytes under the variant the
	// timeline loader asks for.
	g, err := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: gifRef.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	body, got, err := s.OpenVariant(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: gifRef.ArtifactID, Grant: g.Token}, VariantThumbnail, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	served, _ := io.ReadAll(body)
	body.Close()
	if got.MediaType != MediaGIF || !bytes.Equal(served, animation.Bytes()) {
		t.Fatal("gif variant did not serve the original animation")
	}
}
