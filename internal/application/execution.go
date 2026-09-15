package application

// ComposeExecutionAuthority is the P1B execution-authority half of the
// composition root. It moved here from cmd/hcmnext unchanged: the command now
// only decides that -execution-authority was asked for, and this package
// decides what that gate is made of.

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
)

// ComposeExecutionAuthority builds the P1B execution-authority wiring
// -execution-authority=true asks for: the caller-driven promotion execution
// driver (internal/platform/execution.NewPromotionExecution) over pool, its
// governed terminal write (internal/workflow/execute/effects.LedgerTerminalWriter,
// never a second implementation of that write), and the exact tenant-key-to-
// uuid derivation the composed pgstore.Store's own tenant table uses. It
// fills cellConfig's execution-shaped fields in place; every other field
// cellConfig already carries is untouched. evidence is the cell's own sink,
// handed to the driver so its APPROVAL_COMPLETED/TASK_SUBMITTED/
// TERMINAL_WRITTEN entries land beside the cell's gateway and gate evidence.
func ComposeExecutionAuthority(cellConfig *app.CellConfig, pool *pgxadapter.Pool, evidence *app.MemoryEvidenceSink, cfg ServeConfig) error {
	if cellConfig == nil {
		return fmt.Errorf("application: the execution authority needs a cell configuration")
	}
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		return fmt.Errorf("build the ledger event digest registry: %w", err)
	}
	terminal := &effects.LedgerTerminalWriter{
		Appender:       ledgerport.NewAppender(registry),
		ProjectionName: "workflow.promotion_outcome",
		SourceRef:      "cmd/hcmnext:execution-authority",
	}
	var startRetryFor func(context.Context, execute.StartRetryIdentity) (*transactioncommit.RetryOptions, error)
	if cfg.ExecutionRetry {
		now := func() time.Time { return time.Now().UTC() }
		if cellConfig.Now != nil {
			now = cellConfig.Now
		}
		startRetryFor = composeExecutionRetryFor(pool, cfg, now)
	}
	execution, err := platformexecution.NewPromotionExecution(platformexecution.PromotionExecutionConfig{
		DB:                         pool,
		StartRetryFor:              startRetryFor,
		Terminal:                   terminal,
		Plan:                       platformexecution.PromotionPlan(cfg.WorkflowPlan),
		ApproverPrincipalID:        cfg.ExecutionApprover,
		ManagerApproverPrincipalID: cfg.ExecutionManagerApprover,
		FinancePartnerPrincipalID:  cfg.ExecutionFinancePartner,
		AuthorityDigest:            cfg.ExecutionAuthorityDigest,
		RequiredRole:               cfg.ExecutionAuthorityRole,
		Clock:                      cellConfig.Now,
		Telemetry:                  cellConfig.Telemetry,
		Evidence:                   evidence,
		TimerDataset:               cfg.TimerDataset(),
		// WF-COMP-006 / WF-RUN-035: published versions, their approvals and
		// quarantine survive restart; serve never self-approves in memory.
		Versions: workflowversionstore.Store{DB: pool},
	})
	if err != nil {
		return fmt.Errorf("build the promotion execution driver: %w", err)
	}
	cellConfig.Executor = execution.Executor
	cellConfig.ExecutionAuthority = execution.Authority
	cellConfig.ExecutionResolver = execution.Resolver
	cellConfig.ExecutionVersions = execution.Versions
	cellConfig.ExecutionCellID = cfg.CellID
	cellConfig.TenantUUID = tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)
	// The Promotion journey engine (UX-009) claims and completes the routed
	// approval WorkItem and reads the instance back through the same pool the
	// driver runs on, acting as the approver this composition routes to.
	cellConfig.ExecutionDB = pool
	cellConfig.ExecutionApprover = cfg.ExecutionApprover
	return nil
}

// tenantKeyMapper adapts the composed store's own tenant-row derivation onto
// the tenant-key type the cell speaks. Every place this root needs that
// mapping - the execution driver above and the operator surface's
// workflow-instance reader in serve.go - builds it the same way from the same
// store function, so the driver, the reader and the store cannot disagree
// about which row a tenant key names.
//
// It is generic so that the row-key type is never written down here. That
// type belongs to the store adapter; a composition root that named it would
// be importing the store's identifier library
// (definitions/architecture/dependency-roles.yaml confines that library to
// the kernel, intent, data, ledger and command roots) in order to describe a
// value it only ever forwards.
func tenantKeyMapper[Key ~string, Row any](derive func(string) Row) func(Key) Row {
	return func(tenant Key) Row { return derive(string(tenant)) }
}
