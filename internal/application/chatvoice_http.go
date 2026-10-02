package application

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const ChatVoicePath = "/api/chat/voice/"

type VoiceHTTP struct{ Service VoiceService }
type voiceHTTPError struct {
	Code string `json:"code"`
}

func voiceJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func voiceError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, chat.ErrPermissionDenied):
		status, code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, chat.ErrInvalidArgument):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, chat.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, ErrVoiceOutsideServiceBarred):
		status, code = http.StatusConflict, "outside_barred"
	case errors.Is(err, ErrVoiceTooLong):
		status, code = http.StatusRequestEntityTooLarge, "too_long"
	}
	voiceJSON(w, status, voiceHTTPError{Code: code})
}
func (h VoiceHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := trust.FromContext(r.Context())
	_, admitted := transport.InvocationFromContext(r.Context())
	if !ok || p == nil || !admitted {
		voiceError(w, chat.ErrPermissionDenied)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		voiceJSON(w, http.StatusMethodNotAllowed, voiceHTTPError{Code: "method_not_allowed"})
		return
	}
	principal := chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}
	decode := func(value any) bool {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 6<<20))
		d.DisallowUnknownFields()
		if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
			voiceError(w, chat.ErrInvalidArgument)
			return false
		}
		return true
	}
	switch strings.TrimPrefix(r.URL.Path, ChatVoicePath) {
	case "send":
		var input VoiceSendRequest
		if !decode(&input) {
			return
		}
		out, err := h.Service.Send(r.Context(), principal, input)
		if err != nil {
			voiceError(w, err)
			return
		}
		voiceJSON(w, http.StatusOK, out)
	case "read":
		var input chat.TranscriptionRequest
		if !decode(&input) {
			return
		}
		if h.Service.Transcripts == nil {
			voiceError(w, chat.ErrVoiceUnavailable)
			return
		}
		out, err := h.Service.Transcripts.ReadVoiceTranscript(r.Context(), principal, input)
		if err != nil {
			voiceError(w, err)
			return
		}
		voiceJSON(w, http.StatusOK, out)
	case "retry":
		var input struct {
			Request  chat.TranscriptionRequest
			Revision uint64
		}
		if !decode(&input) {
			return
		}
		if h.Service.Transcripts == nil {
			voiceError(w, chat.ErrVoiceUnavailable)
			return
		}
		out, err := h.Service.Transcripts.RetryVoiceTranscript(r.Context(), principal, input.Request, input.Revision)
		if err != nil {
			voiceError(w, err)
			return
		}
		voiceJSON(w, http.StatusOK, out)
	case "report":
		var input chat.TranscriptionRequest
		if !decode(&input) {
			return
		}
		if h.Service.Transcripts == nil {
			voiceError(w, chat.ErrVoiceUnavailable)
			return
		}
		if err := h.Service.Transcripts.ReportVoiceTranscript(r.Context(), principal, input); err != nil {
			voiceError(w, err)
			return
		}
		voiceJSON(w, http.StatusOK, struct {
			Reported bool `json:"reported"`
		}{true})
	case "correct":
		var input struct {
			Request  chat.TranscriptionRequest
			Revision uint64
			Text     string
		}
		if !decode(&input) {
			return
		}
		if h.Service.Transcripts == nil {
			voiceError(w, chat.ErrVoiceUnavailable)
			return
		}
		out, err := h.Service.Transcripts.CorrectVoiceTranscript(r.Context(), principal, input.Request, input.Revision, input.Text)
		if err != nil {
			voiceError(w, err)
			return
		}
		voiceJSON(w, http.StatusOK, out)
	case "policy":
		var input struct{ TenantID, ConversationID string }
		if !decode(&input) {
			return
		}
		out, err := h.Service.Policy(r.Context(), principal, input.TenantID, input.ConversationID)
		if err != nil {
			voiceError(w, err)
			return
		}
		voiceJSON(w, http.StatusOK, out)
	case "settings":
		var input struct{ TenantID string }
		if !decode(&input) {
			return
		}
		out, err := h.Service.Settings(r.Context(), principal, input.TenantID)
		if err != nil {
			voiceError(w, err)
			return
		}
		voiceJSON(w, http.StatusOK, out)
	case "switch":
		var input VoiceSwitchRequest
		if !decode(&input) {
			return
		}
		if err := h.Service.SetSwitch(r.Context(), principal, input); err != nil {
			voiceError(w, err)
			return
		}
		voiceJSON(w, http.StatusOK, struct {
			Saved bool `json:"saved"`
		}{true})
	case "speak":
		var input VoiceSpeakRequest
		if !decode(&input) {
			return
		}
		if h.Service.Speaker == nil {
			voiceError(w, chat.ErrVoiceUnavailable)
			return
		}
		speech, err := h.Service.Speaker.Speak(r.Context(), principal, input)
		if err != nil {
			voiceError(w, err)
			return
		}
		// The speech is returned once and kept nowhere: no cache, no store. The
		// sentence timings ride beside it only when the engine reported them.
		if len(speech.Timings) > 0 {
			timings := make([]chatui.ListenTiming, 0, len(speech.Timings))
			for _, t := range speech.Timings {
				timings = append(timings, chatui.ListenTiming{Text: t.Text, StartMS: t.StartMS, EndMS: t.EndMS})
			}
			if header := chatui.EncodeListenTimings(timings); header != "" {
				w.Header().Set(chatui.ListenTimingsHeader, header)
				w.Header().Set("Access-Control-Expose-Headers", chatui.ListenTimingsHeader)
			}
		}
		w.Header().Set("Content-Type", speech.ContentType)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(speech.Audio)
	default:
		voiceJSON(w, http.StatusNotFound, voiceHTTPError{Code: "not_found"})
	}
}
func OverlayChatVoice(next http.Handler, service VoiceService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatVoicePath) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, err := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if err != nil {
			voiceJSON(w, err.HTTPStatus(), voiceHTTPError{Code: "permission_denied"})
			return
		}
		VoiceHTTP{Service: service}.ServeHTTP(w, r.WithContext(ctx))
	})
}
