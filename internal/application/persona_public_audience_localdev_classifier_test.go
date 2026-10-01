package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type personaLocalDevClassWriterFake struct {
	calls                      int
	tenant, room, post, digest string
	class                      dlp.DataClass
	err                        error
}

func (s *personaLocalDevClassWriterFake) PutPublicChatPostClassification(_ context.Context, tenant, room, post, digest string, class dlp.DataClass) error {
	s.calls++
	s.tenant, s.room, s.post, s.digest, s.class = tenant, room, post, digest, class
	return s.err
}

type personaLocalDevSyntheticPostFake struct{ known bool }

func personaLocalDevClassContext(t *testing.T, tenant, subject string) context.Context {
	t.Helper()
	now := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "accepted-source", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-credential"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

func (s personaLocalDevSyntheticPostFake) IsKnownSyntheticPersonaPost(context.Context, chat.Post) bool {
	return s.known
}

func TestTodo_AGENTP_012_LocalDevSourceClassifier(t *testing.T) {
	classifier, err := NewLocalDevPersonaPostClassifier(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	writer := &personaLocalDevClassWriterFake{}
	classifier.store = writer
	ctx := personaLocalDevClassContext(t, "ironridge-demo", "alice")
	for _, tc := range []struct {
		body  string
		class dlp.DataClass
	}{
		{"@policy-helper How do I request vacation?", dlp.ClassInternal},
		{"salary amount is USD 150000", dlp.ClassCompensation},
		{"patient diagnosis is confidential", dlp.ClassMedical},
		{"routing number for bank account", dlp.ClassBank},
		{"alice@example.test", dlp.ClassPII},
		{"passport and work permit", dlp.ClassImmigration},
		{"disciplinary investigation", dlp.ClassCase},
		{"disability and union membership", dlp.ClassSpecialCategory},
		{"salary and medical diagnosis", dlp.ClassSpecialCategory},
	} {
		post := chat.Post{ID: "post", TenantID: "ironridge-demo", ConversationID: "room", AuthorID: "alice", AuthorHomeTenantID: "ironridge-demo", Body: tc.body}
		if err := classifier.ClassifyAcceptedHumanPost(ctx, post); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(tc.body))
		if writer.class != tc.class || writer.digest != "sha256:"+hex.EncodeToString(sum[:]) || writer.tenant != post.TenantID || writer.room != post.ConversationID || writer.post != post.ID {
			t.Fatalf("classified %q as %s want %s digest=%s", tc.body, writer.class, tc.class, writer.digest)
		}
	}
	post := chat.Post{ID: "post", TenantID: "ironridge-demo", ConversationID: "room", AuthorID: "seed", AuthorHomeTenantID: "ironridge-demo", Body: "Exact fictional policy note"}
	if err := classifier.ClassifyKnownSyntheticPost(context.Background(), post, personaLocalDevSyntheticPostFake{known: true}); err != nil || writer.class != dlp.ClassInternal {
		t.Fatalf("known seed=%s %v", writer.class, err)
	}
	writer.err = errors.New("classification storage unavailable")
	if err := classifier.ClassifyKnownSyntheticPost(context.Background(), post, personaLocalDevSyntheticPostFake{known: true}); !errors.Is(err, writer.err) {
		t.Fatalf("store error lost=%v", err)
	}
}

func TestTodo_AGENTP_012_LocalDevSourceClassifier_Security(t *testing.T) {
	classifier, err := NewLocalDevPersonaPostClassifier(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	writer := &personaLocalDevClassWriterFake{}
	classifier.store = writer
	ctx := personaLocalDevClassContext(t, "ironridge-demo", "alice")
	base := chat.Post{ID: "post", TenantID: "ironridge-demo", ConversationID: "room", AuthorID: "alice", AuthorHomeTenantID: "ironridge-demo", Body: "Policy question"}
	for _, mutate := range []func(*chat.Post){func(p *chat.Post) { p.TenantID = "production" }, func(p *chat.Post) { p.AuthorID = "another-human" }, func(p *chat.Post) { p.AuthorHomeTenantID = "foreign" }, func(p *chat.Post) { p.ID = "" }, func(p *chat.Post) { p.ConversationID = "" }, func(p *chat.Post) { p.Body = "" }, func(p *chat.Post) { p.Deleted = true }} {
		post := base
		mutate(&post)
		if err := classifier.ClassifyAcceptedHumanPost(ctx, post); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
			t.Fatalf("forged post=%+v err=%v", post, err)
		}
	}
	if err := classifier.ClassifyAcceptedHumanPost(context.Background(), base); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("unauthenticated=%v", err)
	}
	if err := classifier.ClassifyAcceptedHumanPost(personaRunAudienceContext(t, "ironridge-demo", "alice"), base); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("expired session=%v", err)
	}
	if err := classifier.ClassifyKnownSyntheticPost(ctx, base, personaLocalDevSyntheticPostFake{known: false}); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("unverified seed=%v", err)
	}
	classifier.enabled = false
	if err := classifier.ClassifyAcceptedHumanPost(ctx, base); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("production flag=%v", err)
	}
	if writer.calls != 0 {
		t.Fatalf("unauthorized classifications written=%d", writer.calls)
	}
}

