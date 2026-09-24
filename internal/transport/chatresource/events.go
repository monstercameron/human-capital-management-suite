package chatresource

import (
	"context"
	"net/http"
	"strconv"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type errorReportingWatcher interface {
	WatchConversationWithErrors(context.Context, chatcore.WatchConversationRequest) (<-chan chatcore.WatchEvent, <-chan error, error)
}

// events exposes one bounded pull from the canonical, revocation-aware watch.
// It buffers the response so a terminal permission failure cannot follow a
// partial success response.
func (h handler) events(w http.ResponseWriter, r *http.Request) {
	ctx, p, ok := h.admit(w, r, "/hcmnext.chat.v1.ConversationService/WatchConversation")
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == "" || len(id) > 200 {
		writeError(w, http.StatusBadRequest, "chat.invalid_request")
		return
	}
	q := r.URL.Query()
	for _, key := range []string{"resume_cursor", "max_events", "wait_ms"} {
		if len(q[key]) > 1 {
			writeError(w, http.StatusBadRequest, "chat.invalid_request")
			return
		}
	}
	resume := q.Get("resume_cursor")
	if len(resume) > 2048 || q.Has("after_sequence") {
		writeError(w, http.StatusBadRequest, "chat.invalid_cursor")
		return
	}
	limit := 50
	if q.Has("max_events") {
		v, err := strconv.Atoi(q.Get("max_events"))
		if err != nil || v < 1 || v > 50 {
			writeError(w, http.StatusBadRequest, "chat.invalid_request")
			return
		}
		limit = v
	}
	wait := 1000
	if q.Has("wait_ms") {
		v, err := strconv.Atoi(q.Get("wait_ms"))
		if err != nil || v < 1 || v > 5000 {
			writeError(w, http.StatusBadRequest, "chat.invalid_request")
			return
		}
		wait = v
	}
	watcher, ok := h.service.(errorReportingWatcher)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "chat.unavailable")
		return
	}
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, failures, err := watcher.WatchConversationWithErrors(watchCtx, chatcore.WatchConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: id, ResumeCursor: resume})
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		writeServiceError(w, err)
		return
	}
	if ctx.Err() != nil {
		return
	}
	if stream == nil || failures == nil {
		writeError(w, http.StatusServiceUnavailable, "chat.unavailable")
		return
	}
	deadline := time.NewTimer(time.Duration(wait) * time.Millisecond)
	defer deadline.Stop()
	result := eventPage{Events: make([]eventResponse, 0, limit)}
	for len(result.Events) < limit {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			h.writeEventPage(w, ctx, p, id, result)
			return
		case err, open := <-failures:
			if open && err != nil {
				writeServiceError(w, err)
				return
			}
			failures = nil
		case item, open := <-stream:
			if !open {
				// The producer publishes a terminal error before it closes its
				// failure channel. Read that outcome before returning a page.
				if failures != nil {
					if err, open := <-failures; open && err != nil {
						writeServiceError(w, err)
						return
					}
				}
				h.writeEventPage(w, ctx, p, id, result)
				return
			}
			result.ResumeCursor = item.ResumeCursor
			if item.Event.Kind == chatcore.PostCreated || item.Event.Kind == chatcore.PostEdited || item.Event.Kind == chatcore.PostDeleted {
				result.Events = append(result.Events, projectEvent(item.Event))
			}
		}
	}
	h.writeEventPage(w, ctx, p, id, result)
}

// writeEventPage rechecks the current installation at the response boundary.
// The watch may have queued an event before a revocation during long polling.
func (h handler) writeEventPage(w http.ResponseWriter, ctx context.Context, p chatcore.Principal, id string, page eventPage) {
	if ctx.Err() != nil {
		return
	}
	if _, err := h.service.GetConversation(ctx, chatcore.GetConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: id}); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

type eventPage struct {
	Events       []eventResponse `json:"events"`
	ResumeCursor string          `json:"resume_cursor"`
}

type eventResponse struct {
	Kind     chatcore.ConversationEventKind `json:"kind"`
	Sequence uint64                         `json:"sequence"`
	Revision uint64                         `json:"revision"`
	Post     any                            `json:"post,omitempty"`
	Removed  bool                           `json:"removed"`
}

func projectEvent(v chatcore.ConversationEvent) eventResponse {
	out := eventResponse{Kind: v.Kind, Sequence: v.Sequence, Revision: v.Revision, Removed: v.Removed}
	if v.Post != nil {
		out.Post = postJSON(*v.Post)
	}
	return out
}
