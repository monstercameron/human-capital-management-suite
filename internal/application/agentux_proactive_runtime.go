package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type announcementModelDocumentsKey struct{}

type announcementRuntimeKey struct{}
type announcementDraftKey struct{}

// AgentAnnouncementRuntime uses the mention runtime's route, budget ledger,
// model gateway and final-output schema. Its source is a definition, never a
// human thread or a delegated human credential.
type AgentAnnouncementRuntime struct {
	Sources    AgentAnnouncementSourceProvider
	Store      agentAnnouncementRepository
	Agents     *agentstore.Store
	Work       *DatabasePersonaRunModelWorkSource
	Base       PersonaRunStarterConfig
	Principals AgentAnnouncementServicePrincipalSource
	Documents  AgentAnnouncementDocumentResolver
	Worker     PersonaPrivateChatWorkloadIdentitySource
	Persister  PersonaRunFinalOutputPersister
	Chat       *chatstore.Store
	// Routes and RouteCache give the announcement's direct store write the
	// same placement lease an ordinary Chat write carries; the store refuses
	// a write without one.
	Routes            chatrouting.Directory
	RouteCache        *chatrouting.RouteCache
	Audience          chatrecipient.AudienceFloorAuthority
	LegalEntity       func(context.Context, string, string) (string, error)
	Now               func() time.Time
	Names             func(context.Context, string, string) (string, error)
	DocumentAuthority AgentAnnouncementHubAuthority
	BodyClasses       PersonaPublicReplyTextClassificationSource
}

func (r *AgentAnnouncementRuntime) record(ctx context.Context, request agentrun.Request) (agentstore.Announcement, error) {
	if r == nil || r.Store == nil || r.Work == nil || request.Source.Kind != agentrun.SourceAnnouncement || request.Persona == nil {
		return agentstore.Announcement{}, ErrAgentAnnouncementDenied
	}
	var record agentstore.Announcement
	var err error
	preview, _ := ctx.Value(announcementPreviewKey{}).(bool)
	if draft, ok := ctx.Value(announcementDraftKey{}).(agentstore.Announcement); ok && preview && draft.ID == request.Context.ID {
		record = draft
	} else {
		record, err = r.Store.Get(ctx, r.Work.tenantUUID(values.TenantId(request.Source.TenantID)), request.Context.ID)
	}
	if err != nil || record.TenantKey != request.Source.TenantID || record.InstallationID != request.InstallationID || record.PersonaID != request.Persona.ID || record.ConversationID != request.Audience.ID || record.OwnerID != request.Principal.RequesterID || record.State != agentstore.AnnouncementActive && !(record.State == agentstore.AnnouncementPaused && stringsAnnouncementNow(request.Source.Ref)) {
		return record, ErrAgentAnnouncementDenied
	}
	digest := announcementDefinitionDigest(record)
	if request.Context.SnapshotID != fmt.Sprint(record.Revision) || request.Context.Digest != digest {
		return record, agentstore.ErrAnnouncementRevision
	}
	return record, nil
}

func announcementDefinitionDigest(record agentstore.Announcement) string {
	raw, _ := json.Marshal([]any{record.InstallationID, record.PersonaID, record.ConversationID, record.OwnerID, record.Instruction, record.Documents, record.Zone, record.Revision})
	return personaRunBytesDigest(raw)
}

func stringsAnnouncementNow(occurrence string) bool {
	return len(occurrence) >= 4 && occurrence[:4] == "now:"
}

