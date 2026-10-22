package application

import (
	"context"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// chattoneY2Rig is the served reword path over the test PostgreSQL, with the
// recorded (fake) rewrite model answering from the given recording. Nothing in
// it can reach a paid model.
func chattoneY2Rig(t *testing.T, recorded []ChattoneRewordRecorded) (*chatlangRig, func(mode string, membersMayView bool)) {
	t.Helper()
	ctx := context.Background()
	rig := newChatlangRig(t)
	reworder, err := NewChattoneReworder(NewChattoneRewordReplay(recorded), chattoneAllowAllPolicy{}, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == EnvChatTranslationEngine {
			return "fixture"
		}
		return ""
	}
	runtime, reason, err := composeServedChatlang(ctx, chatlangServeInput{Config: ServeConfig{Profile: ServeProfileLocalDev}, Chat: rig.chat, Env: env, Tenants: []string{"host"}, Reworder: reworder})
	if err != nil || runtime == nil {
		t.Fatalf("composed: %v %q", err, reason)
	}
	rig.runtime = runtime
	setting := func(mode string, membersMayView bool) {
		t.Helper()
		if _, err := rig.chat.store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "host", Mode: mode, MembersMayViewOriginal: membersMayView, UpdatedBy: "alice"}); err != nil {
			t.Fatal(err)
		}
	}
	return rig, setting
}

// chattoneY2Bodies lists what a person's list of the conversation hands them
// (the thread replies come in the same list).
func chattoneY2Bodies(t *testing.T, rig *chatlangRig, subject string) map[string]string {
	t.Helper()
	page, err := rig.chat.service.ListPosts(rig.as(subject), chatcore.ListPostsRequest{Principal: rig.person(subject), TenantID: "host", ConversationID: rig.room, Page: chatcore.Page{PageSize: 50}})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, post := range page.Posts {
		out[post.ID] = post.Body
	}
	return out
}

// chattoneY2Event waits for the live event of one post.
func chattoneY2Event(t *testing.T, events <-chan chatcore.WatchEvent, id string, wait context.Context) chatcore.Post {
	t.Helper()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("the stream ended before the message arrived")
			}
			if event.Event.Post != nil && event.Event.Post.ID == id {
				return *event.Event.Post
			}
		case <-wait.Done():
			t.Fatal("no live event arrived")
		}
	}
}

