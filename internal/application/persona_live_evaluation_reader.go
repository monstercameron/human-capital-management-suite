package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentbudgetstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcandidateevalstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaLiveInvocationReader interface {
	Lookup(context.Context, string, string, string) (agentinvoke.Invocation, error)
}

type personaLiveAdmissionReader interface {
	GetByID(context.Context, string) (agentrun.Record, error)
}

type personaLiveUsageReader interface {
	SettledTaskUsage(context.Context, values.TenantId, string) (agentbudget.SettledTaskUsage, error)
}

// PersonaLiveDeliveryEvidenceReader resolves committed chat delivery, not the
// model's claimed recipient or the worker's return value.
type PersonaLiveDeliveryEvidenceReader interface {
	ReadPersonaEvaluationDelivery(context.Context, agentpersonastore.FinalOutputRecord, string) ([]string, error)
}

// PersonaLiveDisclosureEvidence is resolved from the committed delivery
// owner's disclosure authorization, independently of the worker and model.
type PersonaLiveDisclosureEvidence struct {
	AuthorizedRecipients []string
	AudienceFloorDigest  string
}

type PersonaLiveDisclosureEvidenceReader interface {
	ReadPersonaEvaluationDisclosure(context.Context, agentpersonastore.FinalOutputRecord, string) (PersonaLiveDisclosureEvidence, error)
}

// PersonaLiveCaseEvidenceReader joins the evaluator journal with invocation,
// accepted/refused admission, run checkpoints, locally executed tools, sealed
// output, committed chat delivery, and settled usage from their owner stores.
type PersonaLiveCaseEvidenceReader struct {
	Scope            PersonaCandidateScope
	Journal          *agentcandidateevalstore.Store
	Invocations      personaLiveInvocationReader
	Admissions       personaLiveAdmissionReader
	Runs             runstate.Store
	Outputs          *agentpersonastore.TenantStore
	OutputVerifier   *agentsecurity.FinalOutputRecoveryVerifier
	OutputRehydrator agentsecurity.FinalOutputRecoveryRehydrator
	Threads          agentinvoke.ThreadReader
	Suite            agenteval.PersonaSuite
	Delivery         PersonaLiveDeliveryEvidenceReader
	Disclosure       PersonaLiveDisclosureEvidenceReader
	Usage            personaLiveUsageReader
	Selection        agentmodel.ModelSelection
}

func (r *PersonaLiveCaseEvidenceReader) AuthorizeSyntheticPersonaEvaluation(ctx context.Context, target agenteval.PersonaEvaluationTarget) error {
	if r == nil || r.Scope == nil || r.Journal == nil || r.Invocations == nil || r.Admissions == nil || r.Runs == nil || r.Outputs == nil || r.OutputVerifier == nil || r.OutputRehydrator == nil || r.Threads == nil || len(r.Suite.Cases) == 0 || r.Delivery == nil || r.Disclosure == nil || r.Usage == nil || target.ModelDigest != "sha256:"+r.Selection.ProfileDigest {
		return agenteval.ErrPersonaEvaluation
	}
	return r.Scope.AuthorizeSyntheticPersonaEvaluation(ctx, target)
}

