package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	localAgentDemoTenant    = "ironridge-demo"
	localAgentDemoPersonaID = "hcmnext.local.persona.policy_helper"
	localAgentDemoAgentID   = "policy-helper"
	localAgentDemoAdmin     = "ir-001-walt-brennan"
	localAgentDemoSynthetic = "ironridge-agent-eval"
	localAgentDemoFreshFor  = 24 * time.Hour
)

// LocalAgentDemoConfig contains only operator-owned configuration. The
// preparation code derives persona, version, model and conversation identity
// from durable state and fixed local fixtures, never from these values.
type LocalAgentDemoConfig struct {
	Profile, Tenant                                                     string
	DatabaseURL, AgentDatabaseURL, ChatDatabaseURL, DocumentDatabaseURL string
	ReviewAuthorityDatabaseURL, PolicyAuthorityDatabaseURL              string
	RouteAuthorityDatabaseURL, EvaluationProvisionerDatabaseURL         string
	// AgentOwnerDatabaseURL is the optional agent-database owner connection.
	// The run policy table grants the serving role SELECT only, so a cell whose
	// roles are separated needs the owner to insert the local policy row.
	AgentOwnerDatabaseURL string
	// ChatOwnerDatabaseURL is the optional chat-database owner connection used
	// only to repair a direct conversation an earlier preparation left
	// unrouted; the serving role may not delete conversation rows.
	ChatOwnerDatabaseURL string
	Now                  func() time.Time
}

// LocalAgentDemoSummary is the intentionally non-sensitive command receipt.
type LocalAgentDemoSummary struct {
	Persona, DisplayName, State string
	Version                     int64
	IconsSet                    int
	EvaluationRecorded          bool
	Published                   bool
	InstallationsCreated        int
	ModelRouteCreated           bool
	RunPolicyCreated            bool
	ProviderDeploymentCreated   bool
	ChatConversationRepaired    bool
	// PolicyDocumentPlacements counts the conversations that received the
	// demo policy document as an official placement in this run.
	PolicyDocumentPlacements      int
	HolidayDocumentPlacements     int
	DuplicateInstallationsRetired int
	// SkillGrantsCreated counts the skill grants a prepared agent's pinned
	// skills were missing for its audience (CHATLIVE-001).
	SkillGrantsCreated     int
	InstallationsRefreshed int
	AssistantVersion       int64
	AssistantState         string
	AmbientAgents          []AgentUXAmbientPreparationReceipt
	// Starters is one line for every starter the demo workspace is meant to
	// offer: prepared by this run, or not, with the reason.
	Starters       []LocalAgentDemoStarterReceipt
	WorkspaceIndex WorkspaceIndexPreparation
	// WorkspaceDocumentsShared counts the demo documents that became readable
	// by every workspace member in this run.
	WorkspaceDocumentsShared int
}

func (s LocalAgentDemoSummary) Changed() bool {
	return s.IconsSet > 0 || s.EvaluationRecorded || s.Published || s.InstallationsCreated > 0 || s.InstallationsRefreshed > 0 || s.ModelRouteCreated || s.RunPolicyCreated || s.ProviderDeploymentCreated || s.ChatConversationRepaired || s.PolicyDocumentPlacements > 0 || s.HolidayDocumentPlacements > 0 || s.DuplicateInstallationsRetired > 0 || s.SkillGrantsCreated > 0 || s.WorkspaceDocumentsShared > 0 || s.WorkspaceIndex.DocumentsIndexed > 0 || s.WorkspaceIndex.SectionsIndexed > 0
}

