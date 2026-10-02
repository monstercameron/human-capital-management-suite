package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

type cvsAccess struct {
	policy chat.VoicePolicy
	kind   chat.ConversationKind
}

func (a cvsAccess) AuthorizeVoice(context.Context, chat.Principal, string, string, string) (chat.VoicePolicy, chat.ConversationKind, error) {
	return a.policy, a.kind, nil
}

// cvsAdmin lets one subject change the workspace's switch and another's channel
// only for the named channel.
type cvsAdmin struct{ workspace, channel string }

func (a cvsAdmin) AuthorizeFilters(_ context.Context, actor chatfilter.Actor, channel string) error {
	if channel == "" && actor.Subject == a.workspace {
		return nil
	}
	if channel != "" && channel == a.channel && actor.Subject != "" {
		return nil
	}
	return chat.ErrPermissionDenied
}
func (cvsAdmin) CanReadFilterConversation(context.Context, chatfilter.Actor, string) bool {
	return true
}

type cvsSwitches struct {
	workspace, personal bool
	puts                []string
}

func (s *cvsSwitches) VoiceSwitchValues(context.Context, string, string) (bool, bool, error) {
	return s.workspace, s.personal, nil
}
func (s *cvsSwitches) PutVoiceSwitch(_ context.Context, tenant, by, scope, scopeID string, enabled bool) error {
	s.puts = append(s.puts, strings.Join([]string{tenant, by, scope, scopeID}, "|"))
	return nil
}

type cvsBarred struct{ workspace bool }

func (b cvsBarred) VoiceListenBarred(context.Context, string, string, string) (bool, []string, error) {
	return b.workspace, nil, nil
}

func cvsService(access cvsAccess) VoiceService {
	return VoiceService{Access: access, Decoder: &chatvoiceDecoderFixture{}, Media: &chatvoiceMediaFixture{}, Messages: &chatvoiceWriterFixture{}}
}

