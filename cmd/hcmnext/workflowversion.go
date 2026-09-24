// workflowversion.go implements "hcmnext workflow-version": the operator's
// governed control over the durable compiled workflow version registry
// (internal/data/workflowversionstore, WF-COMP-006, WF-RUN-009).
//
//	hcmnext workflow-version list -workflow <id>
//	hcmnext workflow-version fixtures -digest <sha256:...> [-out report.json] [-runner ...]
//	hcmnext workflow-version approve -digest <sha256:...> -approved-by ... -authority ... \
//	    -reason ... -fixture-report report.json
//	hcmnext workflow-version activate -digest <sha256:...> [-supersede]
//	hcmnext workflow-version bootstrap-dev
//	hcmnext workflow-version quarantine -digest <sha256:...> -reason ... -evidence ... \
//	    -declared-by ... -approved-by ... -authority ... -policy PAUSE|CONTINUE|BLOCK
//	hcmnext workflow-version lift -digest <sha256:...> -reviewed-by ... \
//	    -validation-evidence ... -reason ... -authority ... -tests-passed
//
// Serve publishes the shipped workflow versions as DRAFT and never approves or
// activates them. A release is three governed steps: fixtures runs the
// version's declared conformance fixtures in-process and writes the sealed
// report; approve re-runs every fixture and records the approval with the
// report, refusing a failed, missing, digest-mismatched or unreproduced report
// and the publisher approving itself; activate activates on that approval.
// bootstrap-dev performs all three for every shipped DRAFT under the distinct
// development release approver, for a local development database only.
//
// A quarantine takes effect for new starts the moment it commits and for live
// instances at their next advancement; a lift needs a reviewer who did not
// declare the quarantine. The command is a one-shot action over the database
// named by -database-url (env HCMNEXT_DATABASE_URL): it binds no listener.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// versionRegistry is the slice of the durable registry the command drives.
type versionRegistry interface {
	platformexecution.VersionRegistry
	Quarantine(ctx context.Context, d workflowversionstore.QuarantineDeclaration) (version.CompiledVersion, error)
	LiftQuarantine(ctx context.Context, l workflowversionstore.QuarantineLift) (version.CompiledVersion, error)
}

// openVersionRegistry opens the registry over a pool on url; the returned
// close releases it.
type openVersionRegistry func(ctx context.Context, url string) (versionRegistry, func(), error)

func openPostgresVersionRegistry(ctx context.Context, url string) (versionRegistry, func(), error) {
	pool, err := pgxadapter.NewPool(ctx, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open the database: %w", err)
	}
	return workflowversionstore.Store{DB: pool}, pool.Close, nil
}

const workflowVersionUsage = "usage: hcmnext workflow-version [list|fixtures|approve|activate|bootstrap-dev|quarantine|lift|migrate preview|migrate execute|migrate-preview|migrate-execute] [flags]"

