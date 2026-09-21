package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/seed"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TenantPrefix marks every tenant slug this package ever registers. A row
// whose tenant carries this prefix can never be mistaken for a pilot
// tenant's: nothing outside this package chooses a tenant slug this way, and
// this package never registers one without it.
const TenantPrefix = "sandbox-promotion-"

// Fixed, non-secret identity this package issues its own sandbox credentials
// under. None of it authenticates anything outside an in-process Sandbox: no
// listener this package opens ever exists for a network peer to present these
// against.
const (
	sandboxIssuer         = "https://issuer.sandbox.hcm-next.invalid"
	sandboxAudience       = "hcm-next-sandbox"
	sandboxExecutionRole  = "promotion_operator"
	sandboxApproverRef    = "principal:sandbox-approver"
	sandboxAuthorityRef   = "authority:sandbox"
	sandboxAuthorityBadge = "sha256:sandbox-authority-amendment"
)

var sandboxSigningKey = []byte("hcm-next-sandbox-fence-signing-key-32+bytes")

// ErrContractMismatch is returned when a caller asks this sandbox to run
// under a mode/environment pair other than the one it was built for.
var ErrContractMismatch = errors.New("sandbox: this sandbox only runs the EXECUTE/SANDBOX contract")

// Config is what [New] needs that it cannot decide for itself.
type Config struct {
	// Pool is the database pool this sandbox's tenant lives on. Required.
	// The caller owns its lifecycle (a *pgtest.DB-backed pool in every test
	// this package itself ships, but nothing here assumes that).
	Pool *pgxadapter.Pool
	// Slug names this sandbox uniquely among any others sharing Pool's
	// schema. New prepends [TenantPrefix]; the caller supplies only the part
	// that distinguishes one sandbox from another. Required.
	Slug string
	// CellID is the cell identifier the composed application registers the
	// tenant against. Empty means "sandbox-cell".
	CellID string
	// Now is the sandbox's pinned clock: every timestamp the composed
	// application and this package's own fence stamp comes from it. Nil
	// means [time.Now] in UTC.
	Now func() time.Time
}

// Sandbox is one synthetic Promotion tenant, composed under the SANDBOX
// EXECUTE contract, with every reach at a real external system fenced. See
// the package doc for what that means and why.
type Sandbox struct {
	pool     *pgxadapter.Pool
	tenant   string
	tenantID tenantUUID
	cellID   string
	now      func() time.Time

	store    *pgstore.Store
	verifier *trust.HMACVerifier
	fence    *Fence
	cell     *app.Cell
	evidence *app.MemoryEvidenceSink

	contract intent.ModeContract
}

