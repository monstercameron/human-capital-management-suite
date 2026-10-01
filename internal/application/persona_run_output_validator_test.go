package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

func TestTodo_AGENTP_012_ChatReplyDescriptorEnforcesItsTypedSchema(t *testing.T) {
	descriptor := PersonaChatReplyToolDescriptor()
	if descriptor.Name != "persona.chat_reply" || descriptor.Schema != PersonaChatReplySchema || descriptor.Class != agentsecurity.ToolDraft {
		t.Fatalf("unexpected chat reply descriptor: %#v", descriptor)
	}
	result, err := descriptor.Validate(PersonaChatReply{Text: "A plain answer."})
	if err != nil || !result.Validated || result.Schema != PersonaChatReplySchema {
		t.Fatalf("valid reply descriptor result = %#v, %v", result, err)
	}
	if _, err := descriptor.Validate("forged dynamic type"); err == nil {
		t.Fatal("descriptor accepted an untyped value")
	}
	if _, err := descriptor.Validate(PersonaChatReply{Text: "Use https://example.com"}); err == nil {
		t.Fatal("descriptor accepted a remote link")
	}
	reply := PersonaChatReply{Text: "A plain answer."}
	if len(reply.DraftFields()) != 0 || len(reply.DraftReferences()) != 0 || len(reply.DraftClaims()) != 0 {
		t.Fatal("chat reply unexpectedly declares business fields, records, or claims")
	}
	detached, err := reply.DetachDraft()
	if err != nil || detached == nil {
		t.Fatalf("detach reply = %v, %v", detached, err)
	}
	canonical, err := reply.DraftCanonicalBytes()
	if err != nil || string(canonical) != reply.Text {
		t.Fatalf("canonical reply = %q, %v", canonical, err)
	}
}

func TestTodo_AGENTP_012_ValidatorRequiresComposition(t *testing.T) {
	if _, err := NewPersonaRunOutputValidator(PersonaRunOutputValidatorConfig{}); !errors.Is(err, ErrPersonaRunOutputValidatorUnavailable) {
		t.Fatalf("incomplete validator composition err = %v", err)
	}
}

type personaRunChatReplyAuthorityFake struct {
	authority          PersonaRunChatReplyAuthority
	observationBinding PersonaRunOutputObservationBinding
	err                error
}

func (f personaRunChatReplyAuthorityFake) ResolvePersonaRunChatReplyAuthority(context.Context, agentrun.Record, runstate.Run) (PersonaRunChatReplyAuthority, error) {
	return f.authority, f.err
}

