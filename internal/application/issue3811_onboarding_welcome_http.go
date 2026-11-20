package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3811: the one route of a welcome card, under /api/chat/ (the prefix the
// page's content security policy lets the Chat client call):
//
//	GET /api/chat/conversations/{id}/onboarding?post={post}   the new hire's own checklist for each card
//
// Offering a welcome is not a route: the deployment's onboarding event consumer
// calls OnboardingWelcome.Offer. The route is read-only and asks the use case,
// which decides who may see what.

// OnboardingWelcomePort is the use case as the HTTP surface uses it.
type OnboardingWelcomePort interface {
	Views(context.Context, *trust.Principal, string, []string) (map[string]OnboardingWelcomeView, error)
}

type onboardingWelcomeReply struct {
	Views map[string]OnboardingWelcomeView `json:"views"`
}

type onboardingWelcomeError struct {
	Code string `json:"code"`
}

// onboardingWelcomeRoute parses the path into its conversation.
func onboardingWelcomeRoute(path string) (conversation string, ok bool) {
	rest, found := strings.CutPrefix(path, chatFormPrefix)
	if !found {
		return "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[1] == "onboarding" && parts[0] != "" {
		return parts[0], true
	}
	return "", false
}

func onboardingWelcomeHTTPError(w http.ResponseWriter, err error) {
	status, body := http.StatusServiceUnavailable, onboardingWelcomeError{Code: "unavailable"}
	switch {
	case errors.Is(err, ErrOnboardingWelcomeInvalid):
		status, body = http.StatusBadRequest, onboardingWelcomeError{Code: "invalid_argument"}
	case errors.Is(err, ErrOnboardingWelcomeNotFound):
		status, body = http.StatusNotFound, onboardingWelcomeError{Code: "not_found"}
	case errors.Is(err, ErrOnboardingWelcomeDenied):
		status, body = http.StatusForbidden, onboardingWelcomeError{Code: "permission_denied"}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// OnboardingWelcomeHandler answers the welcome route for an admitted person.
type OnboardingWelcomeHandler struct{ Welcome OnboardingWelcomePort }

func (h OnboardingWelcomeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	conversation, ok := onboardingWelcomeRoute(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(onboardingWelcomeError{Code: "method_not_allowed"})
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok || verified == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(onboardingWelcomeError{Code: "unauthenticated"})
		return
	}
	if verified.SubjectKind() != trust.SubjectKindHuman {
		onboardingWelcomeHTTPError(w, ErrOnboardingWelcomeDenied)
		return
	}
	if h.Welcome == nil {
		onboardingWelcomeHTTPError(w, ErrOnboardingWelcomeUnavailable)
		return
	}
	views, err := h.Welcome.Views(r.Context(), verified, conversation, r.URL.Query()["post"])
	if err != nil {
		onboardingWelcomeHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(onboardingWelcomeReply{Views: views})
}

// OverlayOnboardingWelcome mounts the welcome route with the admission the
// neighbouring Chat surfaces use. Paths that are not the route pass through.
func OverlayOnboardingWelcome(next http.Handler, welcome OnboardingWelcomePort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := OnboardingWelcomeHandler{Welcome: welcome}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := onboardingWelcomeRoute(r.URL.Path); !ok {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(onboardingWelcomeError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
