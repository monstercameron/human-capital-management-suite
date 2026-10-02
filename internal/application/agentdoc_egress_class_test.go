package application

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestAgentDocEgress_ClassificationBoundary(t *testing.T) {
	ctx := context.Background()
	admission := agentrun.Record{Request: agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "tenant-a"}, Principal: agentrun.PrincipalChain{InvokerID: "user-a"}}}
	invoker := agentdocref.Invoker{TenantID: "tenant-a", SubjectID: "user-a"}
	classes := []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}
	route := PersonaRunModelRoute{ProfileClass: trustdlp.ClassInternal, Egress: agentegress.Profile{AllowedClasses: classes}, Route: agentmodel.RouteRequest{Task: agentmodel.TaskProfile{DataClasses: []string{"PUBLIC", "INTERNAL"}}}}
	for _, label := range []string{"INTERNAL", "PUBLIC", "CONFIDENTIAL", "RESTRICTED", "UNKNOWN"} {
		t.Run(label, func(t *testing.T) {
			resolver, reference := agentDocEgressHubDocument(t, "tenant-a", true, label)
			documents, _, err := resolver.Resolve(ctx, invoker, []agentdocref.Reference{reference})
			if err != nil || len(documents) != 1 {
				t.Fatalf("resolve classified document: %v count=%d", err, len(documents))
			}
			class, err := personaReferenceDocumentDataClass(ctx, resolver, admission, documents, route)
			if label == "PUBLIC" || label == "INTERNAL" || label == "CONFIDENTIAL" {
				want := trustdlp.DataClass(label)
				if label == "CONFIDENTIAL" {
					want = trustdlp.ClassConfidential
				}
				if err != nil || class != want {
					t.Fatalf("class=%s err=%v want=%s", class, err, want)
				}
			} else if !errors.Is(err, errAgentDocumentResolution) || class != "" {
				t.Fatalf("classification outside route accepted: class=%s err=%v", class, err)
			}
			owner := resolver.(personaReferenceDocumentClassificationSource)
			for _, foreign := range []agentdocref.Invoker{{TenantID: "tenant-b", SubjectID: "user-a"}, {TenantID: "tenant-a", SubjectID: "user-b"}} {
				if class, err := owner.PersonaReferenceDocumentClass(ctx, foreign, documents[0]); err == nil || class != "" {
					t.Fatalf("classification escaped invoker access: invoker=%+v class=%s err=%v", foreign, class, err)
				}
			}
			if label == "PUBLIC" {
				narrow := route
				narrow.Route.Task.DataClasses = []string{"INTERNAL"}
				if _, err := personaReferenceDocumentDataClass(ctx, resolver, admission, documents, narrow); !errors.Is(err, errAgentDocumentResolution) {
					t.Fatalf("document exceeded task class ceiling: %v", err)
				}
				narrow = route
				narrow.Egress.AllowedClasses = []trustdlp.DataClass{trustdlp.ClassInternal}
				if _, err := personaReferenceDocumentDataClass(ctx, resolver, admission, documents, narrow); !errors.Is(err, errAgentDocumentResolution) {
					t.Fatalf("document exceeded egress class ceiling: %v", err)
				}
			}
		})
	}
	if _, err := personaReferenceDocumentDataClass(ctx, nil, admission, []agentdocref.ResolvedDocument{{Version: 1}}, route); !errors.Is(err, errAgentDocumentResolution) {
		t.Fatalf("missing classification owner accepted: %v", err)
	}
}

type agentDocEgressMixedClasses struct{ agentDocEgressResolver }

func (agentDocEgressMixedClasses) PersonaReferenceDocumentClass(_ context.Context, _ agentdocref.Invoker, document agentdocref.ResolvedDocument) (trustdlp.DataClass, error) {
	if document.Version == 1 {
		return trustdlp.ClassPublic, nil
	}
	return trustdlp.ClassInternal, nil
}

func TestAgentDocEgress_MixedClassification(t *testing.T) {
	route := PersonaRunModelRoute{ProfileClass: trustdlp.ClassInternal, Egress: agentegress.Profile{AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}}, Route: agentmodel.RouteRequest{Task: agentmodel.TaskProfile{DataClasses: []string{"PUBLIC", "INTERNAL"}}}}
	if _, err := personaReferenceDocumentDataClass(context.Background(), agentDocEgressMixedClasses{}, agentrun.Record{}, []agentdocref.ResolvedDocument{{Version: 1}, {Version: 2}}, route); !errors.Is(err, errAgentDocumentResolution) {
		t.Fatalf("mixed document classes accepted: %v", err)
	}
}

func TestAgentDocEgress_LocalProcessingTerms(t *testing.T) {
	terms := LocalPersonaOpenAIProcessingTerms("local-test-model")
	var searched []trustdlp.DataClass
	for _, rule := range terms.SourceRules {
		if rule.Class == "persona-untrusted-tool-result" {
			searched = rule.Classes
		}
	}
	if !slices.Equal(searched, []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}) {
		t.Fatalf("policy-search provider classes=%v", searched)
	}
	for _, source := range []string{"persona-untrusted-reference-document", "persona-reference-document"} {
		found := false
		for _, rule := range terms.SourceRules {
			if rule.Class == source {
				found = slices.Equal(rule.Classes, searched)
			}
		}
		if !found {
			t.Fatalf("reference source %s lacks the policy-search class rule", source)
		}
	}
}