func (f personaRunChatReplyAuthorityFake) ResolvePersonaRunOutputObservationAuthority(_ context.Context, binding PersonaRunOutputObservationBinding, _ agentrun.Record, _ runstate.Run) (PersonaRunOutputObservationAuthority, error) {
	if f.err != nil {
		return PersonaRunOutputObservationAuthority{}, f.err
	}
	if f.observationBinding != binding {
		return PersonaRunOutputObservationAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	return PersonaRunOutputObservationAuthority{Binding: f.observationBinding, Output: f.authority}, nil
}

type personaRunFinalOutputPersisterFake struct {
	projection agentsecurity.FinalOutputPersistence
	calls      int
	err        error
}

func (f *personaRunFinalOutputPersisterFake) PersistPersonaRunFinalOutput(_ context.Context, projection agentsecurity.FinalOutputPersistence) error {
	f.calls++
	f.projection = projection
	return f.err
}

func personaRunOutputFixture(t *testing.T) (*SealedPersonaRunOutputValidator, agentrun.Record, runstate.Run, *personaRunFinalOutputPersisterFake, *personaRunChatReplyAuthorityFake) {
	t.Helper()
	descriptor := PersonaChatReplyToolDescriptor()
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{descriptor})
	if err != nil {
		t.Fatal(err)
	}
	call := agentsecurity.ToolCall{
		Agent:      agentsecurity.AgentIdentity{Identity: "workload:persona", AgentID: "agent-a", Tenant: "tenant-a", Purpose: "persona-chat", ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 2},
		Delegation: []agentsecurity.DelegationLink{{GrantID: "grant-a", Delegator: "alice", Delegate: "agent-a", Tenant: "tenant-a", Purpose: "persona-chat", ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 2}},
		Tenant:     "tenant-a", Purpose: "persona-chat", Tool: "persona.chat_reply", Capability: "persona.reply", Version: 1,
		Nonce: "invocation-a", Args: map[string]any{"invocation": "invocation-a"}, InputTaint: []string{string(agentsecurity.TaintDerived)},
		Provenance: []string{PersonaChatReplyProvenance, "chat.current"}, CostBudget: 1, DataScope: []string{"chat.current"},
	}
	call.ArgsDigest, err = agentsecurity.DigestArguments(call.Args)
	if err != nil {
		t.Fatal(err)
	}
	securityAdmission, err := gateway.Admit(call)
	if err != nil {
		t.Fatal(err)
	}
	postText := "Can you help me understand this policy?"
	postHash := sha256.Sum256([]byte(postText))
	evidence, err := gateway.Observe(agentsecurity.SourceChat, postText, agentsecurity.KindObservation, agentsecurity.Citation{
		SourceID: "chat:post-a", Location: "conversation:room-a/post-a", Digest: "sha256:" + hex.EncodeToString(postHash[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	record := agentrun.Record{
		ID: "invocation-a", RequestDigest: "sha256:request", Decision: agentrun.DecisionAccepted,
		Request: agentrun.Request{
			Source:         agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourcePersonaMention, Key: "invocation-a", Ref: "post-a"},
			Persona:        &agentrun.PersonaRef{ID: "persona-a", Version: "7", Digest: "sha256:" + strings.Repeat("a", 64)},
			InstallationID: "install-a", Principal: agentrun.PrincipalChain{InvokerID: "alice"},
			Agent:   agentrun.VersionRef{AgentID: "agent-a", Version: "4", Digest: "sha256:" + strings.Repeat("d", 64)},
			Purpose: "persona-chat", Audience: agentrun.AudienceScope{ID: "room-a"}, Context: agentrun.ContextScope{ID: "thread-a"},
		},
	}
	run := runstate.Run{ID: record.ID, TenantID: "tenant-a", AdmissionID: record.ID, RequestDigest: record.RequestDigest, AgentID: "agent-a", AgentVersion: "4", AgentDigest: record.Request.Agent.Digest, ActorID: "alice"}
	persister := &personaRunFinalOutputPersisterFake{}
	observationBinding := PersonaRunOutputObservationBinding{
		SyntheticTenantID: "tenant-a", EvaluationRunID: "eval-run-1", CaseID: "case-1",
		CaseDigest: "sha256:" + strings.Repeat("e", 64), PersonaDigest: record.Request.Persona.Digest,
		ModelDigest: "sha256:" + strings.Repeat("f", 64), InvocationID: record.Request.Source.Key,
		ConversationID: record.Request.Audience.ID, ThreadID: record.Request.Context.ID, PostID: record.Request.Source.Ref,
	}
	authority := &personaRunChatReplyAuthorityFake{authority: PersonaRunChatReplyAuthority{
		Schema:      PersonaRunOutputSchemaRef{ID: PersonaChatReplySchema, Version: 1, Digest: PersonaChatReplySchemaDigest, PersonaDigest: record.Request.Persona.Digest},
		ModelDigest: observationBinding.ModelDigest, Gateway: gateway, Admission: securityAdmission, Grounding: []agentsecurity.Datum{evidence},
	}, observationBinding: observationBinding}
	validator, err := NewPersonaRunOutputValidator(PersonaRunOutputValidatorConfig{Authority: authority, ObservationAuthority: authority, Persister: persister})
	if err != nil {
		t.Fatal(err)
	}
	return validator, record, run, persister, authority
}

func TestTodo_AGENTP_012_ChatReplyIsGroundedSealedAndBound(t *testing.T) {
	schemaDigest := sha256.Sum256([]byte(PersonaChatReplyJSONSchema))
	if got := "sha256:" + hex.EncodeToString(schemaDigest[:]); got != PersonaChatReplySchemaDigest {
		t.Fatalf("chat reply schema digest = %q, want %q", got, PersonaChatReplySchemaDigest)
	}
	validator, admission, run, persister, _ := personaRunOutputFixture(t)
	projection, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: "I can help explain the policy.", Finish: agentmodel.FinishComplete})
	if err != nil {
		t.Fatal(err)
	}
	identity := projection.Identity()
	if identity.TenantID != "tenant-a" || identity.InvocationID != "invocation-a" || identity.AdmissionID != admission.ID || identity.RunID != run.ID || identity.InvokerID != "alice" || identity.ConversationID != "room-a" || identity.ThreadID != "thread-a" || identity.PostID != "post-a" || identity.PersonaID != "persona-a" || identity.PersonaVersion != "7" || identity.InstallationID != "install-a" || identity.OutputID == "" {
		t.Fatalf("projection identity is not bound to admission: %#v", identity)
	}
	if projection.Digest() == "" || projection.SemanticDigest() == "" || persister.calls != 1 {
		t.Fatalf("sealed projection was not durably persisted: digest=%q semantic=%q calls=%d", projection.Digest(), projection.SemanticDigest(), persister.calls)
	}
	_, answer, err := projection.Payload()
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Parts) != 1 || answer.Parts[0].Text != "I can help explain the policy." || len(answer.Parts[0].Citations) != 1 || answer.Parts[0].Citations[0].SourceID != "chat:post-a" {
		t.Fatalf("reply lost typed grounding: %#v", answer)
	}
}