// New composes a fresh sandbox: it bootstraps cfg.Slug's tenant row, builds
// the fenced connectivity connector, composes the same *app.Cell a
// production cell runs (Store, Verifier, the P1B execution-authority wiring
// [application.ComposeExecutionAuthority] itself builds), and seeds the
// tenant from the Promotion fixture corpus via [Sandbox.Reset].
//
// It opens no network listener. Every seam a caller of this package touches
// is a Go value or a Go method call; there is nothing here for a dial-out to
// reach even if some future adapter tried.
func New(ctx context.Context, cfg Config) (*Sandbox, error) {
	if cfg.Pool == nil {
		return nil, fmt.Errorf("sandbox: a database pool is required")
	}
	if cfg.Slug == "" {
		return nil, fmt.Errorf("sandbox: a slug is required")
	}
	cellID := cfg.CellID
	if cellID == "" {
		cellID = "sandbox-cell"
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	tenant := TenantPrefix + cfg.Slug
	tenantID := pgstore.TenantID(tenant)

	store, err := pgstore.New(cfg.Pool, pgstore.WithCellID(cellID), pgstore.WithClock(now))
	if err != nil {
		return nil, fmt.Errorf("sandbox: build the intent store: %w", err)
	}
	if err := store.Bootstrap(ctx, tenant); err != nil {
		return nil, fmt.Errorf("sandbox: bootstrap tenant %s: %w", tenant, err)
	}

	fence := NewFence(now)
	incumbent, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		return nil, fmt.Errorf("sandbox: build the stand-in incumbent: %w", err)
	}
	connector := NewFencedConnector(incumbent, fence, incumbent.Descriptor().SourceRef)

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      sandboxSigningKey,
		Issuer:   sandboxIssuer,
		Audience: sandboxAudience,
		Now:      now,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox: build the credential verifier: %w", err)
	}

	evidence := app.NewMemoryEvidenceSink()
	cellConfig := app.CellConfig{
		Store:       store,
		Verifier:    verifier,
		Audience:    sandboxAudience,
		MaxDeadline: 30 * time.Second,
		Now:         now,
		Incumbent:   connector,
		Evidence:    evidence,
		// Telemetry is left nil: a sandbox cell publishes no spans or
		// metrics to anything, so there is no exporter seam left for a real
		// collector to be configured onto.
	}

	// Serve resolves only ACTIVE compiled versions from the durable registry
	// and never self-approves. A sandbox is a development environment, so it
	// publishes, approves and activates the shipped versions exactly as
	// `hcmnext workflow-version bootstrap-dev` does for a development
	// database; without it every EXECUTE is refused as "not configured".
	if _, err := platformexecution.BootstrapDevVersions(context.Background(), workflowversionstore.Store{DB: cfg.Pool}, now()); err != nil {
		return nil, fmt.Errorf("sandbox: activate the shipped workflow versions: %w", err)
	}

	if err := application.ComposeExecutionAuthority(&cellConfig, cfg.Pool, evidence, application.ServeConfig{
		CellID:                   cellID,
		ExecutionAuthorityDigest: sandboxAuthorityBadge,
		ExecutionAuthorityRole:   sandboxExecutionRole,
		ExecutionApprover:        sandboxApproverRef,
	}); err != nil {
		return nil, fmt.Errorf("sandbox: compose the execution authority: %w", err)
	}

	cell, err := app.NewCell(cellConfig)
	if err != nil {
		return nil, fmt.Errorf("sandbox: compose the cell: %w", err)
	}

	contract, err := intent.ModeContractFor(intent.ModeExecute, intent.EnvironmentSandbox)
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve the EXECUTE/SANDBOX contract: %w", err)
	}

	sb := &Sandbox{
		pool: cfg.Pool, tenant: tenant, tenantID: tenantID, cellID: cellID, now: now,
		store: store, verifier: verifier, fence: fence, cell: cell, evidence: evidence,
		contract: contract,
	}

	if _, err := sb.reseed(ctx); err != nil {
		return nil, fmt.Errorf("sandbox: seed tenant %s: %w", tenant, err)
	}
	return sb, nil
}

// TenantSlug returns this sandbox's [TenantPrefix]-marked tenant slug.
func (sb *Sandbox) TenantSlug() string { return sb.tenant }

// TenantID returns this sandbox's tenant uuid, the same derivation
// [pgstore.TenantID] gives its slug, spelled as [16]byte rather than
// github.com/google/uuid.UUID (see [tenantUUID]).
func (sb *Sandbox) TenantID() tenantUUID { return sb.tenantID }

// Cell returns the composed application. Exposed for tests and callers that
// need to reach beyond [Sandbox.RunPromotionProof] - the operator surfaces,
// the evidence sink - without this package growing one accessor per field
// [app.Cell] already exports.
func (sb *Sandbox) Cell() *app.Cell { return sb.cell }

// Fence returns the fence every outbound adapter this sandbox composed is
// wired behind.
func (sb *Sandbox) Fence() *Fence { return sb.fence }

// Contract returns the fixed (EXECUTE, SANDBOX) [intent.ModeContract] this
// sandbox runs under.
func (sb *Sandbox) Contract() intent.ModeContract { return sb.contract }

// Evidence returns the cell-wide evidence sink [Sandbox.RunPromotionProof]
// and every capability handler the composed cell runs record onto.
func (sb *Sandbox) Evidence() *app.MemoryEvidenceSink { return sb.evidence }

// ResetReport is what one [Sandbox.Reset] did.
type ResetReport struct {
	TenantID tenantUUID
	Deleted  map[string]int64
	// Preserved names the tenant-scoped tables Reset left untouched because
	// they (or a table an append-only row of theirs still references) are
	// append-only: a tenant's evidence, ledger and registry history is not
	// part of what an ordinary Reset clears. See [appendOnlyTables] and
	// [preservedTables].
	Preserved  []string
	SeedDigest string
	Aggregates *aggregates.LoadedFixtures
}

