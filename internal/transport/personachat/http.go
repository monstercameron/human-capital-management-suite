package personachat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Handler delegates every authorization and projection decision to Surface.
type Handler struct {
	Surface       Surface
	WatchInterval time.Duration
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.Surface == nil {
		writeError(w, ErrUnavailable)
		return
	}
	switch {
	case r.URL.Path == Path && r.Method == http.MethodGet:
		result, err := h.Surface.Directory(r.Context(), r.URL.Query().Get("conversation_id"))
		writeResult(w, result, err)
	case r.URL.Path == Path+"/invocations" && r.Method == http.MethodGet:
		if r.URL.Query().Get("watch") == "1" {
			h.watch(w, r)
			return
		}
		result, err := h.Surface.Progress(r.Context(), r.URL.Query().Get("conversation_id"))
		writeResult(w, result, err)
	case strings.HasPrefix(r.URL.Path, Path+"/invocations/") && strings.HasSuffix(r.URL.Path, "/retry") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, Path+"/invocations/"), "/retry")
		if id == "" || strings.Contains(id, "/") {
			writeError(w, ErrInvalid)
			return
		}
		var input struct {
			IdempotencyKey string `json:"idempotency_key"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, ErrInvalid)
			return
		}
		result, err := h.Surface.Retry(r.Context(), id, input.IdempotencyKey)
		writeResult(w, result, err)
	default:
		http.NotFound(w, r)
	}
}

func (h Handler) watch(w http.ResponseWriter, r *http.Request) {
	initial, err := h.Surface.Progress(r.Context(), r.URL.Query().Get("conversation_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	previous, err := json.Marshal(initial)
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "event: invocations\ndata: %s\n\n", previous); err != nil {
		return
	}
	flusher.Flush()
	interval := h.WatchInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// The application rechecks membership and ownership on every read.
			result, err := h.Surface.Progress(r.Context(), r.URL.Query().Get("conversation_id"))
			if err != nil {
				_, _ = fmt.Fprint(w, "event: unavailable\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			current, err := json.Marshal(result)
			if err != nil {
				return
			}
			if !bytes.Equal(current, previous) {
				if _, err := fmt.Fprintf(w, "event: invocations\ndata: %s\n\n", current); err != nil {
					return
				}
				previous = current
			} else {
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
			}
			flusher.Flush()
		}
	}
}

func writeResult(w http.ResponseWriter, result any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func writeError(w http.ResponseWriter, err error) {
	code, status := "unavailable", http.StatusServiceUnavailable
	switch {
	case errors.Is(err, ErrUnauthenticated):
		code, status = "unauthenticated", http.StatusUnauthorized
	case errors.Is(err, ErrDenied):
		code, status = "denied", http.StatusForbidden
	case errors.Is(err, ErrInvalid):
		code, status = "invalid", http.StatusBadRequest
	case errors.Is(err, ErrConflict):
		code, status = "conflict", http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{code})
}