// ValidateLocalAgentDemoConfig rejects the command before any connection is
// opened unless it is the fixed local-development preparation and every
// database endpoint is loopback.
func ValidateLocalAgentDemoConfig(config LocalAgentDemoConfig) error {
	if config.Profile != ServeProfileLocalDev || config.Tenant != localAgentDemoTenant {
		return fmt.Errorf("agent demo preparation requires -profile local-dev and -tenant %s", localAgentDemoTenant)
	}
	urls := map[string]string{
		"database-url": config.DatabaseURL, "agent-database-url": config.AgentDatabaseURL,
		"chat-database-url": config.ChatDatabaseURL, "document-database-url": config.DocumentDatabaseURL,
		"persona-review-authority-database-url":      config.ReviewAuthorityDatabaseURL,
		"agent-policy-authority-database-url":        config.PolicyAuthorityDatabaseURL,
		"persona-model-route-authority-database-url": config.RouteAuthorityDatabaseURL,
		"agent-eval-provisioner-database-url":        config.EvaluationProvisionerDatabaseURL,
	}
	if config.AgentOwnerDatabaseURL != "" {
		urls["agent-owner-database-url"] = config.AgentOwnerDatabaseURL
	}
	if config.ChatOwnerDatabaseURL != "" {
		urls["chat-owner-database-url"] = config.ChatOwnerDatabaseURL
	}
	for name, raw := range urls {
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" || !loopbackDatabaseHost(parsed.Hostname()) {
			return fmt.Errorf("-%s must be a loopback PostgreSQL URL", name)
		}
	}
	return nil
}

