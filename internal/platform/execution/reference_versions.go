package execution

import (
	"fmt"
	goruntime "runtime"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// FixtureHireCompile proves the frozen New employee hire 1.0.0 plan is
// exactly the plan this build preserves.
const FixtureHireCompile = "fixture:workflow.new-hire/reproducible-compile@v1"
const FixtureHireCompileV1_1 = "fixture:workflow.new-hire/reproducible-compile@v2"

// PublishReferenceVersions publishes the workflows that are proven through the
// driver but not served: today New employee hire (WF-HIRE-001). It is separate
// from [PublishShippedVersions] on purpose. A shipped version is one the
// served composition can run, and nothing serves a hire yet, so a standard
// deployment never publishes it; the local-development bootstrap does, so an
// author can open and read it beside Promotion. Publication is idempotent and
// approves and activates nothing.
func PublishReferenceVersions(store version.Store, at time.Time) ([]version.CompiledVersion, error) {
	tools := map[string]string{"go": goruntime.Version(), "publisher": "internal/platform/execution"}
	shipped := []struct {
		semanticVersion string
		definition      func() workflow.Definition
		compile         func() (*workflow.CompiledWorkflow, error)
		options         workflow.Options
		fixture         string
	}{
		{hireexec.SemanticVersionV1_0, hireexec.DefinitionV1_0, func() (*workflow.CompiledWorkflow, error) { return hireexec.CompileV1_0() }, hireexec.CompileOptionsV1_0(), FixtureHireCompile},
		{hireexec.SemanticVersion, hireexec.Definition, func() (*workflow.CompiledWorkflow, error) { return hireexec.Compile() }, hireexec.CompileOptions(), FixtureHireCompileV1_1},
	}
	out := make([]version.CompiledVersion, 0, len(shipped))
	for _, item := range shipped {
		plan, err := item.compile()
		if err != nil {
			return nil, fmt.Errorf("platform execution: compile new-hire workflow %s: %w", item.semanticVersion, err)
		}
		published, err := version.Publish(store, item.definition(), plan, item.options, version.PublishMeta{
			SemanticVersion: item.semanticVersion, PublishedAt: at, PublishedBy: versionPublisher,
			ToolVersions: tools, FixtureRefs: []string{item.fixture},
		})
		if err != nil {
			return nil, fmt.Errorf("platform execution: publish new-hire workflow %s: %w", item.semanticVersion, err)
		}
		out = append(out, published)
	}
	return out, nil
}

func hireCompileFixture(v version.CompiledVersion) error {
	return reproducesPlan(v, hireCompileFor(v.SemanticVersion))
}

func hireCompileFor(semanticVersion string) func() (*workflow.CompiledWorkflow, error) {
	switch semanticVersion {
	case hireexec.SemanticVersionV1_0:
		return func() (*workflow.CompiledWorkflow, error) { return hireexec.CompileV1_0() }
	case hireexec.SemanticVersion:
		return func() (*workflow.CompiledWorkflow, error) { return hireexec.Compile() }
	default:
		return func() (*workflow.CompiledWorkflow, error) {
			return nil, fmt.Errorf("platform execution: unknown new-hire semantic version %q", semanticVersion)
		}
	}
}
