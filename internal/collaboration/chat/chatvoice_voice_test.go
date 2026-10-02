package chat

import (
	"context"
	"errors"
	"math"
	"testing"
)

type chatvoiceDecoder struct {
	facts VoiceInspection
	err   error
	calls int
}

func (d *chatvoiceDecoder) InspectVoice(context.Context, []byte) (VoiceInspection, error) {
	d.calls++
	return d.facts, d.err
}
func TestTodo_CHATVOICE_002(t *testing.T) {
	d := &chatvoiceDecoder{facts: VoiceInspection{ContentType: "audio/webm", DurationMS: 120000, AudioOnly: true, Opus: true}}
	a, err := ValidateVoiceUpload(context.Background(), d, "audio/webm;codecs=opus", []byte("fixture"), []float64{0, 0.5, 1})
	if err != nil || a.DurationMS != 120000 || a.TranscriptState != TranscriptPending || a.Bytes != 7 {
		t.Fatalf("attachment=%+v err=%v", a, err)
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if !(VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true}).Allows(Direct) || (VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true}).Allows(PublicChannel) || (VoicePolicy{ChannelEnabled: true, PersonalEnabled: true}).Allows(Direct) {
		t.Fatal("voice defaults or workspace switch bypassed")
	}
	if (VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true, ChannelEnabled: true}).Allows(ConversationKind("unknown")) {
		t.Fatal("unknown conversation kind admitted")
	}
}
func TestTodo_CHATVOICE_002_Security(t *testing.T) {
	for _, a := range []VoiceAttachment{{ContentType: "image/png", Bytes: 1, DurationMS: 1000}, {ContentType: "audio/webm", Bytes: 1, DurationMS: 120001}, {ContentType: "audio/ogg", Bytes: VoiceMaxBytes + 1, DurationMS: 1000}, {ContentType: "audio/ogg", Bytes: 1, DurationMS: 1000, Waveform: []float64{math.Inf(1)}}} {
		if !errors.Is(a.Validate(), ErrInvalidArgument) {
			t.Fatalf("invalid metadata admitted: %+v", a)
		}
	}
	good := VoiceInspection{ContentType: "audio/webm", DurationMS: 1000, AudioOnly: true, Opus: true}
	for _, tc := range []struct {
		name, declared string
		content        []byte
		facts          VoiceInspection
		wave           []float64
	}{
		{"mismatch", "audio/ogg", []byte("fixture"), good, nil}, {"oversize", "audio/webm", make([]byte, VoiceMaxBytes+1), good, nil}, {"empty", "audio/webm", nil, good, nil}, {"not-audio", "audio/webm", []byte("text"), VoiceInspection{ContentType: "text/plain", DurationMS: 1000}, nil}, {"too-long", "audio/webm", []byte("fixture"), VoiceInspection{ContentType: "audio/webm", DurationMS: 120001, AudioOnly: true, Opus: true}, nil}, {"wrong-codec", "audio/webm", []byte("fixture"), VoiceInspection{ContentType: "audio/webm", DurationMS: 1000, AudioOnly: true}, nil}, {"nan", "audio/webm", []byte("fixture"), good, []float64{math.NaN()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateVoiceUpload(context.Background(), &chatvoiceDecoder{facts: tc.facts}, tc.declared, tc.content, tc.wave)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	if _, err := ValidateVoiceUpload(context.Background(), nil, "audio/webm", []byte("fixture"), nil); !errors.Is(err, ErrVoiceUnavailable) {
		t.Fatalf("missing decoder=%v", err)
	}
	if _, err := ValidateVoiceUpload(context.Background(), UnavailableVoiceDecoder{}, "audio/webm", []byte("fixture"), nil); !errors.Is(err, ErrVoiceUnavailable) {
		t.Fatalf("unavailable decoder=%v", err)
	}
}

func TestTodo_CHATVOICE_004_Security(t *testing.T) {
	for _, raw := range []string{"https://example.invalid/audio", "//example.invalid/audio", "/v1/chat/media/../private", "/v1/chat/media/%2e%2e", "/v1/chat/media/a%2fb", "/v1/chat/media/", "blob:", "blob:fixture\n", "/v1/chat/media/a\\b"} {
		if VoicePlaybackURL(raw) {
			t.Fatalf("unsafe media URL %q", raw)
		}
	}
	for _, raw := range []string{"blob:fixture", "/v1/chat/media/artifact?grant=fixture"} {
		if !VoicePlaybackURL(raw) {
			t.Fatalf("protected media URL refused %q", raw)
		}
	}
}
func TestTodo_CHATVOICE_003(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		f := FixtureTranscriber{Result: VoiceTranscript{State: TranscriptReady, Language: locale, Model: "fixture", Version: "1", Segments: []TranscriptSegment{{StartMS: 0, EndMS: 1000, Text: "fixture " + locale, Confidence: 0.9}}}}
		out, err := f.Transcribe(context.Background(), TranscriptionRequest{DurationMS: 1000})
		if err != nil || out.Text() != "fixture "+locale {
			t.Fatalf("%s result=%+v err=%v", locale, out, err)
		}
		out.Segments[0].Text = "mutated"
		if f.Result.Segments[0].Text == "mutated" {
			t.Fatal("fixture result aliases its source")
		}
	}
}
func TestTodo_CHATVOICE_003_Fault(t *testing.T) {
	out, err := (UnavailableTranscriber{}).Transcribe(context.Background(), TranscriptionRequest{})
	if !errors.Is(err, ErrVoiceUnavailable) || out.State != TranscriptUnavailable {
		t.Fatalf("missing engine=%+v,%v", out, err)
	}
	out, err = (FixtureTranscriber{Failure: ErrVoiceUnavailable}).Transcribe(context.Background(), TranscriptionRequest{})
	if !errors.Is(err, ErrVoiceUnavailable) || out.State != TranscriptFailed {
		t.Fatalf("failed fixture=%+v,%v", out, err)
	}
}
func TestTodo_CHATVOICE_003_Security(t *testing.T) {
	for _, segments := range [][]TranscriptSegment{{{StartMS: 0, EndMS: 1001, Text: "late", Confidence: 1}}, {{StartMS: 0, EndMS: 500, Text: "first", Confidence: 1}, {StartMS: 400, EndMS: 600, Text: "overlap", Confidence: 1}}, {{StartMS: 0, EndMS: 1000, Text: "", Confidence: 1}}, {{StartMS: 0, EndMS: 1000, Text: "text", Confidence: math.NaN()}}} {
		v := VoiceTranscript{State: TranscriptReady, Language: "en-US", Model: "fixture", Version: "1", Segments: segments}
		if !errors.Is(v.Validate(1000), ErrInvalidArgument) {
			t.Fatalf("accepted invalid segments %+v", segments)
		}
	}
}
func TestTodo_CHATVOICE_005(t *testing.T) {
	v := VoiceTranscript{State: TranscriptReady, Language: "en-US", Model: "fixture", Version: "1", Segments: []TranscriptSegment{{StartMS: 0, EndMS: 100, Text: "model output", Confidence: 0.5}}, Correction: "author correction"}
	if v.Text() != "author correction" {
		t.Fatal("automated consumer ignored correction")
	}
	if err := v.Validate(100); err != nil {
		t.Fatal(err)
	}
}
