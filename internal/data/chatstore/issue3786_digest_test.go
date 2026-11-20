package chatstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

func TestIssue3786_DigestPref_Integration(t *testing.T) {
	_, r := issue3733Seed(t)
	ctx := context.Background()
	if _, found, err := r.DigestPref(ctx, "host", "u-1"); err != nil || found {
		t.Fatalf("never chosen: found=%v err=%v", found, err)
	}
	seen := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	want := chatrecipient.DigestPref{Cadence: chatrecipient.DigestDaily, Minute: 485, Timezone: "Europe/Berlin", LastDigestAt: seen}
	if err := r.PutDigestPref(ctx, "host", "u-1", want); err != nil {
		t.Fatal(err)
	}
	got, found, err := r.DigestPref(ctx, "host", "u-1")
	if err != nil || !found || got.Cadence != want.Cadence || got.Minute != 485 || got.Timezone != "Europe/Berlin" || !got.LastDigestAt.Equal(seen) {
		t.Fatalf("got %+v found=%v err=%v", got, found, err)
	}
	// It moves forward only.
	if err := r.AdvanceDigest(ctx, "host", "u-1", seen.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = r.DigestPref(ctx, "host", "u-1"); !got.LastDigestAt.Equal(seen) {
		t.Fatalf("moved backwards: %v", got.LastDigestAt)
	}
	later := seen.Add(30 * time.Minute)
	if err := r.AdvanceDigest(ctx, "host", "u-1", later); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = r.DigestPref(ctx, "host", "u-1"); !got.LastDigestAt.Equal(later) {
		t.Fatalf("did not advance: %v", got.LastDigestAt)
	}
	// A bad cadence is refused by the table.
	if err := r.PutDigestPref(ctx, "host", "u-1", chatrecipient.DigestPref{Cadence: "hourly", Timezone: "UTC", LastDigestAt: later}); err == nil {
		t.Fatal("an unknown cadence was stored")
	}
}

func TestIssue3786_DigestPrefIsPrivateToThePerson_Security(t *testing.T) {
	_, r := issue3733Seed(t)
	ctx := context.Background()
	if err := r.PutDigestPref(ctx, "host", "u-1", chatrecipient.DigestPref{Cadence: chatrecipient.DigestQuietEnd, Minute: 480, Timezone: "UTC", LastDigestAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := r.DigestPref(ctx, "host", "u-2"); found {
		t.Fatal("another person reads the choice")
	}
	if _, found, _ := r.DigestPref(ctx, "other-tenant", "u-1"); found {
		t.Fatal("another company reads the choice")
	}
}

func TestIssue3786_DigestCandidates_Integration(t *testing.T) {
	_, r := issue3733Seed(t)
	rows, err := r.DigestCandidates(context.Background(), "host", "host", "u-1", time.Now().Add(-chatrecipient.DigestWindow), 50)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]chatrecipient.DigestItem{}
	for _, row := range rows {
		got[row.PostID] = row
	}
	// A muted conversation's mention is a candidate (it was held back); a
	// plain message, a direct message and everything the person cannot read are not.
	for id, kind := range map[string]string{"mention": chatrecipient.NotificationMention, "muted-mention": chatrecipient.NotificationMention, "mo-mention": chatrecipient.NotificationMention, "reply-f": chatrecipient.NotificationReply} {
		if got[id].Kind != kind || got[id].Read || got[id].AuthorID != "u-2" {
			t.Fatalf("%s = %+v", id, got[id])
		}
	}
	if !got["muted-mention"].Muted || got["mention"].Muted {
		t.Fatalf("muted flags: %+v %+v", got["muted-mention"], got["mention"])
	}
	if got["reply-f"].ThreadRootID != "root-f" || got["reply-f"].ConversationName != "ops" {
		t.Fatalf("reply = %+v", got["reply-f"])
	}
	for _, id := range []string{"plain", "mention-of-other", "own", "gone", "reply-n", "held", "dm", "away", "left", "archived", "ancient", "mo-plain"} {
		if _, ok := got[id]; ok {
			t.Fatalf("%s must not be a digest candidate", id)
		}
	}
	// Someone who is nobody in these rooms, and another company, get nothing.
	for _, who := range [][2]string{{"host", "u-3"}, {"other-tenant", "u-1"}} {
		other, err := r.DigestCandidates(context.Background(), who[0], "host", who[1], time.Now().Add(-chatrecipient.DigestWindow), 50)
		if err != nil || len(other) != 0 {
			t.Fatalf("%v sees %+v err=%v", who, other, err)
		}
	}
}
