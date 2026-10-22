package chatmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
)

// TestTodo_CHATBUG_095_Integration drives the media route end to end: an
// image and an animated GIF upload with their stored dimensions, the member
// mints a grant and reads servable picture bytes under it, and a reader the
// post authority does not know is refused at the grant and at the read.
func TestTodo_CHATBUG_095_Integration(t *testing.T) {
	now := time.Unix(100, 0)
	store, err := core.NewFilesystemStore(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}
	// The membership rule the application wires in: only participants of the
	// conversation may mint or use a grant for its artifacts.
	authorize := func(_ context.Context, req core.AccessRequest) error {
		if req.PrincipalID != "member" || req.ConversationID != "room" {
			return core.ErrUnauthorized
		}
		return nil
	}
	service := core.New(core.Config{Store: store, Scanner: mediaScanner{}, Now: func() time.Time { return now }, Authorize: authorize})
	principal := "member"
	h := NewHandler(service, func(*http.Request) (string, string, string, bool) { return "tenant", "room", principal, true })

	still := chatbug095PNG(t, 640, 480)
	stillRef, err := service.Upload(context.Background(), core.UploadRequest{TenantID: "tenant", ConversationID: "room", PrincipalID: "member", DeclaredType: "image/png", Content: still})
	if err != nil {
		t.Fatal(err)
	}
	if stillRef.Width != 640 || stillRef.Height != 480 {
		t.Fatalf("stored still dimensions=%dx%d", stillRef.Width, stillRef.Height)
	}
	frame := image.NewPaletted(image.Rect(0, 0, 120, 90), color.Palette{color.Black, color.White})
	var animation bytes.Buffer
	if err := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 20}}); err != nil {
		t.Fatal(err)
	}
	gifRef, err := service.Upload(context.Background(), core.UploadRequest{TenantID: "tenant", ConversationID: "room", PrincipalID: "member", DeclaredType: "image/gif", Content: animation.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	if gifRef.Width != 120 || gifRef.Height != 90 {
		t.Fatalf("stored gif dimensions=%dx%d", gifRef.Width, gifRef.Height)
	}

	get := func(path, grant string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		if grant != "" {
			r.Header.Set("X-Chat-Media-Grant", grant)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	grantFor := func(id string) string {
		t.Helper()
		w := get("/v1/chat/media/"+id+"/grant", "")
		if w.Code != 200 {
			t.Fatalf("grant for %s status=%d body=%s", id, w.Code, w.Body.String())
		}
		var answer struct {
			Grant string `json:"grant"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil || answer.Grant == "" {
			t.Fatalf("grant answer=%s err=%v", w.Body.String(), err)
		}
		return answer.Grant
	}

	// A stranger the post authority refuses gets no grant and reads nothing.
	principal = "stranger"
	if w := get("/v1/chat/media/"+stillRef.ArtifactID+"/grant", ""); w.Code != 403 {
		t.Fatalf("stranger grant status=%d", w.Code)
	}
	principal = "member"
	memberGrant := grantFor(stillRef.ArtifactID)
	principal = "stranger"
	if w := get("/v1/chat/media/"+stillRef.ArtifactID+"?variant=thumbnail", memberGrant); w.Code != 403 {
		t.Fatalf("stranger read with a member grant status=%d", w.Code)
	}
	principal = "member"

	// The member's thumbnail is servable picture bytes at a decodable size.
	w := get("/v1/chat/media/"+stillRef.ArtifactID+"?variant=thumbnail", memberGrant)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/") {
		t.Fatalf("thumbnail status=%d type=%s", w.Code, w.Header().Get("Content-Type"))
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	if err != nil || config.Width != 640 || config.Height != 480 {
		t.Fatalf("thumbnail decodes to %dx%d err=%v", config.Width, config.Height, err)
	}
	// The GIF variant serves the original animation unchanged.
	gifGrant := grantFor(gifRef.ArtifactID)
	w = get("/v1/chat/media/"+gifRef.ArtifactID+"?variant=thumbnail", gifGrant)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/gif" || !bytes.Equal(w.Body.Bytes(), animation.Bytes()) {
		t.Fatalf("gif variant status=%d type=%s bytes=%d", w.Code, w.Header().Get("Content-Type"), w.Body.Len())
	}
}

func chatbug095PNG(t *testing.T, width, height int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 40, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
