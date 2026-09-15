// workflowversion.go implements "hcmnext workflow-version": the operator's
// governed control over the durable compiled workflow version registry
// (internal/data/workflowversionstore, WF-RUN-009).
//
//	hcmnext workflow-version list -workflow <id>
//	hcmnext workflow-version quarantine -digest <sha256:...> -reason ... -evidence ... \
//	    -declared-by ... -approved-by ... -authority ... -policy PAUSE|CONTINUE|BLOCK
//	hcmnext workflow-version lift -digest <sha256:...> -reviewed-by ... \
//	    -validation-evidence ... -reason ... -authority ... -tests-passed
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
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// versionRegistry is the slice of the durable registry the command drives.
type versionRegistry interface {
	List(workflowID string) ([]version.CompiledVersion, error)
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

func runWorkflowVersion(args []string, stdout, stderr io.Writer, now func() time.Time, open openVersionRegistry) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: hcmnext workflow-version [list|quarantine|lift] [flags]")
		return 2
	}
	action := args[0]
	fs := flag.NewFlagSet("workflow-version "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	databaseURL := fs.String("database-url", os.Getenv(EnvDatabaseURL), "PostgreSQL URL (env "+EnvDatabaseURL+")")
	workflowID := fs.String("workflow", "", "workflow id to list (list)")
	digest := fs.String("digest", "", "compiled-plan digest of the version (quarantine, lift)")
	reason := fs.String("reason", "", "why the version is quarantined or returned to service")
	evidence := fs.String("evidence", "", "incident evidence reference the quarantine rests on (quarantine)")
	declaredBy := fs.String("declared-by", "", "principal declaring the quarantine (quarantine)")
	approvedBy := fs.String("approved-by", "", "second principal approving the quarantine; must differ from -declared-by (quarantine)")
	policy := fs.String("policy", "", "live-instance disposition: PAUSE, CONTINUE or BLOCK (quarantine)")
	reviewedBy := fs.String("reviewed-by", "", "reviewer returning the version to service; must not have declared the quarantine (lift)")
	validation := fs.String("validation-evidence", "", "validation evidence justifying the release (lift)")
	authority := fs.String("authority", "", "authority the action is taken under (quarantine, lift)")
	testsPassed := fs.Bool("tests-passed", false, "the version's validation suite passed (lift)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if strings.TrimSpace(*databaseURL) == "" {
		fmt.Fprintf(stderr, "hcmnext workflow-version: -database-url or %s is required\n", EnvDatabaseURL)
		return 2
	}
	ctx := context.Background()
	registry, closeRegistry, err := open(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-version: %v\n", err)
		return 1
	}
	defer closeRegistry()

	var out version.CompiledVersion
	switch action {
	case "list":
		if strings.TrimSpace(*workflowID) == "" {
			fmt.Fprintln(stderr, "hcmnext workflow-version list: -workflow is required")
			return 2
		}
		versions, err := registry.List(*workflowID)
		if err != nil {
			fmt.Fprintf(stderr, "hcmnext workflow-version list: %v\n", err)
			return 1
		}
		for _, v := range versions {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", v.WorkflowID, v.SemanticVersion, v.Status, v.CompiledPlanDigest)
		}
		return 0
	case "quarantine":
		out, err = registry.Quarantine(ctx, workflowversionstore.QuarantineDeclaration{
			DeclarationID: uuid.New(), CompiledPlanDigest: *digest, Reason: *reason, EvidenceRef: *evidence,
			DeclaredBy: *declaredBy, ApprovedBy: *approvedBy, Authority: *authority,
			LivePolicy: workflowversionstore.LivePolicy(strings.ToUpper(strings.TrimSpace(*policy))), RecordedAt: now().UTC(),
		})
	case "lift":
		out, err = registry.LiftQuarantine(ctx, workflowversionstore.QuarantineLift{
			DeclarationID: uuid.New(), CompiledPlanDigest: *digest, ReviewedBy: *reviewedBy,
			ValidationEvidenceRef: *validation, Reason: *reason, Authority: *authority,
			TestsPassed: *testsPassed, RecordedAt: now().UTC(),
		})
	default:
		fmt.Fprintf(stderr, "hcmnext workflow-version: unknown action %q; usage: hcmnext workflow-version [list|quarantine|lift]\n", action)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-version %s: %v\n", action, err)
		if errors.Is(err, workflowversionstore.ErrInvalid) {
			return 2
		}
		return 1
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", out.WorkflowID, out.SemanticVersion, out.Status, out.CompiledPlanDigest)
	return 0
}
