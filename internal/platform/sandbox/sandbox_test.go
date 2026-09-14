package sandbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// baseTime pins every sandbox this suite composes to one instant, so a
// digest or a receipt this suite asserts on never drifts with wall time.
var baseTime = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func fixedNow() time.Time { return baseTime }

// newSandboxPool opens one fresh pgtest schema, every real migration
// applied, and the pgxadapter.Pool [Sandbox] itself runs on. Sharing one
// pool/schema across more than one [Sandbox] in a test is deliberate: it is
// what lets a test prove two sandboxes' tenants stay disjoint in the same
// physical tables, which is the claim SANDBOX-001 actually needs proven.
func newSandboxPool(t *testing.T) *pgxadapter.Pool {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newTestSandbox(t *testing.T, pool *pgxadapter.Pool, slug string) *Sandbox {
	t.Helper()
	sb, err := New(context.Background(), Config{Pool: pool, Slug: slug, Now: fixedNow})
	if err != nil {
		t.Fatalf("New(%q): %v", slug, err)
	}
	return sb
}

// totalTenantRows sums every tenant-scoped row this tenant owns, across every
// table the schema's own catalog names - mutable and append-only alike.
func totalTenantRows(t *testing.T, pool *pgxadapter.Pool, sb *Sandbox) int64 {
	t.Helper()
	n, err := tenantScopedRowCount(context.Background(), pool, sb.TenantID())
	if err != nil {
		t.Fatalf("tenantScopedRowCount: %v", err)
	}
	return n
}

// mutableTenantRows sums this tenant's rows only across the tables
// [DeleteTenantRows] would actually target: append-only tables (and anything
// [preservedTables] closes over because of one) are excluded, because a
// Reset never clears them and their row count can only grow as a sandbox is
// used - counting them would make "back to baseline" comparison after a
// promotion run spuriously fail once any evidence has accumulated.
func mutableTenantRows(t *testing.T, pool *pgxadapter.Pool, sb *Sandbox) int64 {
	t.Helper()
	ctx := context.Background()
	tables, err := tenantScopedTables(ctx, pool)
	if err != nil {
		t.Fatalf("tenantScopedTables: %v", err)
	}
	appendOnly, err := appendOnlyTables(ctx, pool)
	if err != nil {
		t.Fatalf("appendOnlyTables: %v", err)
	}
	edges, err := tenantForeignKeys(ctx, pool, tables)
	if err != nil {
		t.Fatalf("tenantForeignKeys: %v", err)
	}
	preserved := preservedTables(appendOnly, edges)

	var total int64
	for _, table := range tables {
		if preserved[table] {
			continue
		}
		var n int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+quoteIdent(table)+` WHERE tenant_id = $1`, sb.TenantID()).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		total += n
	}
	return total
}

// promotionInput is the canonical Promotion fixture proposal: Omar Reyes from
// OPS-HRBP2/P2 to OPS-HRBP3/P3, the same legacy raise
// internal/domains/promotion certifies as READY. [workspace.JourneyEngine]
// mints its own idempotency key per Propose call, so two calls with this same
// input are two distinct intents, not a replay of one.
//
// TargetPositionID is deliberately absent: PROMOUX-004 checks a non-empty
// value against the real Position domain, target_position_ref is no longer
// a required kernel input (internal/intent/definitions/definitions.go), and
// this environment has no job_position row for any corpus fixture (the
// corpus predates the Position domain). internal/domains/promotion's own
// promotion_test.go baseRequest -- the exact scenario this fixture mirrors
// -- drops the field for the identical reason.
func promotionInput() workspace.ProposalInput {
	return workspace.ProposalInput{
		WorkerRef:      "omar-reyes",
		TargetJobCode:  "OPS-HRBP3",
		TargetGrade:    "P3",
		ProposedBase:   "98000.00",
		EffectiveDate:  "2026-06-01",
		BusinessReason: "promotion_into_senior_hrbp",
	}
}

// ---------------------------------------------------------------------------
// PRIMARY
// ---------------------------------------------------------------------------