// Reset drops every row this sandbox's tenant owns in a mutable table and
// re-seeds it from the Promotion fixture corpus. It never touches another
// tenant's rows: every delete it issues (through [DeleteTenantRows]) carries
// an exact tenant_id equality predicate naming this sandbox's own tenant, in
// every tenant-scoped table PostgreSQL's own catalog names - never a
// hand-maintained list this package could let drift. It never touches an
// append-only table either, for the same reason a real tenant's evidence
// trail cannot be rewritten: see [ResetReport.Preserved].
//
// Reset is safe to call concurrently with another [Sandbox]'s Reset over the
// same schema: the two operate under disjoint tenant_id predicates and touch
// no shared row.
func (sb *Sandbox) Reset(ctx context.Context) (ResetReport, error) {
	return sb.reseed(ctx)
}

// reseed is Reset's implementation, also used by [New] to seed a freshly
// composed sandbox for the first time.
func (sb *Sandbox) reseed(ctx context.Context) (ResetReport, error) {
	deleteTx, err := sb.pool.Begin(ctx)
	if err != nil {
		return ResetReport{}, fmt.Errorf("sandbox: begin reset: %w", err)
	}
	defer deleteTx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, deleteTx, sb.tenantID); err != nil {
		return ResetReport{}, err
	}
	deleted, preserved, err := DeleteTenantRows(ctx, deleteTx, sb.tenantID)
	if err != nil {
		return ResetReport{}, err
	}
	if err := deleteTx.Commit(ctx); err != nil {
		return ResetReport{}, fmt.Errorf("sandbox: commit reset delete: %w", err)
	}

	// The tenant's own registration row is mutable and gets deleted above
	// unless an append-only row (a definition_version registration, say)
	// still references it, in which case it was preserved automatically. In
	// both cases re-registering it here is a safe no-op: Bootstrap is
	// idempotent by tenant id.
	if err := sb.store.Bootstrap(ctx, sb.tenant); err != nil {
		return ResetReport{}, fmt.Errorf("sandbox: re-register tenant %s: %w", sb.tenant, err)
	}

	seedTx, err := sb.pool.Begin(ctx)
	if err != nil {
		return ResetReport{}, fmt.Errorf("sandbox: begin seed: %w", err)
	}
	defer seedTx.Rollback(ctx)
	summary, err := seed.Seed(ctx, seedTx, sb.tenantID)
	if err != nil {
		return ResetReport{}, fmt.Errorf("sandbox: seed tenant %s: %w", sb.tenant, err)
	}
	if err := seedTx.Commit(ctx); err != nil {
		return ResetReport{}, fmt.Errorf("sandbox: commit seed: %w", err)
	}

	return ResetReport{
		TenantID: sb.tenantID, Deleted: deleted, Preserved: preserved,
		SeedDigest: summary.Digest, Aggregates: summary.Aggregates,
	}, nil
}

// PromotionProof is what one [Sandbox.RunPromotionProof] run produced: the
// full propose -> execute -> decide journey, through to its resulting stage.
type PromotionProof struct {
	IntentID           string
	ProposalRevisionID string
	MaterialDigest     string
	Stage              workspace.JourneyStage
	Detail             workspace.JourneyDetail
	// Approvers names, in decision order, the routed principal each approval
	// was decided by. None of them is the sandbox initiator.
	Approvers []string
}

// RunPromotionProof drives in through [workspace.JourneyEngine.Propose]
// (create the intent and simulate it), [workspace.JourneyEngine.Execute]
// (run the caller-driven driver up to its first approval) and
// [workspace.JourneyEngine.Decide] (claim and complete that approval as the
// routed approver, approved, resuming the driver to completion), entirely in
// this sandbox's own process: [Sandbox.Cell]'s Journey field is the same
// workspace.JourneyEngine internal/transport/cell's HTTP workspace handler
// calls, and nothing in this call chain opens a socket.
//
// It is the "full propose/execute/decide run" SANDBOX-001 asks this sandbox
// to prove leaves no row outside the sandbox tenant and reaches no fenced
// destination. in names no idempotency key of its own - the journey mints one
// from this sandbox's own id source - so a caller wanting two distinct proofs
// gives two distinct in.WorkerRef/target combinations, or accepts that a
// byte-identical proposal against the same worker resolves to the same
// intent.
func (sb *Sandbox) RunPromotionProof(ctx context.Context, in workspace.ProposalInput) (*PromotionProof, error) {
	return sb.executeUnder(ctx, sb.contract.Mode, sb.contract.Environment, in)
}