func (r *PersonaLiveCaseEvidenceReader) ReadPersonaCase(ctx context.Context, target agenteval.PersonaEvaluationTarget, execution agenteval.PersonaCaseExecution) (agenteval.PersonaCaseEvidence, error) {
	if err := r.AuthorizeSyntheticPersonaEvaluation(ctx, target); err != nil {
		return agenteval.PersonaCaseEvidence{}, err
	}
	record, err := r.Journal.Read(ctx, target, execution.InvocationID)
	if err != nil || record.TaskID != execution.TaskID {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	invocation, err := r.Invocations.Lookup(ctx, target.SyntheticTenantID, target.InvokerID, execution.InvocationID)
	if err != nil || invocation.TenantID != target.SyntheticTenantID || invocation.InvokerID != target.InvokerID ||
		invocation.PersonaID != target.PersonaID || invocation.State != agentinvoke.InvocationStarted || !personaRunVersionMatches(target.PersonaVersion, invocation.PersonaVersion) {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	if err := r.verifyCaseInput(ctx, record, invocation); err != nil {
		return agenteval.PersonaCaseEvidence{}, err
	}
	admission, err := r.Admissions.GetByID(ctx, record.TaskID)
	if err != nil || admission.ID != record.TaskID || admission.Request.Source.TenantID != target.SyntheticTenantID ||
		admission.Request.Source.Key != invocation.ID || admission.Request.Principal.InvokerID != target.InvokerID ||
		admission.Request.Persona == nil || admission.Request.Persona.ID != target.PersonaID ||
		admission.Request.Persona.Version != strconv.FormatInt(target.PersonaVersion, 10) && admission.Request.Persona.Version != "v"+strconv.FormatInt(target.PersonaVersion, 10) ||
		admission.Request.Persona.Digest != target.ProfileDigest || record.AdmissionDigest != "sha256:"+admission.RequestDigest {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	observed := agenteval.PersonaCaseEvidence{SyntheticTenantID: target.SyntheticTenantID, PersonaID: target.PersonaID,
		PersonaVersion: target.PersonaVersion, ProfileDigest: target.ProfileDigest, ModelDigest: target.ModelDigest,
		CaseDigest: record.CaseDigest, TaskID: record.TaskID, InvocationID: record.InvocationID,
		Outcome: record.Outcome, RefusalCode: record.RefusalCode, RefusalPointer: record.RefusalPointer, CompletedAt: record.CompletedAt}
	if admission.Decision == agentrun.DecisionRefused {
		if record.Outcome != "REFUSED" || admission.RefusalCode != record.RefusalCode || len(record.Skills) != 0 || len(record.DeliveredTo) != 0 {
			return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
		}
		calls, err := r.Journal.ReadModelCalls(ctx, target, record.TaskID)
		if err != nil || len(calls) != 0 {
			return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
		}
		observed.EvidenceDigest = personaLiveEvidenceDigest(record.EvidenceDigest, admission, observed)
		return observed, nil
	}
	run, err := r.Runs.Get(ctx, record.TaskID)
	if err != nil || run.ID != record.TaskID || run.TenantID != target.SyntheticTenantID || run.ActorID != target.InvokerID ||
		run.AdmissionID != admission.ID || run.RequestDigest != admission.RequestDigest {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	if record.Outcome == "FAILED" || record.Outcome == "REFUSED" {
		if run.State != runstate.StateFailed || run.TerminalCode != record.RefusalCode || !run.UpdatedAt.Equal(record.CompletedAt) || len(record.Skills) != 0 || len(record.DeliveredTo) != 0 {
			return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
		}
		usage, usageErr := r.Usage.SettledTaskUsage(ctx, values.TenantId(target.SyntheticTenantID), run.ID)
		if usageErr != nil {
			if !errors.Is(usageErr, agentbudgetstore.ErrTaskMissing) {
				return agenteval.PersonaCaseEvidence{}, usageErr
			}
			for _, checkpoint := range run.Checkpoints {
				if checkpoint.Phase == runstate.PhaseModelCall || checkpoint.Phase == runstate.PhaseToolCall || checkpoint.Phase == runstate.PhaseValidation || checkpoint.Phase == runstate.PhaseDelivery {
					return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
				}
			}
		} else {
			if usage.TenantID != target.SyntheticTenantID || usage.TaskID != run.ID || usage.Usage.SpendMicros < 0 {
				return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
			}
			observed.SettledCostMicros = usage.Usage.SpendMicros
		}
		calls, err := r.Journal.ReadModelCalls(ctx, target, run.ID)
		if err != nil || verifyPersonaLiveModelCalls(target, r.Selection, run, usage, calls, false) != nil {
			return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
		}
		if record.Outcome == "REFUSED" {
			if run.TerminalCode != "OUT_OF_SCOPE" || len(calls) != 1 || calls[0].ActionPolicy == nil || calls[0].ActionPolicy.ProfileDigest != target.ProfileDigest || !slices.Equal(calls[0].ActionPolicy.Allowed, []agentmodel.RequestedAction{agentmodel.ActionReadPolicy}) {
				return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
			}
			allowed, actionErr := agentmodel.CheckRequestedActions(*calls[0].ActionPolicy, calls[0].RequestedActions)
			if actionErr != nil || allowed {
				return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
			}
			tools, toolErr := r.Outputs.ListToolResults(ctx, run.ID)
			if toolErr != nil || len(tools) != 0 {
				return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
			}
			for _, checkpoint := range run.Checkpoints {
				if checkpoint.Phase == runstate.PhaseToolCall || checkpoint.Phase == runstate.PhaseValidation || checkpoint.Phase == runstate.PhaseDelivery {
					return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
				}
			}
		}
		observed.EvidenceDigest = personaLiveEvidenceDigest(record.EvidenceDigest, struct {
			Admission agentrun.Record
			Run       runstate.Run
			Usage     agentbudget.SettledTaskUsage
			Models    []agentcandidateevalstore.ModelCall
		}{admission, run, usage, calls}, observed)
		return observed, nil
	}
	if run.State != runstate.StateCompleted || record.Outcome != "COMPLETED" {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	output, err := r.Outputs.GetFinalOutputByInvocation(ctx, invocation.ID)
	if err != nil || output.TenantID.String() != target.SyntheticTenantID || output.RunID != run.ID || output.AdmissionID != admission.ID ||
		output.InvokerID != target.InvokerID || output.PersonaID != target.PersonaID || output.PersistenceDigest != record.OutputDigest ||
		output.ConversationID != invocation.ConversationID || output.ThreadID != invocation.ThreadID || output.ParentPostID != invocation.PostID {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	persisted, err := r.Outputs.RecoverFinalOutput(ctx, output.OutputID, r.OutputVerifier, r.OutputRehydrator)
	if err != nil || persisted.Digest() != output.PersistenceDigest {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	hasModel, hasValidation, hasDelivery := false, false, false
	for _, checkpoint := range run.Checkpoints {
		switch checkpoint.Phase {
		case runstate.PhaseModelCall:
			hasModel = hasModel || personaRequestDigest(checkpoint.Digest)
		case runstate.PhaseValidation:
			hasValidation = hasValidation || checkpoint.Ref == output.OutputID && checkpoint.Digest == persisted.SemanticDigest()
		case runstate.PhaseDelivery:
			hasDelivery = hasDelivery || checkpoint.Digest == record.DeliveryDigest
		}
	}
	if !hasModel || !hasValidation || !hasDelivery {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	tools, err := r.Outputs.ListToolResults(ctx, run.ID)
	if err != nil {
		return agenteval.PersonaCaseEvidence{}, err
	}
	for _, tool := range tools {
		if tool.InvocationID != invocation.ID || tool.InvokerID != target.InvokerID || tool.PersonaID != target.PersonaID || tool.AdmissionDigest != record.AdmissionDigest {
			return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
		}
		observed.Skills = append(observed.Skills, tool.SkillID)
	}
	slices.Sort(observed.Skills)
	observed.Skills = slices.Compact(observed.Skills)
	observed.PlanDigest = PersonaLivePlanDigest(target.ProfileDigest, invocation.Skills, tools)
	if record.PlanDigest != observed.PlanDigest || !sameLiveStrings(record.Skills, observed.Skills) {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	observed.DeliveredTo, err = r.Delivery.ReadPersonaEvaluationDelivery(ctx, output, record.DeliveryDigest)
	if err != nil || !sameLiveStrings(record.DeliveredTo, observed.DeliveredTo) {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	disclosure, err := r.Disclosure.ReadPersonaEvaluationDisclosure(ctx, output, record.DeliveryDigest)
	if err != nil || !personaRequestDigest(disclosure.AudienceFloorDigest) || len(disclosure.AuthorizedRecipients) == 0 {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	observed.AuthorizedRecipients = disclosure.AuthorizedRecipients
	observed.AudienceFloorDigest = disclosure.AudienceFloorDigest
	usage, err := r.Usage.SettledTaskUsage(ctx, values.TenantId(target.SyntheticTenantID), run.ID)
	if err != nil || usage.TenantID != target.SyntheticTenantID || usage.TaskID != run.ID || usage.Usage.Steps <= 0 || usage.Usage.Tokens <= 0 || usage.Usage.SpendMicros < 0 {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	observed.SettledCostMicros = usage.Usage.SpendMicros
	calls, err := r.Journal.ReadModelCalls(ctx, target, run.ID)
	if err != nil || verifyPersonaLiveModelCalls(target, r.Selection, run, usage, calls, true) != nil {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	if record.BaselineInvocationID != "" {
		baseline, err := r.Journal.Read(ctx, target, record.BaselineInvocationID)
		if err != nil || baseline.InvocationID == record.InvocationID || baseline.BaselineInvocationID != "" || baseline.RequestDigest != record.RequestDigest || !baseline.CompletedAt.Before(record.CompletedAt) || baseline.Outcome != "COMPLETED" {
			return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
		}
		baselineObserved, err := r.ReadPersonaCase(ctx, target, agenteval.PersonaCaseExecution{TaskID: baseline.TaskID, InvocationID: baseline.InvocationID})
		if err != nil || baseline.BaselineInvocationID != "" {
			return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
		}
		observed.BaselinePlanDigest = baselineObserved.PlanDigest
	}
	observed.EvidenceDigest = personaLiveEvidenceDigest(record.EvidenceDigest, struct {
		Admission agentrun.Record
		Run       runstate.Run
		Output    string
		Usage     agentbudget.SettledTaskUsage
		Models    []agentcandidateevalstore.ModelCall
	}{admission, run, output.PersistenceDigest, usage, calls}, observed)
	return observed, nil
}

// The evaluator observes the dispatcher journal and the worker checkpoints
// separately. Successful runs require every billed call to appear in both.
func verifyPersonaLiveModelCalls(target agenteval.PersonaEvaluationTarget, selection agentmodel.ModelSelection, run runstate.Run, usage agentbudget.SettledTaskUsage, calls []agentcandidateevalstore.ModelCall, completed bool) error {
	if len(calls) > 2 || completed && len(calls) == 0 || target.ModelDigest != "sha256:"+selection.ProfileDigest {
		return agenteval.ErrPersonaEvaluation
	}
	byStep := map[string]agentcandidateevalstore.ModelCall{}
	var tokens, cost int64
	for _, call := range calls {
		if call.Target != target || call.TaskID != run.ID || call.Provider != selection.Identity || !personaRequestDigest(call.RequestDigest) || !personaRequestDigest(call.ResultDigest) || call.LeaseID == "" || call.CompletedAt.After(run.UpdatedAt) || call.Usage.TotalTokens < 0 || call.Usage.CostMicros < 0 {
			return agenteval.ErrPersonaEvaluation
		}
		if _, duplicate := byStep[call.StepID]; duplicate {
			return agenteval.ErrPersonaEvaluation
		}
		byStep[call.StepID] = call
		tokens += call.Usage.TotalTokens
		cost += call.Usage.CostMicros
	}
	seen := map[string]bool{}
	for _, checkpoint := range run.Checkpoints {
		if checkpoint.Phase != runstate.PhaseModelCall {
			continue
		}
		call, exists := byStep[checkpoint.Ref]
		if !exists || seen[checkpoint.Ref] || call.ResultDigest != checkpoint.Digest || call.CompletedAt.After(checkpoint.At) {
			return agenteval.ErrPersonaEvaluation
		}
		seen[checkpoint.Ref] = true
	}
	if completed && len(seen) != len(calls) || usage.Usage.Steps != int64(len(calls)) || usage.Usage.Tokens != tokens || usage.Usage.SpendMicros != cost {
		return agenteval.ErrPersonaEvaluation
	}
	return nil
}

func (r *PersonaLiveCaseEvidenceReader) verifyCaseInput(ctx context.Context, record agentcandidateevalstore.Record, invocation agentinvoke.Invocation) error {
	var matched *agenteval.PersonaCase
	for _, testCase := range r.Suite.Cases {
		if agenteval.PersonaCaseDigest(testCase) == record.CaseDigest {
			copy := testCase
			matched = &copy
			break
		}
	}
	if matched == nil || record.RequestDigest != personaLiveEvidenceDigest("invoker-request", matched.Prompt, nil) {
		return agenteval.ErrPersonaEvaluation
	}
	posts, err := r.Threads.ReadThread(ctx, agentinvoke.ThreadReadRequest{TenantID: invocation.TenantID, ConversationID: invocation.ConversationID, ThreadID: invocation.ThreadID, InvokerID: invocation.InvokerID, InvokingPostID: invocation.PostID, Limit: agentinvoke.MaxThreadPosts})
	if err != nil {
		return agenteval.ErrPersonaEvaluation
	}
	invoking, peer := false, matched.PeerText == ""
	for _, post := range posts {
		if post.ID == invocation.PostID && post.AuthorID == invocation.InvokerID && !post.Bot && post.Body == matched.Prompt {
			invoking = true
		}
		if post.AuthorID != invocation.InvokerID && post.Body == matched.PeerText && matched.PeerText != "" {
			peer = true
		}
	}
	if !invoking || !peer {
		return agenteval.ErrPersonaEvaluation
	}
	return nil
}

// PersonaLivePlanDigest compares the authority and executed skill projection.
// Invocation IDs, peer text, source-taint digests and model prose are excluded.
func PersonaLivePlanDigest(profile string, skills agentinvoke.SkillScopes, tools []agentpersonastore.ToolResultRecord) string {
	type skillPin struct {
		ID      string
		Version uint32
		Digest  string
	}
	pins := make([]skillPin, 0, len(tools))
	for _, tool := range tools {
		pins = append(pins, skillPin{tool.SkillID, tool.SkillVersion, tool.SkillDigest})
	}
	slices.SortFunc(pins, func(a, b skillPin) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	pins = slices.Compact(pins)
	return personaLiveEvidenceDigest("plan", struct {
		Profile  string
		Skills   agentinvoke.SkillScopes
		Executed []skillPin
	}{profile, skills, pins}, nil)
}

func sameLiveStrings(left, right []string) bool {
	left, right = slices.Clone(left), slices.Clone(right)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}

func personaLiveEvidenceDigest(journal string, proof, evidence any) string {
	encoded, _ := json.Marshal(struct {
		Journal         string
		Proof, Evidence any
	}{journal, proof, evidence})
	sum := sha256.Sum256(append([]byte("hcm-next-persona-live-evidence/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