func runWorkflowVersion(args []string, stdout, stderr io.Writer, now func() time.Time, open openVersionRegistry) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, workflowVersionUsage)
		return 2
	}
	action := args[0]
	actionArgs := args[1:]
	// Keep the original hyphenated spellings as aliases while exposing the
	// operator-facing nested command named by the workflow migration contract.
	if action == "migrate" && len(args) > 1 {
		switch args[1] {
		case "preview":
			action = "migrate-preview"
			actionArgs = args[2:]
		case "execute":
			action = "migrate-execute"
			actionArgs = args[2:]
		}
	}
	fs := flag.NewFlagSet("workflow-version "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	databaseURL := fs.String("database-url", os.Getenv(EnvDatabaseURL), "PostgreSQL URL (env "+EnvDatabaseURL+")")
	workflowID := fs.String("workflow", "", "workflow id to list (list)")
	digest := fs.String("digest", "", "compiled-plan digest of the version (fixtures, approve, activate, quarantine, lift)")
	reason := fs.String("reason", "", "why the version is approved, quarantined or returned to service")
	evidence := fs.String("evidence", "", "incident evidence reference the quarantine rests on (quarantine)")
	declaredBy := fs.String("declared-by", "", "principal declaring the quarantine (quarantine)")
	approvedBy := fs.String("approved-by", "", "approving principal; never the publisher (approve), must differ from -declared-by (quarantine)")
	policy := fs.String("policy", "", "live-instance disposition: PAUSE, CONTINUE or BLOCK (quarantine)")
	reviewedBy := fs.String("reviewed-by", "", "reviewer returning the version to service; must not have declared the quarantine (lift)")
	validation := fs.String("validation-evidence", "", "validation evidence justifying the release (lift)")
	authority := fs.String("authority", "", "authority the action is taken under (approve, quarantine, lift)")
	testsPassed := fs.Bool("tests-passed", false, "the version's validation suite passed (lift)")
	reportPath := fs.String("fixture-report", "", "path of the fixture report written by fixtures (approve)")
	out := fs.String("out", "", "file to write the fixture report to; empty writes it to stdout (fixtures)")
	runner := fs.String("runner", "cmd/hcmnext:workflow-version-fixtures", "who ran the fixtures (fixtures)")
	supersede := fs.Bool("supersede", false, "quarantine a different version of the workflow that is already active (activate)")
	tenantFlag := fs.String("tenant", "", "tenant id of the paused instance (migrate preview|execute)")
	instanceFlag := fs.String("instance", "", "instance id of the paused instance (migrate preview|execute)")
	sourceDigest := fs.String("source-digest", "", "compiled-plan digest the instance currently pins (migrate preview|execute)")
	targetDigest := fs.String("target-digest", "", "compiled-plan digest to preview or migrate onto (migrate preview|execute)")
	migratedBy := fs.String("migrated-by", "", "principal executing the migration; must differ from -approved-by (migrate execute)")
	if err := fs.Parse(actionArgs); err != nil {
		return 2
	}
	if strings.TrimSpace(*databaseURL) == "" {
		fmt.Fprintf(stderr, "hcmnext workflow-version: -database-url or %s is required\n", EnvDatabaseURL)
		return 2
	}
	switch action {
	case "list", "fixtures", "approve", "activate", "bootstrap-dev", "quarantine", "lift", "migrate-preview", "migrate-execute":
	default:
		fmt.Fprintf(stderr, "hcmnext workflow-version: unknown action %q; %s\n", action, workflowVersionUsage)
		return 2
	}
	ctx := context.Background()
	if action == "migrate-preview" || action == "migrate-execute" {
		return runWorkflowMigrate(ctx, action, *databaseURL, *tenantFlag, *instanceFlag, *sourceDigest, *targetDigest, *approvedBy, *reason, *migratedBy, stdout, stderr, now)
	}
	registry, closeRegistry, err := open(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-version: %v\n", err)
		return 1
	}
	defer closeRegistry()

	var versions []version.CompiledVersion
	switch action {
	case "list":
		if strings.TrimSpace(*workflowID) == "" {
			fmt.Fprintln(stderr, "hcmnext workflow-version list: -workflow is required")
			return 2
		}
		versions, err = registry.List(*workflowID)
	case "fixtures":
		return runFixtures(registry, *digest, *runner, *out, stdout, stderr, now)
	case "approve":
		var report releasefixture.Report
		report, err = readFixtureReport(*reportPath)
		if err == nil {
			_, err = platformexecution.ApproveRelease(ctx, registry, platformexecution.ShippedFixtures(), platformexecution.ReleaseApproval{
				CompiledPlanDigest: *digest, ApprovedBy: *approvedBy, Authority: *authority, Reason: *reason,
				Report: report, ApprovedAt: now().UTC(),
			})
		}
		if err == nil {
			var approved version.CompiledVersion
			approved, _, err = registry.GetByDigest(*digest)
			versions = []version.CompiledVersion{approved}
		}
	case "activate":
		var activated version.CompiledVersion
		activated, err = registry.ActivateApproved(ctx, *digest, *supersede)
		versions = []version.CompiledVersion{activated}
	case "bootstrap-dev":
		versions, err = platformexecution.BootstrapDevVersions(ctx, registry, now().UTC())
	case "quarantine":
		var quarantined version.CompiledVersion
		quarantined, err = registry.Quarantine(ctx, workflowversionstore.QuarantineDeclaration{
			DeclarationID: uuid.New(), CompiledPlanDigest: *digest, Reason: *reason, EvidenceRef: *evidence,
			DeclaredBy: *declaredBy, ApprovedBy: *approvedBy, Authority: *authority,
			LivePolicy: workflowversionstore.LivePolicy(strings.ToUpper(strings.TrimSpace(*policy))), RecordedAt: now().UTC(),
		})
		versions = []version.CompiledVersion{quarantined}
	case "lift":
		var lifted version.CompiledVersion
		lifted, err = registry.LiftQuarantine(ctx, workflowversionstore.QuarantineLift{
			DeclarationID: uuid.New(), CompiledPlanDigest: *digest, ReviewedBy: *reviewedBy,
			ValidationEvidenceRef: *validation, Reason: *reason, Authority: *authority,
			TestsPassed: *testsPassed, RecordedAt: now().UTC(),
		})
		versions = []version.CompiledVersion{lifted}
	}
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-version %s: %v\n", action, err)
		if errors.Is(err, workflowversionstore.ErrInvalid) || errors.Is(err, platformexecution.ErrReleaseApproval) {
			return 2
		}
		return 1
	}
	for _, v := range versions {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", v.WorkflowID, v.SemanticVersion, v.Status, v.CompiledPlanDigest)
	}
	return 0
}

// runFixtures runs the declared fixtures of the version at digest and writes
// the sealed report. It exits 1 when any fixture failed; the report is written
// either way, so the failure detail is kept.
func runFixtures(registry versionRegistry, digest, runner, out string, stdout, stderr io.Writer, now func() time.Time) int {
	v, found, err := registry.GetByDigest(digest)
	if err == nil && !found {
		err = fmt.Errorf("no published version carries digest %q", digest)
	}
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-version fixtures: %v\n", err)
		return 1
	}
	report := releasefixture.Run(platformexecution.ShippedFixtures(), v, runner, now().UTC())
	encoded, err := report.Encode()
	if err == nil {
		if out == "" {
			_, err = stdout.Write(encoded)
		} else {
			err = os.WriteFile(out, encoded, 0o600)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-version fixtures: write the report: %v\n", err)
		return 1
	}
	if err := releasefixture.Verify(report, v); err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-version fixtures: %v\n", err)
		return 1
	}
	return 0
}

func readFixtureReport(path string) (releasefixture.Report, error) {
	if strings.TrimSpace(path) == "" {
		return releasefixture.Report{}, fmt.Errorf("%w: -fixture-report is required", platformexecution.ErrReleaseApproval)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return releasefixture.Report{}, fmt.Errorf("read the fixture report: %w", err)
	}
	return releasefixture.Decode(b)
}
