// Package opcmd is the REV-017-02 operator-invokable entry point for the
// tested recovery packages (RECOVERY-001..004, internal/operations/
// recovery). RECOVERY-001 through RECOVERY-004 proved restore, pilot-restore
// and game-day mechanics only inside their own package tests; nothing
// outside go test could invoke DrillPilotRestore or ExecuteGameDay. Main is
// the composition root an operator binary calls with os.Args (the same
// pattern as internal/transport/admin/hcmctl.Main): it parses one of three
// subcommands, calls RestoreTenant/DrillPilotRestore against an isolated
// target or ExecuteGameDay against a named scenario, and prints the same
// RPO/RTO/restored-count/tombstone evidence the recovery package tests
// assert.
//
// Safety is structural, not advisory: restore paths force
// Isolated=true/ProductionEffects=false on every request (there is no flag
// to express a production restore), and drill/game-day destinations naming
// production are refused before the recovery library is reached.
//
// Exit codes: 0 means a report was produced (READY, FENCED and FAILED_GATE
// are drill outcomes, so they still exit zero with the receipt as
// evidence); 1 means the recovery library refused the envelope; 2 means a
// flag, input or usage error.
package opcmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/recovery"
)

// Main parses args, runs one recovery subcommand and prints its evidence to
// stdout. It never calls os.Exit; the caller maps the return to a process
// exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "opcmd: a subcommand is required")
		fmt.Fprintln(stderr, usage())
		return 2
	}
	name, rest := args[0], args[1:]
	var err error
	switch name {
	case "matrix":
		err = runMatrix(rest, stdout)
	case "restore-tenant":
		err = runRestoreTenant(rest, stdout)
	case "restore-drill":
		err = runRestoreDrill(rest, stdout)
	case "gameday":
		err = runGameDay(rest, stdout)
	default:
		fmt.Fprintf(stderr, "opcmd: unknown subcommand %q\n", name)
		fmt.Fprintln(stderr, usage())
		return 2
	}
	if err == nil {
		return 0
	}
	if uerr, ok := err.(*usageError); ok {
		fmt.Fprintln(stderr, "opcmd:", uerr.Error())
		fmt.Fprintln(stderr, usage())
		return 2
	}
	fmt.Fprintln(stderr, "opcmd: error:", err.Error())
	return 1
}

func usage() string {
	return "usage: opcmd <matrix|restore-tenant|restore-drill|gameday> [flags]"
}

// runMatrix prints the validated recovery policy without contacting a store,
// reading key material or initiating backup/restore work.
func runMatrix(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return usagef("matrix does not accept arguments")
	}
	matrix := recovery.DefaultMatrix()
	if err := matrix.Validate(); err != nil {
		return err
	}
	output := struct {
		Version   int                 `json:"version"`
		Contracts []recovery.Contract `json:"contracts"`
	}{Version: recovery.Version(), Contracts: matrix.Ordered()}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// usageError marks flag/input/usage failures (exit 2) as distinct from
// recovery-library refusals (exit 1).
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) *usageError {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseTime(value, name string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, usagef("flag --%s must be RFC3339: %q", name, value)
	}
	return t, nil
}

func parseDuration(value, name string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, usagef("flag --%s must be a Go duration: %q", name, value)
	}
	return d, nil
}

func newSubFlagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}

// rowJSON is the operator JSON shape for one restored row.
type rowJSON struct {
	ID     string `json:"id"`
	Tenant string `json:"tenant"`
	Plane  string `json:"plane"`
	Digest string `json:"digest"`
}

func parseRows(raw string) ([]recovery.RestoredRow, error) {
	var rows []rowJSON
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&rows); err != nil {
		return nil, usagef("flag --rows must be a JSON array: %v", err)
	}
	if len(rows) == 0 {
		return nil, usagef("flag --rows must name at least one restored row")
	}
	out := make([]recovery.RestoredRow, 0, len(rows))
	for i, r := range rows {
		if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Tenant) == "" ||
			strings.TrimSpace(r.Plane) == "" || strings.TrimSpace(r.Digest) == "" {
			return nil, usagef("row %d needs id, tenant, plane and digest", i)
		}
		out = append(out, recovery.RestoredRow{ID: r.ID, TenantID: r.Tenant, Plane: r.Plane, Digest: r.Digest})
	}
	return out, nil
}

func require(value, name string) error {
	if strings.TrimSpace(value) == "" {
		return usagef("flag --%s is required", name)
	}
	return nil
}

