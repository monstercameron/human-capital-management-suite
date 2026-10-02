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

const agentDemoCommand = "agent-demo"

const (
	fieldAgentDemoProfile           = "profile"
	fieldAgentDatabaseURL           = "agent-database-url"
	fieldReviewAuthorityDatabaseURL = "persona-review-authority-database-url"
	fieldPolicyAuthorityDatabaseURL = "agent-policy-authority-database-url"
	fieldRouteAuthorityDatabaseURL  = "persona-model-route-authority-database-url"
	fieldEvalProvisionerDatabaseURL = "agent-eval-provisioner-database-url"
	fieldAgentOwnerDatabaseURL      = "agent-owner-database-url"
	fieldChatOwnerDatabaseURL       = "chat-owner-database-url"
)

func agentDemoSpec(rest []string) bootstrap.Spec {
	return bootstrap.Spec{
		Role: bootstrap.RoleMigrate,
		Args: rest,
		ConfigFields: []bootstrap.Field{
			{Name: fieldAgentDemoProfile, Usage: "required local development profile"},
			{Name: fieldTenant, Usage: "tenant slug (ironridge-demo)"},
			{Name: "database-url", Env: EnvDatabaseURL, Usage: "core PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldAgentDatabaseURL, Env: "HCMNEXT_AGENT_DATABASE_URL", Usage: "agent PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldChatDatabaseURL, Env: EnvChatDatabaseURL, Usage: "chat PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldDocumentDatabaseURL, Env: EnvDocumentDatabaseURL, Usage: "document PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldReviewAuthorityDatabaseURL, Env: "HCMNEXT_PERSONA_REVIEW_AUTHORITY_DATABASE_URL", Usage: "persona review authority PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldPolicyAuthorityDatabaseURL, Env: application.EnvLocalAgentPolicyAuthorityDatabaseURL, Usage: "agent policy authority PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldRouteAuthorityDatabaseURL, Env: "HCMNEXT_PERSONA_MODEL_ROUTE_AUTHORITY_DATABASE_URL", Usage: "persona model route authority PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldEvalProvisionerDatabaseURL, Env: "HCMNEXT_AGENT_EVAL_PROVISIONER_DATABASE_URL", Usage: "agent evaluation provisioner PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldAgentOwnerDatabaseURL, Env: "HCMNEXT_AGENT_OWNER_DATABASE_URL", Usage: "optional agent PostgreSQL owner URL for the local run policy row", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldChatOwnerDatabaseURL, Env: "HCMNEXT_CHAT_OWNER_DATABASE_URL", Usage: "optional chat PostgreSQL owner URL for repairing an unrouted direct conversation", Kind: bootstrap.KindString, Secret: true},
		},
		Validate: func(values *bootstrap.Values) error {
			return application.ValidateLocalAgentDemoConfig(agentDemoConfig(values))
		},
		Build: func(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			config := agentDemoConfig(deps.Values)
			work := bootstrap.Workload{Name: "migrate-agent-demo", Run: func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, migrationTimeout)
				defer cancel()
				return runAgentDemoCommand(ctx, config, os.Stdout)
			}}
			return bootstrap.Runtime{Workloads: []bootstrap.Workload{work}}, nil
		},
	}
}

func agentDemoConfig(values *bootstrap.Values) application.LocalAgentDemoConfig {
	return application.LocalAgentDemoConfig{
		Profile: values.String(fieldAgentDemoProfile), Tenant: values.String(fieldTenant),
		DatabaseURL: values.String("database-url"), AgentDatabaseURL: values.String(fieldAgentDatabaseURL),
		ChatDatabaseURL: values.String(fieldChatDatabaseURL), DocumentDatabaseURL: values.String(fieldDocumentDatabaseURL),
		ReviewAuthorityDatabaseURL: values.String(fieldReviewAuthorityDatabaseURL), PolicyAuthorityDatabaseURL: values.String(fieldPolicyAuthorityDatabaseURL),
		RouteAuthorityDatabaseURL: values.String(fieldRouteAuthorityDatabaseURL), EvaluationProvisionerDatabaseURL: values.String(fieldEvalProvisionerDatabaseURL), AgentOwnerDatabaseURL: values.String(fieldAgentOwnerDatabaseURL), ChatOwnerDatabaseURL: values.String(fieldChatOwnerDatabaseURL),
		Now: time.Now,
	}
}

func runAgentDemoCommand(ctx context.Context, config application.LocalAgentDemoConfig, out io.Writer) error {
	summary, err := application.PrepareLocalAgentDemo(ctx, config)
	if err != nil {
		if summary.WorkspaceIndex.Unavailable != "" {
			fmt.Fprintf(out, "Preparation incomplete.%s Warning: %s\n", formatWorkspaceAccess(summary.WorkspaceIndex), summary.WorkspaceIndex.Unavailable)
		}
		return err
	}
	_, err = fmt.Fprintln(out, formatAgentDemoSummary(summary))
	return err
}

func formatAgentDemoSummary(summary application.LocalAgentDemoSummary) string {
	change := "already prepared; no changes"
	if summary.Changed() {
		change = fmt.Sprintf("evaluation=%t publication=%t installations=%d installations_refreshed=%d duplicates_retired=%d model_route=%t run_policy=%t provider_deployment=%t chat_repair=%t policy_document_placements=%d holiday_guide_placements=%d documents_shared_with_workspace=%d skill_grants=%d", summary.EvaluationRecorded, summary.Published, summary.InstallationsCreated, summary.InstallationsRefreshed, summary.DuplicateInstallationsRetired, summary.ModelRouteCreated, summary.RunPolicyCreated, summary.ProviderDeploymentCreated, summary.ChatConversationRepaired, summary.PolicyDocumentPlacements, summary.HolidayDocumentPlacements, summary.WorkspaceDocumentsShared, summary.SkillGrantsCreated)
	}
	receipt := fmt.Sprintf("Policy Helper v%d: %s. Assistant v%d: %s. %s. Documents: Paid time off policy; 2026 holiday guide.", summary.Version, summary.State, summary.AssistantVersion, summary.AssistantState, change)
	receipt += fmt.Sprintf(" Agent icons set: %d.", summary.IconsSet)
	for _, agent := range summary.AmbientAgents {
		name := "Task Catcher"
		if agent.Agent == "reminder" {
			name = "Reminder"
		}
		receipt += fmt.Sprintf(" %s v%d: %s; private offers=%d public offers=%d.", name, agent.Version, agent.State, agent.PrivateOffers, agent.PublicOffers)
	}
	index := summary.WorkspaceIndex
	if index.Unavailable != "" {
		return receipt + formatWorkspaceAccess(index) + " Warning: " + index.Unavailable
	}
	if index.Model != "" {
		receipt += fmt.Sprintf(" Workspace index: model=%s documents_indexed=%d sections_indexed=%d workspace_documents=%d waiting=%d indexed_at=%s.", index.Model, index.DocumentsIndexed, index.SectionsIndexed, index.Workspace.WorkspaceDocuments, index.Workspace.WorkspacePending, index.Workspace.WorkspaceIndexedAt.UTC().Format(time.RFC3339))
	}
	return receipt
}

func formatWorkspaceAccess(index application.WorkspaceIndexPreparation) string {
	if !index.WorkspaceAccessKnown {
		return " Workspace-public access could not be determined."
	}
	return fmt.Sprintf(" Workspace-public documents=%d waiting=%d.", index.Workspace.WorkspaceDocuments, index.Workspace.WorkspacePending)
}