func (r *AgentAnnouncementRuntime) facts(ctx context.Context, record agentstore.Announcement) (agentpersona.PersonaProfile, agentmanifest.Manifest, AgentAnnouncementInstallationIdentity, PersonaRunEffectivePolicy, string, error) {
	identity, err := r.Principals.CurrentAnnouncementServicePrincipal(ctx, record.TenantKey, record.InstallationID)
	if err != nil || identity.PersonaID != record.PersonaID || identity.ConversationID != record.ConversationID {
		return agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, identity, PersonaRunEffectivePolicy{}, "", ErrAgentAnnouncementDenied
	}
	store, err := r.Work.personas.ForTenant(ctx, record.TenantKey)
	if err != nil {
		return agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, identity, PersonaRunEffectivePolicy{}, "", err
	}
	version, install, err := store.ReadCurrentPersonaAuthority(ctx, record.ConversationID, record.PersonaID)
	var profile agentpersona.PersonaProfile
	if err != nil || install.InstallationID != record.InstallationID || json.Unmarshal(version.Profile, &profile) != nil {
		return profile, agentmanifest.Manifest{}, identity, PersonaRunEffectivePolicy{}, "", fmt.Errorf("%w: current installation: %v", ErrAgentAnnouncementDenied, err)
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != version.ContentDigest {
		return profile, agentmanifest.Manifest{}, identity, PersonaRunEffectivePolicy{}, "", fmt.Errorf("%w: sealed profile: %v", ErrAgentAnnouncementDenied, err)
	}
	resolver, err := r.Work.manifests.ForTenant(ctx, record.TenantKey)
	if err != nil {
		return profile, agentmanifest.Manifest{}, identity, PersonaRunEffectivePolicy{}, "", err
	}
	manifest, err := resolver.ResolveAgentManifestContext(ctx, profile.Manifest)
	if err != nil || !validPersonaChatReplySchemaPin(manifest.OutputSchema) || !personaInstructionsMatchDigest(profile.Instructions, manifest.InstructionsDigest) {
		return profile, manifest, identity, PersonaRunEffectivePolicy{}, "", fmt.Errorf("%w: sealed instructions: %v", ErrAgentAnnouncementDenied, err)
	}
	legal, err := r.LegalEntity(ctx, record.TenantKey, record.OwnerID)
	if err != nil {
		return profile, manifest, identity, PersonaRunEffectivePolicy{}, "", err
	}
	policy, err := r.Work.budgets.Resolve(ctx, record.TenantKey, legal)
	return profile, manifest, identity, policy, legal, err
}

func (r *AgentAnnouncementRuntime) VerifyAdmission(ctx context.Context, request agentrun.Request) (snapshot agentrun.AuthoritySnapshot, verifyErr error) {
	defer func() {
		if errors.Is(verifyErr, ErrAgentAnnouncementDenied) || errors.Is(verifyErr, ErrAgentAnnouncementNotPublic) || errors.Is(verifyErr, agentstore.ErrAnnouncementRevision) {
			verifyErr = errors.Join(verifyErr, &agentrun.AdmissionRefusal{Code: "AUTHORITY_REFUSED"})
		}
	}()
	if !request.Deadline.After(r.Now()) {
		return agentrun.AuthoritySnapshot{}, ErrAgentAnnouncementDenied
	}
	key, keyErr := (announcementAdmissionSourceKeys{r}).ResolveSourceKey(ctx, request)
	if keyErr != nil || key != request.Source.Key || request.CauseID != key {
		return agentrun.AuthoritySnapshot{}, ErrAgentAnnouncementDenied
	}
	record, err := r.record(ctx, request)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	profile, manifest, identity, policy, legal, err := r.facts(ctx, record)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	digest, _ := manifest.Digest()
	if request.Principal.Mode != agentrun.ModeSponsored || request.Principal.SponsorID != identity.SubjectID || request.Principal.AgentPrincipalID != identity.PrincipalID || request.Principal.InvokerID != "" || request.Principal.DelegatedCredentialRef != "" || request.Agent != (agentrun.VersionRef{AgentID: manifest.ID, Version: fmt.Sprint(manifest.Version), Digest: digest}) || request.Persona.Digest != mustAnnouncementProfileDigest(profile) || request.Persona.Version != fmt.Sprint(profile.Version) || legal != request.LegalEntity || request.Purpose != personaChatReplyPurpose {
		return agentrun.AuthoritySnapshot{}, ErrAgentAnnouncementDenied
	}
	audience, err := r.currentAudience(ctx, record.TenantKey, record.ConversationID)
	if err != nil || audience != request.Audience {
		return agentrun.AuthoritySnapshot{}, ErrAgentAnnouncementDenied
	}
	if _, err := r.resolved(ctx, record); err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	return agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal, Audience: audience, Context: request.Context, BudgetCeiling: policy.Budget, GrantRef: "announcement:" + record.ID + ":" + fmt.Sprint(record.Revision), PolicyDigest: policy.PolicyDigest}, nil
}

func mustAnnouncementProfileDigest(profile agentpersona.PersonaProfile) string {
	sealed, _ := agentpersona.Seal(profile)
	return sealed.Digest
}

