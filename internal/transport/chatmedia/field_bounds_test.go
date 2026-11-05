package chatmedia

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// TestTodo_CHAT_036_OverlongAccessibilityFieldIsRefused pins the boundary's
// own field bound: a transcript or alt text past MaxUploadFieldBytes is
// refused, not silently truncated to the bound. Truncation rewrites the
// author's accessibility text into a shorter text nobody wrote, and the
// collaboration service stores whatever it is handed verbatim.
func TestTodo_CHAT_036_OverlongAccessibilityFieldIsRefused(t *testing.T) {
	now := time.Unix(100, 0)
	for _, tc := range []struct {
		name  string
		field string
		value string
		want  int
	}{
		{"transcript at the bound is kept", "transcript", strings.Repeat("w", MaxUploadFieldBytes), http.StatusOK},
		{"transcript one over is refused", "transcript", strings.Repeat("w", MaxUploadFieldBytes+1), http.StatusBadRequest},
		{"alt text at the bound is kept", "alt_text", strings.Repeat("w", MaxUploadFieldBytes), http.StatusOK},
		{"alt text one over is refused", "alt_text", strings.Repeat("w", MaxUploadFieldBytes+1), http.StatusBadRequest},
	} {
		h, _ := testHandler(&now)
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField(tc.field, tc.value)
		fh := make(textproto.MIMEHeader)
		fh.Set("Content-Disposition", `form-data; name="file"; filename="x.png"`)
		fh.Set("Content-Type", "image/png")
		part, _ := mw.CreatePart(fh)
		_, _ = part.Write(pngBytes("pixels"))
		_ = mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/media/upload", &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s: status = %d, want %d (%s)", tc.name, w.Code, tc.want, w.Body.String())
		}
	}
}