// TestTodo_SANDBOX_001 is the PRIMARY case: a full propose/execute/decide run
// inside the sandbox succeeds, leaves no row outside the sandbox tenant, and
// reaches no fenced destination.
func TestTodo_SANDBOX_001(t *testing.T) {
	pool := newSandboxPool(t)
	neighbor := newTestSandbox(t, pool, "neighbor")
	sb := newTestSandbox(t, pool, "primary")

	neighborBefore := totalTenantRows(t, pool, neighbor)
	if neighborBefore == 0 {
		t.Fatal("the neighbor sandbox has no rows of its own to prove untouched")
	}

	proof, err := sb.RunPromotionProof(context.Background(), promotionInput())
	if err != nil {
		t.Fatalf("RunPromotionProof: %v", err)
	}
	if proof.IntentID == "" {
		t.Fatal("proof names no intent id")
	}
	if proof.ProposalRevisionID == "" {
		t.Fatal("proof minted no proposal revision; propose did not run")
	}
	if proof.MaterialDigest == "" {
		t.Fatal("proof carries no material digest")
	}
	if proof.Detail.Instance == nil {
		t.Fatal("proof names no executed workflow instance; execute did not run")
	}
	// Decide: Decide claimed and completed the routed approval WorkItem and
	// resumed the driver; a proof that reached this far reached a real
	// governance decision (intent_decision), not a bypass - see
	// internal/intent/app/journey_decide.go's WF-RUN-027 recording.
	if proof.Stage != workspace.JourneyStageCompleted {
		t.Fatalf("stage = %s, want COMPLETED", proof.Stage)
	}
	if proof.Detail.Ledger == nil {
		t.Fatal("proof recorded no terminal ledger write")
	}
	assertDecidedByRoutedApprovers(t, pool, sb, proof)

	if got := sb.Fence().Count(); got != 0 {
		t.Fatalf("fence recorded %d refusals during a run that should reach nothing fenced: %+v", got, sb.Fence().Refusals())
	}

	if got := totalTenantRows(t, pool, neighbor); got != neighborBefore {
		t.Fatalf("the neighbor tenant's row count changed from %d to %d during another sandbox's run", neighborBefore, got)
	}
}

// ---------------------------------------------------------------------------
// GOLDEN
// ---------------------------------------------------------------------------

// TestTodo_SANDBOX_001_Golden proves Reset restores a named baseline: two
// resets of the same sandbox produce the same seed digest, independent of
// the fresh random ids each one assigns.
func TestTodo_SANDBOX_001_Golden(t *testing.T) {
	pool := newSandboxPool(t)
	sb := newTestSandbox(t, pool, "golden")

	// New already performed this tenant's first-ever seed (aggregates.LoadFixtures
	// runs exactly once per tenant lifetime, gated by an append-only
	// definition_version marker - see internal/data/seed.Seed's own doc
	// comment), so the aggregate corpus is already loaded by the time a test
	// can call Reset explicitly.
	if got := mutableTenantRows(t, pool, sb); got == 0 {
		t.Fatal("the sandbox tenant has no rows after New's own initial seed")
	}

	first, err := sb.Reset(context.Background())
	if err != nil {
		t.Fatalf("first Reset: %v", err)
	}
	if first.SeedDigest == "" {
		t.Fatal("first reset produced no seed digest")
	}
	// Aggregates is nil here: the append-only marker row already exists from
	// New's own initial seed, so this reset's seed.Seed call verifies the
	// existing content digest rather than loading it again - the correct,
	// idempotent behavior.

	second, err := sb.Reset(context.Background())
	if err != nil {
		t.Fatalf("second Reset: %v", err)
	}
	if second.SeedDigest != first.SeedDigest {
		t.Fatalf("seed digest drifted across resets: %q vs %q", first.SeedDigest, second.SeedDigest)
	}
	if second.TenantID != first.TenantID {
		t.Fatalf("reset changed the tenant identity: %x vs %x", first.TenantID, second.TenantID)
	}
}

// ---------------------------------------------------------------------------
// SECURITY
// ---------------------------------------------------------------------------

// TestTodo_SANDBOX_001_Security is the refusal matrix: an execution asking
// for the EXECUTE/PRODUCTION contract inside the sandbox is refused before
// this sandbox reads or writes anything, and a connector naming a real
// destination is refused rather than followed (the [FencedConnector] half of
// this claim is exercised in depth by fence_test.go; this test is the
// composed-cell half).
func TestTodo_SANDBOX_001_Security(t *testing.T) {
	pool := newSandboxPool(t)
	sb := newTestSandbox(t, pool, "security")

	before := totalTenantRows(t, pool, sb)

	_, err := sb.RunPromotionProofUnder(context.Background(), intent.ModeExecute, intent.EnvironmentProduction, promotionInput())
	if err == nil {
		t.Fatal("RunPromotionProofUnder admitted the EXECUTE/PRODUCTION contract inside a sandbox")
	}
	if !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("error = %v, want ErrContractMismatch", err)
	}

	_, err = sb.RunPromotionProofUnder(context.Background(), intent.ModeSimulate, intent.EnvironmentSandbox, promotionInput())
	if err == nil {
		t.Fatal("RunPromotionProofUnder admitted SIMULATE/SANDBOX; this sandbox only runs EXECUTE/SANDBOX")
	}

	if got := totalTenantRows(t, pool, sb); got != before {
		t.Fatalf("row count changed from %d to %d for a run that should have been refused up front", before, got)
	}

	t.Run("the sandbox's own contract is EXECUTE/SANDBOX", func(t *testing.T) {
		c := sb.Contract()
		if c.Mode != intent.ModeExecute || c.Environment != intent.EnvironmentSandbox {
			t.Fatalf("Contract() = %s/%s, want EXECUTE/SANDBOX", c.Mode, c.Environment)
		}
		if c.AllowsExternalEffect {
			t.Fatal("the sandbox's own contract allows an external effect")
		}
		if c.AllowsApprovalConsumption {
			t.Fatal("the sandbox's own contract allows consuming a live approval")
		}
		if !c.AllowsDomainCommit {
			t.Fatal("the sandbox's own contract forbids a domain commit; sandbox truth could never be written at all")
		}
	})
}

