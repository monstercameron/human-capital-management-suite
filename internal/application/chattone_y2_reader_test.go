package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestChattoneRewordDirectHook: a direct or group conversation is never asked to
// be reworded, whatever the setting; a failed lookup fails the decision.
func TestChattoneRewordDirectHook(t *testing.T) {
	ctx := context.Background()
	store := newChattoneRewordFake()
	_, _ = store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "tenant", Mode: "on", MembersMayViewOriginal: false, UpdatedBy: "admin"})
	apply := func(direct func(context.Context, string, string) (bool, error)) (chatrender.Policy, error) {
		policy := chattonePolicy()
		err := (ChattoneRewordPolicy{Store: store, Direct: direct}).Apply(ctx, "tenant", "room", "dana", false, heatedBody, &policy)
		return policy, err
	}
	asked := func(p chatrender.Policy) bool {
		for _, k := range p.AllowedKinds {
			if k == chatrender.Reword {
				return true
			}
		}
		return p.RequireReworded
	}
	if p, err := apply(func(context.Context, string, string) (bool, error) { return false, nil }); err != nil || !asked(p) || !p.RequireReworded {
		t.Fatalf("a channel: %+v %v", p, err)
	}
	if p, err := apply(func(context.Context, string, string) (bool, error) { return true, nil }); err != nil || asked(p) || !p.AllowOriginal {
		t.Fatalf("a direct conversation was asked to be reworded: %+v %v", p, err)
	}
	failed := errors.New("no such conversation row")
	if _, err := apply(func(context.Context, string, string) (bool, error) { return false, failed }); !errors.Is(err, failed) {
		t.Fatalf("a failed lookup: %v", err)
	}
	// A message nothing would reword never asks where it was written.
	called := false
	calm := chattonePolicy()
	_ = (ChattoneRewordPolicy{Store: store, Direct: func(context.Context, string, string) (bool, error) { called = true; return false, nil }}).Apply(ctx, "tenant", "room", "dana", false, calmBody, &calm)
	if called {
		t.Fatal("a calm message cost a conversation lookup")
	}
}

type y2Selections struct {
	text  string
	state string
	err   error
	reads int
}

func (s *y2Selections) ReadRenderingSelection(context.Context, chatstore.RenderingScope, string) (chatrender.Rendering, chatrender.Mark, error) {
	s.reads++
	return chatrender.Rendering{Text: s.text}, chatrender.Mark{State: s.state}, s.err
}

// TestChattoneReaderProjector: who is shown what of a heated message in a channel
// whose members may not read messages as written.
func TestChattoneReaderProjector(t *testing.T) {
	ctx := context.Background()
	store := newChattoneRewordFake()
	_, _ = store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "tenant", Channel: "closed", Mode: "offered", MembersMayViewOriginal: false, UpdatedBy: "admin"})
	_, _ = store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "tenant", Channel: "open", Mode: "on", MembersMayViewOriginal: true, UpdatedBy: "admin"})
	reader := chat.Principal{TenantID: "tenant", SubjectID: "reader"}
	post := func(id, room, author, body string) chat.Post {
		return chat.Post{ID: id, TenantID: "tenant", ConversationID: room, AuthorID: author, AuthorHomeTenantID: "tenant", Body: body, Revision: 1}
	}
	project := func(selections *y2Selections, posts ...chat.Post) []chat.ReaderBody {
		return chattoneReaderProjector{Policy: &ChattoneRewordPolicy{Store: store}, Renderings: selections}.ProjectReaderBodies(ctx, reader, posts)
	}

	ready := &y2Selections{text: "reworded", state: "ready"}
	got := project(ready,
		post("heated", "closed", "dana", heatedBody),
		post("calm", "closed", "dana", calmBody),
		post("own", "closed", "reader", heatedBody),
		post("open", "open", "dana", heatedBody),
		post("elsewhere", "other", "dana", heatedBody),
		chat.Post{ID: "gone", TenantID: "tenant", ConversationID: "closed", AuthorID: "dana", Deleted: true},
	)
	want := []chat.ReaderBody{{Text: "reworded", Replaced: true}, {}, {}, {}, {}, {}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("post %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	if ready.reads != 1 {
		t.Fatalf("only the heated message of a closed channel is read for: %d reads", ready.reads)
	}

	// Not ready, not made, not readable: a neutral line, never the original.
	pending := chatui.RenderingText("", "pending")
	unavailable := chatui.RenderingText("", "unavailable")
	for name, tc := range map[string]struct {
		selections *y2Selections
		want       string
	}{
		"pending":            {&y2Selections{state: "pending"}, pending},
		"unavailable":        {&y2Selections{state: "unavailable"}, unavailable},
		"a failed read":      {&y2Selections{err: errors.New("down")}, unavailable},
		"an empty rendering": {&y2Selections{state: "ready"}, unavailable},
	} {
		got := project(tc.selections, post("heated", "closed", "dana", heatedBody))
		if len(got) != 1 || !got[0].Replaced || got[0].Text != tc.want || got[0].Text == heatedBody {
			t.Errorf("%s: %+v", name, got)
		}
	}
	// The message's own policy lets this reader read it as written: nothing hidden.
	if got := project(&y2Selections{text: heatedBody, state: "original"}, post("heated", "closed", "dana", heatedBody)); got[0].Replaced {
		t.Fatalf("a message the policy lets the reader read: %+v", got)
	}
	// No renderings composed, no settings composed.
	if got := (chattoneReaderProjector{Policy: &ChattoneRewordPolicy{Store: store}}).ProjectReaderBodies(ctx, reader, []chat.Post{post("heated", "closed", "dana", heatedBody)}); got[0].Text != unavailable {
		t.Fatalf("no renderings composed: %+v", got)
	}
	if got := (chattoneReaderProjector{}).ProjectReaderBodies(ctx, reader, []chat.Post{post("heated", "closed", "dana", heatedBody)}); got[0].Replaced {
		t.Fatalf("no reword settings composed: %+v", got)
	}
}
