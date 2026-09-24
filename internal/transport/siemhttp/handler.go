// Package siemhttp serves the authenticated customer SIEM feed endpoint.
package siemhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	app "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/siem"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const Path = "/api/v1/security/siem/feed"

type Reader interface {
	ReadAuthenticated(context.Context, *trust.Principal, string, siem.Cursor, int, time.Time, *app.SIEMCredentialRing) (siem.Feed, error)
}

type RingResolver interface {
	ResolveSIEMRing(context.Context, *trust.Principal, string) (*app.SIEMCredentialRing, error)
}

type Handler struct {
	Config            transport.Config
	Reader            Reader
	Rings             RingResolver
	ClassifyReadError func(error) int
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if key != "subscription_id" && key != "after" && key != "digest" && key != "limit" || len(values) != 1 {
			http.Error(w, "invalid feed query", http.StatusBadRequest)
			return
		}
	}
	id := query.Get("subscription_id")
	if id == "" || strings.TrimSpace(id) != id {
		http.Error(w, "invalid feed query", http.StatusBadRequest)
		return
	}
	var cursor siem.Cursor
	var err error
	if raw := query.Get("after"); raw != "" {
		cursor.Sequence, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || cursor.Sequence == 0 {
			http.Error(w, "invalid feed cursor", http.StatusBadRequest)
			return
		}
		cursor.Digest = query.Get("digest")
		if len(cursor.Digest) != 64 {
			http.Error(w, "invalid feed cursor", http.StatusBadRequest)
			return
		}
	} else if query.Has("digest") {
		http.Error(w, "invalid feed cursor", http.StatusBadRequest)
		return
	}
	limit := 100
	if raw := query.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			http.Error(w, "invalid feed limit", http.StatusBadRequest)
			return
		}
	}
	if h.Config.Verifier == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	scheme, token := parseAuthorization(r.Header.Get(transport.AuthorizationMetadataKey))
	principal, err := h.Config.Verifier.Verify(r.Context(), trust.Credential{Scheme: scheme, Token: token, Audience: h.Config.Audience})
	if err != nil || principal == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if h.Rings == nil || h.Reader == nil {
		http.Error(w, "feed unavailable", http.StatusServiceUnavailable)
		return
	}
	ring, err := h.Rings.ResolveSIEMRing(r.Context(), principal, id)
	if err != nil || ring == nil {
		http.Error(w, "feed unavailable", http.StatusForbidden)
		return
	}
	now := time.Now
	if h.Config.Now != nil {
		now = h.Config.Now
	}
	cursor.Tenant = pgstore.TenantID(string(principal.Tenant())).String()
	feed, err := h.Reader.ReadAuthenticated(r.Context(), principal, id, cursor, limit, now(), ring)
	if err != nil {
		status := http.StatusForbidden
		if h.ClassifyReadError != nil {
			status = h.ClassifyReadError(err)
		} else if errors.Is(err, siem.ErrInvalidCursor) {
			status = http.StatusConflict
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(feed)
}

func parseAuthorization(value string) (string, string) {
	value = strings.TrimSpace(value)
	if scheme, token, ok := strings.Cut(value, " "); ok {
		return scheme, strings.TrimSpace(token)
	}
	return "", value
}
