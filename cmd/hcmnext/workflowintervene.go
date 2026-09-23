// workflowintervene.go implements "hcmnext workflow-intervene": the
// operator's governed trigger for every executable workflow intervention
// kind (REV-009-02).
//
//	hcmnext workflow-intervene <kind> -tenant <uuid> -instance <uuid> -version <n> \
//	    [-node <id> [-attempt <n>] [-route <r>] [-target <node>] \
//	     [-replacement <uuid>] [-observation EFFECT_APPLIED|EFFECT_NOT_APPLIED]] \
//	    -evidence <csv> -reason <ref> -operator <id> [-idempotency-key <k>]
//
// kind is one of pause|resume|cancel|retry|skip|satisfy|override|rewind|
// supersede|reconcile. compensate names a kind with no composed runner and
// is refused. The command composes the same governed cell the served
// transport uses -- the durable operator journal, the trust-store JIT
// authority, the promotion plan set and a preflight-simulating controller --
// over the database named by -database-url (env HCMNEXT_DATABASE_URL), and
// drives the kind through the controller's transport-shaped Handle endpoint,
// so the served path covers all ten kinds on one endpoint. It binds no
// listener. Exit 0 means the call completed and names its governed outcome
// (APPLIED, DENIED, ...); exit 2 is a usage error, exit 1 a store or
// composition failure.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/operatorjournal"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// openInterventionCell opens the database the intervention operator command
// runs against. It is a variable so tests drive the same served path against
// embedded PostgreSQL without a network listener.
var openInterventionCell = func(ctx context.Context, url string) (dbport.Beginner, func(), error) {
	pool, err := pgxadapter.NewPool(ctx, url, nil)
	if err != nil {
		return nil, nil, err
	}
	return pool, pool.Close, nil
}

// interveneKinds maps the CLI kind token to the operator gateway kind. Every
// executable intervention kind is present; compensate has no composed
// runner, so it is refused as a usage error rather than recorded.
func interveneKinds() map[string]operator.Kind {
	return map[string]operator.Kind{
		"pause": operator.KindWorkflowPause, "resume": operator.KindWorkflowResume,
		"cancel": operator.KindWorkflowCancel, "retry": operator.KindWorkflowRetryNode,
		"skip": operator.KindWorkflowSkip, "satisfy": operator.KindWorkflowSatisfy,
		"override": operator.KindWorkflowOverride, "rewind": operator.KindWorkflowRewind,
		"supersede": operator.KindWorkflowSupersede, "reconcile": operator.KindWorkflowReconcile,
	}
}

const workflowInterveneUsage = "usage: hcmnext workflow-intervene <pause|resume|cancel|retry|skip|satisfy|override|rewind|supersede|reconcile> -tenant <uuid> -instance <uuid> -version <n> [-node <id>] [-attempt <n>] [-route <r>] [-target <node>] [-replacement <uuid>] [-observation <EFFECT_APPLIED|EFFECT_NOT_APPLIED>] -evidence <csv> -reason <ref> -operator <id> [-idempotency-key <k>] [-database-url <url>]"

func runWorkflowIntervene(args []string, stdout, stderr io.Writer, now func() time.Time, open func(context.Context, string) (dbport.Beginner, func(), error)) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, workflowInterveneUsage)
		return 2
	}
	kind, ok := interveneKinds()[strings.ToLower(args[0])]
	if !ok {
		fmt.Fprintf(stderr, "hcmnext workflow-intervene: unknown kind %q; %s\n", args[0], workflowInterveneUsage)
		return 2
	}
	fs := flag.NewFlagSet("workflow-intervene "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	databaseURL := fs.String("database-url", os.Getenv(EnvDatabaseURL), "PostgreSQL URL (env "+EnvDatabaseURL+")")
	tenantFlag := fs.String("tenant", "", "tenant id (required)")
	instanceFlag := fs.String("instance", "", "instance id (required)")
	versionFlag := fs.Int64("version", 0, "expected instance version, at least 1 (required)")
	nodeFlag := fs.String("node", "", "node the intervention acts on (retry, skip, satisfy, override, reconcile)")
	attemptFlag := fs.Uint("attempt", 0, "failed attempt the retry re-runs (retry)")
	routeFlag := fs.String("route", "", "declared route the intervention takes (satisfy, override)")
	targetFlag := fs.String("target", "", "earlier node control returns to (rewind)")
	replacementFlag := fs.String("replacement", "", "replacement instance id (supersede)")
	observationFlag := fs.String("observation", "", "what the reconcile observed: EFFECT_APPLIED or EFFECT_NOT_APPLIED (reconcile)")
	evidenceFlag := fs.String("evidence", "", "comma-separated evidence references, at least one (required)")
	reasonFlag := fs.String("reason", "", "reason reference (required)")
	operatorFlag := fs.String("operator", "", "requesting operator principal (required)")
	keyFlag := fs.String("idempotency-key", "", "idempotency key (default intervene-<kind>-<instance>-<version>)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if strings.TrimSpace(*databaseURL) == "" {
		fmt.Fprintf(stderr, "hcmnext workflow-intervene: -database-url or %s is required\n", EnvDatabaseURL)
		return 2
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(*tenantFlag))
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-intervene: invalid -tenant: %v\n", err)
		return 2
	}
	instanceID, err := uuid.Parse(strings.TrimSpace(*instanceFlag))
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-intervene: invalid -instance: %v\n", err)
		return 2
	}
	if *versionFlag < 1 {
		fmt.Fprintln(stderr, "hcmnext workflow-intervene: -version must be at least 1")
		return 2
	}
	if strings.TrimSpace(*reasonFlag) == "" || strings.TrimSpace(*operatorFlag) == "" {
		fmt.Fprintln(stderr, "hcmnext workflow-intervene: -reason and -operator are required")
		return 2
	}
	key := strings.TrimSpace(*keyFlag)
	if key == "" {
		key = fmt.Sprintf("intervene-%s-%s-%d", strings.ToLower(args[0]), instanceID.String(), *versionFlag)
	}
	ctx := context.Background()
	db, closeDB, err := open(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-intervene: %v\n", err)
		return 1
	}
	defer closeDB()
	res, err := runIntervene(ctx, db, kind, tenantID, instanceID, *versionFlag, interveneParams{
		NodeID: *nodeFlag, Attempt: *attemptFlag, Route: *routeFlag, TargetNodeID: *targetFlag,
		Replacement: *replacementFlag, Observation: *observationFlag, Evidence: *evidenceFlag,
		Reason: *reasonFlag, Operator: *operatorFlag, Key: key,
	}, now)
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-intervene: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "outcome: %s code=%s instance=%s status=%s version=%d\n",
		res.Outcome, res.Code, res.InstanceID, res.InstanceStatus, res.InstanceVersion)
	if res.DecisionID != "" {
		fmt.Fprintf(stdout, "decision: %s digest=%s\n", res.DecisionID, res.DecisionDigest)
	}
	if res.Cause != "" {
		fmt.Fprintf(stdout, "cause: %s\n", res.Cause)
	}
	if res.ReceiptDigest != "" {
		fmt.Fprintf(stdout, "receipt: %s\n", res.ReceiptDigest)
	}
	return 0
}

