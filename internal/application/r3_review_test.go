package application

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
)

func r3UploadHandler(t *testing.T, slots int) *chatattach001HTTP {
	t.Helper()
	root := t.TempDir()
	store, err := chatmedia.NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	allow := func(context.Context, chatmedia.AccessRequest) error { return nil }
	media := chatmedia.New(chatmedia.Config{Store: store, Scanner: chatmedia.Chatattach001Scanner{}, Authorize: allow, MaxBytes: chatmedia.Chatattach001MaxBytes})
	return &chatattach001HTTP{
		uploads:       &chatmedia.Chatattach001Uploads{Media: media, Root: root, Authorize: allow},
		principal:     func(*http.Request) (string, string, string, bool) { return "tenant", "room", "alice", true },
		readAuthorize: allow,
		slots:         make(chan struct{}, slots),
	}
}

func r3UploadRequest(t *testing.T) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("hello"))
	_ = form.Close()
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/media/upload?host_tenant_id=tenant&conversation_id=room", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	return r
}

// The page shows status 429 as "the attachment storage limit is reached" and
// drops the file, so a busy server must not use it, and a short queue must not
// turn away the sixth of ten files chosen together.
func TestR3_UploadWaitsForASlotInsteadOfAnswering429(t *testing.T) {
	h := r3UploadHandler(t, 1)
	h.slots <- struct{}{}
	go func() {
		time.Sleep(60 * time.Millisecond)
		<-h.slots
	}()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r3UploadRequest(t))
	if w.Code != http.StatusOK {
		t.Fatalf("a queued upload answered %d %q", w.Code, w.Body.String())
	}
}

func TestR3_BusyServerAnswersRetryable503NotQuota429(t *testing.T) {
	old := chatattach001SlotWait
	chatattach001SlotWait = 20 * time.Millisecond
	t.Cleanup(func() { chatattach001SlotWait = old })
	h := r3UploadHandler(t, 1)
	h.slots <- struct{}{}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r3UploadRequest(t))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Fatalf("busy answered %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
}