// RunPromotionProofUnder is [Sandbox.RunPromotionProof] parameterised by the
// mode/environment pair the caller is asking this sandbox to run under. A
// sandbox only ever runs its own fixed (EXECUTE, SANDBOX) contract
// ([Sandbox.Contract]); asking for any other pair - EXECUTE/PRODUCTION most
// of all - is refused with [ErrContractMismatch] before this sandbox reads or
// writes anything at all.
func (sb *Sandbox) RunPromotionProofUnder(ctx context.Context, mode intent.Mode, env intent.Environment, in workspace.ProposalInput) (*PromotionProof, error) {
	return sb.executeUnder(ctx, mode, env, in)
}

func (sb *Sandbox) executeUnder(ctx context.Context, mode intent.Mode, env intent.Environment, in workspace.ProposalInput) (*PromotionProof, error) {
	if mode != sb.contract.Mode || env != sb.contract.Environment {
		return nil, fmt.Errorf("%w: asked for %s/%s", ErrContractMismatch, mode, env)
	}

	principalCtx, err := sb.authenticatedContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("sandbox: authenticate the sandbox principal: %w", err)
	}

	proposed, err := sb.cell.Journey.Propose(principalCtx, in)
	if err != nil {
		return nil, fmt.Errorf("sandbox: propose: %w", err)
	}
	if proposed.Stage == workspace.JourneyStageBlocked {
		return nil, fmt.Errorf("sandbox: propose blocked for intent %s: the simulation produced no executable plan", proposed.IntentID)
	}

	detail, err := sb.cell.Journey.Execute(principalCtx, proposed.IntentID)
	if err != nil {
		return nil, fmt.Errorf("sandbox: execute %s: %w", proposed.IntentID, err)
	}

	// PROMOUX-015: the initiator may not decide its own promotion's
	// approvals, so each open approval is decided by the principal the
	// composed routing actually assigned it to -- read back from the routed
	// WorkItem itself, never assumed -- under that principal's own sandbox
	// credential, until the journey leaves approval.
	var approvers []string
	decided := detail
	for guard := 0; awaitingApproval(decided.Summary.Stage); guard++ {
		if guard >= maxProofApprovals {
			return nil, fmt.Errorf("sandbox: decide %s: still at %s after %d decisions", proposed.IntentID, decided.Summary.Stage, guard)
		}
		approver, routeErr := routedApproverOf(decided)
		if routeErr != nil {
			return nil, fmt.Errorf("sandbox: decide %s: %w", proposed.IntentID, routeErr)
		}
		approverCtx, authErr := sb.approverContext(ctx, approver)
		if authErr != nil {
			return nil, fmt.Errorf("sandbox: authenticate routed approver %s: %w", approver, authErr)
		}
		decided, err = sb.cell.Journey.Decide(approverCtx, proposed.IntentID, workspace.Decision{
			Approve: true, Reason: "sandbox proof: routed approver approves",
		})
		if err != nil {
			return nil, fmt.Errorf("sandbox: decide %s as %s: %w", proposed.IntentID, approver, err)
		}
		approvers = append(approvers, approver)
	}

	return &PromotionProof{
		IntentID:           decided.Summary.IntentID,
		ProposalRevisionID: proposed.ProposalRevisionID,
		MaterialDigest:     proposed.MaterialDigest,
		Stage:              decided.Summary.Stage,
		Detail:             decided,
		Approvers:          approvers,
	}, nil
}

// authenticatedContext issues and verifies a fresh sandbox-only credential
// and returns a context carrying its [trust.Principal], the same way
// internal/transport/cell's authentication interceptor does for a wire call -
// except there is no wire here, so this package does that half of the
// interceptor's job itself. [workspace.JourneyEngine.Propose] derives the
// intent's trusted initiator from this same principal on its own, the same
// way internal/transport.ApplyTrustedContext would for a wire call.
func (sb *Sandbox) authenticatedContext(ctx context.Context) (context.Context, error) {
	return sb.contextFor(ctx, sb.initiatorSubject(),
		[]string{"intent_author", string(authz.RoleCompAdmin), sandboxExecutionRole}, []string{sandboxAuthorityRef})
}

