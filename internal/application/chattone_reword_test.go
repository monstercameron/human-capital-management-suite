package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// chattoneRewordFake is an in-memory stand-in for the chat store's reword
// tables with the same validation the store applies.
type chattoneRewordFake struct {
	settings map[string]map[string]chatstore.RewordSetting
	choices  map[string]map[string]string
	saves    int
}

func newChattoneRewordFake() *chattoneRewordFake {
	return &chattoneRewordFake{settings: map[string]map[string]chatstore.RewordSetting{}, choices: map[string]map[string]string{}}
}

func (f *chattoneRewordFake) LoadRewordSettings(_ context.Context, tenant string) ([]chatstore.RewordSetting, error) {
	var out []chatstore.RewordSetting
	for _, row := range f.settings[tenant] {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out, nil
}
func (f *chattoneRewordFake) SaveRewordSetting(_ context.Context, in chatstore.RewordSetting) (int64, error) {
	if in.Mode != "off" && in.Mode != "offered" && in.Mode != "on" || in.Tenant == "" || in.UpdatedBy == "" {
		return 0, chatstore.ErrRewordInvalid
	}
	if f.settings[in.Tenant] == nil {
		f.settings[in.Tenant] = map[string]chatstore.RewordSetting{}
	}
	f.settings[in.Tenant][in.Channel] = in
	f.saves++
	return 1, nil
}
func (f *chattoneRewordFake) DeleteRewordOverride(_ context.Context, tenant, channel string) error {
	delete(f.settings[tenant], channel)
	return nil
}
func (f *chattoneRewordFake) LoadReaderChoices(_ context.Context, tenant, person string) ([]chatstore.ReaderChoice, error) {
	var out []chatstore.ReaderChoice
	for channel, tone := range f.choices[tenant+"|"+person] {
		out = append(out, chatstore.ReaderChoice{Channel: channel, Tone: tone})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out, nil
}
func (f *chattoneRewordFake) SaveReaderChoice(_ context.Context, tenant, person, channel, tone string) error {
	if tone != "as-written" && tone != "reworded" {
		return chatstore.ErrRewordInvalid
	}
	key := tenant + "|" + person
	if f.choices[key] == nil {
		f.choices[key] = map[string]string{}
	}
	f.choices[key][channel] = tone
	return nil
}
func (f *chattoneRewordFake) DeleteReaderChoice(_ context.Context, tenant, person, channel string) error {
	delete(f.choices[tenant+"|"+person], channel)
	return nil
}

type chattoneRewordAdmin struct{ allow bool }

func (a chattoneRewordAdmin) ManageWritingStyles(context.Context, chatrewrite.Identity) error {
	if a.allow {
		return nil
	}
	return personachat.ErrDenied
}

func chattoneRewordService(t *testing.T, admin bool) (*ChattoneService, context.Context, *chattoneRewordFake) {
	t.Helper()
	service, ctx, _, _, _, _ := chattoneFixture(t)
	store := newChattoneRewordFake()
	service.Reword = &ChattoneRewordPolicy{Store: store}
	service.Administration = chattoneRewordAdmin{allow: admin}
	return service, ctx, store
}

const (
	heatedBody = "You idiot, fix the build."
	calmBody   = "Please fix the build when you can."
)

func chattonePolicy() chatrender.Policy {
	return chatrender.Policy{Original: chatrender.Rendering{Tenant: "tenant", Message: "m1", Revision: 1, Tone: chatrender.AsWritten, Text: heatedBody, Language: "en", SourceLanguage: "en"}, AllowOriginal: true, AllowedKinds: []chatrender.Kind{chatrender.Mask}}
}

// TestTodo_CHATTONE_003: the administrator's two settings, the channel override
// and the writer's exemption decide, for one heated message and one reader,
// what policy applies. Calm messages and "off" change nothing.
func TestTodo_CHATTONE_003(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name         string
		mode         string
		view         bool
		author       bool
		body         string
		wantKind     bool
		wantRequired bool
		wantOriginal bool
	}{
		{"off", "off", true, false, heatedBody, false, false, true},
		{"offered, readers may view", "offered", true, false, heatedBody, true, false, true},
		{"on, readers may view", "on", true, false, heatedBody, true, false, true},
		{"on, readers may not view", "on", false, false, heatedBody, true, true, false},
		{"offered, readers may not view", "offered", false, false, heatedBody, true, true, false},
		{"the writer keeps both", "on", false, true, heatedBody, true, false, true},
		{"calm message untouched", "on", false, false, calmBody, false, false, true},
	} {
		store := newChattoneRewordFake()
		_, _ = store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "tenant", Mode: tc.mode, MembersMayViewOriginal: tc.view, UpdatedBy: "admin"})
		policy := chattonePolicy()
		policy.Original.Text = tc.body
		if err := (ChattoneRewordPolicy{Store: store}).Apply(ctx, "tenant", "room", "dana", tc.author, tc.body, &policy); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		hasKind := false
		for _, k := range policy.AllowedKinds {
			hasKind = hasKind || k == chatrender.Reword
		}
		if hasKind != tc.wantKind || policy.RequireReworded != tc.wantRequired || policy.AllowOriginal != tc.wantOriginal {
			t.Errorf("%s: kind=%v required=%v original=%v, want %v %v %v", tc.name, hasKind, policy.RequireReworded, policy.AllowOriginal, tc.wantKind, tc.wantRequired, tc.wantOriginal)
		}
	}

	// A channel override replaces the workspace's choice for that channel only.
	store := newChattoneRewordFake()
	_, _ = store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "tenant", Mode: "off", MembersMayViewOriginal: true, UpdatedBy: "admin"})
	_, _ = store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "tenant", Channel: "incident", Mode: "on", MembersMayViewOriginal: false, UpdatedBy: "admin"})
	policy := ChattoneRewordPolicy{Store: store}
	incident, _ := policy.Effective(ctx, "tenant", "incident")
	other, _ := policy.Effective(ctx, "tenant", "random")
	if incident != (ChattoneRewordSettings{Mode: "on", MembersMayViewOriginal: false, Override: true}) || other != (ChattoneRewordSettings{Mode: "off", MembersMayViewOriginal: true}) {
		t.Fatalf("effective: %+v / %+v", incident, other)
	}
	if none, _ := (ChattoneRewordPolicy{Store: newChattoneRewordFake()}).Effective(ctx, "tenant", "room"); none != (ChattoneRewordSettings{Mode: "off", MembersMayViewOriginal: true}) {
		t.Fatalf("a workspace with no setting: %+v", none)
	}

	// Masked text is never reworded, and a failed read leaves nothing applied.
	masked := chattonePolicy()
	masked.RequireMask, masked.AllowOriginal = true, false
	_ = policy.Apply(ctx, "tenant", "incident", "dana", false, heatedBody, &masked)
	if masked.RequireReworded || len(masked.AllowedKinds) != 1 {
		t.Fatalf("masked text was made rewordable: %+v", masked)
	}
	if err := (ChattoneRewordPolicy{}).Apply(ctx, "tenant", "incident", "dana", false, heatedBody, new(chatrender.Policy)); !errors.Is(err, errChattoneRewordNotComposed) {
		t.Fatalf("no store: %v", err)
	}

	// The reader's own tone: channel override, then general, then the workspace default.
	_ = store.SaveReaderChoice(ctx, "tenant", "dana", "", "as-written")
	for _, tc := range []struct {
		person, channel string
		want            chatrender.Tone
	}{
		{"morgan", "random", chatrender.AsWritten},  // mode off in this channel: their tone before the feature
		{"morgan", "incident", chatrender.Reworded}, // administrator does not allow originals: reworded
		{"dana", "random", chatrender.AsWritten},    // off: unchanged
		{"dana", "incident", chatrender.Reworded},   // choice does not exist where originals are hidden
		{"dana", "", chatrender.AsWritten},          // no channel: the default
	} {
		got, err := policy.ReaderTone(ctx, "tenant", tc.person, tc.channel, chatrender.AsWritten)
		if err != nil || got != tc.want {
			t.Errorf("%s in %q: %v %v, want %v", tc.person, tc.channel, got, err, tc.want)
		}
	}
	_, _ = store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "tenant", Channel: "standup", Mode: "on", MembersMayViewOriginal: true, UpdatedBy: "admin"})
	for _, tc := range []struct {
		person string
		choice string
		want   chatrender.Tone
	}{
		{"morgan", "", chatrender.Reworded},          // "on" makes reworded the default
		{"dana", "as-written", chatrender.AsWritten}, // a person's own choice wins
		{"lee", "reworded", chatrender.Reworded},
	} {
		if tc.choice != "" {
			_ = store.SaveReaderChoice(ctx, "tenant", tc.person, "standup", tc.choice)
		}
		if got, _ := policy.ReaderTone(ctx, "tenant", tc.person, "standup", chatrender.AsWritten); got != tc.want {
			t.Errorf("%s in standup: %v, want %v", tc.person, got, tc.want)
		}
	}
}