// interveneParams are the CLI's typed intervention flags.
type interveneParams struct {
	NodeID       string
	Attempt      uint
	Route        string
	TargetNodeID string
	Replacement  string
	Observation  string
	Evidence     string
	Reason       string
	Operator     string
	Key          string
}

// runIntervene composes the served workflow-control cell over db -- the
// durable operator journal, the trust-store JIT authority, the promotion
// plan set and a preflight-simulating controller, exactly the composition
// internal/intent/app builds for the served transport -- resolves the
// tenant's storage key from the database, and drives the kind through the
// controller's transport-shaped Handle endpoint.
func runIntervene(ctx context.Context, db dbport.Beginner, kind operator.Kind, tenantID, instanceID uuid.UUID, version int64, p interveneParams, now func() time.Time) (workflowcontrol.Response, error) {
	tenantKey, err := lookupTenantKey(ctx, db, tenantID)
	if err != nil {
		return workflowcontrol.Response{}, err
	}
	ids := func(t values.TenantId) (uuid.UUID, error) {
		if t != tenantKey {
			return uuid.Nil, fmt.Errorf("%w: tenant %s has no storage identity", workflowcontrol.ErrInvalidCommand, t)
		}
		return tenantID, nil
	}
	plan, err := promotionexec.Compile()
	if err != nil {
		return workflowcontrol.Response{}, fmt.Errorf("compile promotion plan: %w", err)
	}
	simulation, err := promotionexec.CompileSimulation()
	if err != nil {
		return workflowcontrol.Response{}, fmt.Errorf("compile promotion simulation plan: %w", err)
	}
	frozen, err := promotionexec.CompileV1_0()
	if err != nil {
		return workflowcontrol.Response{}, fmt.Errorf("compile the frozen promotion plan: %w", err)
	}
	frozenSimulation, err := promotionexec.CompileSimulationV1_0()
	if err != nil {
		return workflowcontrol.Response{}, fmt.Errorf("compile the frozen promotion simulation plan: %w", err)
	}
	journal := &operatorjournal.Journal{DB: db, TenantIDs: operatorjournal.TenantIDs(ids)}
	authority := workflowcontrol.JITAuthority{Grants: truststore.New(db), TenantIDs: ids, Clock: now}
	ctrl, err := workflowcontrol.New(db, journal, workflowcontrol.NewPlanSet(plan, simulation, frozen, frozenSimulation), authority, now,
		workflowcontrol.WithPreflightSimulation())
	if err != nil {
		return workflowcontrol.Response{}, fmt.Errorf("compose workflow control: %w", err)
	}
	var evidence []string
	for _, ref := range strings.Split(p.Evidence, ",") {
		if ref = strings.TrimSpace(ref); ref != "" {
			evidence = append(evidence, ref)
		}
	}
	return ctrl.Handle(ctx, ids, workflowcontrol.Request{Kind: kind, Tenant: tenantKey, InstanceID: instanceID.String(),
		ExpectedVersion: uint64(version), NodeID: p.NodeID, ExpectedAttempt: uint32(p.Attempt),
		IdempotencyKey: p.Key, ReasonRef: p.Reason, Operator: p.Operator,
		Route: p.Route, TargetNodeID: p.TargetNodeID, Replacement: p.Replacement,
		Observation: p.Observation, EvidenceRefs: evidence})
}

// lookupTenantKey resolves the tenant's storage key from the database, so
// the controller's tenant mapping names the real key rather than an
// invented one.
func lookupTenantKey(ctx context.Context, db dbport.Beginner, tenantID uuid.UUID) (values.TenantId, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return "", fmt.Errorf("bind tenant %s: %w", tenantID, err)
	}
	var key string
	if err := tx.QueryRow(ctx, `SELECT tenant_key FROM tenant WHERE tenant_id = $1`, tenantID).Scan(&key); err != nil {
		return "", fmt.Errorf("load tenant %s: %w", tenantID, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		return "", fmt.Errorf("rollback tenant lookup: %w", err)
	}
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("tenant %s carries no key", tenantID)
	}
	return values.TenantId(key), nil
}
