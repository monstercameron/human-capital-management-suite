package productclient

import (
	"errors"
	"strings"
	"testing"
)

func TestChatNoticeStorageIsViewerScopedAndClosed(t *testing.T) {
	keyA, keyB := ChatNoticesStorageKey(strings.Repeat("a", 64)), ChatNoticesStorageKey(strings.Repeat("b", 64))
	if keyA == "" || keyB == "" || keyA == keyB {
		t.Fatalf("viewer-scoped keys = %q and %q", keyA, keyB)
	}
	value, ok := EncodeChatNotices(ChatNoticeTranslationBar)
	if !ok {
		t.Fatal("the translation bar notice was rejected")
	}
	if err := ValidateBrowserStorageWrite(keyA, value); err != nil {
		t.Fatalf("valid dismissed-notice record rejected: %v", err)
	}
	if err := ValidateBrowserStorageRead(keyA); err != nil {
		t.Fatalf("dismissed-notice key rejected for read: %v", err)
	}
	if notice, ok := DecodeChatNotices(value); !ok || notice != ChatNoticeTranslationBar {
		t.Fatalf("decoded notice = %q/%v", notice, ok)
	}
}

func TestChatNoticeStorageRejectsAnythingElse(t *testing.T) {
	key := ChatNoticesStorageKey(strings.Repeat("c", 64))
	for _, raw := range []string{"", "1", "translation-bar,other", `{"tenant":"tenant-a"}`, strings.Repeat("x", 200)} {
		if err := ValidateBrowserStorageWrite(key, raw); !errors.Is(err, ErrBrowserStateValue) {
			t.Errorf("record %q error = %v, want bounded value rejection", raw, err)
		}
	}
	for _, bad := range []string{"tenant-a", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if ChatNoticesStorageKey(bad) != "" {
			t.Errorf("identity-shaped scope %q produced a key", bad)
		}
		if err := ValidateBrowserStorageRead(ChatNoticesStorageKeyPrefix + bad); !errors.Is(err, ErrBrowserStateAuthority) {
			t.Errorf("identity-derived read key %q error = %v", bad, err)
		}
	}
	if _, ok := EncodeChatNotices("anything"); ok {
		t.Fatal("an unknown notice was encoded")
	}
}