// TestTodo_CHATTONE_003_Flows: an administrator saves and clears settings and a
// person saves and clears their own choice, each answered with what now applies.
func TestTodo_CHATTONE_003_Flows(t *testing.T) {
	service, ctx, _ := chattoneRewordService(t, true)
	view, err := service.ReadReword(ctx, "room")
	if err != nil || !view.Available || view.Workspace.Mode != "off" || !view.CanAdmin || view.ChoiceAllowed {
		t.Fatalf("default view: %+v %v", view, err)
	}
	view, err = service.AdministerReword(ctx, ChattoneRewordAdmin{ConversationID: "room", Scope: "workspace", Mode: "offered", MembersMayViewOriginal: true})
	if err != nil || view.Workspace.Mode != "offered" || !view.ChoiceAllowed {
		t.Fatalf("workspace save: %+v %v", view, err)
	}
	view, err = service.AdministerReword(ctx, ChattoneRewordAdmin{ConversationID: "room", Scope: "channel", Mode: "on", MembersMayViewOriginal: false})
	if err != nil || !view.Channel.Override || view.Channel.Mode != "on" || view.ChoiceAllowed || view.Workspace.Mode != "offered" {
		t.Fatalf("channel override: %+v %v", view, err)
	}
	if _, err = service.ChooseReword(ctx, ChattoneRewordChoice{ConversationID: "room", Scope: "general", Tone: "reworded"}); !errors.Is(err, personachat.ErrDenied) {
		t.Fatalf("a choice where originals are hidden: %v", err)
	}
	view, err = service.AdministerReword(ctx, ChattoneRewordAdmin{ConversationID: "room", Scope: "channel", Clear: true})
	if err != nil || view.Channel.Override || view.Channel.Mode != "offered" || !view.ChoiceAllowed {
		t.Fatalf("clearing the override: %+v %v", view, err)
	}
	view, err = service.ChooseReword(ctx, ChattoneRewordChoice{ConversationID: "room", Scope: "channel", Tone: "reworded"})
	if err != nil || view.ForChannel != "reworded" || view.General != "" {
		t.Fatalf("a channel choice: %+v %v", view, err)
	}
	view, err = service.ChooseReword(ctx, ChattoneRewordChoice{ConversationID: "room", Scope: "general", Tone: "as-written"})
	if err != nil || view.General != "as-written" || view.ForChannel != "reworded" {
		t.Fatalf("a general choice: %+v %v", view, err)
	}
	view, err = service.ChooseReword(ctx, ChattoneRewordChoice{ConversationID: "room", Scope: "channel", Clear: true})
	if err != nil || view.ForChannel != "" || view.General != "as-written" {
		t.Fatalf("clearing a choice: %+v %v", view, err)
	}
	for name, call := range map[string]func() error{
		"unknown mode": func() error {
			_, e := service.AdministerReword(ctx, ChattoneRewordAdmin{ConversationID: "room", Scope: "workspace", Mode: "sometimes"})
			return e
		},
		"unknown scope": func() error {
			_, e := service.AdministerReword(ctx, ChattoneRewordAdmin{ConversationID: "room", Scope: "planet", Mode: "on"})
			return e
		},
		"clear the workspace": func() error {
			_, e := service.AdministerReword(ctx, ChattoneRewordAdmin{ConversationID: "room", Scope: "workspace", Clear: true})
			return e
		},
		"unknown tone": func() error {
			_, e := service.ChooseReword(ctx, ChattoneRewordChoice{ConversationID: "room", Scope: "general", Tone: "angry"})
			return e
		},
		"clear general": func() error {
			_, e := service.ChooseReword(ctx, ChattoneRewordChoice{ConversationID: "room", Scope: "general", Clear: true})
			return e
		},
	} {
		if err := call(); !errors.Is(err, chatrewrite.ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
}

// TestTodo_CHATTONE_003_Security: only an administrator changes the workspace's
// choice, a person changes only their own, another workspace's settings are
// never read, and the HTTP edge refuses what it does not know.
func TestTodo_CHATTONE_003_Security(t *testing.T) {
	member, ctx, store := chattoneRewordService(t, false)
	if _, err := member.AdministerReword(ctx, ChattoneRewordAdmin{ConversationID: "room", Scope: "workspace", Mode: "on"}); !errors.Is(err, personachat.ErrDenied) || store.saves != 0 {
		t.Fatalf("a member changed the workspace's choice: %v saves=%d", err, store.saves)
	}
	if view, err := member.ReadReword(ctx, "room"); err != nil || view.CanAdmin {
		t.Fatalf("a member was offered administration: %+v %v", view, err)
	}
	// Another workspace's row is invisible to this one.
	_, _ = store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "other", Mode: "on", MembersMayViewOriginal: false, UpdatedBy: "x"})
	if view, _ := member.ReadReword(ctx, "room"); view.Workspace.Mode != "off" || view.Channel.Mode != "off" {
		t.Fatalf("another workspace's setting crossed: %+v", view)
	}
	// Not the conversation the person may write in; not signed in; not composed.
	if _, err := member.ReadReword(ctx, "elsewhere"); !errors.Is(err, personachat.ErrDenied) {
		t.Fatalf("a conversation the person is not in: %v", err)
	}
	if _, err := member.ReadReword(context.Background(), "room"); !errors.Is(err, personachat.ErrUnauthenticated) {
		t.Fatalf("an anonymous read: %v", err)
	}
	if _, err := (&ChattoneService{Now: member.Now}).ReadReword(ctx, "room"); !errors.Is(err, errChattoneNotComposed) {
		t.Fatalf("a service without settings: %v", err)
	}

	service, ctx, _ := chattoneRewordService(t, true)
	handler := ChattoneHandler{Surface: service}
	post := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, ChattonePath+path, bytes.NewBufferString(body)).WithContext(ctx)
		handler.ServeHTTP(w, r)
		return w
	}
	// A choice names no person: a body that tries to is refused whole.
	if w := post("/reword/choice", `{"conversation_id":"room","scope":"general","tone":"reworded","person":"someone-else"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("a choice naming a person: %d %s", w.Code, w.Body)
	}
	if w := post("/reword/admin", `{"conversation_id":"room","scope":"workspace","mode":"on","tenant":"other"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("a setting naming a tenant: %d %s", w.Code, w.Body)
	}
	if w := post("/reword/admin", `{"conversation_id":"room","scope":"workspace","mode":"on"} {}`); w.Code != http.StatusBadRequest {
		t.Fatalf("trailing data: %d", w.Code)
	}
	if w := post("/reword/admin", `{"conversation_id":"room","scope":"workspace","mode":"on","members_may_view_original":true}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"mode":"on"`) {
		t.Fatalf("an administrator's save: %d %s", w.Code, w.Body)
	}
	read := httptest.NewRecorder()
	handler.ServeHTTP(read, httptest.NewRequest(http.MethodGet, ChattonePath+"/reword?conversation_id=room", nil).WithContext(ctx))
	var view ChattoneRewordView
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &view) != nil || view.Workspace.Mode != "on" || read.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("read: %d %s", read.Code, read.Body)
	}
	for _, c := range []struct{ method, path string }{{http.MethodDelete, "/reword"}, {http.MethodGet, "/reword/admin"}, {http.MethodPost, "/reword/unknown"}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(c.method, ChattonePath+c.path, nil).WithContext(ctx))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d", c.method, c.path, w.Code)
		}
	}
	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, ChattonePath+"/reword?conversation_id=room", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", anonymous.Code)
	}
	// A surface that serves no reword settings answers "unavailable", not a panic.
	plain := httptest.NewRecorder()
	ChattoneHandler{Surface: chattoneSurfaceStub{}}.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, ChattonePath+"/reword?conversation_id=room", nil).WithContext(ctx))
	if plain.Code != http.StatusServiceUnavailable {
		t.Fatalf("a surface without settings: %d", plain.Code)
	}
}

