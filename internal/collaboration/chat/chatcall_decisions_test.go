package chat

import (
	"strings"
	"testing"
)

// TestTodo_CHATCALL_001 pins the scope record: call kinds with ceilings, who
// may start where, the history lines, ringing rules, the recording default,
// the out-of-scope list and the boundary with voice messages.
func TestTodo_CHATCALL_001(t *testing.T) {
	decisions := CallDecisions()
	if len(decisions) != 7 {
		t.Fatalf("decisions = %d, want 7", len(decisions))
	}
	wantTopics := []string{
		"call kinds in delivery order and their ceilings",
		"who may start a call where",
		"what a call leaves behind",
		"presence, ringing, quiet hours and do-not-disturb",
		"recording, live captions and transcripts",
		"what is explicitly out of scope",
		"boundary with voice messages",
	}
	for i, d := range decisions {
		if d.Number != i+1 {
			t.Fatalf("decision %d has number %d", i+1, d.Number)
		}
		if d.Topic != wantTopics[i] {
			t.Fatalf("decision %d topic = %q, want %q", i+1, d.Topic, wantTopics[i])
		}
		if strings.TrimSpace(d.Decision) == "" || strings.TrimSpace(d.Rejected) == "" || strings.TrimSpace(d.Why) == "" {
			t.Fatalf("decision %d is missing its proposal, rejection or reason: %+v", i+1, d)
		}
	}
	record := RenderCallDecisions()
	for _, phrase := range []string{
		"Call started", "Call ended", "Missed call",
		"California", "Germany", "works council",
		"phone numbers", "external guests", "webinars",
		"CHATVOICE-001",
	} {
		if !strings.Contains(record, phrase) {
			t.Fatalf("record does not pin %q", phrase)
		}
	}
	// Decision 7 cites the voice rule it borders: a voice message is a
	// self-recording of at most two minutes, never of other people.
	if !strings.Contains(VoiceDecisionRecord(), "at most two minutes") {
		t.Fatal("the voice record no longer states the boundary this record cites")
	}

	kinds := CallKinds()
	if len(kinds) != 4 {
		t.Fatalf("call kinds = %d, want 4", len(kinds))
	}
	byID := map[string]CallKind{}
	for _, k := range kinds {
		byID[k.ID] = k
		for _, where := range k.Where {
			switch where {
			case Direct, Group, PublicChannel, PrivateChannel:
			default:
				t.Fatalf("kind %s admits calls in %q, which is not a conversation kind", k.ID, where)
			}
		}
	}
	oneVoice, ok := byID["ONE_TO_ONE_VOICE"]
	if !ok || oneVoice.MaxParticipants != 2 || oneVoice.MaxSharers != 0 {
		t.Fatalf("one-to-one voice kind = %+v", oneVoice)
	}
	oneVideo, ok := byID["ONE_TO_ONE_VIDEO"]
	if !ok || oneVideo.MaxParticipants != 2 || oneVideo.MaxSharers != 0 {
		t.Fatalf("one-to-one video kind = %+v", oneVideo)
	}
	for _, id := range []string{"ONE_TO_ONE_VOICE", "ONE_TO_ONE_VIDEO"} {
		k := byID[id]
		if !containsKind(k.Where, Direct) || !containsKind(k.Where, Group) {
			t.Fatalf("kind %s is not offered in direct and group conversations: %v", id, k.Where)
		}
	}
	dropin, ok := byID["CHANNEL_DROP_IN"]
	if !ok || dropin.MaxParticipants != 25 {
		t.Fatalf("channel drop-in kind = %+v", dropin)
	}
	for _, want := range []ConversationKind{Group, PublicChannel, PrivateChannel} {
		if !containsKind(dropin.Where, want) {
			t.Fatalf("drop-in is not offered in %s: %v", want, dropin.Where)
		}
	}
	if containsKind(dropin.Where, Direct) {
		t.Fatalf("a drop-in call in a two-person conversation is a one-to-one call: %v", dropin.Where)
	}
	share, ok := byID["SCREEN_SHARE"]
	if !ok || share.MaxParticipants != 0 || share.MaxSharers != 1 {
		t.Fatalf("screen sharing is one sharer inside a call, got %+v", share)
	}

	if CallStartedLine != "Call started" {
		t.Fatalf("CallStartedLine = %q", CallStartedLine)
	}
	if got := FormatCallEndedLine(12, 3); got != "Call ended · 12 min · 3 people" {
		t.Fatalf("FormatCallEndedLine(12, 3) = %q", got)
	}
	if got := FormatCallEndedLine(0, 0); got != "Call ended" {
		t.Fatalf("an undetailed end line must stay a plain end line, got %q", got)
	}

	decisions[0].Topic = "changed"
	if CallDecisions()[0].Topic == "changed" {
		t.Fatal("the record is mutable by a caller")
	}
	kinds[0].Name = "changed"
	if CallKinds()[0].Name == "changed" {
		t.Fatal("the kinds are mutable by a caller")
	}
}

func containsKind(kinds []ConversationKind, want ConversationKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

func TestTodo_CHATCALL_001_Golden(t *testing.T) {
	const digest = "9051e33ed4393a36737d6c90836838b342cbba54551bd395f351ec7df55305e5"
	if got := strings.Count(RenderCallDecisions(), "\n"); got < 7 {
		t.Fatalf("record has %d lines", got)
	}
	if CallDecisionsDigest() != digest {
		t.Fatalf("the decision record changed; digest is now %s", CallDecisionsDigest())
	}
}
