package productclient

import (
	"errors"
	"strings"
	"testing"
)

func TestChatFirstReadStorageIsViewerScopedAndClosed(t *testing.T) {
	keyA, keyB := ChatFirstReadStorageKey(strings.Repeat("a", 64)), ChatFirstReadStorageKey(strings.Repeat("b", 64))
	if keyA == "" || keyB == "" || keyA == keyB || keyA == ChatNoticesStorageKey(strings.Repeat("a", 64)) {
		t.Fatalf("viewer-scoped keys = %q and %q", keyA, keyB)
	}
	value, ok := EncodeChatFirstRead(ChatNoticeFirstRead)
	if !ok {
		t.Fatal("the first-read notice was rejected")
	}
	if err := ValidateBrowserStorageWrite(keyA, value); err != nil {
		t.Fatalf("valid first-read record rejected: %v", err)
	}
	if err := ValidateBrowserStorageRead(keyA); err != nil {
		t.Fatalf("first-read key rejected for read: %v", err)
	}
	if notice, ok := DecodeChatFirstRead(value); !ok || notice != ChatNoticeFirstRead {
		t.Fatalf("decoded notice = %q/%v", notice, ok)
	}
	for _, raw := range []string{"", "1", "first-read,other", `{"tenant":"tenant-a"}`} {
		if err := ValidateBrowserStorageWrite(keyA, raw); !errors.Is(err, ErrBrowserStateValue) {
			t.Errorf("record %q error = %v, want bounded value rejection", raw, err)
		}
	}
	for _, bad := range []string{"tenant-a", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if ChatFirstReadStorageKey(bad) != "" {
			t.Errorf("identity-shaped scope %q produced a key", bad)
		}
		if err := ValidateBrowserStorageRead(ChatFirstReadStorageKeyPrefix + bad); !errors.Is(err, ErrBrowserStateAuthority) {
			t.Errorf("identity-derived read key %q error = %v", bad, err)
		}
		if err := ValidateBrowserStorageWrite(ChatFirstReadStorageKeyPrefix+bad, ChatNoticeFirstRead); !errors.Is(err, ErrBrowserStateAuthority) {
			t.Errorf("identity-derived write key %q error = %v", bad, err)
		}
	}
	if _, ok := EncodeChatFirstRead("anything"); ok {
		t.Fatal("an unknown notice was encoded")
	}
}
