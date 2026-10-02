package application

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

func TestChatvoiceSpokenText(t *testing.T) {
	for body, want := range map[string]string{
		"Plain sentence.": "Plain sentence.",
		"See [the policy](https://hr.example.com/p)":     "See the policy",
		"[](https://hr.example.com/p)":                   "link to hr.example.com",
		"Read https://wiki.example.com/page.":            "Read link to wiki.example.com.",
		"ask @Dana and mail dana@example.com":            "ask Dana and mail dana@example.com",
		"## Heading\n> quoted\n`code` and ~~gone~~":      "Heading quoted code and gone",
		"Ten days [[1]] and more [[2:leave-policy]] end": "Ten days source 1 and more source 2 end",
	} {
		if got := VoiceSpokenText(body); got != want {
			t.Fatalf("%q spoke %q, want %q", body, got, want)
		}
	}
	if VoiceSpokenText("  **  ") != "" {
		t.Fatal("formatting marks alone were spoken")
	}
}

func TestChatvoiceFixtureEngines(t *testing.T) {
	out, err := chatVoiceFixtureEngine{}.Transcribe(context.Background(), chat.TranscriptionRequest{DurationMS: 7000})
	if err != nil || out.Validate(7000) != nil || out.Model != "fixture" || !strings.Contains(out.Text(), "No speech model was called") {
		t.Fatalf("fixture transcript %+v err=%v", out, err)
	}
	speech, err := chatVoiceFixtureSpeech{}.Speak(context.Background(), agentmodel.SpeakRequest{Text: "hi"})
	if err != nil || speech.ContentType != "audio/wav" || string(speech.Audio[:4]) != "RIFF" || string(speech.Audio[8:12]) != "WAVE" {
		t.Fatalf("fixture speech %v", err)
	}
	if got := binary.LittleEndian.Uint32(speech.Audio[4:8]); int(got) != len(speech.Audio)-8 {
		t.Fatalf("RIFF length %d for %d bytes", got, len(speech.Audio))
	}
}

func TestChatvoicePortFromEnvironment(t *testing.T) {
	env := func(values map[string]string) func(string) string { return func(k string) string { return values[k] } }
	guard, _ := NewVoiceAudioGuard(cvSettings{}, DefaultVoiceBudgetPolicy(), nil)
	if _, err := newChatVoicePort(env(nil), guard); !errors.Is(err, agentmodel.ErrAudioNotConfigured) {
		t.Fatalf("no key must not build a port: %v", err)
	}
	port, err := newChatVoicePort(env(map[string]string{"MODEL_API_KEY": "k"}), guard)
	if err != nil {
		t.Fatal(err)
	}
	if transcribe, speech := port.Models(); transcribe.ModelID != agentmodel.DefaultTranscribeModel || speech.ModelID != agentmodel.DefaultSpeechModel || transcribe.Version != chatVoiceModelVersionDefault {
		t.Fatalf("defaults %+v %+v", transcribe, speech)
	}
	port, err = newChatVoicePort(env(map[string]string{"MODEL_API_KEY": "k", EnvChatVoiceTranscribeModel: "gpt-4o-transcribe", EnvChatVoiceSpeechModel: "tts-1", EnvChatVoiceModelVersion: "2026-09-01"}), guard)
	if err != nil {
		t.Fatal(err)
	}
	if transcribe, speech := port.Models(); transcribe.ModelID != "gpt-4o-transcribe" || speech.ModelID != "tts-1" || transcribe.Version != "2026-09-01" {
		t.Fatalf("configured models %+v %+v", transcribe, speech)
	}
	if _, err = NewVoiceAudioGuard(nil, DefaultVoiceBudgetPolicy(), nil); !errors.Is(err, agentmodel.ErrAudioNotConfigured) {
		t.Fatal("a guard without the outside-service settings was built")
	}
	if b := chatVoiceBudget(env(map[string]string{EnvChatVoiceMonthlyBudget: "900", EnvChatVoiceDailyBudget: "bad"})); b.TenantMonthlyMicros != 900 || b.UserDailyMicros != DefaultVoiceBudgetPolicy().UserDailyMicros {
		t.Fatalf("budget %+v", b)
	}
}

type cvAccess struct {
	policy chat.VoicePolicy
	kind   chat.ConversationKind
	err    error
}

func (a cvAccess) AuthorizeVoice(context.Context, chat.Principal, string, string, string) (chat.VoicePolicy, chat.ConversationKind, error) {
	return a.policy, a.kind, a.err
}

type cvSwitches struct{ saved []string }

func (s *cvSwitches) PutVoiceSwitch(_ context.Context, tenant, by, scope, scopeID string, enabled bool) error {
	s.saved = append(s.saved, tenant+"|"+by+"|"+scope+"|"+scopeID+"|"+map[bool]string{true: "on", false: "off"}[enabled])
	return nil
}

type cvAdmin struct{ allow map[string]bool }

func (a cvAdmin) AuthorizeFilters(_ context.Context, _ chatfilter.Actor, channel string) error {
	if a.allow[channel] {
		return nil
	}
	return chatfilter.ErrDenied
}
func (cvAdmin) CanReadFilterConversation(context.Context, chatfilter.Actor, string) bool { return true }

