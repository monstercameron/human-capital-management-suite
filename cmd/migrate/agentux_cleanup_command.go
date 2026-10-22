package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// agentDemoCleanupCommand archives the old verification questions and failures
// out of the demo workspace's #general. It is a separate command on purpose: the
// preparation (agent-demo) never deletes anything, and the owner decides when
// the old posts go. Run it once, after the preparation, when the channel should
// read as the demo history only:
//
//	migrate agent-demo-cleanup -profile local-dev -tenant ironridge-demo
const agentDemoCleanupCommand = "agent-demo-cleanup"

func agentDemoCleanupSpec(rest []string) bootstrap.Spec {
	return bootstrap.Spec{
		Role: bootstrap.RoleMigrate,
		Args: rest,
		ConfigFields: []bootstrap.Field{
			{Name: fieldAgentDemoProfile, Usage: "required local development profile"},
			{Name: fieldTenant, Usage: "tenant slug (ironridge-demo)"},
			{Name: "database-url", Env: EnvDatabaseURL, Usage: "core PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldChatDatabaseURL, Env: EnvChatDatabaseURL, Usage: "chat PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
		},
		Validate: func(values *bootstrap.Values) error {
			return application.ValidateLocalAgentDemoCleanupConfig(agentDemoCleanupConfig(values))
		},
		Build: func(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			config := agentDemoCleanupConfig(deps.Values)
			work := bootstrap.Workload{Name: "migrate-agent-demo-cleanup", Run: func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, migrationTimeout)
				defer cancel()
				return runAgentDemoCleanupCommand(ctx, config, os.Stdout)
			}}
			return bootstrap.Runtime{Workloads: []bootstrap.Workload{work}}, nil
		},
	}
}

func agentDemoCleanupConfig(values *bootstrap.Values) application.LocalAgentDemoCleanupConfig {
	return application.LocalAgentDemoCleanupConfig{Profile: values.String(fieldAgentDemoProfile), Tenant: values.String(fieldTenant), DatabaseURL: values.String("database-url"), ChatDatabaseURL: values.String(fieldChatDatabaseURL), Now: time.Now}
}

func runAgentDemoCleanupCommand(ctx context.Context, config application.LocalAgentDemoCleanupConfig, out io.Writer) error {
	removed, err := application.RunLocalAgentDemoCleanup(ctx, config)
	fmt.Fprintln(out, formatAgentDemoCleanup(removed))
	return err
}

func formatAgentDemoCleanup(removed []string) string {
	if len(removed) == 0 {
		return "Nothing to archive: #general holds no old verification questions."
	}
	return fmt.Sprintf("Archived %d old verification posts out of #general (questions and their Policy Helper replies).", len(removed))
}