func runRestoreTenant(args []string, stdout io.Writer) error {
	sub := newSubFlagSet("restore-tenant")
	tenant := sub.String("tenant", "", "tenant id (required)")
	recoveryPoint := sub.String("recovery-point", "", "recovery point, RFC3339 (required)")
	restoredAt := sub.String("restored-at", "", "restore completion, RFC3339 (required)")
	ledgerHead := sub.String("ledger-head", "", "ledger head reference (required)")
	migrationDigest := sub.String("migration-digest", "", "migration journal digest (required)")
	leaseEpoch := sub.Uint64("lease-epoch", 0, "runtime lease epoch, >0 (required)")
	rowsRaw := sub.String("rows", "", "restored rows as JSON array (required)")
	deletedIDs := sub.String("deleted-ids", "", "comma-separated tombstoned row ids")
	heldIDs := sub.String("held-ids", "", "comma-separated held row ids")
	conformAll := sub.Bool("conform-all", false, "attest the full acceptance set (unconforming restores stay FENCED)")
	if err := sub.Parse(args); err != nil {
		return usagef("parsing flags: %v", err)
	}
	for name, value := range map[string]string{
		"tenant": *tenant, "recovery-point": *recoveryPoint, "restored-at": *restoredAt,
		"ledger-head": *ledgerHead, "migration-digest": *migrationDigest, "rows": *rowsRaw,
	} {
		if err := require(value, name); err != nil {
			return err
		}
	}
	if *leaseEpoch == 0 {
		return usagef("flag --lease-epoch is required and must be positive")
	}
	point, err := parseTime(*recoveryPoint, "recovery-point")
	if err != nil {
		return err
	}
	restored, err := parseTime(*restoredAt, "restored-at")
	if err != nil {
		return err
	}
	rows, err := parseRows(*rowsRaw)
	if err != nil {
		return err
	}
	var conformance recovery.RestoreConformance
	if *conformAll {
		conformance = recovery.RestoreConformance{
			LedgerHeadsValid: true, ForeignKeysValid: true, RuntimeLeasesValid: true,
			HoldsApplied: true, DeletionsApplied: true, HashesValid: true,
		}
	}
	// Isolation is forced: this entry point cannot express a production
	// restore.
	receipt, err := recovery.RestoreTenant(recovery.TenantRestoreRequest{
		TenantID: *tenant, RecoveryPoint: point, RestoredAt: restored,
		Rows: rows, DeletedIDs: splitCSV(*deletedIDs), HeldIDs: splitCSV(*heldIDs),
		LedgerHead: *ledgerHead, MigrationJournalDigest: *migrationDigest,
		RuntimeLeaseEpoch: *leaseEpoch, Conformance: conformance,
		Isolated: true, ProductionEffects: false,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, recovery.ExplainRestore(receipt))
	fmt.Fprintf(stdout, "tenant=%s recovery_point=%s restored_at=%s ledger_head=%s migration_digest=%s lease_epoch=%d\n",
		receipt.TenantID,
		receipt.RecoveryPoint.UTC().Format(time.RFC3339),
		receipt.RestoredAt.UTC().Format(time.RFC3339),
		receipt.LedgerHead, receipt.MigrationJournalDigest, receipt.RuntimeLeaseEpoch)
	return nil
}

func runRestoreDrill(args []string, stdout io.Writer) error {
	sub := newSubFlagSet("restore-drill")
	drill := sub.String("drill", "", "drill id (required)")
	destination := sub.String("destination", "", "isolated recovery destination, recovery-* (required)")
	startedAt := sub.String("started-at", "", "drill start, RFC3339 (required)")
	tenantRaw := sub.String("tenant-request", "", "tenant restore envelope as JSON (required)")
	runtimeRaw := sub.String("runtime", "", "restored runtime inventory as JSON (required)")
	if err := sub.Parse(args); err != nil {
		return usagef("parsing flags: %v", err)
	}
	for name, value := range map[string]string{
		"drill": *drill, "destination": *destination, "started-at": *startedAt,
		"tenant-request": *tenantRaw, "runtime": *runtimeRaw,
	} {
		if err := require(value, name); err != nil {
			return err
		}
	}
	started, err := parseTime(*startedAt, "started-at")
	if err != nil {
		return err
	}
	var tenant recovery.TenantRestoreRequest
	if err := json.NewDecoder(strings.NewReader(*tenantRaw)).Decode(&tenant); err != nil {
		return usagef("flag --tenant-request must be a JSON tenant restore envelope: %v", err)
	}
	var runtime recovery.RestoredRuntimeState
	if err := json.NewDecoder(strings.NewReader(*runtimeRaw)).Decode(&runtime); err != nil {
		return usagef("flag --runtime must be a JSON runtime inventory: %v", err)
	}
	// Isolation is forced even when the envelope omits it: operator drills
	// cannot express a production restore.
	tenant.Isolated = true
	tenant.ProductionEffects = false
	report, err := recovery.DrillPilotRestore(recovery.PilotRestoreRequest{
		DrillID: *drill, Destination: *destination, StartedAt: started,
		Tenant: tenant, Runtime: runtime,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, report.Explain())
	fmt.Fprintf(stdout, "tombstones=%d holds=%d timers=%d signals=%d frontier_nodes=%d outbox_entries=%d idempotency_keys=%d\n",
		report.TombstonesApplied, report.HoldsApplied,
		report.RestoredCounts.Timers, report.RestoredCounts.Signals,
		report.RestoredCounts.FrontierNodes, report.RestoredCounts.OutboxEntries,
		report.RestoredCounts.IdempotencyKeys)
	for _, f := range report.Findings {
		fmt.Fprintf(stdout, "finding=%s %s\n", f.Code, f.Detail)
	}
	return nil
}

var gameDayAssets = map[string]recovery.LostAsset{
	"database": recovery.LostDatabase, "region": recovery.LostRegion,
	"ledger-shard": recovery.LostLedgerShard, "signing-key": recovery.LostSigningKey,
	"admin": recovery.LostAdmin, "identity-provider": recovery.LostIdP,
	"provider": recovery.LostProvider,
}

func runGameDay(args []string, stdout io.Writer) error {
	sub := newSubFlagSet("gameday")
	drill := sub.String("drill", "", "drill id (required)")
	asset := sub.String("asset", "", "lost asset: database|region|ledger-shard|signing-key|admin|identity-provider|provider (required)")
	owner := sub.String("owner", "", "decision owner (required)")
	degradedMode := sub.String("degraded-mode", "", "safe degraded mode (required)")
	budgetRPO := sub.String("budget-rpo", "", "RPO budget, Go duration (required)")
	budgetRTO := sub.String("budget-rto", "", "RTO budget, Go duration (required)")
	fence := sub.String("fence", "", "drill fence, recovery-* (required)")
	cutoverFence := sub.String("cutover-fence", "", "cutover fence, recovery-* (required)")
	failbackFence := sub.String("failback-fence", "", "failback fence, recovery-* (required)")
	observedRPO := sub.String("observed-rpo", "", "observed RPO, Go duration (required)")
	observedRTO := sub.String("observed-rto", "", "observed RTO, Go duration (required)")
	incident := sub.String("incident", "", "incident evidence ref (required)")
	advisory := sub.String("advisory", "", "advisory evidence ref (required)")
	repair := sub.String("repair", "", "repair evidence ref (required)")
	postReview := sub.String("post-review", "", "post-review evidence ref (required)")
	failedBack := sub.Bool("failed-back", false, "set once failback completed")
	if err := sub.Parse(args); err != nil {
		return usagef("parsing flags: %v", err)
	}
	flags := map[string]string{
		"drill": *drill, "asset": *asset, "owner": *owner, "degraded-mode": *degradedMode,
		"budget-rpo": *budgetRPO, "budget-rto": *budgetRTO, "fence": *fence,
		"cutover-fence": *cutoverFence, "failback-fence": *failbackFence,
		"observed-rpo": *observedRPO, "observed-rto": *observedRTO,
		"incident": *incident, "advisory": *advisory, "repair": *repair, "post-review": *postReview,
	}
	for name, value := range flags {
		if err := require(value, name); err != nil {
			return err
		}
	}
	lost, ok := gameDayAssets[strings.TrimSpace(*asset)]
	if !ok {
		return usagef("flag --asset must be one of database, region, ledger-shard, signing-key, admin, identity-provider, provider")
	}
	budgetP, err := parseDuration(*budgetRPO, "budget-rpo")
	if err != nil {
		return err
	}
	budgetT, err := parseDuration(*budgetRTO, "budget-rto")
	if err != nil {
		return err
	}
	obsP, err := parseDuration(*observedRPO, "observed-rpo")
	if err != nil {
		return err
	}
	obsT, err := parseDuration(*observedRTO, "observed-rto")
	if err != nil {
		return err
	}
	report, err := recovery.ExecuteGameDay(recovery.GameDayInput{
		Scenario: recovery.GameDayScenario{
			DrillID: strings.TrimSpace(*drill), Asset: lost, Owner: strings.TrimSpace(*owner),
			DegradedMode: strings.TrimSpace(*degradedMode),
			BudgetRPO:    budgetP, BudgetRTO: budgetT,
			Fence: strings.TrimSpace(*fence), CutoverFence: strings.TrimSpace(*cutoverFence),
			FailbackFence: strings.TrimSpace(*failbackFence),
		},
		ObservedRPO: obsP, ObservedRTO: obsT,
		Evidence: recovery.GameDayEvidence{
			Incident: strings.TrimSpace(*incident), Advisory: strings.TrimSpace(*advisory),
			Repair: strings.TrimSpace(*repair), PostReview: strings.TrimSpace(*postReview),
		},
		FailedBack: *failedBack,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "gameday drill=%s asset=%s status=%s observed_rpo=%s observed_rto=%s failed_back=%t\n",
		report.DrillID, report.Asset, report.Status, report.ObservedRPO, report.ObservedRTO, report.FailedBack)
	for _, f := range report.Findings {
		fmt.Fprintf(stdout, "finding=%s %s\n", f.Code, f.Detail)
	}
	fmt.Fprintf(stdout, "digest=%s\n", report.Digest)
	return nil
}