// ---------------------------------------------------------------------------
// RECOVERY
// ---------------------------------------------------------------------------

// TestTodo_SANDBOX_001_Recovery proves Reset recovers a sandbox that
// accumulated runtime state (an executed intent, its workflow instance) back
// to the seeded baseline: after Reset, none of that runtime state remains,
// and the tenant is exactly as seeded again.
func TestTodo_SANDBOX_001_Recovery(t *testing.T) {
	pool := newSandboxPool(t)
	sb := newTestSandbox(t, pool, "recovery")

	baseline, err := sb.Reset(context.Background())
	if err != nil {
		t.Fatalf("baseline Reset: %v", err)
	}
	// Mutable rows only: an append-only evidence row this run writes (a
	// definition_version registration, a ledger/evidence entry) is never
	// cleared by Reset and would otherwise make "back to baseline" a
	// moving target. See mutableTenantRows.
	baselineRows := mutableTenantRows(t, pool, sb)

	proof, err := sb.RunPromotionProof(context.Background(), promotionInput())
	if err != nil {
		t.Fatalf("RunPromotionProof: %v", err)
	}
	assertDecidedByRoutedApprovers(t, pool, sb, proof)
	grownRows := mutableTenantRows(t, pool, sb)
	if grownRows <= baselineRows {
		t.Fatalf("running a promotion did not grow the tenant's own mutable row count: baseline %d, after %d", baselineRows, grownRows)
	}

	recovered, err := sb.Reset(context.Background())
	if err != nil {
		t.Fatalf("recovery Reset: %v", err)
	}
	if recovered.SeedDigest != baseline.SeedDigest {
		t.Fatalf("recovered seed digest %q, want the baseline %q", recovered.SeedDigest, baseline.SeedDigest)
	}
	if got := mutableTenantRows(t, pool, sb); got != baselineRows {
		t.Fatalf("mutable row count after recovery = %d, want the baseline %d", got, baselineRows)
	}
}

// ---------------------------------------------------------------------------
// RACE
// ---------------------------------------------------------------------------

// TestTodo_SANDBOX_001_Race runs two sandboxes' Reset concurrently, many
// times, over one shared schema, and proves neither ever observes the
// other's row count: every reset is scoped by its own tenant id, so
// concurrent resets over the same physical tables never interfere.
func TestTodo_SANDBOX_001_Race(t *testing.T) {
	pool := newSandboxPool(t)
	a := newTestSandbox(t, pool, "race-a")
	b := newTestSandbox(t, pool, "race-b")

	baselineA, err := a.Reset(context.Background())
	if err != nil {
		t.Fatalf("baseline reset a: %v", err)
	}
	baselineB, err := b.Reset(context.Background())
	if err != nil {
		t.Fatalf("baseline reset b: %v", err)
	}
	expectA := totalTenantRows(t, pool, a)
	expectB := totalTenantRows(t, pool, b)

	const rounds = 5
	var wg sync.WaitGroup
	errs := make(chan error, rounds*2)
	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := a.Reset(context.Background()); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := b.Reset(context.Background()); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent reset failed: %v", err)
	}

	finalA, err := a.Reset(context.Background())
	if err != nil {
		t.Fatalf("final reset a: %v", err)
	}
	finalB, err := b.Reset(context.Background())
	if err != nil {
		t.Fatalf("final reset b: %v", err)
	}
	if finalA.SeedDigest != baselineA.SeedDigest {
		t.Fatalf("tenant a's seed digest drifted: %q vs baseline %q", finalA.SeedDigest, baselineA.SeedDigest)
	}
	if finalB.SeedDigest != baselineB.SeedDigest {
		t.Fatalf("tenant b's seed digest drifted: %q vs baseline %q", finalB.SeedDigest, baselineB.SeedDigest)
	}
	if got := totalTenantRows(t, pool, a); got != expectA {
		t.Fatalf("tenant a's row count = %d after concurrent resets, want %d (no cross-contamination from b)", got, expectA)
	}
	if got := totalTenantRows(t, pool, b); got != expectB {
		t.Fatalf("tenant b's row count = %d after concurrent resets, want %d (no cross-contamination from a)", got, expectB)
	}
	if a.TenantID() == b.TenantID() {
		t.Fatal("two distinct sandbox slugs collided onto the same tenant id")
	}
}

