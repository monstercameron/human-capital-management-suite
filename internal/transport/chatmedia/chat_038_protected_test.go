package chatmedia

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	core "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

type chat038Scanner struct{}

func (chat038Scanner) Scan(context.Context, string, io.Reader) (quarantine.Verdict, error) {
	return quarantine.Verdict{Safe: true}, nil
}

func TestTodo_CHAT_038(t *testing.T) {
	for _, tc := range []struct {
		header string
		start  int64
		end    *int64
		valid  bool
	}{
		{header: "", start: 0, valid: true},
		{header: "bytes=0-0", start: 0, end: rangeEnd(1), valid: true},
		{header: "bytes=12-", start: 12, valid: true},
		{header: "bytes=0-9223372036854775807", valid: false},
		{header: "bytes=3-2", valid: false},
		{header: "bytes=1", valid: false},
		{header: "bytes=-4", valid: false},
		{header: "bytes=1-2,4-5", valid: false},
		{header: "bytes=1-2-3", valid: false},
		{header: "bytes=+1-2", valid: false},
		{header: "bytes=1- 2", valid: false},
	} {
		t.Run(tc.header, func(t *testing.T) {
			start, end, err := byteRange(tc.header)
			if tc.valid {
				if err != nil || start != tc.start || !equalRangeEnd(end, tc.end) {
					t.Fatalf("byteRange(%q) = (%d, %v, %v)", tc.header, start, end, err)
				}
				return
			}
			if !errors.Is(err, core.ErrRange) {
				t.Fatalf("byteRange(%q) error = %v, want ErrRange", tc.header, err)
			}
		})
	}
}

func TestTodo_CHAT_038_Security(t *testing.T) {
	now := time.Unix(100, 0)
	h, service := testHandler(&now)
	ref, err := service.Upload(context.Background(), core.UploadRequest{
		TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: chat038PNG(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Authorize(context.Background(), core.AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request := func(token, variant, rangeHeader string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/v1/media/"+ref.ArtifactID+"?variant="+variant, nil)
		r.Header.Set("X-Chat-Media-Grant", token)
		if rangeHeader != "" {
			r.Header.Set("Range", rangeHeader)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if got := request("forged", core.VariantThumbnail, "bytes=0-3"); got.Code != 403 {
		t.Fatalf("forged thumbnail range status=%d", got.Code)
	}
	if got := request(grant.Token, core.VariantThumbnail, "bytes=0-3"); got.Code != 206 || got.Body.Len() != 4 {
		t.Fatalf("authorized thumbnail range status=%d bytes=%d", got.Code, got.Body.Len())
	}
	service.Revoke(ref.ArtifactID)
	for _, variant := range []string{"", core.VariantThumbnail, core.VariantDisplay} {
		if got := request(grant.Token, variant, "bytes=0-3"); got.Code != 403 {
			t.Fatalf("revoked variant %q range status=%d", variant, got.Code)
		}
	}
}

func TestTodo_CHAT_038_Integration(t *testing.T) {
	now := time.Unix(100, 0)
	store, err := core.NewFilesystemStore(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}
	service := core.New(core.Config{
		Store: store, Scanner: chat038Scanner{}, Now: func() time.Time { return now },
		Authorize: func(context.Context, core.AccessRequest) error { return nil },
	})
	h := NewHandler(service, func(*http.Request) (string, string, string, bool) { return "t", "c", "p", true })
	ref, err := service.Upload(context.Background(), core.UploadRequest{
		TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: chat038PNG(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Authorize(context.Background(), core.AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/v1/media/"+ref.ArtifactID+"?variant=thumbnail", nil)
	r.Header.Set("X-Chat-Media-Grant", grant.Token)
	r.Header.Set("Range", "bytes=0-7")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.Len() != 8 || w.Header().Get("Content-Range") == "" {
		t.Fatalf("filesystem thumbnail range status=%d bytes=%d headers=%v", w.Code, w.Body.Len(), w.Header())
	}
	r = httptest.NewRequest("GET", "/v1/media/"+ref.ArtifactID+"?variant=thumbnail", nil)
	r.Header.Set("X-Chat-Media-Grant", grant.Token)
	r.Header.Set("Range", "bytes=0-900000000")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("filesystem oversized range status=%d", w.Code)
	}
	service.Revoke(ref.ArtifactID)
	for _, variant := range []string{"", core.VariantThumbnail, core.VariantDisplay} {
		r = httptest.NewRequest("GET", "/v1/media/"+ref.ArtifactID+"?variant="+variant, nil)
		r.Header.Set("X-Chat-Media-Grant", grant.Token)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("filesystem revoked variant %q status=%d", variant, w.Code)
		}
	}
}

func chat038PNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	im := image.NewNRGBA(image.Rect(0, 0, 800, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 800; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 40, A: 255})
		}
	}
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func rangeEnd(n int64) *int64 { return &n }

func equalRangeEnd(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
