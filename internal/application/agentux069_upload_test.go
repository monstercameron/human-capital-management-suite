package application

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

func agentUX069PNG(t *testing.T, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, canvas); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}

// The owner's own image uploads through the icon surface, is reported with a
// same-origin URL, and is served back re-encoded to an authenticated human.
func TestAgentUX069_UploadSurface(t *testing.T) {
	store, scoped, ctx, now, _ := agentIconApplicationFixture(t)
	agentIconCreateApplicationDraft(t, scoped, ctx, now, "upload-surface")
	surface := &AgentIconSurface{Store: store, Now: func() time.Time { return now }}
	preview, err := surface.PreviewAgentIcon(ctx, transport.AgentIconCommand{PersonaID: "upload-surface", ExpectedRevision: 1, Action: "upload", Image: agentUX069PNG(t, 200, 200), ImageMediaType: "image/png"})
	if err != nil || preview.Revision != 1 || preview.ImageURL != "" {
		t.Fatal("upload preview wrote state or skipped validation", preview, err)
	}
	if _, err = surface.PreviewAgentIcon(ctx, transport.AgentIconCommand{PersonaID: "upload-surface", ExpectedRevision: 1, Action: "upload", Image: []byte("nope"), ImageMediaType: "image/png"}); !errors.Is(err, transport.ErrAgentIconInvalid) {
		t.Fatal("invalid preview accepted", err)
	}
	reply, err := surface.ChangeAgentIcon(ctx, transport.AgentIconCommand{PersonaID: "upload-surface", ExpectedRevision: 1, Action: "upload", Image: agentUX069PNG(t, 200, 100), ImageMediaType: "image/png"})
	if err != nil || reply.Revision != 2 || !strings.HasPrefix(reply.ImageURL, transport.AgentIconPath+"/image?") || !strings.Contains(reply.ImageURL, "upload-surface") {
		t.Fatal("upload", reply, err)
	}
	served, err := surface.AgentIconImage(ctx, "upload-surface")
	if err != nil || served.MediaType != "image/jpeg" || served.Revision != 2 || len(served.Content) == 0 {
		t.Fatal("serve", served.Revision, served.MediaType, len(served.Content), err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(served.Content))
	if err != nil || format != "jpeg" || config.Width != 160 || config.Height != 160 {
		t.Fatalf("not a re-encoded square proxy: %dx%d %s %v", config.Width, config.Height, format, err)
	}
	if _, err = surface.AgentIconImage(ctx, "nobody"); !errors.Is(err, transport.ErrAgentIconDenied) {
		t.Fatal("missing image leaked", err)
	}
}