func TestTodo_AGENTP_012_ChatReplyRefusesUngroundedOrUnsafeText(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{name: "remote link", text: "Read https://outside.example/policy"},
		{name: "model citation", text: "The policy says this [1]."},
		{name: "empty", text: "  \n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validator, admission, run, persister, authority := personaRunOutputFixture(t)
			authority.authority.Grounding = nil
			_, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: tc.text, Finish: agentmodel.FinishComplete})
			if err == nil || persister.calls != 0 {
				t.Fatalf("unsafe/ungrounded reply accepted or persisted: err=%v calls=%d", err, persister.calls)
			}
		})
	}
}

func TestTodo_AGENTP_012_ChatReplyRejectsChangedSchemaAndPersisterFailure(t *testing.T) {
	t.Run("changed published schema", func(t *testing.T) {
		validator, admission, run, persister, authority := personaRunOutputFixture(t)
		authority.authority.Schema.Digest = "sha256:" + strings.Repeat("c", 64)
		_, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: "A grounded response.", Finish: agentmodel.FinishComplete})
		if !errors.Is(err, ErrPersonaRunOutputValidatorUnavailable) || persister.calls != 0 {
			t.Fatalf("changed schema was not refused before persistence: err=%v calls=%d", err, persister.calls)
		}
	})
	t.Run("store error does not return success", func(t *testing.T) {
		validator, admission, run, persister, _ := personaRunOutputFixture(t)
		persister.err = errors.New("store unavailable")
		projection, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: "A grounded response.", Finish: agentmodel.FinishComplete})
		if err == nil || projection.Digest() != "" || persister.calls != 1 {
			t.Fatalf("persistence failure contract mismatch: digest=%q err=%v calls=%d", projection.Digest(), err, persister.calls)
		}
	})
}

func TestTodo_AGENTP_012_ChatReplyRejectsUnboundModelOutcome(t *testing.T) {
	validator, admission, run, persister, _ := personaRunOutputFixture(t)
	result := agentmodel.ModelResult{Text: "A grounded response.", Finish: agentmodel.FinishToolCalls, ToolProposals: []agentmodel.ToolProposal{{Name: "people.read"}}}
	if _, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, result); !errors.Is(err, agentsecurity.ErrIncompleteOutput) || persister.calls != 0 {
		t.Fatalf("incomplete model result crossed validator: err=%v calls=%d", err, persister.calls)
	}
}

func TestTodo_AGENTP_012_ChatReplyFailsClosedOnAuthorityAndIdentityDrift(t *testing.T) {
	t.Run("authority unavailable", func(t *testing.T) {
		validator, admission, run, persister, authority := personaRunOutputFixture(t)
		authority.err = errors.New("current grants unavailable")
		_, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: "A grounded response.", Finish: agentmodel.FinishComplete})
		if err == nil || persister.calls != 0 {
			t.Fatalf("authority failure did not stop persistence: err=%v calls=%d", err, persister.calls)
		}
	})
	t.Run("run identity changed", func(t *testing.T) {
		validator, admission, run, persister, _ := personaRunOutputFixture(t)
		run.ActorID = "mallory"
		_, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: "A grounded response.", Finish: agentmodel.FinishComplete})
		if !errors.Is(err, agentsecurity.ErrIncompleteOutput) || persister.calls != 0 {
			t.Fatalf("identity drift crossed the validator: err=%v calls=%d", err, persister.calls)
		}
	})
}

