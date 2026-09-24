package productclient

import (
	"errors"
	"strings"
	"testing"
)

func TestChatVisitStorageIsBoundedAndViewerScoped(t *testing.T) {
	scopeA := strings.Repeat("a", 64)
	scopeB := strings.Repeat("b", 64)
	keyA, keyB := ChatVisitsStorageKey(scopeA), ChatVisitsStorageKey(scopeB)
	if keyA == "" || keyB == "" || keyA == keyB {
		t.Fatalf("viewer-scoped keys = %q and %q", keyA, keyB)
	}
	visits := []ChatVisit{{ConversationID: "room-1", Count: 3}, {ConversationID: "dm-2", Count: 1}}
	value, ok := EncodeChatVisits(visits)
	if !ok {
		t.Fatal("valid chat visit record was rejected")
	}
	if err := ValidateBrowserStorageWrite(keyA, value); err != nil {
		t.Fatalf("valid chat visit record rejected: %v", err)
	}
	decoded, ok := DecodeChatVisits(value)
	if !ok || len(decoded) != 2 || decoded[0] != visits[0] || decoded[1] != visits[1] {
		t.Fatalf("decoded chat visits = %#v/%v", decoded, ok)
	}
	if err := ValidateBrowserStorageRead(keyA); err != nil {
		t.Fatalf("chat visit key rejected for read: %v", err)
	}
}

func TestChatVisitStorageRejectsUnboundedOrMalformedData(t *testing.T) {
	key := ChatVisitsStorageKey(strings.Repeat("c", 64))
	for _, raw := range []string{
		`[{"id":"room","count":0}]`,
		`[{"id":"room","count":-1}]`,
		`[{"id":"room","count":1000001}]`,
		`[{"id":"room","count":1},{"id":"room","count":2}]`,
		`[{"id":" room ","count":1}]`,
		`[{"id":"room\nadmin","count":1}]`,
		`{"tenant":"tenant-a"}`,
		strings.Repeat("x", MaxChatVisitPayloadBytes+1),
	} {
		if err := ValidateBrowserStorageWrite(key, raw); !errors.Is(err, ErrBrowserStateValue) {
			t.Errorf("malformed chat visit record error = %v, want bounded value rejection", err)
		}
	}
	if err := ValidateBrowserStorageRead(ChatVisitsStorageKeyPrefix + "tenant-a"); !errors.Is(err, ErrBrowserStateAuthority) {
		t.Errorf("identity-derived read key error = %v", err)
	}
	if _, ok := EncodeChatVisits(make([]ChatVisit, MaxChatVisitEntries+1)); ok {
		t.Fatal("oversized chat visit set was encoded")
	}
}
