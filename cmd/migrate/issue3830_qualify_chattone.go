package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// qualifyChattoneCommand evaluates the writing-style model on the real
// provider and, only when it passes, writes the approval document carrying the
// run's own suite digest. It replaces an owner-stated digest with measured
// evidence for that task:
//
//	MODEL_API_KEY=... migrate qualify-chattone -profile local-dev
//
// It needs no database. The key is read from the environment, used for the
// calls and never printed or written. The drafts sent are synthetic.
const qualifyChattoneCommand = "qualify-chattone"

const (
	fieldQualifyAPIKey = "model-api-key"
	fieldQualifyModel  = "model"
)

func qualifyChattoneSpec(rest []string) bootstrap.Spec {
	return bootstrap.Spec{
		Role: bootstrap.RoleMigrate,
		Args: rest,
		ConfigFields: []bootstrap.Field{
			{Name: fieldAgentDemoProfile, Usage: "required local development profile"},
			{Name: fieldQualifyAPIKey, Env: "MODEL_API_KEY", Usage: "provider key to evaluate the model with (MODEL_API_KEY)", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldQualifyModel, Usage: "optional model to evaluate; empty selects the model the prepared local deployment prices"},
		},
		Validate: func(values *bootstrap.Values) error {
			return application.ValidateLocalChattoneQualifyConfig(qualifyChattoneConfig(values))
		},
		Build: func(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			config := qualifyChattoneConfig(deps.Values)
			work := bootstrap.Workload{Name: "migrate-qualify-chattone", Run: func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
				defer cancel()
				return runQualifyChattoneCommand(ctx, config, os.Stdout)
			}}
			return bootstrap.Runtime{Workloads: []bootstrap.Workload{work}}, nil
		},
	}
}

func qualifyChattoneConfig(values *bootstrap.Values) application.LocalChattoneQualifyConfig {
	return application.LocalChattoneQualifyConfig{Profile: values.String(fieldAgentDemoProfile), APIKey: values.String(fieldQualifyAPIKey), Model: values.String(fieldQualifyModel)}
}

func runQualifyChattoneCommand(ctx context.Context, config application.LocalChattoneQualifyConfig, out io.Writer) error {
	result, err := application.RunLocalChattoneQualification(ctx, config, time.Now)
	fmt.Fprintln(out, formatQualifyChattone(result, err))
	return err
}

func formatQualifyChattone(result application.LocalChattoneQualifyResult, err error) string {
	q := result.Qualification
	if len(q.Cases) == 0 {
		if err == nil {
			return "No evaluation was run."
		}
		return "The evaluation did not run: " + err.Error()
	}
	passed := 0
	for _, c := range q.Cases {
		if c.Passed {
			passed++
		}
	}
	text := fmt.Sprintf("Writing style evaluation of %s: %d of %d cases passed, cost %d micro-units, suite %s.", q.Model.ModelID, passed, len(q.Cases), q.CostMicros, q.SuiteDigest)
	if q.Reword != nil {
		text += fmt.Sprintf(" Reword suite: passed=%t.", q.Reword.Passed)
	}
	if result.RecordPath != "" {
		text += "\n  record: " + result.RecordPath
	}
	if errors.Is(err, application.ErrChattoneNotQualified) {
		return text + "\nThe model did not qualify; no deployment was written. Read the record for the failing cases."
	}
	if err != nil {
		return text + "\nThe deployment was not written: " + err.Error()
	}
	state := "already current"
	if result.Written {
		state = "written"
	}
	return text + fmt.Sprintf("\n  deployment: %s (%s)\nMeasured evidence, not stated. Start a local-dev server; it reads this file without further settings.", result.Path, state)
}
