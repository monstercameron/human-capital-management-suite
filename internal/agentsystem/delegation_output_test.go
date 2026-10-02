package agentsystem

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/schemaflux"
)

func TestTodo_AGENT_050_ValidatedOutput(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	// A model answer's citations are kept as the result's sources, and a
	// returned result may only name sources the parent and the child are both
	// authorized for. The child therefore cites the worker record its authority
	// covers; TestTodo_AGENT_050_OutputSourceRevocation proves any other source
	// is refused.
	f.provider.ReplyFunc(func(_ int, _ schemaflux.CompletionRequest) (string, error) {
		return `{"text":"worker is active","citations":["worker:42"]}`, nil
	})
	ctx := context.Background()
	parent := f.start(t, "parent-output", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	request := specialistRequest(t, f, parent, "child-output")
	child, err := f.runner.Delegate(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.SpecialistResult(ctx, parent.ID, request.ParentCredential, child.ID); err == nil {
		t.Fatal("pending child returned output")
	}
	child, err = f.runner.Step(ctx, child.ID, ModeOnBehalfOf)
	if err != nil {
		t.Fatal(err)
	}
	output, err := f.runner.SpecialistResult(ctx, parent.ID, request.ParentCredential, child.ID)
	if err != nil || output.TaskID != child.ID || output.ParentTaskID != parent.ID || output.TaskVersion != child.Version || output.AnswerText != child.Ledger.AnswerText || output.Digest == "" || len(output.Results) != 1 || len(output.Taint) != 1 || output.Taint[0] != taintDerived {
		t.Fatalf("validated child output = %+v, %v", output, err)
	}
	if got := output.Results[0].SourceIDs; len(got) != 2 || got[0] != documentUsageMarker || got[1] != "worker:42" {
		t.Fatalf("returned result sources = %v, want the usage marker and the cited worker record", got)
	}
	output.Results[0].Taint[0] = "FORGED"
	again, err := f.runner.SpecialistResult(ctx, parent.ID, request.ParentCredential, child.ID)
	if err != nil || again.Results[0].Taint[0] == "FORGED" || again.Digest != output.Digest {
		t.Fatalf("output aliases durable ledger: %+v, %v", again, err)
	}
	if _, err := f.runner.SpecialistResult(ctx, parent.ID, "browser-token", child.ID); err == nil {
		t.Fatal("browser credential returned child output")
	}
	for i := range child.Ledger.Entries {
		if child.Ledger.Entries[i].Kind == "STEP_RESULT" {
			child.Ledger.Entries[i].Revoked = true
		}
	}
	child.Version++
	if err := f.runner.tasks.Save(ctx, child, child.Version-1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.SpecialistResult(ctx, parent.ID, request.ParentCredential, child.ID); err == nil {
		t.Fatal("revoked source result returned")
	}
}

func TestTodo_AGENT_050_OutputSourceRevocation(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	ctx := context.Background()
	parent := f.start(t, "parent-source", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	request := specialistRequest(t, f, parent, "child-source")
	child, err := f.runner.Delegate(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	child, err = f.runner.Step(ctx, child.ID, ModeOnBehalfOf)
	if err != nil {
		t.Fatal(err)
	}
	for i := range child.Ledger.Entries {
		if child.Ledger.Entries[i].Kind == "STEP_RESULT" {
			child.Ledger.Entries[i].SourceIDs = []string{"outside-parent"}
		}
	}
	child.Version++
	if err := f.runner.tasks.Save(ctx, child, child.Version-1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.SpecialistResult(ctx, parent.ID, request.ParentCredential, child.ID); err == nil {
		t.Fatal("result with source outside current parent authority returned")
	}
}