func TestTodo_AGENT_004_PersonaOutputObservationIsSealedAndReadOnly(t *testing.T) {
	validator, admission, run, persister, authority := personaRunOutputFixture(t)
	result := agentmodel.ModelResult{Text: "A response grounded in the current conversation.", Finish: agentmodel.FinishComplete}
	observation, err := validator.ObservePersonaRunOutput(context.Background(), authority.observationBinding, admission, run, result)
	if err != nil {
		t.Fatal(err)
	}
	if err := observation.Verify(); err != nil {
		t.Fatalf("issued observation failed verification: %v", err)
	}
	if observation.Verdict != PersonaOutputObservedSafe || observation.Reason != PersonaOutputReasonNone ||
		observation.Binding.CaseID != "case-1" || observation.CandidateDigest == "" || observation.OutputDigest == "" ||
		observation.EvidenceDigest == "" || len(observation.GroundingSources) != 1 ||
		observation.GroundingSources[0].SourceID != "chat:post-a" || persister.calls != 0 {
		t.Fatalf("unsafe, unbound or effectful observation: %#v persister calls=%d", observation, persister.calls)
	}
	observation.Verdict = PersonaOutputObservedRejected
	if err := observation.Verify(); !errors.Is(err, ErrPersonaRunOutputValidatorUnavailable) {
		t.Fatalf("modified observation verified: %v", err)
	}
	if err := (PersonaRunOutputObservation{}).Verify(); !errors.Is(err, ErrPersonaRunOutputValidatorUnavailable) {
		t.Fatalf("caller-created zero observation verified: %v", err)
	}
}

func TestTodo_AGENT_004_PersonaOutputObservationReportsSafetyRefusal(t *testing.T) {
	validator, admission, run, persister, authority := personaRunOutputFixture(t)
	observation, err := validator.ObservePersonaRunOutput(context.Background(), authority.observationBinding, admission, run, agentmodel.ModelResult{
		Text: "Read https://external.example/secret", Finish: agentmodel.FinishComplete,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := observation.Verify(); err != nil {
		t.Fatalf("refusal observation failed verification: %v", err)
	}
	if observation.Verdict != PersonaOutputObservedRejected || observation.Reason != PersonaOutputReasonUnsafeText || observation.OutputDigest != "" || len(observation.GroundingSources) != 1 || persister.calls != 0 {
		t.Fatalf("unsafe candidate was not observed as refused: %#v calls=%d", observation, persister.calls)
	}
}

func TestTodo_AGENT_004_PersonaOutputObservationNeedsRealSyntheticAuthority(t *testing.T) {
	validator, admission, run, persister, authority := personaRunOutputFixture(t)
	withoutObservationAuthority, err := NewPersonaRunOutputValidator(PersonaRunOutputValidatorConfig{
		Authority: personaRunChatReplyAuthorityFake{}, Persister: persister,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = withoutObservationAuthority.ObservePersonaRunOutput(context.Background(), PersonaRunOutputObservationBinding{
		SyntheticTenantID: admission.Request.Source.TenantID, EvaluationRunID: "eval-run-1", CaseID: "case-1",
		CaseDigest: "sha256:" + strings.Repeat("e", 64), PersonaDigest: admission.Request.Persona.Digest, ModelDigest: "sha256:" + strings.Repeat("f", 64),
		InvocationID: admission.Request.Source.Key, ConversationID: admission.Request.Audience.ID, ThreadID: admission.Request.Context.ID, PostID: admission.Request.Source.Ref,
	}, admission, run, agentmodel.ModelResult{Text: "A response.", Finish: agentmodel.FinishComplete})
	if !errors.Is(err, ErrPersonaRunOutputValidatorUnavailable) || persister.calls != 0 {
		t.Fatalf("missing synthetic authority produced evidence: err=%v calls=%d", err, persister.calls)
	}
	changedBinding := authority.observationBinding
	changedBinding.CaseDigest = "sha256:" + strings.Repeat("1", 64)
	if _, err := validator.ObservePersonaRunOutput(context.Background(), changedBinding, admission, run, agentmodel.ModelResult{Text: "A response.", Finish: agentmodel.FinishComplete}); !errors.Is(err, ErrPersonaRunOutputValidatorUnavailable) || persister.calls != 0 {
		t.Fatalf("unregistered case binding produced evidence: err=%v calls=%d", err, persister.calls)
	}
}