// TestChattoneRewordNoOriginalLeak_Integration sends a heated message into a
// channel whose administrator does not let members view messages as written and
// reads it every way a member can: their list, the live stream, search, a thread,
// a link preview and their saved messages. None of them carries the original; the
// writer's carry it.
func TestChattoneRewordNoOriginalLeak_Integration(t *testing.T) {
	const heatedReply = "You idiot, you broke it again."
	rig, setting := chattoneY2Rig(t, chattoneRewordReplayFixture(t))
	setting(chatstore.RewordModeOffered, false)

	wait, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	memberCtx, memberStop := context.WithCancel(rig.as("bruno"))
	defer memberStop()
	writerCtx, writerStop := context.WithCancel(rig.as("alice"))
	defer writerStop()
	memberEvents, err := rig.chat.service.WatchConversation(memberCtx, chatcore.WatchConversationRequest{Principal: rig.person("bruno"), TenantID: "host", ConversationID: rig.room})
	if err != nil {
		t.Fatal(err)
	}
	writerEvents, err := rig.chat.service.WatchConversation(writerCtx, chatcore.WatchConversationRequest{Principal: rig.person("alice"), TenantID: "host", ConversationID: rig.room})
	if err != nil {
		t.Fatal(err)
	}

	heated := rig.send("heated", w10Heated)
	reply, err := rig.chat.service.SendPost(rig.as("alice"), chatcore.SendPostRequest{Principal: rig.person("alice"), TenantID: "host", ConversationID: rig.room, ParentID: heated.ID, Body: heatedReply, IdempotencyKey: "heated-reply"})
	if err != nil {
		t.Fatal(err)
	}

	// The live stream: the member's event is not the original; the writer's is.
	if got := chattoneY2Event(t, memberEvents, heated.ID, wait); strings.Contains(got.Body, "idiot") || got.Body == "" {
		t.Fatalf("the member's live event: %q", got.Body)
	}
	if got := chattoneY2Event(t, writerEvents, heated.ID, wait); got.Body != w10Heated {
		t.Fatalf("the writer's live event: %q", got.Body)
	}

	// The member's list asks for the rewording; once it is made, it is what they read.
	first := chattoneY2Bodies(t, rig, "bruno")
	for id, body := range first {
		if strings.Contains(body, "idiot") {
			t.Fatalf("the original reached the member's list before the rewording was ready: %s %q", id, body)
		}
	}
	rig.drain()
	member := chattoneY2Bodies(t, rig, "bruno")
	if member[heated.ID] != w10Reword {
		t.Fatalf("the member's list does not carry the reworded text: %q", member[heated.ID])
	}
	// The reply was heated too and the recording has no answer for it: it reads as
	// not available, never as what was written.
	if body := member[reply.ID]; body == "" || strings.Contains(body, "idiot") || body == heatedReply {
		t.Fatalf("the member's thread reply: %q", body)
	}
	writer := chattoneY2Bodies(t, rig, "alice")
	if writer[heated.ID] != w10Heated || writer[reply.ID] != heatedReply {
		t.Fatalf("the writer's list lost the originals: %q %q", writer[heated.ID], writer[reply.ID])
	}

	// Search: a word that only the original has finds nothing for the member, a word
	// the reworded text keeps finds it with the reworded text, and the writer finds
	// the original either way.
	search := func(subject, query string) []chatcore.SearchResult {
		t.Helper()
		found, err := rig.chat.service.Search(rig.as(subject), chatcore.SearchRequest{Principal: rig.person(subject), TenantID: "host", Query: query, Page: chatcore.Page{PageSize: 20}})
		if err != nil {
			t.Fatal(err)
		}
		return found.Results
	}
	if hits := search("bruno", "idiot"); len(hits) != 0 {
		t.Fatalf("a member's search matched the original's own word: %+v", hits)
	}
	hits := search("bruno", "deploy")
	if len(hits) != 1 || hits[0].Post.ID != heated.ID || hits[0].Post.Body != w10Reword {
		t.Fatalf("a member's search on a word the rewording keeps: %+v", hits)
	}
	if hits := search("alice", "idiot"); len(hits) != 2 {
		t.Fatalf("the writer's search lost the original: %+v", hits)
	}
	// The page's own search (the registry the search view reads) is built on the
	// reader's rendering: the original's own word finds nothing there either.
	if rows, err := rig.chat.renderings.SearchRenderings(rig.as("bruno"), rig.scope("bruno"), "idiot"); err != nil || len(rows) != 0 {
		t.Fatalf("a member's rendering search matched the original: %+v %v", rows, err)
	}

	// A link to the message (a project link's preview, a share link) shows the
	// member what they may read.
	linked := func(subject string) string {
		t.Helper()
		reader, ok := rig.chat.service.(chatcore.AuthorizedReferenceReader)
		if !ok {
			t.Fatal("the served chat service has no reference reader")
		}
		_, post, err := reader.ReadAuthorizedReference(rig.as(subject), rig.person(subject), "host", rig.room, heated.ID)
		if err != nil || post == nil {
			t.Fatalf("link read: %v", err)
		}
		return post.Body
	}
	if body := linked("bruno"); body != w10Reword {
		t.Fatalf("the member's link preview: %q", body)
	}
	if body := linked("alice"); body != w10Heated {
		t.Fatalf("the writer's link preview: %q", body)
	}

	// Saved messages show the member the same.
	saved := func(subject string) string {
		t.Helper()
		ref := chatcore.SavedRequest{Principal: rig.person(subject), TenantID: "host", ConversationID: rig.room, PostID: heated.ID}
		if _, err := rig.chat.core.SaveForLater(rig.as(subject), ref); err != nil {
			t.Fatal(err)
		}
		page, err := rig.chat.core.ListSaved(rig.as(subject), chatcore.SavedListRequest{Principal: rig.person(subject), TenantID: "host", Tab: chatcore.SavedAll, Page: chatcore.Page{PageSize: 10}})
		if err != nil || len(page.Items) != 1 || page.Items[0].Post == nil {
			t.Fatalf("saved list: %+v %v", page, err)
		}
		return page.Items[0].Post.Body
	}
	if body := saved("bruno"); body != w10Reword {
		t.Fatalf("the member's saved message: %q", body)
	}
	if body := saved("alice"); body != w10Heated {
		t.Fatalf("the writer's saved message: %q", body)
	}

	// Handing the message on would copy the original under the member's name.
	other, err := rig.chat.service.CreateConversation(rig.as("alice"), chatcore.CreateConversationRequest{Principal: rig.person("alice"), TenantID: "host", Kind: chatcore.PublicChannel, Name: "elsewhere", IdempotencyKey: "y2-elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	if forwarder, ok := rig.chat.service.(interface {
		ForwardPost(context.Context, chatcore.ForwardPostRequest) (chatcore.Post, error)
	}); ok {
		if _, err := forwarder.ForwardPost(rig.as("bruno"), chatcore.ForwardPostRequest{Principal: rig.person("bruno"), SourceAttribution: chatcore.SourceAttribution{TenantID: "host", ConversationID: rig.room, PostID: heated.ID}, DestinationConversationID: other.ID, IdempotencyKey: "y2-forward"}); err == nil {
			t.Fatal("a member forwarded a message they may not read as written")
		}
	}

	// Where members may read messages as written, nothing is hidden.
	setting(chatstore.RewordModeOffered, true)
	if body := chattoneY2Bodies(t, rig, "bruno")[heated.ID]; body != w10Heated {
		t.Fatalf("a member who may view originals: %q", body)
	}
	if hits := search("bruno", "idiot"); len(hits) != 2 {
		t.Fatalf("a member who may view originals searches the original: %+v", hits)
	}
}

