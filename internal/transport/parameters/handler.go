// Package parameters exposes the authenticated tenant parameter control API.
// It accepts a target scope for writes, while the application derives and
// validates the caller's tenant and full scope path from trusted context.
package parameters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	CollectionPath = "/v1/parameters/"
	ReadConsumerID = "hcmnext.parameters.read"
)

var readConsumer = config.Consumer{Kind: config.ConsumerCapability, ID: ReadConsumerID}

type Service interface {
	ResolveDeclared(context.Context, string, config.Consumer) (config.ParameterValueResolution, error)
	Append(context.Context, config.ParameterValueChange) (config.ParameterValueRevision, error)
}

type Handler struct{ service Service }

func NewHandler(service Service) http.Handler {
	return Handler{service: service}
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		http.Error(w, "parameter service unavailable", http.StatusServiceUnavailable)
		return
	}
	principal, err := trust.MustFromContext(r.Context())
	if err != nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if !principal.HasRole("hcm_admin") {
		http.Error(w, "parameter administration required", http.StatusForbidden)
		return
	}
	key, revisionRoute, ok := parsePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.Method == http.MethodGet && !revisionRoute:
		resolution, err := h.service.ResolveDeclared(r.Context(), key, readConsumer)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resolutionResponse{
			Key: resolution.Key, Environment: string(resolution.Environment), Value: resolution.Value,
			Found: resolution.Found, FromDefault: resolution.FromDefault,
			SourceKind: string(resolution.Source.Kind), SourceID: resolution.Source.ID,
			Revision: resolution.Revision, DefinitionVersion: resolution.DefinitionVersion,
			Author: resolution.Author, Reason: resolution.Reason, Locked: resolution.Locked,
		})
	case r.Method == http.MethodPost && revisionRoute:
		var body appendRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			http.Error(w, "invalid parameter revision request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			http.Error(w, "request must contain one JSON value", http.StatusBadRequest)
			return
		}
		revision, err := h.service.Append(r.Context(), config.ParameterValueChange{
			Key: key, Scope: config.ParameterScope{Kind: config.ParameterScopeKind(body.Scope.Kind), ID: body.Scope.ID},
			Value: body.Value, ExpectedRevision: body.ExpectedRevision, Reason: body.Reason, Locked: body.Locked,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, revisionResponse{
			Key: revision.Key, ScopeKind: string(revision.Scope.Kind), ScopeID: revision.Scope.ID,
			Environment: string(revision.Environment), Revision: revision.Revision, Value: revision.Value,
			Author: revision.Author, Reason: revision.Reason, RecordedAt: revision.RecordedAt,
			Locked: revision.Locked, DefinitionVersion: revision.DefinitionVersion,
		})
	default:
		w.Header().Set("Allow", allowedMethods(revisionRoute))
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type scopeRequest struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type appendRequest struct {
	Scope            scopeRequest `json:"scope"`
	Value            string       `json:"value"`
	ExpectedRevision uint64       `json:"expected_revision"`
	Reason           string       `json:"reason"`
	Locked           bool         `json:"locked"`
}

type resolutionResponse struct {
	Key               string `json:"key"`
	Environment       string `json:"environment"`
	Value             string `json:"value,omitempty"`
	Found             bool   `json:"found"`
	FromDefault       bool   `json:"from_default"`
	SourceKind        string `json:"source_kind,omitempty"`
	SourceID          string `json:"source_id,omitempty"`
	Revision          uint64 `json:"revision,omitempty"`
	DefinitionVersion string `json:"definition_version"`
	Author            string `json:"author,omitempty"`
	Reason            string `json:"reason,omitempty"`
	Locked            bool   `json:"locked"`
}

type revisionResponse struct {
	Key               string    `json:"key"`
	ScopeKind         string    `json:"scope_kind"`
	ScopeID           string    `json:"scope_id"`
	Environment       string    `json:"environment"`
	Revision          uint64    `json:"revision"`
	Value             string    `json:"value"`
	Author            string    `json:"author"`
	Reason            string    `json:"reason"`
	RecordedAt        time.Time `json:"recorded_at"`
	Locked            bool      `json:"locked"`
	DefinitionVersion string    `json:"definition_version"`
}

func parsePath(path string) (key string, revision bool, ok bool) {
	if !strings.HasPrefix(path, CollectionPath) {
		return "", false, false
	}
	rest := strings.TrimPrefix(path, CollectionPath)
	parts := strings.Split(rest, "/")
	if len(parts) == 1 && validPathKey(parts[0]) {
		return parts[0], false, true
	}
	if len(parts) == 2 && parts[1] == "revisions" && validPathKey(parts[0]) {
		return parts[0], true, true
	}
	return "", false, false
}

func validPathKey(key string) bool {
	if key == "" || strings.ContainsAny(key, "\\%?#") {
		return false
	}
	return true
}

func allowedMethods(revision bool) string {
	if revision {
		return http.MethodPost
	}
	return http.MethodGet
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, config.ErrParameterIdentityUnavailable):
		status = http.StatusUnauthorized
	case errors.Is(err, config.ErrParameterConsumerDenied), errors.Is(err, config.ErrParameterScopePathInvalid), errors.Is(err, config.ErrParameterScopeLocked):
		status = http.StatusForbidden
	case errors.Is(err, config.ErrParameterValueMissing):
		status = http.StatusNotFound
	case errors.Is(err, config.ErrParameterRevisionConflict):
		status = http.StatusConflict
	case errors.Is(err, config.ErrParameterValueInvalid), errors.Is(err, config.ErrParameterValueTypeMismatch):
		status = http.StatusBadRequest
	}
	http.Error(w, fmt.Sprintf("parameter request failed: %s", http.StatusText(status)), status)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
