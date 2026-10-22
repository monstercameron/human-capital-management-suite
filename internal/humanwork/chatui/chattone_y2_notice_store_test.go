package chatui

import "testing"

func TestFirstReadNoticeStore(t *testing.T) {
	saved := FirstReadNoticeStore
	defer func() { FirstReadNoticeStore = saved }()

	FirstReadNoticeStore.Seen, FirstReadNoticeStore.Remember = nil, nil
	if chatlangNoticeStoredSeen() {
		t.Fatal("a missing store said the notice was seen")
	}
	chatlangRememberNoticeSeen()

	seen := false
	FirstReadNoticeStore.Seen = func() bool { return seen }
	FirstReadNoticeStore.Remember = func() { seen = true }
	if chatlangNoticeStoredSeen() {
		t.Fatal("the notice was seen before it was answered")
	}
	chatlangRememberNoticeSeen()
	if !chatlangNoticeStoredSeen() {
		t.Fatal("an answered notice was not remembered")
	}
}