// approverContext issues a sandbox-only credential for one routed approver.
// It carries no execution role and no authority reference: membership of the
// routed WorkItem, not a role, is a decision's authority. comp_admin is only
// what lets the approver read the subject's compensation to re-simulate the
// proposal it decides.
func (sb *Sandbox) approverContext(ctx context.Context, approver string) (context.Context, error) {
	if approver == sb.initiatorSubject() {
		return nil, fmt.Errorf("sandbox: the approval is routed to the sandbox initiator %s", approver)
	}
	return sb.contextFor(ctx, approver, []string{string(authz.RoleCompAdmin)}, nil)
}

// initiatorSubject is the subject of the sandbox principal that proposes and
// executes every proof.
func (sb *Sandbox) initiatorSubject() string { return "sandbox-principal:" + sb.tenant }

func (sb *Sandbox) contextFor(ctx context.Context, subject string, roles, authorityRefs []string) (context.Context, error) {
	now := sb.now()
	token, err := sb.verifier.Issue(trust.Claims{
		Issuer:               sandboxIssuer,
		Audience:             sandboxAudience,
		Subject:              subject,
		SubjectKind:          "human",
		Tenant:               sb.tenant,
		OrganizationScopeID:  "org-" + sb.tenant,
		Roles:                roles,
		AuthorityRefs:        authorityRefs,
		Purposes:             []string{authz.PurposeCompensationReview},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-" + subject,
		IssuedAtUnix:         now.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return nil, fmt.Errorf("issue sandbox credential: %w", err)
	}
	principal, err := sb.verifier.Verify(ctx, trust.Credential{Scheme: "Bearer", Token: token, Audience: sandboxAudience})
	if err != nil {
		return nil, fmt.Errorf("verify sandbox credential: %w", err)
	}
	return trust.WithPrincipal(ctx, principal), nil
}

// maxProofApprovals bounds the decide loop: the executable promotion plan
// raises finance, manager and at most one reapproval gate.
const maxProofApprovals = 4

// awaitingApproval reports whether an approval is open at stage.
func awaitingApproval(stage workspace.JourneyStage) bool {
	switch stage {
	case workspace.JourneyStageAwaitingApproval, workspace.JourneyStageFinanceApproval,
		workspace.JourneyStageManagerApproval, workspace.JourneyStageReapproval:
		return true
	}
	return false
}

// routedApproverOf names the principal the journey's open approval WorkItem
// is routed to: the owner routing chose, or the sole candidate its recorded
// resolution admits. An open approval routed to nobody, or left open to a
// candidate set, is refused rather than guessed at.
func routedApproverOf(detail workspace.JourneyDetail) (string, error) {
	for _, item := range detail.WorkItems {
		if item.Kind != workitem.KindApproval || !openApproval(item.Status) {
			continue
		}
		if owner := item.Assignment.ChosenOwner; owner != "" {
			return owner, nil
		}
		if candidates := item.Assignment.Resolution.Candidates; len(candidates) == 1 && candidates[0].PrincipalID != "" {
			return candidates[0].PrincipalID, nil
		}
		return "", fmt.Errorf("sandbox: approval %s (%s) names no single routed approver", item.WorkItemID, item.NodeID)
	}
	return "", fmt.Errorf("sandbox: the journey is at %s but no approval WorkItem is open", detail.Summary.Stage)
}

// openApproval reports whether a routed approval can still be decided from
// status.
func openApproval(status workitem.Status) bool {
	switch status {
	case workitem.StatusRouted, workitem.StatusAssigned, workitem.StatusAvailable,
		workitem.StatusClaimed, workitem.StatusInProgress:
		return true
	}
	return false
}

// tenantScopedRowCount is a small test/diagnostic helper: it sums row counts
// across every tenant-scoped table PostgreSQL's own catalog names, using the
// same discovery [DeleteTenantRows] uses, so a test asserting "nothing
// outside this tenant changed" reads the same table set Reset itself would
// consider (mutable and append-only alike - this helper counts rows, it does
// not delete them, so an append-only table's rows are fair to include).
func tenantScopedRowCount(ctx context.Context, q dbport.Querier, tenantID tenantUUID) (int64, error) {
	tables, err := tenantScopedTables(ctx, q)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, table := range tables {
		var n int64
		if err := q.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id = $1`, quoteIdent(table)), tenantID).Scan(&n); err != nil {
			return 0, fmt.Errorf("sandbox: count %s for %s: %w", table, tenantID, err)
		}
		total += n
	}
	return total, nil
}
