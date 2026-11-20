package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// chatTaskModelsCommand writes the translation, writing-style and
// ambient-screen approval documents beside the prepared model deployment, so a
// local-dev run can use those model-backed Chat features. Run it once after
// agent-demo, naming the evaluation suite you ran and answer for:
//
//	migrate chat-task-models -profile local-dev -suite-digest sha256:<64 hex digits>
//
// It needs no database and never reads or prints the model API key or the
// signing seeds. A rerun with the same digest changes nothing.
const chatTaskModelsCommand = "chat-task-models"

const fieldChatTaskSuiteDigest = "suite-digest"

func chatTaskModelsSpec(rest []string) bootstrap.Spec {
	return bootstrap.Spec{
		Role: bootstrap.RoleMigrate,
		Args: rest,
		ConfigFields: []bootstrap.Field{
			{Name: fieldAgentDemoProfile, Usage: "required local development profile"},
			{Name: fieldChatTaskSuiteDigest, Usage: "sha256 digest of the evaluation suite you ran and are answerable for (sha256: and 64 hex digits)"},
		},
		Validate: func(values *bootstrap.Values) error {
			return application.ValidateLocalChatTaskModelsConfig(chatTaskModelsConfig(values))
		},
		Build: func(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			config := chatTaskModelsConfig(deps.Values)
			work := bootstrap.Workload{Name: "migrate-chat-task-models", Run: func(context.Context) error {
				return runChatTaskModelsCommand(config, os.Stdout)
			}}
			return bootstrap.Runtime{Workloads: []bootstrap.Workload{work}}, nil
		},
	}
}

func chatTaskModelsConfig(values *bootstrap.Values) application.LocalChatTaskModelsConfig {
	return application.LocalChatTaskModelsConfig{Profile: values.String(fieldAgentDemoProfile), SuiteDigest: values.String(fieldChatTaskSuiteDigest)}
}

func runChatTaskModelsCommand(config application.LocalChatTaskModelsConfig, out io.Writer) error {
	docs, err := application.WriteLocalChatTaskModels(config)
	fmt.Fprintln(out, formatChatTaskModels(docs, config.SuiteDigest))
	return err
}

func formatChatTaskModels(docs []application.LocalChatTaskModelDocument, suite string) string {
	if len(docs) == 0 {
		return "No chat model documents were written."
	}
	text := fmt.Sprintf("Chat model documents (evaluation stated by you: suite %s; nothing was measured here):", suite)
	for _, doc := range docs {
		state := "already current"
		if doc.Written {
			state = "written"
		}
		text += fmt.Sprintf("\n  %s: %s (%s)", doc.Task, doc.Path, state)
	}
	return text + "\nSet MODEL_API_KEY and start a local-dev server; it reads these files without further settings."
}
