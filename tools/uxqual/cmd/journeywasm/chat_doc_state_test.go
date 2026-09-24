package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatDocPreviewClaimBatchesAndCaches(t *testing.T) {
	origin := "https://hcm.example"
	model := chatui.Model{EmbedOrigin: origin, Draft: "draft doc:d3",
		Messages: []chatui.Message{{ID: "m1", Body: "old doc:d1 and doc:d2"}, {ID: "m2", Body: "new " + origin + "/workspace/app/docs?document=d1"}}}
	ids := chatDocPreviewIDs(model)
	if strings.Join(ids, ",") != "d3,d1,d2" {
		t.Fatalf("ids = %v", ids)
	}
	cache := &chatDocPreviewCache{}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	claims, epoch, shown := cache.claim("t\x00alice", ids, now)
	if len(claims) != 3 || shown["d1"].State != "loading" {
		t.Fatalf("first claim = %v %+v", claims, shown)
	}
	// In flight: a second render claims nothing more.
	if again, _, _ := cache.claim("t\x00alice", ids, now); len(again) != 0 {
		t.Fatalf("pending re-claimed %v", again)
	}
	answers := map[string]chatui.DocPreview{
		"d1": chatDocPreviewAnswer("d1", true, "Guide", "Ana", "Sep 1, 2026", "Welcome"),
		"d2": chatDocPreviewAnswer("d2", false, "Secret plan", "Boss", "x", "salary"),
		"d3": {ID: "d3", State: "unavailable"},
	}
	if !cache.finish("t\x00alice", epoch, answers, now) {
		t.Fatal("finish refused")
	}
	if d2 := cache.snapshot("t\x00alice")["d2"]; d2.Readable || d2.Title != "" || d2.Owner != "" || d2.Snippet != "" || d2.State != "ready" {
		t.Fatalf("unreadable preview kept %+v", d2)
	}
	if fresh, _, shown := cache.claim("t\x00alice", ids, now.Add(30*time.Second)); len(fresh) != 0 || shown["d1"].Title != "Guide" {
		t.Fatalf("fresh claim = %v %+v", fresh, shown)
	}
	// Stale answers are read again, showing the last answer meanwhile.
	stale, _, shown := cache.claim("t\x00alice", ids, now.Add(2*time.Minute))
	if len(stale) != 3 || shown["d1"].Title != "Guide" {
		t.Fatalf("stale claim = %v %+v", stale, shown)
	}
	// Another viewer starts empty, and the old viewer's read cannot land.
	if other, _, shown := cache.claim("t\x00bob", ids, now); len(other) != 3 || shown["d1"].Title != "" {
		t.Fatalf("persona switch reused answers: %v %+v", other, shown)
	}
	if cache.finish("t\x00alice", epoch, answers, now) {
		t.Fatal("a read for the previous viewer landed")
	}
}

func TestChatDocPreviewMergeAndFingerprint(t *testing.T) {
	model := chatui.Model{SelectedID: "room", EmbedOrigin: "https://hcm.example", Messages: []chatui.Message{{Body: "doc:d1"}}}
	before := chatDocPreviewFingerprint(model)
	old := model.DocPreviews
	if !mergeChatDocPreviews(&model, map[string]chatui.DocPreview{"d1": {ID: "d1", State: "ready", Readable: true, Title: "Guide"}}) {
		t.Fatal("merge reported no change")
	}
	if old != nil || model.DocPreviews["d1"].Title != "Guide" {
		t.Fatalf("merge = %+v", model.DocPreviews)
	}
	snapshot := model.DocPreviews
	if mergeChatDocPreviews(&model, map[string]chatui.DocPreview{"d1": {ID: "d1", State: "ready", Readable: true, Title: "Guide"}}) {
		t.Fatal("an unchanged merge asked for a render")
	}
	mergeChatDocPreviews(&model, map[string]chatui.DocPreview{"d1": {ID: "d1", State: "ready"}})
	if snapshot["d1"].Title != "Guide" {
		t.Fatal("merge wrote into a map a render may hold")
	}
	if after := chatDocPreviewFingerprint(model); after == before {
		t.Fatal("fingerprint ignores resolved previews")
	}
}

func TestChatPersonFragmentSubject(t *testing.T) {
	for hash, want := range map[string]string{"#person=hc-050-rafael-torres": "hc-050-rafael-torres", "#person=a%40b": "a@b"} {
		if got, ok := chatPersonFragmentSubject(hash); !ok || got != want {
			t.Fatalf("%q = %q %v", hash, got, ok)
		}
	}
	for _, hash := range []string{"#channel=x", "#person=", "#person=%3Cscript%3E", "#person=%20x", "#person=%zz"} {
		if _, ok := chatPersonFragmentSubject(hash); ok {
			t.Fatalf("%q accepted", hash)
		}
	}
	f := &chatPersonFragment{}
	if !f.claim("a") || f.claim("a") || f.claim("") || !f.claim("a") {
		t.Fatal("claim does not open each address once")
	}
}

func TestDocsSuggestRanking(t *testing.T) {
	people := []chatui.SearchPerson{{ID: "hc-1", Name: "Rafael Torres"}, {ID: "hc-2", Name: "Sam Lee"}, {ID: "hc-3", Name: "Sam Lee"}, {ID: "hc-4", Name: "Ana Rafaela"}}
	rows := docsSuggestPeople(people, "raf")
	if len(rows) != 2 || rows[0].ID != "hc-1" || rows[0].Insert != "@rafael.torres" || rows[1].ID != "hc-4" {
		t.Fatalf("people = %+v", rows)
	}
	sam := docsSuggestPeople(people, "sam")
	if len(sam) != 2 || sam[0].Insert != "@"+sam[0].ID || sam[1].Insert != "@"+sam[1].ID {
		t.Fatalf("shared handle rows = %+v", sam)
	}
	rooms := []chatui.Conversation{
		{ID: "c1", Name: "people-ops", Kind: chatui.PublicChannel, MemberCount: 12},
		{ID: "c2", Name: "people-leads", Kind: chatui.PrivateChannel, MemberCount: 3, Joined: true},
		{ID: "dm", Name: "people", Kind: chatui.DirectMessage},
	}
	channels := docsSuggestChannels(rooms, "peo", "{count} members")
	if len(channels) != 2 || channels[0].ID != "c2" || channels[0].Insert != "[#people-leads](channel:c2)" || channels[1].Detail != "12 members" {
		t.Fatalf("channels = %+v", channels)
	}
	if none := docsSuggestChannels(rooms, "zzz", ""); len(none) != 0 {
		t.Fatalf("no match = %+v", none)
	}
}