func (r *AgentAnnouncementRuntime) currentAudience(ctx context.Context, tenant, conversation string) (agentrun.AudienceScope, error) {
	current, err := r.currentConversation(ctx, tenant, conversation)
	if err != nil {
		return agentrun.AudienceScope{}, err
	}
	snapshot, err := r.Audience.CurrentAudience(ctx, current)
	if err != nil || !snapshot.Complete || !snapshot.GuestAndExternalComplete || snapshot.Revision == 0 {
		return agentrun.AudienceScope{}, ErrAgentAnnouncementDenied
	}
	raw, _ := json.Marshal(snapshot.CurrentMembers)
	return agentrun.AudienceScope{ID: conversation, SnapshotID: fmt.Sprint(snapshot.Revision), Digest: personaRunBytesDigest(raw)}, nil
}

func (r *AgentAnnouncementRuntime) currentConversation(ctx context.Context, tenant, id string) (chat.Conversation, error) {
	c := chat.Conversation{TenantID: tenant, ID: id}
	err := r.Chat.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT kind,lifecycle<>'ACTIVE',settings_revision FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&c.Kind, &c.Archived, &c.Revision)
	})
	if c.Archived {
		return chat.Conversation{}, errors.Join(ErrAgentAnnouncementDenied, ErrAgentAnnouncementChannelLocked)
	}
	if err != nil || !agentAnnouncementChannel(c.Kind) || c.Revision == 0 {
		return chat.Conversation{}, fmt.Errorf("%w: current conversation kind=%s revision=%d: %v", ErrAgentAnnouncementDenied, c.Kind, c.Revision, err)
	}
	return c, nil
}

func (r *AgentAnnouncementRuntime) resolved(ctx context.Context, record agentstore.Announcement) ([]AgentAnnouncementResolvedDocument, error) {
	ctx = context.WithValue(ctx, announcementDraftKey{}, record)
	if record.PersonaID == AgentUXDemoBirthdayPersonaID {
		_, err := r.agentuxDemoBirthdaySource(ctx, record)
		return nil, err
	}
	return r.Documents.ResolveAnnouncementDocuments(ctx, record.TenantKey, record.InstallationID, record.Documents)
}

func announcementResolvedReferences(documents []AgentAnnouncementResolvedDocument) []agentdocref.ResolvedDocument {
	out := make([]agentdocref.ResolvedDocument, 0, len(documents))
	for _, doc := range documents {
		number, _ := strconv.ParseUint(doc.Version, 10, 64)
		out = append(out, agentdocref.ResolvedDocument{Reference: agentdocref.Reference{DocumentID: doc.DocumentID, VersionMode: agentdocref.ModePinned, PinnedVersion: number, SectionAnchor: doc.SectionAnchor, Label: doc.Title}, Version: number, Title: doc.Title, Content: doc.Content})
	}
	return out
}

func (r *AgentAnnouncementRuntime) buildModelWork(ctx context.Context, admission agentrun.Record, run runstate.Run) (PersonaRunModelWork, error) {
	record, err := r.record(ctx, admission.Request)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	profile, manifest, _, policy, _, err := r.facts(ctx, record)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	route, err := r.Work.resolveRoute(ctx, admission, run, manifest, profile, policy)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	documents, err := r.resolved(ctx, record)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	if expected, ok := ctx.Value(announcementModelDocumentsKey{}).([]AgentAnnouncementResolvedDocument); ok && !reflect.DeepEqual(expected, documents) {
		return PersonaRunModelWork{}, ErrPersonaRunOutputRejected
	}
	refs := announcementResolvedReferences(documents)
	profile.Guidance = renderPersonaGuidanceDocumentTokens(profile, refs)
	if !isNilPersonaOutputPort(r.Work.remaining) {
		remaining, err := r.Work.remaining.RemainingPersonaRunModelBudget(ctx, admission, run)
		if err != nil {
			return PersonaRunModelWork{}, err
		}
		policy.Budget.MaxInputTokens = minUint(policy.Budget.MaxInputTokens, remaining.MaxInputTokens)
		policy.Budget.MaxOutputTokens = minUint(policy.Budget.MaxOutputTokens, remaining.MaxOutputTokens)
		policy.Budget.MaxCostMicros = minUint(policy.Budget.MaxCostMicros, remaining.MaxCostMicros)
		if policy.Budget.MaxInputTokens == 0 || policy.Budget.MaxOutputTokens == 0 || policy.Budget.MaxCostMicros == 0 {
			return PersonaRunModelWork{}, errPersonaRunModelWork
		}
	}
	goal, err := r.goal(ctx, record)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	request, err := r.Work.buildExecutorRequest(ctx, admission, run, profile, manifest, route, policy, goal, nil, nil)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	request.Model.Tools = nil
	request.Model.ActionPolicy = nil
	request.Outbound.Principal = admission.Request.Principal.SponsorID
	if record.PersonaID == AgentUXDemoBirthdayPersonaID {
		if err := r.agentuxDemoBirthdayModelRequest(ctx, record, &request, route); err != nil {
			return PersonaRunModelWork{}, err
		}
		if err := validateExecutorRequest(request); err != nil {
			return PersonaRunModelWork{}, err
		}
		return PersonaRunModelWork{Request: request}, nil
	}
	if err := addAgentDocumentsToModelRequest(&request, refs, route); err != nil {
		return PersonaRunModelWork{}, err
	}
	class, err := r.documentClass(ctx, record, documents, route)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	classifyPersonaReferenceDocumentFields(&request, class)
	if err := validateExecutorRequest(request); err != nil {
		return PersonaRunModelWork{}, err
	}
	return PersonaRunModelWork{Request: request}, nil
}

