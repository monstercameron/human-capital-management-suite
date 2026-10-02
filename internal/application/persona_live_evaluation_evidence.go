package application

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// PersonaCandidateSourceEvidence rechecks exact candidate definition bytes,
// committed synthetic invoker posts and source references before provider
// egress. It admits no production directory or document bytes.
type PersonaCandidateSourceEvidence struct {
	Definitions *PersonaCandidateDefinitionSource
	Admissions  personaLiveAdmissionReader
	Authority   agentrun.Authority
	Threads     agentinvoke.ThreadReader
	Route       PersonaRunModelRoute
	ToolJournal PersonaRuntimeToolJournal
	ToolSources PersonaRuntimeToolSourceValidator
	Documents   agentdocref.Resolver
}

func (e *PersonaCandidateSourceEvidence) VerifySourceClassification(ctx context.Context, source agentegress.SourceClassificationRequest) error {
	if e == nil || ctx == nil || e.Definitions == nil || e.Admissions == nil || e.Authority == nil || e.Threads == nil {
		return agenteval.ErrPersonaEvaluation
	}
	target := e.Definitions.Target
	binding, ok := ctx.Value(openAIModelDispatchContextKey{}).(openAIModelDispatchBinding)
	if !ok || binding.tenant != target.SyntheticTenantID || source.Tenant != target.SyntheticTenantID || source.Purpose != e.Route.Purpose || (!slices.Contains(source.Provenance, "persona-run:"+binding.runID) && !slices.Contains(source.Provenance, "persona-run:"+binding.stepID)) {
		return agenteval.ErrPersonaEvaluation
	}
	record, err := e.Admissions.GetByID(ctx, binding.runID)
	if err != nil || record.Decision != agentrun.DecisionAccepted || record.Request.Source.TenantID != target.SyntheticTenantID || record.Request.Principal.InvokerID != target.InvokerID || record.Request.Persona == nil || record.Request.Persona.Digest != target.ProfileDigest || record.Request.Persona.ID != target.PersonaID {
		return agenteval.ErrPersonaEvaluation
	}
	current, err := e.Authority.VerifyAdmission(ctx, record.Request)
	if err != nil || !reflect.DeepEqual(current, record.Authority) {
		return agenteval.ErrPersonaEvaluation
	}
	_, profile, manifest, err := e.Definitions.Resolve(ctx)
	if err != nil || target.ModelDigest != "sha256:"+e.Route.Route.Pin.Primary.ProfileDigest {
		return agenteval.ErrPersonaEvaluation
	}
	if source.SourceClass == "persona-model-tool-proposal" || source.SourceClass == "persona-untrusted-tool-result" {
		owner := &PersonaOpenAIModelEvidence{cfg: PersonaOpenAIModelOwnerConfig{ToolJournal: e.ToolJournal, ToolSources: e.ToolSources}}
		return owner.verifyToolSource(ctx, binding, record, source, e.Route)
	}
	if source.SourceClass != "synthetic-fixture" {
		return agenteval.ErrPersonaEvaluation
	}
	posts, err := e.Threads.ReadThread(ctx, agentinvoke.ThreadReadRequest{TenantID: target.SyntheticTenantID, ConversationID: record.Request.Audience.ID, ThreadID: record.Request.Context.ID, InvokerID: target.InvokerID, InvokingPostID: record.Request.Source.Ref, Limit: agentinvoke.MaxThreadPosts})
	if err != nil || len(posts) == 0 {
		return agenteval.ErrPersonaEvaluation
	}
	authoritativeDocumentFields, err := personaAuthoritativeDocumentFields(ctx, e.Documents, e.Threads, record, profile, manifest, e.Route)
	if err != nil {
		return agenteval.ErrPersonaEvaluation
	}
	for name, field := range authoritativeDocumentFields {
		field.source = "synthetic-fixture"
		authoritativeDocumentFields[name] = field
	}
	if _, exists := authoritativeDocumentFields[source.FieldName]; exists {
		if personaAuthoritativeDocumentField(authoritativeDocumentFields, source) {
			return nil
		}
		return agenteval.ErrPersonaEvaluation
	}
	if strings.HasPrefix(source.FieldName, "model.message.") {
		for _, post := range posts {
			if post.TenantID != target.SyntheticTenantID || post.ConversationID != record.Request.Audience.ID || post.ThreadID != record.Request.Context.ID {
				return agenteval.ErrPersonaEvaluation
			}
			if post.AuthorID == target.InvokerID && !post.Bot && source.DataClass == e.Route.InvokerClass && source.ValueDigest == personaRunBytesDigest([]byte(post.Body)) {
				return nil
			}
		}
	} else if strings.HasPrefix(source.FieldName, "model.context.") && source.DataClass == e.Route.ThreadClass {
		for _, post := range posts {
			if post.TenantID != target.SyntheticTenantID || post.ConversationID != record.Request.Audience.ID || post.ThreadID != record.Request.Context.ID {
				return agenteval.ErrPersonaEvaluation
			}
			value := fmt.Sprintf("%s\x00thread\x00%s", post.ID, digestPersonaThreadPost(post))
			if source.ValueDigest == personaRunBytesDigest([]byte(value)) {
				return nil
			}
		}
	}
	return agenteval.ErrPersonaEvaluation
}

var _ agentegress.SourceClassificationVerifier = (*PersonaCandidateSourceEvidence)(nil)