// The switches the composer reads and an administrator or a person sets.
func TestChatvoicePolicyAndSwitches(t *testing.T) {
	ready := VoiceService{Decoder: chatVoiceDecoder{}, Media: chatVoiceUploader{}, Messages: &chatvoiceWriterFixture{}}
	alice := chat.Principal{TenantID: "t", SubjectID: "alice"}
	for name, c := range map[string]struct {
		access cvAccess
		want   VoicePolicyView
	}{
		"direct on":     {cvAccess{chat.VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true}, chat.Direct, nil}, VoicePolicyView{Allowed: true}},
		"channel off":   {cvAccess{chat.VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true}, chat.PublicChannel, nil}, VoicePolicyView{Reason: "channel_off"}},
		"channel on":    {cvAccess{chat.VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true, ChannelEnabled: true}, chat.PrivateChannel, nil}, VoicePolicyView{Allowed: true}},
		"workspace off": {cvAccess{chat.VoicePolicy{PersonalEnabled: true, ChannelEnabled: true}, chat.Direct, nil}, VoicePolicyView{Reason: "workspace_off"}},
		"personal off":  {cvAccess{chat.VoicePolicy{WorkspaceEnabled: true}, chat.Direct, nil}, VoicePolicyView{Reason: "personal_off"}},
	} {
		service := ready
		service.Access = c.access
		if got, err := service.Policy(context.Background(), alice, "t", "room"); err != nil || got.Allowed != c.want.Allowed || got.Reason != c.want.Reason {
			t.Fatalf("%s: %+v err=%v", name, got, err)
		}
	}
	notMember := ready
	notMember.Access = cvAccess{err: chat.ErrNotFound}
	if _, err := notMember.Policy(context.Background(), alice, "t", "room"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("a conversation the person cannot open: %v", err)
	}
	if got, _ := (VoiceService{}).Policy(context.Background(), alice, "t", "room"); got.Allowed || got.Reason != "unavailable" {
		t.Fatalf("an uncomposed service offered voice: %+v", got)
	}

	switches := &cvSwitches{}
	service := VoiceService{Switches: switches, Admin: cvAdmin{allow: map[string]bool{"": true, "room": true}}}
	for _, r := range []VoiceSwitchRequest{{TenantID: "t", Scope: "person", Enabled: false}, {TenantID: "t", Scope: "channel", ConversationID: "room", Enabled: true}, {TenantID: "t", Scope: "workspace", Enabled: false}} {
		if err := service.SetSwitch(context.Background(), alice, r); err != nil {
			t.Fatalf("%+v: %v", r, err)
		}
	}
	if strings.Join(switches.saved, ",") != "t|alice|person|alice|off,t|alice|channel|room|on,t|alice|workspace||off" {
		t.Fatalf("saved %v", switches.saved)
	}
	// A person sets only their own switch; a channel or workspace needs its administrator.
	service.Admin = cvAdmin{}
	for _, r := range []VoiceSwitchRequest{{TenantID: "t", Scope: "channel", ConversationID: "room", Enabled: true}, {TenantID: "t", Scope: "workspace", Enabled: true}, {TenantID: "t", Scope: "channel", Enabled: true}} {
		if err := service.SetSwitch(context.Background(), alice, r); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Fatalf("%+v by a non-administrator: %v", r, err)
		}
	}
	if err := service.SetSwitch(context.Background(), alice, VoiceSwitchRequest{TenantID: "t", Scope: "everyone"}); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("unknown scope: %v", err)
	}
	if len(switches.saved) != 3 {
		t.Fatal("a refused switch was stored")
	}
}

type cvBarred struct {
	workspace bool
	rooms     []string
	err       error
}

func (b cvBarred) VoiceListenBarred(context.Context, string, string, string) (bool, []string, error) {
	return b.workspace, b.rooms, b.err
}

// The features overlay never adds Listen where there is no speech engine or the
// admission fails, and passes every other path and field through untouched.
func TestChatvoiceFeaturesOverlay(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"gates":true,"search":true}`))
	})
	call := func(service VoiceService, path string) map[string]any {
		recorder := httptest.NewRecorder()
		OverlayChatVoiceFeatures(next, service, transport.Config{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		var out map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: %v %q", path, err, recorder.Body.String())
		}
		return out
	}
	if out := call(VoiceService{}, integrate2FeaturesPath); out["listen"] != false || out["gates"] != true || out["listen_barred"] != "" {
		t.Fatalf("no engine: %+v", out)
	}
	engine := VoiceService{Speaker: &VoiceSpeaker{}, Barred: cvBarred{rooms: []string{"a"}}}
	if out := call(engine, integrate2FeaturesPath); out["listen"] != false {
		t.Fatalf("an unadmitted request was offered Listen: %+v", out)
	}
	if out := call(engine, "/api/other"); out["listen"] != nil || out["gates"] != true {
		t.Fatalf("another path was changed: %+v", out)
	}
}