// TestTodo_CHATVOICE_005_Settings: the policy answer carries the three switches
// as they stand and who may change the shared ones, so the settings rows show a
// true state; each reason code is the one the page words.
func TestTodo_CHATVOICE_005_Settings(t *testing.T) {
	alice := chat.Principal{TenantID: "t", SubjectID: "alice"}
	service := cvsService(cvsAccess{policy: chat.VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true}, kind: chat.PublicChannel})
	service.Admin = cvsAdmin{workspace: "alice", channel: "room"}
	view, err := service.Policy(context.Background(), alice, "t", "room")
	if err != nil || view.Allowed || view.Reason != "channel_off" || !view.IsChannel || !view.CanManageChannel || !view.CanManageWorkspace || view.Channel || !view.Workspace || !view.Person {
		t.Fatalf("channel off by default: %+v %v", view, err)
	}
	service.Access = cvsAccess{policy: chat.VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true, ChannelEnabled: true}, kind: chat.PublicChannel}
	if view, _ = service.Policy(context.Background(), alice, "t", "room"); !view.Allowed || view.Reason != "" || !view.Channel {
		t.Fatalf("channel on: %+v", view)
	}
	for reason, policy := range map[string]chat.VoicePolicy{
		"workspace_off": {PersonalEnabled: true, ChannelEnabled: true},
		"personal_off":  {WorkspaceEnabled: true, ChannelEnabled: true},
	} {
		service.Access = cvsAccess{policy: policy, kind: chat.PublicChannel}
		if view, _ = service.Policy(context.Background(), alice, "t", "room"); view.Allowed || view.Reason != reason {
			t.Fatalf("%s: %+v", reason, view)
		}
	}
	// A member who administers nothing is told nothing about administration.
	service.Access = cvsAccess{policy: chat.VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true}, kind: chat.Direct}
	service.Admin = cvsAdmin{}
	bob := chat.Principal{TenantID: "t", SubjectID: "bob"}
	if view, _ = service.Policy(context.Background(), bob, "t", "dm"); !view.Allowed || view.IsChannel || view.CanManageChannel || view.CanManageWorkspace || view.Engine != nil {
		t.Fatalf("direct conversation: %+v", view)
	}

	// The settings pages: the workspace and personal switches, and the engine for
	// an administrator only.
	switches := &cvsSwitches{workspace: false, personal: true}
	service = VoiceService{Admin: cvsAdmin{workspace: "alice"}, SwitchValues: switches, Switches: switches, Barred: cvsBarred{workspace: true}, Engine: VoiceEngineInfo{Kind: "outside", Transcriber: "gpt-4o-mini-transcribe", Speech: "gpt-4o-mini-tts"}}
	view, err = service.Settings(context.Background(), alice, "")
	if err != nil || view.Workspace || !view.Person || !view.CanManageWorkspace || view.Engine == nil || view.Engine.Transcriber != "gpt-4o-mini-transcribe" || view.Engine.Speech != "gpt-4o-mini-tts" || !view.Engine.OutsideKnown || view.Engine.OutsideAllowed {
		t.Fatalf("administrator settings: %+v %v", view, err)
	}
	if view, err = service.Settings(context.Background(), bob, "t"); err != nil || view.CanManageWorkspace || view.Engine != nil {
		t.Fatalf("a member saw the engine: %+v %v", view, err)
	}
	service.Engine = VoiceEngineInfo{}
	if view, _ = service.Settings(context.Background(), alice, "t"); view.Engine == nil || view.Engine.Kind != "none" {
		t.Fatalf("no engine must be said plainly: %+v", view.Engine)
	}
	if _, err = (VoiceService{}).Settings(context.Background(), alice, "t"); !errors.Is(err, chat.ErrVoiceUnavailable) {
		t.Fatalf("no store: %v", err)
	}

	// Saving: the caller's own switch, and the workspace's tenant is the caller's
	// when the page did not name one.
	if err = service.SetSwitch(context.Background(), alice, VoiceSwitchRequest{Scope: "workspace", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err = service.SetSwitch(context.Background(), bob, VoiceSwitchRequest{Scope: "person", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err = service.SetSwitch(context.Background(), bob, VoiceSwitchRequest{Scope: "workspace", Enabled: true}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a member changed the workspace switch: %v", err)
	}
	if got := strings.Join(switches.puts, ","); got != "t|alice|workspace|,t|bob|person|bob" {
		t.Fatalf("stored %q", got)
	}
}

func TestChatvoiceSentences(t *testing.T) {
	for text, want := range map[string][]string{
		"One. Two! Three?":                        {"One.", "Two!", "Three?"},
		"Costs 3.5 euros at wiki.example.com. Ok": {"Costs 3.5 euros at wiki.example.com.", "Ok"},
		"No end":                {"No end"},
		"Wait... what?!  Yes.":  {"Wait...", "what?!", "Yes."},
		"":                      nil,
		"مرحبا. كيف الحال؟ جيد": {"مرحبا.", "كيف الحال؟", "جيد"},
	} {
		got := VoiceSentences(text)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%q: got %q want %q", text, got, want)
		}
	}
}

// The local development engine reports where each sentence is spoken, laid end
// to end over audio that is as long as they are.
func TestChatvoiceFixtureSpeechTimings(t *testing.T) {
	out, err := chatVoiceFixtureSpeech{}.Speak(context.Background(), agentmodel.SpeakRequest{Text: "First one. Second one. Third."})
	if err != nil || out.ContentType != "audio/wav" || len(out.Timings) != 3 {
		t.Fatalf("%+v %v", out, err)
	}
	var previous int64
	for i, timing := range out.Timings {
		if timing.StartMS != previous || timing.EndMS <= timing.StartMS || timing.Text == "" {
			t.Fatalf("timing %d: %+v", i, timing)
		}
		previous = timing.EndMS
	}
	if want := 44 + 2*int(previous)*8000/1000; len(out.Audio) != want {
		t.Fatalf("audio is %d bytes, want %d for %d ms", len(out.Audio), want, previous)
	}
	converted := make([]chatui.ListenTiming, 0, len(out.Timings))
	for _, timing := range out.Timings {
		converted = append(converted, chatui.ListenTiming{Text: timing.Text, StartMS: timing.StartMS, EndMS: timing.EndMS})
	}
	if header := chatui.EncodeListenTimings(converted); header == "" || len(chatui.ParseListenTimings(header)) != 3 {
		t.Fatalf("the page would not accept the fixture's timings: %q", header)
	}
	// Without a model the engine of the product reports none: no mark, no guess.
	if (agentmodel.AudioSpeech{}).Timings != nil {
		t.Fatal("an engine that reports no timings carried some")
	}
}

type cvsThreadReader struct {
	chat.ConversationService
	pages [][]chat.Post
	err   error
}

func (r cvsThreadReader) ListPosts(_ context.Context, req chat.ListPostsRequest) (chat.ListPostsResponse, error) {
	if r.err != nil {
		return chat.ListPostsResponse{}, r.err
	}
	index := 0
	if req.Page.Cursor != "" {
		index = int(req.Page.Cursor[0] - '0')
	}
	out := chat.ListPostsResponse{Posts: r.pages[index]}
	if index+1 < len(r.pages) {
		out.NextCursor = string(rune('0' + index + 1))
	}
	return out, nil
}

type cvsSpeech struct{ text string }

func (s *cvsSpeech) Speak(_ context.Context, r agentmodel.SpeakRequest) (agentmodel.AudioSpeech, error) {
	s.text = r.Text
	return agentmodel.AudioSpeech{Audio: []byte("ID3"), ContentType: "audio/mpeg"}, nil
}

// TestTodo_CHATVOICE_006_Thread: "Listen to this thread" reads the parent and
// its replies in the order they were posted, each led by its author's name; it
// reads nothing from another thread, a deleted reply or a post the reader cannot
// list, and a name from the page is only ever spoken text.
func TestTodo_CHATVOICE_006_Thread(t *testing.T) {
	post := func(id, parent, author, body string, sequence uint64) chat.Post {
		return chat.Post{ID: id, ParentID: parent, AuthorID: author, Body: body, Sequence: sequence, TenantID: "t", ConversationID: "room"}
	}
	deleted := post("gone", "root", "ada", "secret", 4)
	deleted.Deleted = true
	reader := cvsThreadReader{pages: [][]chat.Post{
		{post("reply2", "root", "bob", "Sounds good, see https://wiki.example.com/page.", 3), post("other", "elsewhere", "bob", "unrelated", 2), deleted},
		{post("root", "", "ada", "Hello **team**.", 1), post("outsider", "root", "eve", "wrong room", 5)},
	}}
	reader.pages[1][1].ConversationID = "other-room"
	port := &cvsSpeech{}
	speaker := VoiceSpeaker{Reader: reader, Port: port}
	alice := chat.Principal{TenantID: "t", SubjectID: "alice"}
	names := map[string]string{"ada": "Ada Lovelace", "bob": "Bob [x](y)\n<b>"}
	if _, err := speaker.Speak(context.Background(), alice, VoiceSpeakRequest{TenantID: "t", ConversationID: "room", ThreadID: "root", Names: names}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(port.text, "unrelated") || strings.Contains(port.text, "secret") || strings.Contains(port.text, "wrong room") {
		t.Fatalf("another thread, a deleted reply or another room was read: %q", port.text)
	}
	if !strings.HasPrefix(port.text, "Ada Lovelace. Hello team. Bob") || strings.Index(port.text, "Hello team") > strings.Index(port.text, "Sounds good") || !strings.Contains(port.text, "Sounds good, see link to wiki.example.com.") {
		t.Fatalf("thread text %q", port.text)
	}
	if strings.ContainsAny(port.text, "[]<>\n") {
		t.Fatalf("a name carried markup into the speech: %q", port.text)
	}
	// An author the page did not name is read without a name, not a guessed one.
	if _, err := speaker.Speak(context.Background(), alice, VoiceSpeakRequest{TenantID: "t", ConversationID: "room", ThreadID: "root"}); err != nil || strings.HasPrefix(port.text, ".") || !strings.HasPrefix(port.text, "Hello team.") {
		t.Fatalf("unnamed authors: %q %v", port.text, err)
	}
	// A root that is not there, a reader who may not list, and a thread too long
	// to say in one request each end in a stable refusal.
	if _, err := speaker.Speak(context.Background(), alice, VoiceSpeakRequest{TenantID: "t", ConversationID: "room", ThreadID: "missing"}); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("missing root: %v", err)
	}
	if _, err := (VoiceSpeaker{Reader: cvsThreadReader{err: chat.ErrPermissionDenied}, Port: port}).Speak(context.Background(), alice, VoiceSpeakRequest{TenantID: "t", ConversationID: "room", ThreadID: "root"}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a reader who may not list: %v", err)
	}
	long := cvsThreadReader{pages: [][]chat.Post{{post("root", "", "ada", strings.Repeat("word ", 500), 1), post("r", "root", "bob", strings.Repeat("word ", 500), 2)}}}
	if _, err := (VoiceSpeaker{Reader: long, Port: port}).Speak(context.Background(), alice, VoiceSpeakRequest{TenantID: "t", ConversationID: "room", ThreadID: "root"}); !errors.Is(err, ErrVoiceTooLong) {
		t.Fatalf("too long: %v", err)
	}
}

// The features answer says whether voice messages are composed, so the personal
// and channel switches are offered only where there is something to switch.
func TestChatvoiceFeaturesVoiceFlag(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"gates":true}`))
	})
	call := func(service VoiceService) map[string]any {
		recorder := httptest.NewRecorder()
		OverlayChatVoiceFeatures(next, service, transport.Config{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, integrate2FeaturesPath, nil))
		var out map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := call(VoiceService{}); out["voice"] != false {
		t.Fatalf("voice offered without a composition: %+v", out)
	}
	switches := &cvsSwitches{}
	if out := call(VoiceService{Access: cvsAccess{}, SwitchValues: switches, Switches: switches}); out["voice"] != true || out["gates"] != true {
		t.Fatalf("composed voice not reported: %+v", out)
	}
}