func (r *AgentAnnouncementRuntime) goal(ctx context.Context, record agentstore.Announcement) (string, error) {
	zone, err := time.LoadLocation(record.Zone)
	if err != nil {
		return "", err
	}
	// Format travels with the goal, the last thing the model reads, so its
	// citing rule holds over the general "cite by title, version and section".
	format := agentAnnouncementFormatMessage
	if record.PersonaID == AgentUXDemoBirthdayPersonaID {
		format = agentuxDemoBirthdayGoal()
	}
	if rule, ok := ctx.Value(announcementRepairKey{}).(string); ok {
		format += "\nYour previous answer broke this rule: " + rule + ". Write a corrected announcement from the same documents."
	}
	raw, err := json.Marshal(struct{ Instruction, Today, TimeZone, Format string }{record.Instruction, r.Now().In(zone).Format(time.DateOnly), record.Zone, format})
	return string(raw), err
}

// Announcement authority is added to the existing current owner once, before
// serving. The mention delegate remains exactly the existing owner.
type announcementAdmissionAuthority struct {
	mention      agentrun.Authority
	announcement *AgentAnnouncementRuntime
}

func (a announcementAdmissionAuthority) VerifyAdmission(ctx context.Context, req agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if req.Source.Kind == agentrun.SourceAnnouncement {
		return a.announcement.VerifyAdmission(ctx, req)
	}
	return a.mention.VerifyAdmission(ctx, req)
}