type chattoneSurfaceStub struct{}

func (chattoneSurfaceStub) RewriteDraft(context.Context, ChattoneDraft) (ChattoneReply, error) {
	return ChattoneReply{}, chatrewrite.ErrUnavailable
}
func (chattoneSurfaceStub) ReadSuggestion(context.Context, string) (ChattoneReply, error) {
	return ChattoneReply{}, chatrewrite.ErrUnavailable
}

// TestTodo_CHATTONE_003_NoOriginalForAMember: where the administrator has said
// members may not view messages as written, nothing the selection returns to a
// non-writer is the original: not when the rewording is ready, not while it is
// pending, not when it failed. The writer still gets both.
func TestTodo_CHATTONE_003_NoOriginalForAMember(t *testing.T) {
	ctx := context.Background()
	store := newChattoneRewordFake()
	_, _ = store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "tenant", Mode: "offered", MembersMayViewOriginal: false, UpdatedBy: "admin"})
	reworded := chatrender.Rendering{Tenant: "tenant", Message: "m1", Revision: 1, Tone: chatrender.Reworded, Language: "en", SourceLanguage: "en", Text: "Please fix the build.", Kinds: []chatrender.Kind{chatrender.Reword}, Checks: chatrender.Checks{Meaning: true, Placeholders: true}}
	for _, tc := range []struct {
		name      string
		available chatrender.Available
		wantText  string
		wantState string
	}{
		{"ready", chatrender.Available{Renderings: []chatrender.Rendering{reworded}}, "Please fix the build.", "ready"},
		{"pending", chatrender.Available{Pending: true}, "", "pending"},
		{"failed", chatrender.Available{Failed: true}, "", "unavailable"},
		{"nothing yet", chatrender.Available{}, "", "unavailable"},
	} {
		policy := chattonePolicy()
		if err := (ChattoneRewordPolicy{Store: store}).Apply(ctx, "tenant", "room", "dana", false, heatedBody, &policy); err != nil {
			t.Fatal(err)
		}
		// The reader's own tone would otherwise be "as written".
		got, mark := chatrender.SelectForReader(policy, chatrender.Preference{Tone: chatrender.AsWritten, ReadingLanguage: "en"}, tc.available)
		if got.Text != tc.wantText || mark.State != tc.wantState || mark.CanShowOriginal || mark.CanShowAsWritten {
			t.Errorf("%s: text %q state %q original=%v written=%v", tc.name, got.Text, mark.State, mark.CanShowOriginal, mark.CanShowAsWritten)
		}
		if strings.Contains(got.Text, "idiot") {
			t.Errorf("%s: the original reached a member", tc.name)
		}
	}
	// The writer keeps both views.
	policy := chattonePolicy()
	_ = (ChattoneRewordPolicy{Store: store}).Apply(ctx, "tenant", "room", "dana", true, heatedBody, &policy)
	got, mark := chatrender.SelectForReader(policy, chatrender.Preference{Tone: chatrender.AsWritten, ReadingLanguage: "en"}, chatrender.Available{Renderings: []chatrender.Rendering{reworded}})
	if got.Text != heatedBody || !mark.CanShowOriginal {
		t.Fatalf("the writer lost the original: %q %+v", got.Text, mark)
	}
	// A list or export read applies the same policy: an as-written rendering is not permitted for a member.
	member := chattonePolicy()
	_ = (ChattoneRewordPolicy{Store: store}).Apply(ctx, "tenant", "room", "dana", false, heatedBody, &member)
	asWritten := reworded
	asWritten.Tone, asWritten.Kinds = chatrender.AsWritten, []chatrender.Kind{chatrender.Mask}
	if chatrender.PermittedRendering(member, asWritten) || !chatrender.PermittedRendering(member, reworded) {
		t.Fatal("a member's list or export read permits an as-written rendering")
	}
}