// expectedRoutedApprover is who the composition's routing assigns an
// approval requirement to for a corpus worker (no manager fact, no finance
// partner configured): the prototype approval goes to the configured
// ExecutionApprover, and the executable plan's finance and manager approvals
// to PROMOUX-003's class-scoped derivations of it. It is computed from the
// same derivations internal/platform/execution routes with, independently of
// the WorkItem the proof reads its approver from.
func expectedRoutedApprover(t *testing.T, requirement string) string {
	t.Helper()
	var (
		want string
		err  error
	)
	switch requirement {
	case prototype.ApprovalRequirementID:
		want = sandboxApproverRef
	case promotionexec.ApprovalFinance:
		want, err = promotionexec.FinanceApproverFor(sandboxApproverRef)
	case promotionexec.ApprovalManager:
		want, err = promotionexec.ManagerApproverFor(sandboxApproverRef)
	default:
		t.Fatalf("decision under unexpected requirement %q", requirement)
	}
	if err != nil {
		t.Fatalf("derive the routed approver for %s: %v", requirement, err)
	}
	return want
}

// assertDecidedByRoutedApprovers proves separation of duties on the durable
// record: every human approval decision this proof recorded -- the
// intent_decision row and the completed WorkItem alike -- names the principal
// the composition routed that approval to, never the sandbox initiator, and no
// one principal decided two approvals of the proposal.
func assertDecidedByRoutedApprovers(t *testing.T, pool *pgxadapter.Pool, sb *Sandbox, proof *PromotionProof) {
	t.Helper()
	ctx := context.Background()
	initiator := sb.initiatorSubject()
	rows, err := pool.Query(ctx, `SELECT requirement_id, decided_by FROM intent_decision
		WHERE tenant_id = $1 AND intent_id::text = $2 AND decision_kind = 'HUMAN_APPROVAL' ORDER BY decided_at, requirement_id`,
		sb.TenantID(), proof.IntentID)
	if err != nil {
		t.Fatalf("read intent decisions: %v", err)
	}
	var decisions [][2]string
	for rows.Next() {
		var requirement, by string
		if err := rows.Scan(&requirement, &by); err != nil {
			rows.Close()
			t.Fatalf("scan intent decision: %v", err)
		}
		decisions = append(decisions, [2]string{requirement, by})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("intent decisions: %v", err)
	}
	if len(decisions) == 0 {
		t.Fatal("the proof recorded no human approval decision; nothing proves who decided")
	}
	if len(decisions) != len(proof.Approvers) {
		t.Fatalf("recorded %d human approval decisions %v, the proof reports deciding %v", len(decisions), decisions, proof.Approvers)
	}
	seen := map[string]string{}
	for _, d := range decisions {
		requirement, by := d[0], d[1]
		if by == initiator {
			t.Errorf("%s was decided by the sandbox initiator %s", requirement, initiator)
		}
		if want := expectedRoutedApprover(t, requirement); by != want {
			t.Errorf("%s decided_by = %q, want the routed approver %q", requirement, by, want)
		}
		if prior, dup := seen[by]; dup {
			t.Errorf("%s decided both %s and %s", by, prior, requirement)
		}
		seen[by] = requirement
	}

	var completedByInitiator, completedApprovals int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE completed_by = $2), count(*) FILTER (WHERE status = 'COMPLETED')
		FROM work_item WHERE tenant_id = $1 AND kind = 'APPROVAL' AND proposal_ref = $3`,
		sb.TenantID(), initiator, proof.MaterialDigest).Scan(&completedByInitiator, &completedApprovals); err != nil {
		t.Fatalf("read completed approvals: %v", err)
	}
	if completedByInitiator != 0 {
		t.Errorf("%d approval WorkItems were completed by the sandbox initiator", completedByInitiator)
	}
	if completedApprovals != len(decisions) {
		t.Errorf("%d approval WorkItems completed, want one per recorded decision (%d)", completedApprovals, len(decisions))
	}
}