func (r *AgentAnnouncementRuntime) RunAnnouncement(ctx context.Context, input AgentAnnouncementRunRequest) (result AgentAnnouncementRunResult, runErr error) {
	if r == nil || r.Base.Model == nil || r.Agents == nil || r.Work == nil {
		return AgentAnnouncementRunResult{}, ErrAgentAnnouncementUnavailable
	}
	ctx = context.WithValue(ctx, announcementRuntimeKey{}, r)
	ctx = context.WithValue(ctx, announcementModelDocumentsKey{}, slices.Clone(input.Documents))
	var record agentstore.Announcement
	var err error
	if draft, ok := ctx.Value(announcementDraftKey{}).(agentstore.Announcement); ok && input.Preview {
		record = draft
	} else {
		record, err = r.Store.Get(ctx, r.Work.tenantUUID(values.TenantId(input.TenantID)), input.AnnouncementID)
	}
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	profile, manifest, identity, policy, legal, err := r.facts(ctx, record)
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	audience, err := r.currentAudience(ctx, input.TenantID, input.ConversationID)
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	digest, _ := manifest.Digest()
	request := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: input.TenantID, Kind: agentrun.SourceAnnouncement, Key: input.OccurrenceID, Ref: input.OccurrenceID}, Persona: &agentrun.PersonaRef{ID: profile.PersonaID, Version: fmt.Sprint(profile.Version), Digest: mustAnnouncementProfileDigest(profile)}, Agent: agentrun.VersionRef{AgentID: manifest.ID, Version: fmt.Sprint(manifest.Version), Digest: digest}, InstallationID: record.InstallationID, LegalEntity: legal, Principal: agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: identity.PrincipalID, SponsorID: identity.SubjectID, RequesterID: record.OwnerID}, Purpose: personaChatReplyPurpose, Audience: audience, Context: agentrun.ContextScope{ID: record.ID, SnapshotID: fmt.Sprint(record.Revision), Digest: announcementDefinitionDigest(record)}, Budget: policy.Budget, Deadline: policy.Deadline, CauseID: input.OccurrenceID}
	if frozen, ok := ctx.Value(announcementScheduledRequestKey{}).(agentrun.Request); ok {
		if frozen.Source != request.Source || frozen.Context != request.Context || frozen.InstallationID != record.InstallationID {
			return AgentAnnouncementRunResult{}, ErrAgentAnnouncementDenied
		}
		request = frozen
	}
	ctx, cancel := context.WithTimeout(ctx, request.Deadline.Sub(r.Now()))
	defer cancel()
	factory := &DatabasePersonaRunTenantRuntimeFactory{db: r.Agents, tenantUUID: r.Work.tenantUUID, base: r.Base}
	cfg, err := factory.ForPersonaRunTenant(ctx, input.TenantID)
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	cfg.Authority = r
	repository, err := r.admissionRepository(input.TenantID)
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	cfg.AdmissionStore = repository
	recheck, err := NewPersonaRunAdmissionRechecker(input.TenantID, cfg.AdmissionStore.(PersonaRunAdmissionReader), r)
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: r, Store: cfg.AdmissionStore, Now: r.Now})
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	admission, _, err := service.Admit(ctx, request)
	if err != nil || admission.Decision != agentrun.DecisionAccepted {
		return AgentAnnouncementRunResult{}, errors.Join(ErrAgentAnnouncementDenied, err)
	}
	state, err := runstate.New(cfg.ExecutionStore, recheck)
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	run, err := state.Start(ctx, admission)
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	defer func() {
		if runErr != nil {
			failureCtx := context.WithoutCancel(ctx)
			latest, failureErr := cfg.ExecutionStore.Get(failureCtx, admission.ID)
			if failureErr != nil {
				runErr = errors.Join(runErr, failureErr)
				return
			}
			if latest.State != runstate.StateReady && latest.State != runstate.StateRunning {
				return
			}
			code := "CONTEXT_UNAVAILABLE"
			if errors.Is(runErr, ErrPersonaRunModelFailure) {
				code = "MODEL_UNAVAILABLE"
			}
			if errors.Is(runErr, ErrPersonaRunOutputRejected) {
				code = "OUTPUT_REJECTED"
			}
			if !latest.Deadline.After(r.Now()) {
				_, failureErr = state.Expire(failureCtx, latest.ID, latest.Version, r.Now())
			} else if latest.State == runstate.StateReady || latest.Lease != nil && latest.Lease.Owner == cfg.WorkerID && !latest.Lease.Until.After(r.Now()) {
				_, failureErr = state.Cancel(failureCtx, latest.ID, latest.Version, r.Now())
			} else {
				_, failureErr = state.Fail(failureCtx, latest.ID, cfg.WorkerID, code, false, latest.Fence, latest.Version, r.Now())
			}
			runErr = errors.Join(runErr, failureErr)
		}
	}()
	run, err = state.Claim(ctx, run.ID, cfg.WorkerID, r.Now(), cfg.LeaseTTL)
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	ctx = WithPersonaBackgroundAdmission(ctx, admission)
	var output agentsecurity.FinalOutputPersistence
	if saved, ok := ctx.Value(announcementSavedPreviewKey{}).(AgentAnnouncementRunResult); ok && !input.Preview {
		if announcementReplyRule(saved.Text, input.Today, input.Instruction) != "" {
			return AgentAnnouncementRunResult{}, ErrAgentAnnouncementPreviewChanged
		}
		prior, _, _, err := r.outputRun(ctx, saved.Output)
		if err != nil || prior.Authority.Agent != admission.Authority.Agent || prior.Request.Persona == nil || *prior.Request.Persona != *admission.Request.Persona || prior.Authority.Principal != admission.Authority.Principal || prior.Authority.Audience != admission.Authority.Audience || prior.Authority.PolicyDigest != admission.Authority.PolicyDigest || prior.Request.Context.SnapshotID != admission.Request.Context.SnapshotID {
			return AgentAnnouncementRunResult{}, ErrAgentAnnouncementPreviewChanged
		}
		run, err = state.Checkpoint(ctx, run.ID, cfg.WorkerID, run.Fence, run.Version, runstate.PhaseModelCall, 1, "preview:"+saved.Output.Identity().OutputID, saved.Output.SemanticDigest(), r.Now())
		if err != nil {
			return AgentAnnouncementRunResult{}, err
		}
		output, err = r.seal(ctx, admission, run, saved.Text)
		if err != nil {
			return AgentAnnouncementRunResult{}, err
		}
	} else {
		for step := uint32(1); step <= 2; step++ {
			work, err := r.buildModelWork(ctx, admission, run)
			if err != nil {
				return AgentAnnouncementRunResult{}, err
			}
			if err := bindPersonaRunModelStep(&work.Request, run.ID, step); err != nil {
				return AgentAnnouncementRunResult{}, err
			}
			var model AgentModelExecutorResult
			err = r.Agents.WithAnnouncementSecurityFence(ctx, r.Work.tenantUUID(values.TenantId(input.TenantID)), admission.ID, func() error {
				var modelErr error
				model, modelErr = cfg.Model.Execute(ctx, work.Request)
				return modelErr
			})
			if err != nil || !validPersonaRunModelResult(model.Result) {
				return AgentAnnouncementRunResult{}, errors.Join(ErrPersonaRunModelFailure, err)
			}
			modelDigest, _ := personaRunResultDigest(model.Result)
			run, err = state.Checkpoint(ctx, run.ID, cfg.WorkerID, run.Fence, run.Version, runstate.PhaseModelCall, step, work.Request.StepID, modelDigest, r.Now())
			if err != nil {
				return AgentAnnouncementRunResult{}, err
			}
			rule := announcementReplyRule(model.Result.Text, input.Today, input.Instruction)
			if record.PersonaID == AgentUXDemoBirthdayPersonaID {
				source, sourceErr := r.agentuxDemoBirthdaySource(ctx, record)
				if sourceErr != nil || AgentUXDemoValidateBirthdayText(source, model.Result.Text, "en-US") != nil {
					return AgentAnnouncementRunResult{}, ErrPersonaRunOutputRejected
				}
				rule = ""
			}
			if rule != "" {
				personaChatReplyRefusalCause(model.Result.Text, rule)
				if step == 2 {
					return AgentAnnouncementRunResult{}, announcementReplyRefusal{rule: rule}
				}
				ctx = context.WithValue(ctx, announcementRepairKey{}, rule)
				continue
			}
			err = r.Agents.WithAnnouncementSecurityFence(ctx, r.Work.tenantUUID(values.TenantId(input.TenantID)), admission.ID, func() error {
				var sealErr error
				output, sealErr = r.seal(ctx, admission, run, model.Result.Text)
				return sealErr
			})
			if err != nil {
				return AgentAnnouncementRunResult{}, err
			}
			break
		}
	}
	if err := r.Persister.PersistPersonaRunFinalOutput(ctx, output); err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	run, err = state.Checkpoint(ctx, run.ID, cfg.WorkerID, run.Fence, run.Version, runstate.PhaseValidation, 1, output.Identity().OutputID, output.SemanticDigest(), r.Now())
	if err == nil && input.Preview {
		_, err = state.Checkpoint(ctx, run.ID, cfg.WorkerID, run.Fence, run.Version, runstate.PhaseDelivery, 1, "preview:"+output.Identity().OutputID, output.SemanticDigest(), r.Now())
	}
	return AgentAnnouncementRunResult{Output: output}, err
}