type chattoneRewordFactsFixture struct {
	facts  chatstore.ChatlangJobFacts
	bodies []string
	err    error
}

func (f chattoneRewordFactsFixture) ChatlangJobFacts(_ context.Context, tenant, post string, revision uint64) (chatstore.ChatlangJobFacts, error) {
	if f.err != nil || f.facts.Tenant != tenant || f.facts.Revision != revision {
		return chatstore.ChatlangJobFacts{}, errors.Join(chatrender.ErrDenied, f.err)
	}
	return f.facts, nil
}
func (f chattoneRewordFactsFixture) ChatlangPreviousBodies(context.Context, string, string, int) ([]string, error) {
	return f.bodies, nil
}

// TestTodo_CHATTONE_002_Producer: the "reworded" rendering kind. A heated
// message gets a rendering carrying who produced it and that its checks passed;
// a direct message is never reworded for its reader, an abusive one follows the
// hard filter, a calm one has nothing to produce, and a failing model produces
// nothing (the reader keeps the original).
func TestTodo_CHATTONE_002_Producer(t *testing.T) {
	replay := NewChattoneRewordReplay(chattoneRewordReplayFixture(t))
	reworder, err := NewChattoneReworder(replay, chattoneAllowAllPolicy{}, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	facts := chatstore.ChatlangJobFacts{Tenant: "tenant", Conversation: "room", Author: "dana", AuthorHome: "tenant", Revision: 2,
		Body: "You idiot, the deploy broke prod again. @dana must fix 42 files by 2026-10-01."}
	producer := func(f chattoneRewordFactsFixture) *ChattoneRewordProducer {
		return &ChattoneRewordProducer{Facts: f, Reworder: reworder}
	}
	request := chatrender.Rendering{Tenant: "tenant", Message: "m1", Revision: 2, Tone: chatrender.Reworded, Language: "en", SourceLanguage: "en"}

	got, err := producer(chattoneRewordFactsFixture{facts: facts, bodies: []string{"Is the build green?", "It is red again."}}).Produce(context.Background(), request)
	if err != nil || got.Text != "The deploy broke prod again. @dana must fix 42 files by 2026-10-01." || got.Tone != chatrender.Reworded ||
		got.Producer.Provider != "openai" || got.Producer.Model != "gpt-6-luna" || got.Producer.InstructionDigest != chatrewrite.InstructionDigest() || !got.Checks.Meaning || !got.Checks.Placeholders || got.Confidence < 0.99 {
		t.Fatalf("heated message: %+v %v", got, err)
	}
	// It registers as the kind the policy asks for and runs before translation.
	registry, err := chatrender.NewRegistry(producer(chattoneRewordFactsFixture{facts: facts}).Registration())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := registry.Produce(context.Background(), request, []chatrender.Kind{chatrender.Reword}); err != nil || len(out.Kinds) != 1 || out.Kinds[0] != chatrender.Reword || out.Text == "" {
		t.Fatalf("through the registry: %+v %v", out, err)
	}

	direct := facts
	direct.Direct = true
	abusive := facts
	abusive.Body = "I will find you and hurt you"
	calm := facts
	calm.Body = "Please fix the 42 files by 2026-10-01."
	for name, tc := range map[string]struct {
		f    chattoneRewordFactsFixture
		want error
	}{
		"direct message": {chattoneRewordFactsFixture{facts: direct}, chatrender.ErrDenied},
		"abusive":        {chattoneRewordFactsFixture{facts: abusive}, chatrender.ErrDenied},
		"calm":           {chattoneRewordFactsFixture{facts: calm}, chatrender.ErrInvalid},
		"gone or edited": {chattoneRewordFactsFixture{facts: facts, err: errors.New("gone")}, chatrender.ErrDenied},
		"no recorded text": {chattoneRewordFactsFixture{facts: func() chatstore.ChatlangJobFacts {
			f := facts
			f.Body = "You idiot, nothing was recorded for this 1."
			return f
		}()}, chatrender.ErrInvalid},
	} {
		if got, err := producer(tc.f).Produce(context.Background(), request); !errors.Is(err, tc.want) || got.Text != "" {
			t.Errorf("%s: %+v %v, want %v", name, got, err, tc.want)
		}
	}
	if _, err := (*ChattoneRewordProducer)(nil).Produce(context.Background(), request); !errors.Is(err, chatrender.ErrUnavailable) {
		t.Fatalf("an unconfigured producer: %v", err)
	}
	if _, err := NewChattoneReworder(nil, chattoneAllowAllPolicy{}, nil, nil); err == nil {
		t.Fatal("a reworder without a model was built")
	}
}

type chattoneRewordFilterFixture struct {
	hits  []chatfilter.Hit
	err   error
	input chatfilter.Input
}

func (f *chattoneRewordFilterFixture) Evaluate(_ context.Context, in chatfilter.Input, _ bool) (chatfilter.Result, error) {
	f.input = in
	return chatfilter.Result{Hits: f.hits}, f.err
}

// TestTodo_CHATTONE_002_FilterAction: "reword" is an action of the filter
// registry, so a workspace's own filter can ask for rewording of a message the
// word lists pass; an enforced hit counts, a dry-run hit does not, "off" still
// wins, and an abusive message is left to the hard filters.
func TestTodo_CHATTONE_002_FilterAction(t *testing.T) {
	registry := chatfilter.NewRegistry()
	definition := chatfilter.Definition{ID: "r1", Name: "Sarcasm", Version: "1.0.0", Kind: "words", Action: ChattoneRewordFilterAction, Match: []string{"great job"}}
	if _, err := registry.Compile([]chatfilter.Definition{definition}); err != nil {
		t.Fatalf("the filter registry does not know the reword action: %v", err)
	}
	evaluator, err := registry.Compile([]chatfilter.Definition{definition})
	if err != nil {
		t.Fatal(err)
	}
	result, err := evaluator.Evaluate(chatfilter.Input{Tenant: "tenant", Channel: "room", Subject: "dana", Body: "Oh, great job again."})
	if err != nil || len(result.Hits) != 1 || result.Hits[0].Action != "reword" || result.Action != "reword" {
		t.Fatalf("a reword filter did not hit: %+v %v", result, err)
	}
	if result.Refusal() != nil || result.Masked != "Oh, great job again." {
		t.Fatalf("a reword hit blocked or masked the message: %+v", result)
	}

	ctx := context.Background()
	store := newChattoneRewordFake()
	_, _ = store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "tenant", Mode: "on", MembersMayViewOriginal: true, UpdatedBy: "admin"})
	apply := func(filter *chattoneRewordFilterFixture, body string) chatrender.Policy {
		policy := chattonePolicy()
		p := ChattoneRewordPolicy{Store: store}
		if filter != nil {
			p.Filters = filter
		}
		if err := p.Apply(ctx, "tenant", "room", "dana", false, body, &policy); err != nil {
			t.Fatal(err)
		}
		return policy
	}
	hasReword := func(p chatrender.Policy) bool {
		for _, k := range p.AllowedKinds {
			if k == chatrender.Reword {
				return true
			}
		}
		return false
	}
	sarcastic := "Oh, great job again."
	hit := &chattoneRewordFilterFixture{hits: []chatfilter.Hit{{RuleID: "r1", Action: "reword"}}}
	if p := apply(hit, sarcastic); !hasReword(p) || hit.input.Tenant != "tenant" || hit.input.Channel != "room" || hit.input.Subject != "dana" || hit.input.Body != sarcastic {
		t.Fatalf("an enforced reword hit did not make the message rewordable: %+v input %+v", p, hit.input)
	}
	if p := apply(&chattoneRewordFilterFixture{hits: []chatfilter.Hit{{Action: "reword", DryRun: true}}}, sarcastic); hasReword(p) {
		t.Fatal("a dry-run hit acted")
	}
	if p := apply(&chattoneRewordFilterFixture{hits: []chatfilter.Hit{{Action: "flag"}}}, sarcastic); hasReword(p) {
		t.Fatal("another action made the message rewordable")
	}
	if p := apply(nil, sarcastic); hasReword(p) {
		t.Fatal("a calm message was made rewordable with no filter")
	}
	if p := apply(hit, "I will find you and hurt you"); hasReword(p) {
		t.Fatal("an abusive message was offered for rewording by a filter")
	}
	off := newChattoneRewordFake()
	_, _ = off.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "tenant", Mode: "off", MembersMayViewOriginal: true, UpdatedBy: "admin"})
	policy := chattonePolicy()
	if err := (ChattoneRewordPolicy{Store: off, Filters: hit}).Apply(ctx, "tenant", "room", "dana", false, sarcastic, &policy); err != nil || hasReword(policy) {
		t.Fatalf("a filter overrode the workspace's off: %v", err)
	}
	failing := &chattoneRewordFilterFixture{err: errors.New("filters down")}
	if err := (ChattoneRewordPolicy{Store: store, Filters: failing}).Apply(ctx, "tenant", "room", "dana", false, sarcastic, new(chatrender.Policy)); err == nil {
		t.Fatal("a failed filter read was swallowed")
	}
}