func loopbackDatabaseHost(host string) bool {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// PrepareLocalAgentDemo prepares Policy Helper and Assistant through deterministic
// local evaluation, normal publication and governed placement.
func PrepareLocalAgentDemo(ctx context.Context, config LocalAgentDemoConfig) (LocalAgentDemoSummary, error) {
	var summary LocalAgentDemoSummary
	if ctx == nil {
		return summary, errors.New("local agent demo preparation requires a context")
	}
	if err := ValidateLocalAgentDemoConfig(config); err != nil {
		return summary, err
	}
	now := time.Now().UTC()
	if config.Now != nil {
		now = config.Now().UTC()
	}
	now = now.Truncate(time.Microsecond)
	if now.IsZero() {
		return summary, errors.New("local agent demo clock is unavailable")
	}
	mapper := func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(tenant.String()) }
	core, err := pgxadapter.NewPool(ctx, config.DatabaseURL, nil)
	if err != nil {
		return summary, fmt.Errorf("open local core authority: %w", err)
	}
	defer core.Close()
	agents, err := agentstore.New(ctx, agentstore.Config{DSN: config.AgentDatabaseURL, CoreDSN: config.DatabaseURL, ChatDSN: config.ChatDatabaseURL, DocumentDSN: config.DocumentDatabaseURL})
	if err != nil {
		return summary, fmt.Errorf("open local agent store: %w", err)
	}
	defer agents.Close()
	chat, err := chatstore.New(ctx, chatstore.Config{DSN: config.ChatDatabaseURL})
	if err != nil {
		return summary, fmt.Errorf("open local chat store: %w", err)
	}
	defer chat.Close()
	documents, err := documenthubstore.New(ctx, documenthubstore.Config{DSN: config.DocumentDatabaseURL, ChatDSN: config.ChatDatabaseURL, CoreDSN: config.DatabaseURL})
	if err != nil {
		return summary, fmt.Errorf("open local document store: %w", err)
	}
	defer documents.Close()
	model, modelErr := LocalWorkspaceEmbeddingModel(os.Getenv)
	if modelErr != nil {
		summary.WorkspaceIndex.Unavailable = modelErr.Error()
	}
	members := WorkspaceDocumentDirectory{DB: core, TenantUUID: mapper}
	if people, peopleErr := members.WorkspaceDocumentMembers(ctx, config.Tenant); peopleErr == nil {
		modelID := ""
		if model != nil {
			modelID = model.Model()
		}
		if status, statusErr := documents.WorkspaceIndexStatus(ctx, config.Tenant, modelID, people); statusErr == nil {
			summary.WorkspaceIndex.Workspace = WorkspaceDocumentSearchStatus{WorkspaceDocuments: status.Documents, WorkspaceIndexedAt: status.IndexedAt, WorkspacePending: status.Pending}
			summary.WorkspaceIndex.WorkspaceAccessKnown = true
		}
	}

	key, err := loadOrCreateLocalAgentDemoEvaluationKey(filepath.FromSlash(localAgentDemoEvaluationPath))
	if err != nil {
		return summary, err
	}
	reviews, err := agentpersonastore.NewDurableReviewAuthority(mapper)
	if err != nil {
		return summary, err
	}
	evaluations, err := localAgentDemoEvaluationAuthority(key, config.Tenant, mapper, func() time.Time { return now })
	if err != nil {
		return summary, err
	}
	personas, err := agentpersonastore.NewWithPublicationAuthorities(agents, mapper, reviews, evaluations)
	if err != nil {
		return summary, err
	}
	scoped, err := personas.Scoped(values.TenantId(config.Tenant))
	if err != nil {
		return summary, err
	}
	row, state, err := latestPersonaVersion(ctx, scoped, localAgentDemoPersonaID, "")
	if err != nil {
		return summary, fmt.Errorf("read Policy Helper version: %w", err)
	}
	if state != agentpersonastore.StateInReview && state != agentpersonastore.StatePublished {
		return summary, fmt.Errorf("Policy Helper must be IN_REVIEW or PUBLISHED, found %s", state)
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(row.Profile, &profile) != nil {
		return summary, ErrPersonaDraftInvalid
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != row.ContentDigest || profile.Handle != localAgentDemoAgentID {
		return summary, ErrPersonaDraftInvalid
	}
	material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.FromSlash(localPersonaModelSigningPath))
	if err != nil {
		return summary, err
	}
	if err := ensureLocalAgentDemoPolicyRecords(ctx, config, core, agents, mapper, material, now); err != nil {
		return summary, err
	}

	organization, err := NewAgentHomeOrganizationDirectoryDB(core, mapper).CurrentHomeOrganization(ctx, values.TenantId(config.Tenant), localAgentDemoAdmin)
	if err != nil {
		return summary, fmt.Errorf("resolve administrator home organization: %w", err)
	}
	principal, err := localAgentDemoPrincipal(now, config.Tenant, localAgentDemoAdmin, organization)
	if err != nil {
		return summary, err
	}
	adminCtx := trust.WithPrincipal(ctx, principal)
	legalEntity, err := (PersonaRunLegalEntityResolver{DB: core, TenantUUID: mapper, Now: func() time.Time { return now }}).Resolve(adminCtx, agentinvoke.RunRequest{TenantID: config.Tenant, Mode: agentinvoke.OnBehalfOf, InvokerID: localAgentDemoAdmin})
	if err != nil {
		return summary, fmt.Errorf("resolve administrator legal entity: %w", err)
	}
	summary.RunPolicyCreated, err = ensureLocalAgentDemoRunPolicy(ctx, localAgentDemoRunPolicyDSN(config), mapper(values.TenantId(config.Tenant)), legalEntity, now)
	if err != nil {
		return summary, err
	}
	roleAccess := roleaccessstore.New(core, mapper, productFeatureCatalog()...)
	if err := ensureLocalAgentDemoRoleVisibility(ctx, roleAccess, values.TenantId(config.Tenant), profile.Audience); err != nil {
		return summary, err
	}
	chatRuntime, err := composeChat(ctx, ServeConfig{Profile: config.Profile, DatabaseURL: config.DatabaseURL, ChatEnabled: true, ChatDatabaseURL: config.ChatDatabaseURL, ChatCursorKey: "local-agent-demo-preparation"}, func() time.Time { return now }, newCurrentWorkerChatFacts(roleAccess, core, mapper, nil), core, nil)
	if err != nil {
		return summary, fmt.Errorf("compose local chat service: %w", err)
	}
	defer chatRuntime.close()
	publicConversation := localDevPersonaDemoConversationID(config.Tenant, "general")
	if _, err := ProvisionLocalDevPersonaChatPolicy(ctx, chat, config.Profile, config.Tenant, publicConversation, "general"); err != nil {
		return summary, err
	}
	repairStore := chat
	if config.ChatOwnerDatabaseURL != "" {
		owner, ownerErr := chatstore.New(ctx, chatstore.Config{DSN: config.ChatOwnerDatabaseURL})
		if ownerErr != nil {
			return summary, fmt.Errorf("open local chat owner store: %w", ownerErr)
		}
		defer owner.Close()
		repairStore = owner
	}
	preparation := localAgentDemoPreparation{config: config, core: core, agents: agents, personas: personas, evaluations: evaluations, mapper: mapper, key: key, material: material, admin: principal, chat: chat, repairStore: repairStore, chatService: chatRuntime.service, roleAccess: roleAccess, publicConversation: publicConversation, now: now}
	directConversation := ""
	// The starters are one list (AGENTUX-049); one that has no seed yet is named
	// in the receipt with the reason rather than skipped without a word.
	for _, entry := range localAgentDemoStarterList() {
		if entry.seed == nil {
			continue
		}
		seed := *entry.seed
		prepared, direct, prepareErr := preparation.prepare(adminCtx, seed, profile)
		if prepareErr != nil {
			return summary, prepareErr
		}
		if seed.personaID == localAgentDemoPersonaID {
			directConversation = direct
			summary.Persona, summary.DisplayName, summary.Version, summary.State = row.PersonaID, row.DisplayName, prepared.version, string(prepared.state)
		} else if seed.personaID == localAgentDemoAssistantPersonaID {
			summary.AssistantVersion, summary.AssistantState = prepared.version, string(prepared.state)
		}
		summary.EvaluationRecorded = summary.EvaluationRecorded || prepared.evaluationRecorded
		summary.Published = summary.Published || prepared.published
		summary.ModelRouteCreated = summary.ModelRouteCreated || prepared.modelRouteCreated
		summary.ProviderDeploymentCreated = summary.ProviderDeploymentCreated || prepared.providerDeploymentCreated
		summary.ChatConversationRepaired = summary.ChatConversationRepaired || prepared.chatConversationRepaired
		summary.InstallationsCreated += prepared.installationsCreated
		summary.InstallationsRefreshed += prepared.installationsRefreshed
		summary.DuplicateInstallationsRetired += prepared.duplicateInstallationsGone
		summary.SkillGrantsCreated += prepared.skillGrantsCreated
	}
	summary.Starters = localAgentDemoStarterReceipts(localAgentDemoStarterNames())
	summary.IconsSet, err = scoped.BackfillIcons(ctx, localAgentDemoAdmin, now)
	if err != nil {
		return summary, err
	}
	summary.PolicyDocumentPlacements, err = ensureLocalAgentDemoPolicyDocument(ctx, documents, chat, config.Tenant, localAgentDemoAdmin, publicConversation, []string{publicConversation, directConversation}, now)
	if err != nil {
		return summary, err
	}
	summary.HolidayDocumentPlacements, err = ensureLocalAgentDemoHolidayDocument(ctx, documents, chat, config.Tenant, localAgentDemoAdmin, publicConversation, now)
	if err != nil {
		return summary, err
	}
	// Assistant searches the documents every member may read. The seed shares
	// documents with subsets of people only, so the demo documents are shared
	// with everyone through ordinary read grants.
	summary.WorkspaceDocumentsShared, err = ensureLocalAgentDemoWorkspaceReaders(ctx, documents, WorkspaceDocumentDirectory{DB: core, TenantUUID: mapper}, config.Tenant, localAgentDemoAdmin, publicConversation)
	if err != nil {
		return summary, err
	}
	if modelErr != nil {
		summary.WorkspaceIndex.Unavailable = modelErr.Error()
		members := WorkspaceDocumentDirectory{DB: core, TenantUUID: mapper}
		if people, peopleErr := members.WorkspaceDocumentMembers(ctx, config.Tenant); peopleErr == nil {
			if status, statusErr := documents.WorkspaceIndexStatus(ctx, config.Tenant, "", people); statusErr == nil {
				summary.WorkspaceIndex.Workspace = WorkspaceDocumentSearchStatus{WorkspaceDocuments: status.Documents, WorkspacePending: status.Pending}
				summary.WorkspaceIndex.WorkspaceAccessKnown = true
			}
		}
	} else {
		summary.WorkspaceIndex, err = PrepareWorkspaceDocumentIndex(ctx, documents, config.Tenant, WorkspaceDocumentDirectory{DB: core, TenantUUID: mapper}, model)
		if err != nil {
			return summary, err
		}
	}
	return summary, nil
}

func localAgentDemoEvaluationRunID(row agentpersonastore.PersonaVersion) string {
	name := strings.Trim(strings.ReplaceAll(row.PersonaID, ".", "-"), "-")
	return "local-dev-" + name + "-v" + strconv.FormatInt(row.Version, 10) + "-" + strings.TrimPrefix(row.ContentDigest, "sha256:")[:12]
}

type agentDemoProfileBuilder struct{ manifest agentmanifest.Manifest }

func (b agentDemoProfileBuilder) Build(profile agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	if err := agentmanifest.Compatible(agentmanifest.ManifestRef{ID: profile.Manifest.ID, Version: uint64(profile.Manifest.Version), SchemaVersion: profile.Manifest.SchemaVersion, Digest: profile.Manifest.Digest}, b.manifest); err != nil {
		return agentpersona.PersonaVersion{}, ErrPersonaDraftInvalid
	}
	return agentpersona.Seal(profile)
}

type localAgentDemoAuthorizer struct {
	tenant           values.TenantId
	subject, persona string
}

func (a localAgentDemoAuthorizer) AuthorizePersonaAdminCommand(_ context.Context, actor PersonaAdminCommandActor, action PersonaAdminCommandAction, persona string) error {
	if actor.Principal == nil || actor.Tenant != a.tenant || actor.Subject != a.subject || persona != a.persona || (action != PersonaAdminPublish && action != PersonaAdminInstall && action != PersonaAdminReinstall) {
		return ErrPersonaAdminCommandUnavailable
	}
	return nil
}

func localAgentDemoPrincipal(now time.Time, tenant, subject, organization string) (*trust.Principal, error) {
	return trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman, Roles: []string{"hcm_admin"}, OrganizationScopeID: organization, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "local-agent-demo-preparation", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:local-agent-demo-preparation"})
}

func writeLocalAgentDemoDeployment(path string, deployment PersonaModelDeployment) (bool, error) {
	raw, err := json.MarshalIndent(deployment, "", "  ")
	if err != nil {
		return false, err
	}
	raw = append(raw, '\n')
	if current, err := os.ReadFile(path); err == nil {
		if bytes.Equal(current, raw) {
			return false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return false, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".persona-deployment-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(raw); err != nil || file.Sync() != nil {
		_ = file.Close()
		return false, errors.New("persist prepared persona model deployment")
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}

func ensureLocalAgentDemoPolicyRecords(ctx context.Context, config LocalAgentDemoConfig, core dbport.Beginner, agents *agentstore.Store, mapper func(values.TenantId) uuid.UUID, material LocalPersonaModelSigningMaterial, now time.Time) error {
	seed, err := base64.StdEncoding.Strict().DecodeString(material.PolicySeed)
	if err != nil || len(seed) != ed25519.SeedSize {
		return ErrAgentModelPolicyUnavailable
	}
	key := ed25519.NewKeyFromSeed(seed)
	authority, err := agentmodelpolicystore.NewAuthorityPool(ctx, agentmodelpolicystore.AuthorityPoolConfig{DSN: config.PolicyAuthorityDatabaseURL, AgentDSN: config.AgentDatabaseURL, CoreDSN: config.DatabaseURL, ChatDSN: config.ChatDatabaseURL, DocumentDSN: config.DocumentDatabaseURL})
	if err != nil {
		return err
	}
	defer authority.Close()
	return ensureLocalAgentDemoPolicyUpgrade(ctx, core, agents, authority, mapper, values.TenantId(config.Tenant), key, now)
}

// localAgentDemoRunPolicyDSN prefers the owner connection for the one
// statement the serving role may not run.
func localAgentDemoRunPolicyDSN(config LocalAgentDemoConfig) string {
	if config.AgentOwnerDatabaseURL != "" {
		return config.AgentOwnerDatabaseURL
	}
	return config.AgentDatabaseURL
}

// ensureLocalAgentDemoRoleVisibility gives each role in the agent's audience
// visibility of the audience's organization scopes. The agent gate resolves a
// user's organization scopes from this table; a tenant that never configured
// organization visibility would otherwise offer the agent to nobody. Values
// that are not active roles (population names) and rows that already exist
// are left alone.
func ensureLocalAgentDemoRoleVisibility(ctx context.Context, store *roleaccessstore.Store, tenant values.TenantId, audience agentpersona.Audience) error {
	organizations := append([]string(nil), audience.OrganizationScopes...)
	// Local sign-in credentials carry the demo pack's organization scope; the
	// chat-bound authority check requires that scope to be one the user's
	// roles can see.
	if pack, ok := demoworkforce.PackFor(tenant.String()); ok && !slices.Contains(organizations, pack.OrgScope()) {
		organizations = append(organizations, pack.OrgScope())
	}
	for _, organization := range organizations {
		for _, role := range audience.Roles {
			policy := roleaccess.VisibilityPolicy{RoleID: role, Mode: roleaccess.VisibilityAll, Reason: "local agent demo: audience role may see the audience organization"}
			if _, err := store.SaveVisibility(ctx, tenant, organization, localAgentDemoAdmin, policy); err != nil && !errors.Is(err, roleaccess.ErrVersionConflict) && !errors.Is(err, roleaccess.ErrInvalid) {
				return fmt.Errorf("seed local role visibility for %s: %w", role, err)
			}
		}
	}
	return nil
}

func ensureLocalAgentDemoRunPolicy(ctx context.Context, dsn string, tenant uuid.UUID, legalEntity string, now time.Time) (bool, error) {
	pool, err := pgxadapter.NewPool(ctx, dsn, nil)
	if err != nil {
		return false, err
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return false, err
	}
	var present bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM persona_run_policy WHERE tenant_id=$1 AND legal_entity_id=$2 AND effective_from<=$3 AND(effective_until IS NULL OR effective_until>$3))`, tenant, legalEntity, now).Scan(&present); err != nil {
		return false, err
	}
	if present {
		return false, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `INSERT INTO persona_run_policy(tenant_id,legal_entity_id,revision,effective_from,max_cost_micros,max_input_tokens,max_output_tokens,max_run_duration_ms) VALUES($1,$2,1,$3,$4,8192,2048,120000)`, tenant, legalEntity, now.Add(-time.Minute), LocalPersonaOpenAIMaxCostMicros)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func ensureLocalAgentDemoChatIdentity(ctx context.Context, scoped *agentpersonastore.TenantStore, agentID, personaID string, now time.Time) error {
	identity, err := scoped.LookupPersonaChatIdentity(ctx, agentID)
	if err == nil {
		if identity.PersonaID != personaID || !identity.Active {
			return errors.New("persona chat identity conflicts")
		}
		return nil
	}
	if !errors.Is(err, agentpersonastore.ErrNotFound) {
		return err
	}
	return scoped.RegisterPersonaChatIdentity(ctx, agentID, personaID, now)
}

// ensureLocalAgentDemoDirectConversation prepares the administrator's direct
// conversation with an agent. displayName, when given, is the agent's published
// name, which a newly created conversation is named after (AGENTUX-030); the
// agent's identifier is never used as a name.
func ensureLocalAgentDemoDirectConversation(ctx context.Context, store *chatstore.Store, service chatcore.ConversationService, tenant, admin, agentID string, displayName ...string) (string, bool, error) {
	participants := []chatcore.MemberRef{{TenantID: tenant, SubjectID: admin}, {TenantID: tenant, SubjectID: agentID}}
	id, err := chatcore.DirectPairConversationID(tenant, participants)
	if err != nil {
		return "", false, err
	}
	if store == nil || service == nil {
		return "", false, chatcore.ErrUnavailable
	}
	repaired, err := store.RepairUnroutedDirectConversation(ctx, tenant, id, admin, agentID)
	if err != nil {
		return "", false, err
	}
	resolver, err := NewPersonaDMResolver(chatstore.NewAdapter(store), chatcore.MemberRef{TenantID: tenant, SubjectID: agentID})
	if err != nil {
		return "", repaired, err
	}
	provisioner, err := NewPersonaDMProvisioner(service, resolver, chatcore.MemberRef{TenantID: tenant, SubjectID: agentID})
	if err != nil {
		return "", repaired, err
	}
	provisioner.Policies = store
	if len(displayName) > 0 {
		provisioner.DisplayName = displayName[0]
	}
	resolved, err := provisioner.EnsurePersonaDM(ctx, chatcore.Principal{TenantID: tenant, SubjectID: admin}, tenant)
	if err != nil {
		return "", repaired, err
	}
	if resolved != id {
		return "", repaired, chatcore.ErrConflict
	}
	return id, repaired, nil
}

func localAgentDemoInstalled(ctx context.Context, scoped *agentpersonastore.TenantStore, conversation, persona string, version int64) (bool, error) {
	installed, _, err := localAgentDemoInstallationState(ctx, scoped, nil, localAgentDemoTenant, conversation, persona, version)
	return installed, err
}

func localAgentDemoInstallationState(ctx context.Context, scoped *agentpersonastore.TenantStore, chat *chatstore.Store, tenant, conversation, persona string, version int64) (bool, bool, error) {
	rows, err := scoped.ListActiveInstallations(ctx, conversation)
	if err != nil {
		return false, false, err
	}
	for _, row := range rows {
		if row.PersonaID == persona && row.PersonaVersion == version {
			if chat == nil {
				return true, false, nil
			}
			current, err := chat.CapturePersonaChannelPolicy(ctx, tenant, conversation, "")
			if err != nil {
				return false, false, err
			}
			versionRow, err := scoped.GetVersion(ctx, persona, version)
			if err != nil {
				return false, false, err
			}
			var profile agentpersona.PersonaProfile
			if json.Unmarshal(versionRow.Profile, &profile) != nil {
				return false, false, ErrPersonaDraftInvalid
			}
			desired := agentPersonaPolicyFromChat(current.Policy)
			kind := agentpersona.ConversationChannel
			if current.Kind == string(chatcore.Direct) {
				kind = agentpersona.ConversationDirect
			} else if current.Kind == string(chatcore.Group) {
				kind = agentpersona.ConversationGroup
			}
			ceiling, valid := catalogPlacementTier(desired.MaxTier)
			if !valid {
				return false, false, ErrPersonaDraftInvalid
			}
			if profileTier := profile.TierForConversation(kind); profileTier < ceiling {
				ceiling = profileTier
			}
			desired.MaxTier = ceiling.String()
			desired.AlwaysPrivate = desired.AlwaysPrivate || profile.AlwaysPrivate
			return true, !agentPersonaPolicyEqual(row.ChannelPolicy, desired), nil
		}
	}
	// An agent installed here at an earlier version is installed and stale.
	// Installing again would be refused as a duplicate; the caller replaces it.
	if chat != nil {
		for _, row := range rows {
			if row.PersonaID == persona {
				return true, true, nil
			}
		}
	}
	return false, false, nil
}