func (r *AgentAnnouncementRuntime) seal(ctx context.Context, admission agentrun.Record, run runstate.Run, text string) (agentsecurity.FinalOutputPersistence, error) {
	current, err := r.VerifyAdmission(ctx, admission.Request)
	if err != nil || !reflect.DeepEqual(current, admission.Authority) {
		return agentsecurity.FinalOutputPersistence{}, ErrAgentAnnouncementDenied
	}
	record, err := r.record(ctx, admission.Request)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	worker, err := r.Worker.ResolvePersonaChatWorker(ctx)
	if err != nil || !privatePersonaReplyWorkerMatches(worker, r.Now()) {
		return agentsecurity.FinalOutputPersistence{}, ErrAgentAnnouncementDenied
	}
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{PersonaChatReplyToolDescriptor()})
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	identity := agentsecurity.AgentIdentity{Identity: "workload:" + worker.Issuer() + "/" + worker.Subject() + "#" + worker.Fingerprint(), AgentID: admission.Request.Agent.AgentID, Tenant: record.TenantKey, Purpose: admission.Request.Purpose, ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 1}
	call := agentsecurity.ToolCall{Agent: identity, Tenant: identity.Tenant, Purpose: identity.Purpose, Tool: "persona.chat_reply", Capability: "persona.reply", Version: 1, Nonce: admission.Request.Source.Key, Args: map[string]any{"request_digest": admission.RequestDigest, "run_id": run.ID, "policy_digest": current.PolicyDigest}, InputTaint: []string{string(agentsecurity.TaintDerived)}, Provenance: []string{PersonaChatReplyProvenance, "chat.current"}, CostBudget: 1, DataScope: identity.DataScope}
	call.Delegation = []agentsecurity.DelegationLink{{GrantID: current.GrantRef, Delegator: admission.Request.Principal.SponsorID, Delegate: identity.AgentID, Tenant: identity.Tenant, Purpose: identity.Purpose, ToolSet: identity.ToolSet, DataScope: identity.DataScope, Budget: 1}}
	call.ArgsDigest, err = agentsecurity.DigestArguments(call.Args)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	admitted, err := gateway.Admit(call)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	documents, err := r.resolved(ctx, record)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	if expected, ok := ctx.Value(announcementModelDocumentsKey{}).([]AgentAnnouncementResolvedDocument); ok && !reflect.DeepEqual(expected, documents) {
		return agentsecurity.FinalOutputPersistence{}, ErrPersonaRunOutputRejected
	}
	var grounding []agentsecurity.Datum
	if record.PersonaID == AgentUXDemoBirthdayPersonaID {
		source, sourceErr := r.agentuxDemoBirthdaySource(ctx, record)
		if sourceErr != nil || AgentUXDemoValidateBirthdayText(source, text, "en-US") != nil {
			return agentsecurity.FinalOutputPersistence{}, ErrPersonaRunOutputRejected
		}
		datum, observeErr := agentuxDemoBirthdayGrounding(gateway, source)
		if observeErr != nil {
			return agentsecurity.FinalOutputPersistence{}, observeErr
		}
		grounding = append(grounding, datum)
	}
	for _, doc := range documents {
		source := "document:" + doc.DocumentID + "/version:" + doc.Version
		location := source
		if doc.SectionAnchor != "" {
			location += "/section:" + doc.SectionAnchor
		}
		datum, err := gateway.Observe(agentsecurity.SourceDocument, doc.Content, agentsecurity.KindObservation, agentsecurity.Citation{SourceID: source, Location: location, Digest: doc.Digest})
		if err != nil {
			return agentsecurity.FinalOutputPersistence{}, err
		}
		grounding = append(grounding, datum)
	}
	normalized, err := normalizePersonaChatReply(text)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	conversation, err := r.currentConversation(ctx, record.TenantKey, record.ConversationID)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	class, err := r.bodyClass(ctx, record.TenantKey, normalized)
	if err != nil || r.Audience.AllowDataClass(ctx, conversation, class) != nil {
		return agentsecurity.FinalOutputPersistence{}, ErrPersonaRunOutputRejected
	}
	datum, err := gateway.Infer(normalized, grounding...)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	final, err := gateway.ValidateFinalOutput(ctx, admitted, "persona.chat_reply", agentsecurity.FinalOutputCandidate{Complete: true, Draft: agentsecurity.AgentOutput{Schema: PersonaChatReplySchema, Value: PersonaChatReply{Text: normalized}, Narrative: normalized}, Answer: []agentsecurity.Datum{datum}}, nil, nil, nil)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	outputIdentity := agentsecurity.FinalOutputIdentity{TenantID: record.TenantKey, OutputID: personaRunOutputID(record.TenantKey, admission.Request.Source.Key), InvocationID: admission.Request.Source.Key, AdmissionID: admission.ID, RunID: run.ID, InvokerID: record.OwnerID, ConversationID: record.ConversationID, ThreadID: record.ID, PostID: admission.Request.Source.Ref, PersonaID: record.PersonaID, PersonaVersion: admission.Request.Persona.Version, InstallationID: record.InstallationID}
	return gateway.IssueFinalOutputPersistence(ctx, admitted, final, outputIdentity)
}
