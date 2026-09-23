package cell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type resourceMountService struct{ chatcore.ConversationService }

func TestTodo_CHAT_009_ResourceMount(t *testing.T) {
	now := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "person-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return p, nil })}
	bottom := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := chatHTTPOverlay(bottom, cfg, resourceMountService{})
	r := httptest.NewRequest(http.MethodPost, "/v1/conversations/channel-a/posts", strings.NewReader(`{"body":"hello"}`))
	r.Header.Set("Authorization", "Bearer opaque-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("resource route status=%d body=%s", w.Code, w.Body.String())
	}
}
