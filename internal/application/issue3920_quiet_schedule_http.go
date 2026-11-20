package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3920: GET /api/chat/quiet-schedule returns the signed-in person's weekly
// quiet-hours schedule (one entry per weekday, Sunday first), any one-off pause
// or temporary override, whether optional notices are quiet right now, and the
// next moment they pause or resume. POST saves the weekly schedule and the pause
// together, against the revision the client read. The person always comes from
// the session. The daily interval stays on the existing quiet-hours route.
const ChatQuietSchedulePath = "/api/chat/quiet-schedule"

type ChatQuietSchedulePort interface {
	QuietHours(ctx context.Context, p chat.Principal) (chatrecipient.QuietHours, error)
	QuietSchedule(ctx context.Context, p chat.Principal, now time.Time) (chatrecipient.QuietSchedule, error)
	PutQuietSchedule(ctx context.Context, p chat.Principal, s chatrecipient.QuietSchedule, expected uint64, now time.Time) (chatrecipient.QuietSchedule, error)
}

type ChatQuietDayJSON struct {
	Mode  string `json:"mode"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type ChatQuietPauseJSON struct {
	Kind  string     `json:"kind"`
	Until *time.Time `json:"until,omitempty"`
}

type ChatQuietNextJSON struct {
	At    time.Time `json:"at"`
	Quiet bool      `json:"quiet"`
}

type ChatQuietScheduleReply struct {
	// The daily interval and zone, as kept by the quiet-hours route.
	Timezone    string `json:"timezone"`
	Enabled     bool   `json:"enabled"`
	StartMinute int    `json:"start_minute"`
	EndMinute   int    `json:"end_minute"`
	// The weekly schedule and the one-off pause.
	Revision   uint64             `json:"revision"`
	WeekActive bool               `json:"week_active"`
	Days       []ChatQuietDayJSON `json:"days"`
	Pause      ChatQuietPauseJSON `json:"pause"`
	Now        time.Time          `json:"now"`
	QuietNow   bool               `json:"quiet_now"`
	Next       *ChatQuietNextJSON `json:"next,omitempty"`
}

type ChatQuietScheduleCommand struct {
	ExpectedRevision uint64             `json:"expected_revision"`
	WeekActive       bool               `json:"week_active"`
	Days             []ChatQuietDayJSON `json:"days"`
	Pause            ChatQuietPauseJSON `json:"pause"`
}

type ChatQuietScheduleError struct {
	Code   string `json:"code"`
	Day    *int   `json:"day,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func chatQuietScheduleError(w http.ResponseWriter, err error) {
	out := ChatQuietScheduleError{}
	status := http.StatusServiceUnavailable
	out.Code = "unavailable"
	var week *chatrecipient.WeekError
	switch {
	case errors.Is(err, chat.ErrUnauthenticated):
		status, out.Code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, chat.ErrPermissionDenied):
		status, out.Code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, chat.ErrNotFound):
		status, out.Code = http.StatusNotFound, "not_found"
	case errors.Is(err, chat.ErrConflict):
		status, out.Code = http.StatusConflict, "conflict"
	case errors.Is(err, chat.ErrInvalidArgument):
		status, out.Code = http.StatusBadRequest, "invalid_argument"
		if errors.As(err, &week) {
			day := int(week.Day)
			out.Day, out.Reason = &day, week.Reason
		}
	default:
		// The body never carries the cause.
		if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_quiet_schedule_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}

func chatQuietSchedulePort(runtime composedChat) ChatQuietSchedulePort {
	if runtime.extensions == nil || runtime.extensions.Recipients == nil {
		return nil
	}
	return runtime.extensions.Recipients
}

type ChatQuietScheduleHandler struct {
	Service ChatQuietSchedulePort
	Now     func() time.Time
}

func (h ChatQuietScheduleHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func chatQuietScheduleReply(q chatrecipient.QuietHours, now time.Time) ChatQuietScheduleReply {
	out := ChatQuietScheduleReply{Timezone: q.Timezone, Enabled: q.Enabled, StartMinute: q.StartMinute, EndMinute: q.EndMinute,
		Revision: q.Schedule.Revision, WeekActive: q.Schedule.Week.Active, Days: make([]ChatQuietDayJSON, 7), Now: now,
		Pause: ChatQuietPauseJSON{Kind: q.Schedule.Pause.Kind}}
	for i, d := range q.Schedule.Week.Days {
		mode := d.Mode
		if mode == "" {
			mode = chatrecipient.QuietDayNone
		}
		out.Days[i] = ChatQuietDayJSON{Mode: mode, Start: d.StartMinute, End: d.EndMinute}
	}
	if q.Schedule.Pause.Kind != "" {
		until := q.Schedule.Pause.Until.UTC()
		out.Pause.Until = &until
	}
	out.QuietNow = chatrecipient.QuietAt(q, now)
	if at, quiet, ok := chatrecipient.NextQuietChange(q, now); ok {
		out.Next = &ChatQuietNextJSON{At: at.UTC(), Quiet: quiet}
	}
	return out
}

func (h ChatQuietScheduleHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != ChatQuietSchedulePath {
		http.NotFound(w, r)
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok {
		chatQuietScheduleError(w, chat.ErrUnauthenticated)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	// Only the person's own, signed-in human identity may use their schedule.
	// Decide that before the body is read.
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(ChatQuietScheduleError{Code: "method_not_allowed"})
		return
	}
	if err := chat.ValidateSavedOwner(r.Context(), p, p.TenantID); err != nil {
		chatQuietScheduleError(w, err)
		return
	}
	var command ChatQuietScheduleCommand
	if r.Method == http.MethodPost {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF || len(command.Days) != 7 {
			chatQuietScheduleError(w, chat.ErrInvalidArgument)
			return
		}
	}
	if h.Service == nil {
		chatQuietScheduleError(w, chat.ErrUnavailable)
		return
	}
	now := h.now()
	if r.Method == http.MethodPost {
		next := chatrecipient.QuietSchedule{Week: chatrecipient.QuietWeek{Active: command.WeekActive}, Pause: chatrecipient.QuietPause{Kind: command.Pause.Kind}}
		for i, d := range command.Days {
			next.Week.Days[i] = chatrecipient.QuietDay{Mode: d.Mode, StartMinute: d.Start, EndMinute: d.End}
		}
		if command.Pause.Until != nil {
			next.Pause.Until = command.Pause.Until.UTC()
		}
		if _, err := h.Service.PutQuietSchedule(r.Context(), p, next, command.ExpectedRevision, now); err != nil {
			chatQuietScheduleError(w, err)
			return
		}
	}
	hours, err := h.Service.QuietHours(r.Context(), p)
	if err != nil {
		chatQuietScheduleError(w, err)
		return
	}
	if !hours.Schedule.Pause.Until.After(now) {
		hours.Schedule.Pause = chatrecipient.QuietPause{}
	}
	_ = json.NewEncoder(w).Encode(chatQuietScheduleReply(hours, now))
}

func OverlayChatQuietSchedule(next http.Handler, service ChatQuietSchedulePort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatQuietScheduleHandler{Service: service}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatQuietSchedulePath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(ChatQuietScheduleError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