func TestTodo_AGENTP_012_LocalDevReplyClassifier(t *testing.T) {
	classifier, err := NewLocalDevPersonaPostClassifier(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	writer := &personaLocalDevClassWriterFake{}
	classifier.store = writer
	ctx := personaLocalDevClassContext(t, "ironridge-demo", "alice")
	for _, tc := range []struct {
		text  string
		class dlp.DataClass
	}{
		{"Request vacation in the employee portal.", dlp.ClassInternal},
		{"The employee salary is USD 150000.", dlp.ClassCompensation},
		{"The patient diagnosis is confidential.", dlp.ClassMedical},
		{"Send it to alice@example.test.", dlp.ClassPII},
		{"The bank account uses this routing number.", dlp.ClassBank},
		{"Keep the passport and visa confidential.", dlp.ClassImmigration},
		{"This disciplinary investigation is confidential.", dlp.ClassCase},
		{"The employee has a disability.", dlp.ClassSpecialCategory},
		{"The salary and patient diagnosis are confidential.", dlp.ClassSpecialCategory},
	} {
		class, err := classifier.ClassifyPersonaReplyText(ctx, "ironridge-demo", tc.text)
		if err != nil || class != tc.class {
			t.Fatalf("reply=%q class=%s want=%s err=%v", tc.text, class, tc.class, err)
		}
	}
	if writer.calls != 0 {
		t.Fatalf("generated text wrote a source annotation: %d", writer.calls)
	}
	classifier.inspector, err = dlp.NewInspector(dlp.Detector{ID: "broken-reviewed-detector", Class: dlp.ClassMedical, Severity: dlp.SeverityHigh, Matcher: func([]byte) []dlp.Location { return []dlp.Location{{Start: -1, End: 1}} }})
	if err != nil {
		t.Fatal(err)
	}
	if class, err := classifier.ClassifyPersonaReplyText(ctx, "ironridge-demo", "safe text"); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) || class != "" {
		t.Fatalf("failed inspector supplied class=%s err=%v", class, err)
	}
}

func TestTodo_AGENTP_012_LocalDevReplyClassifier_Security(t *testing.T) {
	ctx := personaLocalDevClassContext(t, "ironridge-demo", "alice")
	for _, tc := range []struct {
		name, tenant, text         string
		ctx                        context.Context
		disabled, missingInspector bool
	}{
		{name: "nonlocal", tenant: "ironridge-demo", text: "safe text", ctx: ctx, disabled: true},
		{name: "production tenant", tenant: "production", text: "safe text", ctx: ctx},
		{name: "foreign caller", tenant: "ironridge-demo", text: "safe text", ctx: personaLocalDevClassContext(t, "another-tenant", "alice")},
		{name: "tenant alias", tenant: " ironridge-demo ", text: "safe text", ctx: ctx},
		{name: "anonymous", tenant: "ironridge-demo", text: "safe text", ctx: context.Background()},
		{name: "missing context", tenant: "ironridge-demo", text: "safe text"},
		{name: "expired caller", tenant: "ironridge-demo", text: "safe text", ctx: personaRunAudienceContext(t, "ironridge-demo", "alice")},
		{name: "blank reply", tenant: "ironridge-demo", text: " \n\t ", ctx: ctx},
		{name: "missing inspector", tenant: "ironridge-demo", text: "safe text", ctx: ctx, missingInspector: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			classifier, err := NewLocalDevPersonaPostClassifier(nil, !tc.disabled)
			if err != nil {
				t.Fatal(err)
			}
			if tc.missingInspector {
				classifier.inspector = nil
			}
			if class, err := classifier.ClassifyPersonaReplyText(tc.ctx, tc.tenant, tc.text); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) || class != "" {
				t.Fatalf("unsupported reply class=%s err=%v", class, err)
			}
		})
	}
	var missing *LocalDevPersonaPostClassifier
	if class, err := missing.ClassifyPersonaReplyText(ctx, "ironridge-demo", "safe"); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) || class != "" {
		t.Fatalf("missing classifier supplied class=%s err=%v", class, err)
	}
}
