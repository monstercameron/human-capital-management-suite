package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestTodo_AGENTP_011_RuntimeToolDocumentClassificationIntegration(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	identity := PersonaRunT0ToolInvocation{TenantID: "tenant-runtime-doc", InvokerID: "owner-a"}
	doc, err := svc.store.CreateDocument(ctx, identity.TenantID, identity.InvokerID, "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	source := DatabasePersonaRuntimeDocumentVersions{Store: svc.store}
	for _, test := range []struct {
		label string
		want  trustdlp.DataClass
	}{{"PUBLIC", trustdlp.ClassPublic}, {"INTERNAL", trustdlp.ClassInternal}, {"CONFIDENTIAL", trustdlp.ClassConfidential}, {"RESTRICTED", trustdlp.ClassRestricted}, {"UNKNOWN", ""}} {
		v, err := svc.store.InsertVersion(ctx, identity.TenantID, documenthubstore.Version{DocumentID: doc, CreatorID: identity.InvokerID, Title: "Policy", Markdown: "Policy text", Classification: test.label})
		if err != nil {
			t.Fatal(err)
		}
		got, err := source.PersonaRuntimeDocumentClass(ctx, identity, doc, v.ID)
		if test.want == "" {
			if !errors.Is(err, errPersonaRuntimeTools) {
				t.Fatalf("unknown source classification accepted: %v", err)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Fatalf("class=%s want=%s err=%v", got, test.want, err)
		}
		foreign := identity
		foreign.TenantID = "other-tenant"
		if _, err := source.PersonaRuntimeDocumentClass(ctx, foreign, doc, v.ID); !errors.Is(err, errPersonaRuntimeTools) {
			t.Fatalf("cross tenant read accepted: %v", err)
		}
		foreign = identity
		foreign.InvokerID = "stranger"
		if _, err := source.PersonaRuntimeDocumentClass(ctx, foreign, doc, v.ID); !errors.Is(err, errPersonaRuntimeTools) {
			t.Fatalf("cross invoker read accepted: %v", err)
		}
	}
	if _, err := (DatabasePersonaRuntimeDocumentVersions{}).PersonaRuntimeDocumentClass(ctx, identity, doc, "version"); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("missing store accepted: %v", err)
	}
}