// TestChattoneRewordReaderNeverEmpty: a reader whose rules let them read a heated
// message as written is never handed an empty text because the rewording could not
// be made. A reader the rules do not let read it as written gets no original and
// an "unavailable" mark the page draws its own sentence for.
func TestChattoneRewordReaderNeverEmpty(t *testing.T) {
	// A recording with no answers is a model that is down.
	rig, setting := chattoneY2Rig(t, nil)

	// Rewording on, members may view the original: the reworded view cannot be
	// made, so the reader is given the message as written, marked as a fallback.
	setting(chatstore.RewordModeOn, true)
	heated := rig.send("heated", w10Heated)
	rig.read("bruno", heated)
	rig.drain()
	got, mark := rig.read("bruno", heated)
	if got.Text != w10Heated || mark.State != "fallback" || !mark.CanShowOriginal {
		t.Fatalf("reworded view not made, original allowed: %q %+v", got.Text, mark)
	}
	wanted := false
	for _, kind := range mark.Wanted {
		wanted = wanted || kind == chatrender.Reword
	}
	if !wanted {
		t.Fatalf("the mark does not say a rewording was wanted: %+v", mark)
	}

	// Members may not view the original: nothing of it is given, and the mark says
	// why the text is empty.
	setting(chatstore.RewordModeOffered, false)
	second := rig.send("heated-2", "You idiot, the build is red again.")
	rig.read("carla", second)
	rig.drain()
	got, mark = rig.read("carla", second)
	if got.Text != "" || mark.State != "unavailable" || mark.CanShowOriginal {
		t.Fatalf("reworded view not made, original not allowed: %q %+v", got.Text, mark)
	}
	// The member's list holds a neutral line, never the original.
	if body := chattoneY2Bodies(t, rig, "carla")[second.ID]; body == "" || strings.Contains(body, "idiot") {
		t.Fatalf("the member's list: %q", body)
	}
	// The writer reads what they wrote.
	if got, _ := rig.read("alice", second); got.Text != "You idiot, the build is red again." {
		t.Fatalf("the writer: %q", got.Text)
	}
}
